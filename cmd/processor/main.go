package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/service/sns"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/idempotency"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/metrics"
	"github.com/fluxa/fluxa/internal/models"
	"github.com/fluxa/fluxa/internal/queue"
	"github.com/fluxa/fluxa/internal/storage"
)

var (
	cfg              *config.Config
	metricsClient    *metrics.Metrics
	dbClient         *db.Client
	idempotencyClient *idempotency.Client
	queueClient      *queue.Client
	s3Client         *storage.Client
	snsClient        *sns.SNS
)

func init() {
	var err error
	cfg, err = config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	metricsClient = metrics.NewMetrics("Fluxa/Processor")

	dbClient, err = db.NewClient(cfg.DSN(), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create database client: %v\n", err)
		os.Exit(1)
	}

	idempotencyClient = idempotency.NewClient(dbClient.GetDB())

	queueClient, err = queue.NewClient(cfg.SQSQueueURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create queue client: %v\n", err)
		os.Exit(1)
	}

	s3Client, err = storage.NewClient(cfg.S3BucketName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create S3 client: %v\n", err)
		os.Exit(1)
	}

	sess, err := session.NewSession()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create AWS session: %v\n", err)
		os.Exit(1)
	}
	snsClient = sns.New(sess)
}

func handler(event events.SQSEvent) error {
	for _, record := range event.Records {
		if err := processMessage(record); err != nil {
			// Return error to allow SQS retry for transient failures
			return fmt.Errorf("failed to process message: %w", err)
		}
	}
	return nil
}

func processMessage(record events.SQSMessage) error {
	// Extract correlation_id from message attributes
	correlationID := "unknown"
	if attr, ok := record.MessageAttributes["correlation_id"]; ok && attr.StringValue != nil {
		correlationID = *attr.StringValue
	}

	logger := logging.NewLogger(correlationID)
	logger.Info("Processing SQS message", map[string]interface{}{
		"message_id": record.MessageId,
	})

	startTime := time.Now()

	// Parse message
	sqsMsg, err := queue.ParseSQSEventMessage(record.Body)
	if err != nil {
		logger.Error("Failed to parse SQS message", err)
		metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "parse_error"})
		// Don't retry parsing errors - allow DLQ
		return nil
	}

	// Idempotency check
	alreadyProcessed, err := idempotencyClient.CheckAndMark(sqsMsg.EventID)
	if err != nil {
		logger.Error("Failed to check idempotency", err)
		metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "idempotency_error"})
		return fmt.Errorf("idempotency check failed: %w", err) // Retry transient DB errors
	}

	if alreadyProcessed {
		logger.Info("Event already processed, skipping", map[string]interface{}{
			"event_id": sqsMsg.EventID,
		})
		return nil // Success - idempotent
	}

	// Fetch payload
	var payloadBytes []byte
	switch sqsMsg.PayloadMode {
	case models.PayloadModeInline:
		if sqsMsg.PayloadInline == nil {
			logger.Error("PayloadInline is nil for INLINE mode", nil)
			metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "missing_payload"})
			idempotencyClient.MarkFailed(sqsMsg.EventID, "missing_payload")
			return nil // Don't retry
		}
		payloadBytes = []byte(*sqsMsg.PayloadInline)

	case models.PayloadModeS3:
		if sqsMsg.S3Key == nil {
			logger.Error("S3Key is nil for S3 mode", nil)
			metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "missing_s3_key"})
			idempotencyClient.MarkFailed(sqsMsg.EventID, "missing_s3_key")
			return nil // Don't retry
		}

		var err error
		payloadBytes, err = s3Client.GetPayload(*sqsMsg.S3Key)
		if err != nil {
			logger.Error("Failed to fetch payload from S3", err)
			metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "s3_fetch_error"})
			return fmt.Errorf("s3 fetch failed: %w", err) // Retry transient S3 errors
		}

	default:
		logger.Error("Invalid payload mode", fmt.Errorf("unknown payload mode: %s", sqsMsg.PayloadMode))
		metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "invalid_payload_mode"})
		idempotencyClient.MarkFailed(sqsMsg.EventID, "invalid_payload_mode")
		return nil // Don't retry
	}

	// Verify payload hash
	hash := sha256.Sum256(payloadBytes)
	calculatedHash := hex.EncodeToString(hash[:])
	if calculatedHash != sqsMsg.PayloadSHA256 {
		logger.Error("Payload hash mismatch", fmt.Errorf("expected %s, got %s", sqsMsg.PayloadSHA256, calculatedHash))
		metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "hash_mismatch"})
		idempotencyClient.MarkFailed(sqsMsg.EventID, "hash_mismatch")
		return nil // Don't retry
	}

	// Parse event
	var event models.Event
	if err := json.Unmarshal(payloadBytes, &event); err != nil {
		logger.Error("Failed to unmarshal event", err)
		metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "unmarshal_error"})
		idempotencyClient.MarkFailed(sqsMsg.EventID, "unmarshal_error")
		return nil // Don't retry
	}

	// Persist to database
	dbStartTime := time.Now()
	var s3Key *string
	if sqsMsg.PayloadMode == models.PayloadModeS3 {
		s3Key = sqsMsg.S3Key
	}
	
	if err := dbClient.InsertEvent(&event, sqsMsg.CorrelationID, sqsMsg.PayloadMode, s3Key); err != nil {
		logger.Error("Failed to insert event into database", err)
		metricsClient.EmitMetric("processed_failure", 1, "Count", map[string]string{"error": "db_error"})
		return fmt.Errorf("database insert failed: %w", err) // Retry transient DB errors
	}

	dbLatency := time.Since(dbStartTime).Milliseconds()
	metricsClient.EmitMetric("db_latency_ms", float64(dbLatency), "Milliseconds", nil)

	// Mark success
	if err := idempotencyClient.MarkSuccess(sqsMsg.EventID); err != nil {
		logger.Error("Failed to mark idempotency success", err)
		// Non-fatal - event is already persisted
	}

	// Publish SNS notification
	notification := map[string]interface{}{
		"event_id": sqsMsg.EventID,
		"status":   "processed",
	}
	notificationBytes, _ := json.Marshal(notification)

	_, err = snsClient.Publish(&sns.PublishInput{
		TopicArn: aws.String(cfg.SNSTopicARN),
		Message:  aws.String(string(notificationBytes)),
		MessageAttributes: map[string]*sns.MessageAttributeValue{
			"event_id": {
				DataType:    aws.String("String"),
				StringValue: aws.String(sqsMsg.EventID),
			},
			"correlation_id": {
				DataType:    aws.String("String"),
				StringValue: aws.String(sqsMsg.CorrelationID),
			},
		},
	})
	if err != nil {
		logger.Error("Failed to publish SNS notification", err)
		// Non-fatal - event is already processed
	}

	// Calculate processing latency in milliseconds
	latencyMs := time.Since(startTime).Milliseconds()

	logger.Info("Successfully processed event", map[string]interface{}{
		"event_id":    sqsMsg.EventID,
		"latency_ms":  latencyMs,
	})
	metricsClient.EmitMetric("processed_success", 1, "Count", nil)
	metricsClient.EmitMetric("process_latency_ms", float64(latencyMs), "Milliseconds", nil)

	return nil
}

func main() {
	defer dbClient.Close()
	lambda.Start(handler)
}

