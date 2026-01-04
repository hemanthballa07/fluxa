package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/fluxa/fluxa/internal/config"
	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/logging"
	"github.com/fluxa/fluxa/internal/metrics"
)

var (
	cfg           *config.Config
	logger        *logging.Logger
	metricsClient *metrics.Metrics
	dbClient      *db.Client
)

func init() {
	var err error
	cfg, err = config.LoadFromEnv()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	metricsClient = metrics.NewMetrics("Fluxa", "query")

	dbClient, err = db.NewClient(cfg.DSN(), 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create database client: %v\n", err)
		os.Exit(1)
	}
}

func handler(request events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	// Extract correlation ID
	correlationID := request.Headers["X-Correlation-ID"]
	if correlationID == "" {
		correlationID = request.RequestContext.RequestID
	}

	logger := logging.NewLogger("query", correlationID)
	logger.Info("Received query request", map[string]interface{}{
		"path":       request.Path,
		"method":     request.HTTPMethod,
		"request_id": request.RequestContext.RequestID,
	})

	// Handle health check
	if request.Path == "/health" && request.HTTPMethod == "GET" {
		response := map[string]string{
			"status": "healthy",
		}
		responseBody, _ := json.Marshal(response)
		return events.APIGatewayProxyResponse{
			StatusCode: 200,
			Body:       string(responseBody),
			Headers:    map[string]string{"Content-Type": "application/json"},
		}, nil
	}

	// Extract event_id from path
	eventID := request.PathParameters["event_id"]
	if eventID == "" {
		logger.Warn("Missing event_id in path")
		_ = metricsClient.EmitMetric("query_failure", 1, "Count", map[string]string{"error": "missing_event_id"})
		return events.APIGatewayProxyResponse{
			StatusCode: 400,
			Body:       `{"error": "event_id is required"}`,
			Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
		}, nil
	}

	// Query database
	record, err := dbClient.GetEventByID(eventID)
	if err == db.ErrNotFound {
		logger.Info("Event not found", map[string]interface{}{"event_id": eventID})
		_ = metricsClient.EmitMetric("query_not_found", 1, "Count", nil)
		return events.APIGatewayProxyResponse{
			StatusCode: 404,
			Body:       fmt.Sprintf(`{"error": "Event not found: %s"}`, eventID),
			Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
		}, nil
	}
	if err != nil {
		logger.Error("Failed to query event", err)
		_ = metricsClient.EmitMetric("query_failure", 1, "Count", map[string]string{"error": "db_error"})
		return events.APIGatewayProxyResponse{
			StatusCode: 500,
			Body:       `{"error": "Internal server error"}`,
			Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
		}, nil
	}

	logger.Info("Successfully retrieved event", map[string]interface{}{"event_id": eventID})
	metricsClient.EmitMetric("query_success", 1, "Count", nil)

	// Convert to response format
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

	responseBody, _ := json.Marshal(response)

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       string(responseBody),
		Headers:    map[string]string{"Content-Type": "application/json", "X-Correlation-ID": correlationID},
	}, nil
}

func main() {
	defer dbClient.Close()
	lambda.Start(handler)
}
