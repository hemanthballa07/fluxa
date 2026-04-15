package processor

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/domain"
	"github.com/fluxa/fluxa/internal/idempotency"
	"github.com/fluxa/fluxa/internal/logging"

	_ "github.com/lib/pq"
)

// noopMetrics satisfies ports.Metrics for tests without importing the prometheus adapter.
type noopMetrics struct{}

func (n *noopMetrics) IncCounter(name string, labels ...string)                      {}
func (n *noopMetrics) ObserveHistogram(name string, value float64, labels ...string) {}

func getTestDB(t *testing.T) *db.Client {
	dsn := "host=localhost port=5432 user=fluxa_user password=fluxa_password dbname=fluxa sslmode=disable"
	client, err := db.NewClient(dsn, 10)
	if err != nil {
		t.Skipf("Skipping integration test: failed to connect to DB: %v", err)
	}
	_, _ = client.GetDB().Exec("DELETE FROM fraud_flags WHERE event_id LIKE 'test-proc-%'")
	_, _ = client.GetDB().Exec("DELETE FROM events WHERE event_id LIKE 'test-proc-%'")
	_, _ = client.GetDB().Exec("DELETE FROM idempotency_keys WHERE event_id LIKE 'test-proc-%'")
	return client
}

func TestProcessor_DuplicateMessage(t *testing.T) {
	dbClient := getTestDB(t)
	defer dbClient.Close()

	proc := &Processor{
		DB:          dbClient,
		Idempotency: idempotency.NewClient(dbClient.GetDB()),
		Storage:     nil, // Not needed for INLINE test
		Publisher:   nil,
		Fraud:       nil, // Skipped when nil
		Metrics:     &noopMetrics{},
		Logger:      logging.NewLogger("test", "test-corr-id"),
	}

	eventID := "test-proc-dup-" + time.Now().Format("20060102150405")
	payload := `{"user_id":"u1","amount":10,"currency":"USD","merchant":"m1","timestamp":"2024-01-01T00:00:00Z"}`
	hash := sha256.Sum256([]byte(payload))

	msg := &domain.QueueMessage{
		EventID:       eventID,
		CorrelationID: "corr-1",
		PayloadMode:   domain.PayloadModeInline,
		PayloadInline: &payload,
		PayloadSHA256: hex.EncodeToString(hash[:]),
		ReceivedAt:    time.Now(),
	}

	if err := proc.ProcessMessage(msg); err != nil {
		t.Fatalf("First processing failed: %v", err)
	}
	if err := proc.ProcessMessage(msg); err != nil {
		t.Fatalf("Second processing (duplicate) failed: %v", err)
	}

	var count int
	if err := dbClient.GetDB().QueryRow("SELECT COUNT(*) FROM events WHERE event_id = $1", eventID).Scan(&count); err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 event row, got %d", count)
	}
}

func TestProcessor_HashMismatch_NoRetry(t *testing.T) {
	dbClient := getTestDB(t)
	defer dbClient.Close()

	idemClient := idempotency.NewClient(dbClient.GetDB())
	proc := &Processor{
		DB:          dbClient,
		Idempotency: idemClient,
		Fraud:       nil,
		Metrics:     &noopMetrics{},
		Logger:      logging.NewLogger("test", "test-corr-id"),
	}

	eventID := "test-proc-hash-" + time.Now().Format("20060102150405")
	payload := "{}"
	msg := &domain.QueueMessage{
		EventID:       eventID,
		CorrelationID: "corr-1",
		PayloadMode:   domain.PayloadModeInline,
		PayloadInline: &payload,
		PayloadSHA256: "bad-hash",
		ReceivedAt:    time.Now(),
	}

	// Process should return nil (permanent failure — ACK'd) but mark idempotency as failed
	if err := proc.ProcessMessage(msg); err != nil {
		t.Errorf("Expected nil error (permanent failure), got %v", err)
	}

	status, err := idemClient.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Status != string(domain.IdempotencyStatusFailed) {
		t.Errorf("Expected status 'failed', got '%s'", status.Status)
	}
	if status.ErrorReason == nil || *status.ErrorReason != "non-retryable: hash_mismatch" {
		t.Errorf("Expected error reason 'non-retryable: hash_mismatch', got %v", status.ErrorReason)
	}
}
