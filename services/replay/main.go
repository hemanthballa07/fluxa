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
	"strings"
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

		event := mapCSVRowToEvent(row, colIdx)
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

// merchantLookup derives a merchant name from IEEE-CIS ProductCD and card4 columns.
// Keys are "productcd+card4" (both lowercased).
var merchantLookup = map[string]string{
	"w+visa":       "Amazon Marketplace",
	"w+mastercard": "Walmart Online",
	"c+mastercard": "Walmart",
	"c+visa":       "Target",
	"r+discover":   "Target RedCard",
	"h+amex":       "Best Buy",
	"s+visa":       "Steam Games",
	"s+mastercard": "PlayStation Store",
}

// ieeeEpoch is 2024-01-01T00:00:00Z — the fixed anchor for TransactionDT offsets.
var ieeeEpoch = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// mapCSVRowToEvent maps an IEEE-CIS train_transaction.csv row to an ingest-compatible
// event payload.
//
// Key IEEE-CIS columns used:
//
//	TransactionDT  — seconds since simulation start; anchored to 2024-01-01T00:00:00Z
//	TransactionAmt — transaction amount in USD
//	card1          — masked card identifier used as user_id
//	ProductCD      — product category (W/C/R/H/S); combined with card4 to derive merchant
//	card4          — card network (visa/mastercard/discover/amex)
//	P_emaildomain  — purchaser email domain
//	isFraud        — ground-truth fraud label (0 or 1)
func mapCSVRowToEvent(row []string, colIdx map[string]int) map[string]interface{} {
	get := func(col string) string {
		if idx, ok := colIdx[col]; ok && idx < len(row) {
			return row[idx]
		}
		return ""
	}

	// Timestamp: fixed epoch + TransactionDT seconds.
	var ts time.Time
	if dtStr := get("TransactionDT"); dtStr != "" {
		if dt, err := strconv.ParseFloat(dtStr, 64); err == nil {
			ts = ieeeEpoch.Add(time.Duration(dt) * time.Second)
		}
	}
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	// Amount.
	amount := 0.0
	if v, err := strconv.ParseFloat(get("TransactionAmt"), 64); err == nil {
		amount = v
	}
	if amount <= 0 {
		amount = 1.0
	}

	// User ID from card1.
	userID := get("card1")
	if userID == "" {
		userID = "unknown"
	}

	// Merchant derived from ProductCD + card4.
	productCD := get("ProductCD")
	card4 := get("card4")
	key := strings.ToLower(productCD) + "+" + strings.ToLower(card4)
	merchant, ok := merchantLookup[key]
	if !ok {
		merchant = "merchant_" + productCD + "_" + card4
	}

	// Ground-truth fraud label — forward as-is; default "0" if absent.
	isFraud := get("isFraud")
	if isFraud != "1" {
		isFraud = "0"
	}

	return map[string]interface{}{
		"user_id":   userID,
		"amount":    amount,
		"currency":  "USD",
		"merchant":  merchant,
		"timestamp": ts.Format(time.RFC3339),
		"metadata": map[string]interface{}{
			"email_domain":          get("P_emaildomain"),
			"card_network":          card4,
			"product_code":          productCD,
			"is_fraud_ground_truth": isFraud,
		},
	}
}
