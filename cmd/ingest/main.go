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
	"github.com/google/uuid"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/metrics"
	"github.com/fluxa/fluxa/internal/models"
	"github.com/fluxa/fluxa/internal/queue"
	"github.com/fluxa/fluxa/internal/storage"
)

var (
	cfg       *config.Config
	logger    *logging.Logger
	metricsClient *metrics.Metrics
	queueClient   *queue.Client
	s3Client      *storage.Client
)

func init() {
	var err error
	cfg, err = config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	metricsClient = metrics.NewMetrics("Fluxa/Ingest")
	
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
}

func handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	startTime := time.Now()
	
	// Generate correlation ID if not present in headers
	correlationID := request.Headers["X-Correlation-ID"]
	if correlationID == "" {
		correlationID = uuid.New().String()
	}

	logger := logging.NewLogger(correlationID)
	logger.Info("Received ingest request", map[string]interface{}{
		"path":       request.Path,
		"method":     request.HTTPMethod,
		"request_id": request.RequestContext.RequestID,
	})

	// Parse request body
	var event models.Event
	if err := json.Unmarshal([]byte(request.Body), &event); err != nil {
		logger.Error("Failed to parse request body", err)
		metricsClient.EmitMetric("ingest_failure", 1, "Count", map[string]string{"error": "parse_error"})
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       fmt.Sprintf(`{"error": "Invalid JSON: %v"}`, err),
			Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
		}, nil
	}

	// Generate event_id if not provided
	if event.EventID == "" {
		event.EventID = uuid.New().String()
	}

	// Validate event
	if err := event.Validate(); err != nil {
		logger.Error("Event validation failed", err)
		metricsClient.EmitMetric("ingest_failure", 1, "Count", map[string]string{"error": "validation_error"})
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       fmt.Sprintf(`{"error": "Validation failed: %v"}`, err),
			Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
		}, nil
	}

	// Serialize payload for size check and hashing
	payloadBytes, err := event.ToJSON()
	if err != nil {
		logger.Error("Failed to serialize event", err)
		metricsClient.EmitMetric("ingest_failure", 1, "Count", map[string]string{"error": "serialization_error"})
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       `{"error": "Internal server error"}`,
			Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
		}, nil
	}

	// Calculate payload hash
	hash := sha256.Sum256(payloadBytes)
	payloadSHA256 := hex.EncodeToString(hash[:])

	// Build SQS message
	sqsMsg := &models.SQSEventMessage{
		EventID:       event.EventID,
		CorrelationID: correlationID,
		PayloadSHA256: payloadSHA256,
		ReceivedAt:    event.Timestamp,
	}

	// Handle payload size
	if queue.ShouldUseS3(len(payloadBytes)) {
		// Store in S3
		s3Key, err := s3Client.PutPayload(event.EventID, payloadBytes)
		if err != nil {
			logger.Error("Failed to store payload in S3", err)
			metricsClient.EmitMetric("ingest_failure", 1, "Count", map[string]string{"error": "s3_error"})
			return events.APIGatewayProxyResponse{
				StatusCode: 500,
				Body:       `{"error": "Internal server error"}`,
				Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
			}, nil
		}

		sqsMsg.PayloadMode = models.PayloadModeS3
		sqsMsg.S3Bucket = &cfg.S3BucketName
		sqsMsg.S3Key = &s3Key
		
		logger.Info("Stored payload in S3", map[string]interface{}{"s3_key": s3Key})
		metricsClient.EmitMetric("s3_puts", 1, "Count", nil)
	} else {
		// Include inline
		payloadStr := string(payloadBytes)
		sqsMsg.PayloadMode = models.PayloadModeInline
		sqsMsg.PayloadInline = &payloadStr
	}

	// Send to SQS
	if err := queueClient.SendEventMessage(sqsMsg); err != nil {
		logger.Error("Failed to send message to SQS", err)
		metricsClient.EmitMetric("ingest_failure", 1, "Count", map[string]string{"error": "sqs_error"})
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       `{"error": "Internal server error"}`,
			Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
		}, nil
	}

	// Calculate latency in milliseconds
	latencyMs := time.Since(startTime).Milliseconds()
	
	logger.Info("Successfully enqueued event", map[string]interface{}{
		"event_id":      event.EventID,
		"payload_mode":  sqsMsg.PayloadMode,
		"latency_ms":    latencyMs,
	})
	
	// Emit metrics
	metricsClient.EmitMetric("ingest_success", 1, "Count", nil)
	metricsClient.EmitMetric("sqs_sent", 1, "Count", nil)
	metricsClient.EmitMetric("ingest_latency_ms", float64(latencyMs), "Milliseconds", nil)
	
	// Emit payload mode counter
	if sqsMsg.PayloadMode == models.PayloadModeInline {
		metricsClient.EmitMetric("payload_inline_count", 1, "Count", nil)
	} else {
		metricsClient.EmitMetric("payload_s3_count", 1, "Count", nil)
	}

	response := map[string]interface{}{
		"event_id": event.EventID,
		"status":   "enqueued",
	}
	responseBody, _ := json.Marshal(response)

	return events.APIGatewayProxyResponse{
		StatusCode: 202,
		Body:       string(responseBody),
		Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
	}, nil
}

func main() {
	lambda.Start(handler)
}

