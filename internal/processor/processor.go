package processor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/idempotency"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/metrics"
	"github.com/fluxa/fluxa/internal/models"
	"github.com/fluxa/fluxa/internal/storage"
)

// Processor handles the core event processing logic
type Processor struct {
	DB          *db.Client
	Idempotency *idempotency.Client
	S3          *storage.Client
	Metrics     *metrics.Metrics
	Logger      *logging.Logger
}

// ProcessMessage handles a single SQS message
func (p *Processor) ProcessMessage(sqsMsg *models.SQSEventMessage) error {
	if err := p.process(sqsMsg); err != nil {
		if _, ok := err.(*models.NonRetryableError); ok {
			// ACK poison messages to prevent retry loops
			return p.failPermanent(sqsMsg.EventID, err.Error())
		}
		// NACK transient errors to trigger SQS retry policy
		p.Logger.Error("Transient failure, triggering retry", err)
		return err
	}

	return nil
}

// process encapsulates the core logic to enable cleaner error handling in ProcessMessage
func (p *Processor) process(sqsMsg *models.SQSEventMessage) error {
	startTime := time.Now()

	p.Logger.Info("Processing event", map[string]interface{}{
		"event_id": sqsMsg.EventID,
	})

	// 1. Idempotency check
	alreadyProcessed, err := p.Idempotency.CheckAndMark(sqsMsg.EventID)
	if err != nil {
		p.Logger.Error("Failed to check idempotency", err)
		_ = p.Metrics.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "idempotency_error"})
		return models.NewRetryableError("idempotency_check_failed", err)
	}

	if alreadyProcessed {
		p.Logger.Info("Event already processed, skipping", map[string]interface{}{"event_id": sqsMsg.EventID})
		return nil
	}

	// 2. Fetch Payload
	var payloadBytes []byte
	switch sqsMsg.PayloadMode {
	case models.PayloadModeInline:
		if sqsMsg.PayloadInline == nil {
			return models.NewNonRetryableError("missing_payload", nil)
		}
		payloadBytes = []byte(*sqsMsg.PayloadInline)

	case models.PayloadModeS3:
		if sqsMsg.S3Key == nil {
			return models.NewNonRetryableError("missing_s3_key", nil)
		}

		var err error
		payloadBytes, err = p.S3.GetPayload(*sqsMsg.S3Key)
		if err != nil {
			p.Logger.Error("Failed to fetch payload from S3", err)
			_ = p.Metrics.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "s3_fetch_error"})
			// S3 fetch could be transient (network) or permanent (deleted).
			// Assuming transient for network errors, but if 404 it should be permanent.
			// For simplicity in this model, we treat fetch errors as retryable (network).
			return models.NewRetryableError("s3_fetch_failed", err)
		}

	default:
		return models.NewNonRetryableError("invalid_payload_mode", nil)
	}

	// 3. Verify Hash
	hash := sha256.Sum256(payloadBytes)
	calculatedHash := hex.EncodeToString(hash[:])
	if calculatedHash != sqsMsg.PayloadSHA256 {
		return models.NewNonRetryableError("hash_mismatch", nil)
	}

	// 4. Parse Event
	var event models.Event
	if err := json.Unmarshal(payloadBytes, &event); err != nil {
		return models.NewNonRetryableError("unmarshal_error", err)
	}

	if err := event.Validate(); err != nil {
		return models.NewNonRetryableError("validation_error", err)
	}

	event.EventID = sqsMsg.EventID
	// 5. Persist to DB
	dbStartTime := time.Now()
	var s3Key *string
	if sqsMsg.PayloadMode == models.PayloadModeS3 {
		s3Key = sqsMsg.S3Key
	}

	if err := p.DB.InsertEvent(&event, sqsMsg.CorrelationID, sqsMsg.PayloadMode, s3Key); err != nil {
		p.Logger.Error("Failed to insert event into database", err)
		_ = p.Metrics.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "db_error"})
		return models.NewRetryableError("db_insert_failed", err)

	}

	_ = p.Metrics.EmitMetric("db_latency_ms", float64(time.Since(dbStartTime).Milliseconds()), "Milliseconds", nil)

	// 6. Mark Success
	if err := p.Idempotency.MarkSuccess(sqsMsg.EventID); err != nil {
		p.Logger.Error("Failed to mark idempotency success", err)
		// Non-fatal, we already processed the event successfully in DB.
		// Idempotency state might be stuck in "processing", but event is safe.
	}

	latencyMs := time.Since(startTime).Milliseconds()
	p.Logger.Info("Successfully processed event", map[string]interface{}{
		"event_id":   sqsMsg.EventID,
		"latency_ms": latencyMs,
	})
	_ = p.Metrics.EmitMetric("processed_success", 1, "Count", nil)
	_ = p.Metrics.EmitMetric("process_latency_ms", float64(latencyMs), "Milliseconds", nil)

	return nil
}

func (p *Processor) failPermanent(eventID, reason string) error {
	p.Logger.Error("Permanent failure: "+reason, nil)
	_ = p.Metrics.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": reason})
	_ = p.Idempotency.MarkFailed(eventID, reason)
	return nil // Don't retry
}
