package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	minioadapter "github.com/fluxa/fluxa/internal/adapters/minio"
	prommetrics "github.com/fluxa/fluxa/internal/adapters/prometheus"
	"github.com/fluxa/fluxa/internal/adapters/rabbitmq"
	scoreradapter "github.com/fluxa/fluxa/internal/adapters/scorer"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/fraud"
	"github.com/fluxa/fluxa/internal/idempotency"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/processor"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.NewLogger("processor", "init")

	dbClient, err := db.NewClient(cfg.DSN(), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create database client: %v\n", err)
		os.Exit(1)
	}
	defer dbClient.Close()

	mqClient, err := rabbitmq.NewClient(cfg.RabbitMQURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to RabbitMQ: %v\n", err)
		os.Exit(1)
	}
	defer mqClient.Close()

	minioClient, err := minioadapter.NewClient(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey, cfg.MinioBucket, cfg.MinioUseSSL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to MinIO: %v\n", err)
		os.Exit(1)
	}

	fraudEngine, err := fraud.NewEngine(cfg.RulesFile, logger)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load fraud rules: %v\n", err)
		os.Exit(1)
	}

	// ML scorer (best-effort, fail-open) — mirrors fraud-grpc. Scores async/replay
	// events where the model has the most signal (the IEEE-CIS distribution).
	scorerEndpoint := os.Getenv("SCORER_ENDPOINT")
	if scorerEndpoint == "" {
		scorerEndpoint = "ml-scorer:9097"
	}
	fraudEngine.Tau = 0.87
	if t := os.Getenv("SCORER_TAU"); t != "" {
		if v, perr := strconv.ParseFloat(t, 64); perr == nil {
			fraudEngine.Tau = v
		}
	}
	var fraudScorer fraud.Scorer
	if sc, scErr := scoreradapter.NewClient(scorerEndpoint, 40*time.Millisecond); scErr != nil {
		logger.Warn("ML scorer client init failed; rules-only", map[string]interface{}{"error": scErr.Error()})
	} else {
		defer sc.Close()
		fraudScorer = sc
	}

	proc := &processor.Processor{
		DB:          dbClient,
		Idempotency: idempotency.NewClient(dbClient.GetDB()),
		Storage:     minioClient,
		Publisher:   mqClient,
		Fraud:       fraudEngine,
		Scorer:      fraudScorer,
		Metrics:     prommetrics.NewMetrics("processor"),
		Logger:      logger,
	}

	// Prometheus metrics endpoint
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":9092", nil); err != nil {
			fmt.Fprintf(os.Stderr, "Metrics server error: %v\n", err)
		}
	}()

	logger.Info("Processor service starting — consuming from 'events' queue", nil)

	ctx := context.Background()
	deliveries, err := mqClient.Consume(ctx, "events")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start consuming: %v\n", err)
		os.Exit(1)
	}

	for d := range deliveries {
		var msg domain.QueueMessage
		if err := json.Unmarshal(d.Body(), &msg); err != nil {
			proc.Logger.Error("Failed to parse queue message — discarding", err)
			_ = d.Ack() // Discard unparseable message
			continue
		}

		proc.Logger = logging.NewLogger("processor", msg.CorrelationID)

		if err := proc.ProcessMessage(&msg); err != nil {
			// Retryable error — nack so broker re-delivers
			_ = d.Nack(true)
		} else {
			_ = d.Ack()
		}
	}

	logger.Info("Consumer channel closed — processor exiting", nil)
}
