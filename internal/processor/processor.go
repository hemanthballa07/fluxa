package processor

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	p.Logger.Info("Processing event", map[string]interface{}{
		"event_id": sqsMsg.EventID,
	})

	startTime := time.Now()

	// 1. Idempotency check
	alreadyProcessed, err := p.Idempotency.CheckAndMark(sqsMsg.EventID)
	if err != nil {
		p.Logger.Error("Failed to check idempotency", err)
		_ = p.Metrics.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "idempotency_error"})
		return fmt.Errorf("idempotency check failed: %w", err)
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
			return p.failPermanent(sqsMsg.EventID, "missing_payload")
		}
		payloadBytes = []byte(*sqsMsg.PayloadInline)

	case models.PayloadModeS3:
		if sqsMsg.S3Key == nil {
			return p.failPermanent(sqsMsg.EventID, "missing_s3_key")
		}

		var err error
		payloadBytes, err = p.S3.GetPayload(*sqsMsg.S3Key)
		if err != nil {
			p.Logger.Error("Failed to fetch payload from S3", err)
			_ = p.Metrics.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "s3_fetch_error"})
			return fmt.Errorf("s3 fetch failed: %w", err)
		}

	default:
		return p.failPermanent(sqsMsg.EventID, "invalid_payload_mode")
	}

	// 3. Verify Hash
	hash := sha256.Sum256(payloadBytes)
	calculatedHash := hex.EncodeToString(hash[:])
	if calculatedHash != sqsMsg.PayloadSHA256 {
		return p.failPermanent(sqsMsg.EventID, "hash_mismatch")
	}

	// 4. Parse Event
	var event models.Event
	if err := json.Unmarshal(payloadBytes, &event); err != nil {
		return p.failPermanent(sqsMsg.EventID, "unmarshal_error")
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
		return fmt.Errorf("database insert failed: %w", err)
	}

	_ = p.Metrics.EmitMetric("db_latency_ms", float64(time.Since(dbStartTime).Milliseconds()), "Milliseconds", nil)

	// 6. Mark Success
	if err := p.Idempotency.MarkSuccess(sqsMsg.EventID); err != nil {
		p.Logger.Error("Failed to mark idempotency success", err)
		// Non-fatal
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
