package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/logging"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		// Replay only needs IngestURL, CSVFile, RatePerSec — not DB credentials.
		// Override validation by using fallback defaults for DB fields if needed.
		fmt.Fprintf(os.Stderr, "Config error (continuing): %v\n", err)
	}

	logger := logging.NewLogger("replay", "init")

	ingestURL := os.Getenv("INGEST_URL")
	if ingestURL == "" {
		ingestURL = "http://localhost:8080"
	}
	csvFile := os.Getenv("CSV_FILE")
	if csvFile == "" {
		csvFile = "/data/transactions.csv"
	}
	ratePerSec := 200
	if v := os.Getenv("RATE_PER_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			ratePerSec = n
		}
	}

	_ = cfg // suppress unused warning; replay uses env vars directly above

	logger.Info("Replay service starting", map[string]interface{}{
		"ingest_url":   ingestURL,
		"csv_file":     csvFile,
		"rate_per_sec": ratePerSec,
	})

	f, err := os.Open(csvFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to open CSV file %q: %v\n", csvFile, err)
		os.Exit(1)
	}
	defer f.Close()

	reader := csv.NewReader(f)

	// Read header row
	header, err := reader.Read()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read CSV header: %v\n", err)
		os.Exit(1)
	}

	// Build column index map
	colIdx := make(map[string]int)
	for i, col := range header {
		colIdx[col] = i
	}

	baseTime := time.Now().UTC()
	ticker := time.NewTicker(time.Second / time.Duration(ratePerSec))
	defer ticker.Stop()

	httpClient := &http.Client{Timeout: 10 * time.Second}
	var sent, failed int

	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			logger.Error("Failed to read CSV row", err)
			failed++
			continue
		}

		<-ticker.C

		event := mapCSVRowToEvent(row, colIdx, baseTime)
		body, err := json.Marshal(event)
		if err != nil {
			failed++
			continue
		}

		resp, err := httpClient.Post(ingestURL+"/events", "application/json", bytes.NewReader(body))
		if err != nil {
			failed++
			if failed%100 == 0 {
				logger.Error("Ingest request failed", err, map[string]interface{}{"sent": sent, "failed": failed})
			}
			continue
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusAccepted {
			sent++
		} else {
			failed++
		}

		if (sent+failed)%1000 == 0 {
			logger.Info("Replay progress", map[string]interface{}{
				"sent":   sent,
				"failed": failed,
			})
		}
	}

	logger.Info("Replay complete", map[string]interface{}{
		"sent":   sent,
		"failed": failed,
	})
}

// mapCSVRowToEvent maps a PaySim CSV row to an ingest-compatible event payload.
// PaySim columns: step, type, amount, nameOrig, oldbalanceOrg, newbalanceOrig,
//                 nameDest, oldbalanceDest, newbalanceDest, isFraud, isFlaggedFraud
func mapCSVRowToEvent(row []string, colIdx map[string]int, baseTime time.Time) map[string]interface{} {
	get := func(col string) string {
		if idx, ok := colIdx[col]; ok && idx < len(row) {
			return row[idx]
		}
		return ""
	}

	// Re-stamp timestamp: step column is hours since simulation start
	var ts time.Time
	if stepStr := get("step"); stepStr != "" {
		if step, err := strconv.ParseFloat(stepStr, 64); err == nil {
			ts = baseTime.Add(time.Duration(step) * time.Hour)
		}
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	amount := 0.0
	if amtStr := get("amount"); amtStr != "" {
		if v, err := strconv.ParseFloat(amtStr, 64); err == nil {
			amount = v
		}
	}

	// Default sensible values for required fields
	userID := get("nameOrig")
	if userID == "" {
		userID = "unknown"
	}
	merchant := get("nameDest")
	if merchant == "" {
		merchant = "unknown"
	}
	if amount <= 0 {
		amount = 1.0
	}

	event := map[string]interface{}{
		"user_id":   userID,
		"amount":    amount,
		"currency":  "USD",
		"merchant":  merchant,
		"timestamp": ts.Format(time.RFC3339),
		"metadata": map[string]interface{}{
			"transaction_type": get("type"),
			"is_fraud":         get("isFraud"),
		},
	}

	return event
}
