package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	minioadapter "github.com/fluxa/fluxa/internal/adapters/minio"
	"github.com/fluxa/fluxa/internal/adapters/rabbitmq"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/ports"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	prommetrics "github.com/fluxa/fluxa/internal/adapters/prometheus"
)

const maxInlineBytes = 256 * 1024 // 256 KB

var (
	cfg       *config.Config
	publisher ports.Publisher
	storage   ports.Storage
	metrics   ports.Metrics
	logger    *logging.Logger
)

func main() {
	var err error
	cfg, err = config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger = logging.NewLogger("ingest", "init")

	publisher, err = rabbitmq.NewClient(cfg.RabbitMQURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to RabbitMQ: %v\n", err)
		os.Exit(1)
	}

	storage, err = minioadapter.NewClient(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioBucket, cfg.MinioUseSSL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to MinIO: %v\n", err)
		os.Exit(1)
	}

	metrics = prommetrics.NewMetrics("ingest")

	// Prometheus metrics endpoint
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":9091", nil); err != nil {
			fmt.Fprintf(os.Stderr, "Metrics server error: %v\n", err)
		}
	}()

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handleIngest)
	mux.HandleFunc("/health", handleHealth)

	logger.Info("Ingest service starting", map[string]interface{}{"port": 8080})
	if err := http.ListenAndServe(":8080", mux); err != nil {
		fmt.Fprintf(os.Stderr, "HTTP server error: %v\n", err)
		os.Exit(1)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func handleIngest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	startTime := time.Now()

	correlationID := r.Header.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = uuid.New().String()
	}

	reqLogger := logging.NewLogger("ingest", correlationID)

	var event domain.Event
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		reqLogger.Error("Failed to parse request body", err, map[string]interface{}{"stage": "validate"})
		metrics.IncCounter("events_ingested_total", "service", "ingest")
		http.Error(w, fmt.Sprintf(`{"error":"invalid JSON: %v"}`, err), http.StatusBadRequest)
		return
	}

	if event.EventID == "" {
		event.EventID = uuid.New().String()
	}
	reqLogger = reqLogger.With(map[string]interface{}{"event_id": event.EventID})

	if err := event.Validate(); err != nil {
		reqLogger.Error("Event validation failed", err, map[string]interface{}{"stage": "validate"})
		http.Error(w, fmt.Sprintf(`{"error":"validation failed: %v"}`, err), http.StatusBadRequest)
		return
	}

	payloadBytes, err := event.ToJSON()
	if err != nil {
		reqLogger.Error("Failed to serialize event", err, map[string]interface{}{"stage": "serialize"})
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	hash := sha256.Sum256(payloadBytes)
	payloadSHA256 := hex.EncodeToString(hash[:])

	msg := &domain.QueueMessage{
		EventID:       event.EventID,
		CorrelationID: correlationID,
		PayloadSHA256: payloadSHA256,
		ReceivedAt:    event.Timestamp,
	}

	if len(payloadBytes) > maxInlineBytes {
		key := fmt.Sprintf("raw/%s/%s.json", time.Now().UTC().Format("2006-01-02"), event.EventID)
		if err := storage.Put(r.Context(), key, payloadBytes); err != nil {
			reqLogger.Error("Failed to store payload in MinIO", err, map[string]interface{}{"stage": "persist_storage"})
			http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
			return
		}
		msg.PayloadMode = domain.PayloadModeS3
		msg.S3Key = &key
		reqLogger.Info("Stored payload in object store", map[string]interface{}{"stage": "persist_storage", "key": key})
	} else {
		payloadStr := string(payloadBytes)
		msg.PayloadMode = domain.PayloadModeInline
		msg.PayloadInline = &payloadStr
	}

	msgBytes, err := json.Marshal(msg)
	if err != nil {
		reqLogger.Error("Failed to marshal queue message", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	if err := publisher.Publish(r.Context(), "events", "events", msgBytes); err != nil {
		reqLogger.Error("Failed to publish to RabbitMQ", err, map[string]interface{}{"stage": "enqueue"})
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	latency := time.Since(startTime).Seconds()
	metrics.IncCounter("events_ingested_total", "service", "ingest")
	metrics.ObserveHistogram("ingest_latency_seconds", latency, "service", "ingest")

	reqLogger.Info("Successfully enqueued event", map[string]interface{}{
		"stage":        "enqueue",
		"payload_mode": string(msg.PayloadMode),
		"latency_ms":   latency * 1000,
	})

	resp := map[string]string{"event_id": event.EventID, "status": "enqueued"}
	respBytes, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", correlationID)
	w.WriteHeader(http.StatusAccepted)
	w.Write(respBytes)
}
