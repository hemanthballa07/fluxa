package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	prommetrics "github.com/fluxa/fluxa/internal/adapters/prometheus"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/ports"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	cfg      *config.Config
	dbClient *db.Client
	metrics  ports.Metrics
	logger   *logging.Logger
)

func main() {
	var err error
	cfg, err = config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger = logging.NewLogger("query", "init")

	dbClient, err = db.NewClient(cfg.DSN(), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create database client: %v\n", err)
		os.Exit(1)
	}
	defer dbClient.Close()

	metrics = prommetrics.NewMetrics("query")

	// Prometheus metrics endpoint
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":9093", nil); err != nil {
			fmt.Fprintf(os.Stderr, "Metrics server error: %v\n", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/events/", handleGetEvent)
	mux.HandleFunc("/health", handleHealth)

	logger.Info("Query service starting", map[string]interface{}{"port": 8083})
	if err := http.ListenAndServe(":8083", mux); err != nil {
		fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		os.Exit(1)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleGetEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	correlationID := r.Header.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = r.Header.Get("X-Request-ID")
	}
	reqLogger := logging.NewLogger("query", correlationID)

	// Extract event_id from path: /events/{event_id}
	eventID := strings.TrimPrefix(r.URL.Path, "/events/")
	if eventID == "" {
		reqLogger.Warn("Missing event_id in path")
		metrics.IncCounter("query_total", "status", "missing_event_id")
		http.Error(w, `{"error":"event_id is required"}`, http.StatusBadRequest)
		return
	}

	record, err := dbClient.GetEventByID(eventID)
	if err == db.ErrNotFound {
		reqLogger.Info("Event not found", map[string]interface{}{"event_id": eventID})
		metrics.IncCounter("query_total", "status", "not_found")
		http.Error(w, fmt.Sprintf(`{"error":"event not found: %s"}`, eventID), http.StatusNotFound)
		return
	}
	if err != nil {
		reqLogger.Error("Failed to query event", err)
		metrics.IncCounter("query_total", "status", "error")
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	reqLogger.Info("Successfully retrieved event", map[string]interface{}{"event_id": eventID})
	metrics.IncCounter("query_total", "status", "found")

	response := map[string]interface{}{
		"event_id":       record.EventID,
		"correlation_id": record.CorrelationID,
		"user_id":        record.UserID,
		"amount":         record.Amount,
		"currency":       record.Currency,
		"merchant":       record.Merchant,
		"timestamp":      record.Timestamp.Format("2006-01-02T15:04:05Z07:00"),
		"metadata":       record.Metadata,
		"payload_mode":   record.PayloadMode,
		"created_at":     record.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
	if record.S3Key != nil {
		response["s3_key"] = *record.S3Key
	}

	respBytes, _ := json.Marshal(response)
	w.Header().Set("Content-Type", "application/json")
	if correlationID != "" {
		w.Header().Set("X-Correlation-ID", correlationID)
	}
	w.WriteHeader(http.StatusOK)
	w.Write(respBytes)
}
