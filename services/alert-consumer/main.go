package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	prommetrics "github.com/fluxa/fluxa/internal/adapters/prometheus"
	"github.com/fluxa/fluxa/internal/adapters/rabbitmq"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	cfg, err := config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger := logging.NewLogger("alert-consumer", "init")

	mqClient, err := rabbitmq.NewClient(cfg.RabbitMQURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to RabbitMQ: %v\n", err)
		os.Exit(1)
	}
	defer mqClient.Close()

	metrics := prommetrics.NewMetrics("alert-consumer")

	// Prometheus metrics endpoint
	go func() {
		http.Handle("/metrics", promhttp.Handler())
		if err := http.ListenAndServe(":9094", nil); err != nil {
			fmt.Fprintf(os.Stderr, "Metrics server error: %v\n", err)
		}
	}()

	logger.Info("Alert consumer starting — consuming from 'alerts' queue", nil)

	ctx := context.Background()
	deliveries, err := mqClient.Consume(ctx, "alerts")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start consuming alerts: %v\n", err)
		os.Exit(1)
	}

	for d := range deliveries {
		var alert domain.AlertMessage
		if err := json.Unmarshal(d.Body(), &alert); err != nil {
			logger.Error("Failed to parse alert message — discarding", err)
			_ = d.Ack()
			continue
		}

		logger.Info("FRAUD ALERT", map[string]interface{}{
			"flag_id":    alert.FlagID,
			"event_id":   alert.EventID,
			"user_id":    alert.UserID,
			"rule_name":  alert.RuleName,
			"rule_value": alert.RuleValue,
			"flagged_at": alert.FlaggedAt.Format("2006-01-02T15:04:05Z07:00"),
		})

		metrics.IncCounter("alerts_consumed_total")
		_ = d.Ack()
	}

	logger.Info("Alert consumer channel closed — exiting", nil)
}
