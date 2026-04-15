package processor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/fraud"
	"github.com/fluxa/fluxa/internal/idempotency"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/ports"
)

// Processor handles the core event processing logic.
type Processor struct {
	DB          *db.Client
	Idempotency *idempotency.Client
	Storage     ports.Storage   // MinIO adapter
	Publisher   ports.Publisher // RabbitMQ adapter (alerts exchange)
	Fraud       *fraud.Engine
	Metrics     ports.Metrics
	Logger      *logging.Logger
}

// ProcessMessage handles a single queue message.
// Returns nil to ACK (including permanent failures), non-nil to NACK for retry.
func (p *Processor) ProcessMessage(msg *domain.QueueMessage) error {
	if err := p.process(msg); err != nil {
		if _, ok := err.(*domain.NonRetryableError); ok {
			// ACK poison messages to prevent retry loops
			return p.failPermanent(msg.EventID, err.Error())
		}
		// NACK transient errors to trigger broker retry
		p.Logger.Error("Transient failure, triggering retry", err)
		return err
	}
	return nil
}

// process encapsulates the core logic to enable cleaner error handling in ProcessMessage.
func (p *Processor) process(msg *domain.QueueMessage) error {
	startTime := time.Now()
	ctx := context.Background()

	p.Logger.Info("Processing event", map[string]interface{}{
		"event_id": msg.EventID,
	})

	// Step 1: Idempotency check
	alreadyProcessed, err := p.Idempotency.CheckAndMark(msg.EventID)
	if err != nil {
		p.Logger.Error("Failed to check idempotency", err)
		p.Metrics.IncCounter("events_processed_total", "service", "processor", "status", "failure")
		return domain.NewRetryableError("idempotency_check_failed", err)
	}
	if alreadyProcessed {
		p.Logger.Info("Event already processed, skipping", map[string]interface{}{"event_id": msg.EventID})
		return nil
	}

	// Step 2: Fetch payload
	var payloadBytes []byte
	switch msg.PayloadMode {
	case domain.PayloadModeInline:
		if msg.PayloadInline == nil {
			return domain.NewNonRetryableError("missing_payload", nil)
		}
		payloadBytes = []byte(*msg.PayloadInline)

	case domain.PayloadModeS3:
		if msg.S3Key == nil {
			return domain.NewNonRetryableError("missing_s3_key", nil)
		}
		payloadBytes, err = p.Storage.Get(ctx, *msg.S3Key)
		if err != nil {
			p.Logger.Error("Failed to fetch payload from storage", err)
			p.Metrics.IncCounter("events_processed_total", "service", "processor", "status", "failure")
			return domain.NewRetryableError("storage_fetch_failed", err)
		}

	default:
		return domain.NewNonRetryableError("invalid_payload_mode", nil)
	}

	// Step 3: Verify hash
	hash := sha256.Sum256(payloadBytes)
	calculatedHash := hex.EncodeToString(hash[:])
	if calculatedHash != msg.PayloadSHA256 {
		return domain.NewNonRetryableError("hash_mismatch", nil)
	}

	// Step 4: Parse and validate event
	var event domain.Event
	if err := json.Unmarshal(payloadBytes, &event); err != nil {
		return domain.NewNonRetryableError("unmarshal_error", err)
	}
	if err := event.Validate(); err != nil {
		return domain.NewNonRetryableError("validation_error", err)
	}
	event.EventID = msg.EventID

	// Step 5: Persist to DB
	dbStart := time.Now()
	var s3Key *string
	if msg.PayloadMode == domain.PayloadModeS3 {
		s3Key = msg.S3Key
	}
	if err := p.DB.InsertEvent(&event, msg.CorrelationID, msg.PayloadMode, s3Key); err != nil {
		p.Logger.Error("Failed to insert event into database", err)
		p.Metrics.IncCounter("events_processed_total", "service", "processor", "status", "failure")
		return domain.NewRetryableError("db_insert_failed", err)
	}
	p.Metrics.ObserveHistogram("process_latency_seconds", time.Since(dbStart).Seconds(), "service", "processor")

	// Step 5.5: Fraud evaluation (best-effort — errors do not abort the pipeline)
	p.evaluateFraud(ctx, &event)

	// Step 6: Mark idempotency success
	if err := p.Idempotency.MarkSuccess(msg.EventID); err != nil {
		p.Logger.Error("Failed to mark idempotency success", err)
		// Non-fatal: event is already safely written to DB
	}

	latency := time.Since(startTime).Seconds()
	p.Logger.Info("Successfully processed event", map[string]interface{}{
		"event_id":   msg.EventID,
		"latency_ms": latency * 1000,
	})
	p.Metrics.IncCounter("events_processed_total", "service", "processor", "status", "success")
	p.Metrics.ObserveHistogram("process_latency_seconds", latency, "service", "processor")

	return nil
}

// evaluateFraud runs all fraud rules and publishes alerts for any flags found.
// Errors are logged but never propagated — the event itself is already safely persisted.
// A nil Fraud engine or Publisher is treated as a no-op (useful in tests).
func (p *Processor) evaluateFraud(ctx context.Context, event *domain.Event) {
	if p.Fraud == nil {
		return
	}
	flags, err := p.Fraud.Evaluate(event, p.DB)
	if err != nil {
		p.Logger.Error("Fraud evaluation error", err)
		return
	}

	for _, flag := range flags {
		if err := p.DB.InsertFraudFlag(&flag); err != nil {
			p.Logger.Error("Failed to insert fraud flag", err, map[string]interface{}{
				"rule_name": flag.RuleName,
				"event_id":  flag.EventID,
			})
			continue
		}

		p.Metrics.IncCounter("fraud_flags_total", "rule", flag.RuleName)

		alertMsg := domain.AlertMessage(flag)
		body, err := json.Marshal(alertMsg)
		if err != nil {
			p.Logger.Error("Failed to marshal alert message", err)
			continue
		}
		if p.Publisher == nil {
			continue
		}
		if err := p.Publisher.Publish(ctx, "alerts", "", body); err != nil {
			p.Logger.Error("Failed to publish alert", err, map[string]interface{}{
				"rule_name": flag.RuleName,
			})
		}
	}

	if len(flags) > 0 {
		p.Logger.Info(fmt.Sprintf("Fraud evaluation: %d flag(s) raised", len(flags)), map[string]interface{}{
			"event_id": event.EventID,
		})
	}
}

// failPermanent logs a permanent failure, marks idempotency as failed, and returns nil (ACK).
func (p *Processor) failPermanent(eventID, reason string) error {
	p.Logger.Error("Permanent failure: "+reason, nil)
	p.Metrics.IncCounter("events_processed_total", "service", "processor", "status", "failure")
	_ = p.Idempotency.MarkFailed(eventID, reason)
	return nil
}
