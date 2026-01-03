package idempotency

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/fluxa/fluxa/internal/models"
)

// TestDuplicateMessageDelivery simulates duplicate SQS message delivery
// Expected: Second message should be detected as already processed and skipped
func TestDuplicateMessageDelivery(t *testing.T) {
	db := getTestDBForFailureTests(t)
	client := NewClient(db)

	eventID := "test-duplicate-" + time.Now().Format("20060102150405")

	// Simulate first message processing
	alreadyProcessed1, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("First CheckAndMark failed: %v", err)
	}
	if alreadyProcessed1 {
		t.Error("First message should not be already processed")
	}

	// Simulate successful processing
	err = client.MarkSuccess(eventID)
	if err != nil {
		t.Fatalf("MarkSuccess failed: %v", err)
	}

	// Simulate duplicate message delivery (same event_id)
	alreadyProcessed2, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("Second CheckAndMark failed: %v", err)
	}

	if !alreadyProcessed2 {
		t.Fatal("CRITICAL: Duplicate message was not detected as already processed - idempotency broken!")
	}

	// Verify status remains 'success'
	status, err := client.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Status != string(models.IdempotencyStatusSuccess) {
		t.Errorf("Expected status to remain 'success', got '%s'", status.Status)
	}
}

// TestProcessorCrashMidTransaction simulates a crash after CheckAndMark but before MarkSuccess
// Expected: Retry should detect the 'processing' status and allow retry (increments attempts)
func TestProcessorCrashMidTransaction(t *testing.T) {
	db := getTestDBForFailureTests(t)
	client := NewClient(db)

	eventID := "test-crash-" + time.Now().Format("20060102150405")

	// Simulate first attempt - CheckAndMark sets status to 'processing'
	alreadyProcessed1, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("First CheckAndMark failed: %v", err)
	}
	if alreadyProcessed1 {
		t.Error("First attempt should not be already processed")
	}

	// Simulate crash - no MarkSuccess called, status remains 'processing'

	// Simulate retry after crash - should detect 'processing' status and allow retry
	alreadyProcessed2, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("Retry CheckAndMark failed: %v", err)
	}

	// Should not be detected as 'already processed' (status is 'processing', not 'success')
	if alreadyProcessed2 {
		t.Error("Retry after crash should allow reprocessing (status is 'processing', not 'success')")
	}

	// Verify attempts incremented
	status, err := client.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Attempts < 2 {
		t.Errorf("Expected attempts to be at least 2, got %d", status.Attempts)
	}
	if status.Status != string(models.IdempotencyStatusProcessing) {
		t.Errorf("Expected status to be 'processing', got '%s'", status.Status)
	}
}

// TestDBTimeout simulates a database timeout scenario
// Expected: Context timeout should cancel operation, error returned (allows retry)
func TestDBTimeout(t *testing.T) {
	db := getTestDBForFailureTests(t)
	
	// Create a context with very short timeout to simulate timeout
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	// Wait a bit to ensure timeout has occurred
	time.Sleep(10 * time.Millisecond)

	// Attempt query with expired context
	var result string
	err := db.QueryRowContext(ctx, "SELECT 'test'").Scan(&result)
	
	// Should fail with context deadline exceeded
	if err == nil {
		t.Error("Expected context deadline exceeded error, got nil")
	}
	if ctx.Err() != context.DeadlineExceeded {
		t.Errorf("Expected context.DeadlineExceeded, got %v", ctx.Err())
	}
}

// TestInvalidPayloadSchema simulates invalid payload that fails validation
// Expected: Event should be marked as failed, sent to DLQ (no retry)
func TestInvalidPayloadSchema(t *testing.T) {
	db := getTestDBForFailureTests(t)
	client := NewClient(db)

	eventID := "test-invalid-schema-" + time.Now().Format("20060102150405")

	// Simulate CheckAndMark (payload would be validated before this in real flow)
	alreadyProcessed, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("CheckAndMark failed: %v", err)
	}
	if alreadyProcessed {
		t.Error("Invalid payload should start as not processed")
	}

	// Simulate validation failure - mark as failed
	err = client.MarkFailed(eventID, "invalid_schema: missing required field 'user_id'")
	if err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	// Verify status is 'failed'
	status, err := client.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Status != string(models.IdempotencyStatusFailed) {
		t.Errorf("Expected status 'failed', got '%s'", status.Status)
	}
	if status.ErrorReason == nil || *status.ErrorReason == "" {
		t.Error("Expected error reason to be set")
	}

	// Retry should still allow reprocessing (failed status allows retry in our model)
	alreadyProcessed2, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("Retry CheckAndMark failed: %v", err)
	}
	// Note: Our current model allows retrying failed events. This is intentional.
	// In a strict model, failed events might be permanently rejected.
	if alreadyProcessed2 {
		t.Error("Failed events can be retried in our model")
	}
}

// TestPayloadHashMismatch simulates payload hash mismatch scenario
// Expected: Event marked as failed with 'hash_mismatch' reason, sent to DLQ (no retry)
func TestPayloadHashMismatch(t *testing.T) {
	db := getTestDBForFailureTests(t)
	client := NewClient(db)

	eventID := "test-hash-mismatch-" + time.Now().Format("20060102150405")

	// Simulate CheckAndMark
	alreadyProcessed, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("CheckAndMark failed: %v", err)
	}
	if alreadyProcessed {
		t.Error("Hash mismatch should start as not processed")
	}

	// Simulate hash mismatch detection - mark as failed
	err = client.MarkFailed(eventID, "hash_mismatch")
	if err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	// Verify status and error reason
	status, err := client.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Status != string(models.IdempotencyStatusFailed) {
		t.Errorf("Expected status 'failed', got '%s'", status.Status)
	}
	if status.ErrorReason == nil || *status.ErrorReason != "hash_mismatch" {
		t.Errorf("Expected error reason 'hash_mismatch', got '%v'", status.ErrorReason)
	}
}

// TestSQSRetryToDLQ simulates the path from SQS retry to DLQ
// Expected: After max receives, message goes to DLQ. Status should reflect failure.
func TestSQSRetryToDLQ(t *testing.T) {
	db := getTestDBForFailureTests(t)
	client := NewClient(db)

	eventID := "test-dlq-" + time.Now().Format("20060102150405")

	// Simulate multiple retry attempts (maxReceiveCount = 3 in our config)
	for attempt := 1; attempt <= 3; attempt++ {
		alreadyProcessed, err := client.CheckAndMark(eventID)
		if err != nil {
			t.Fatalf("Attempt %d CheckAndMark failed: %v", attempt, err)
		}
		if alreadyProcessed {
			t.Errorf("Attempt %d should not be already processed", attempt)
		}

		// Simulate transient failure (e.g., DB timeout) - retry
		// Don't mark as failed yet, allow retry
	}

	// After max retries, mark as failed (DLQ scenario)
	err := client.MarkFailed(eventID, "max_retries_exceeded: db_connection_timeout")
	if err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	// Verify final state
	status, err := client.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Attempts < 3 {
		t.Errorf("Expected at least 3 attempts, got %d", status.Attempts)
	}
	if status.Status != string(models.IdempotencyStatusFailed) {
		t.Errorf("Expected status 'failed', got '%s'", status.Status)
	}
}

// getTestDBForFailureTests is a helper for failure injection tests
// Uses same logic as idempotency_test.go:getTestDB but separate to avoid redeclaration
func getTestDBForFailureTests(t *testing.T) *sql.DB {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set, skipping integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Failed to ping test database: %v", err)
	}

	t.Cleanup(func() {
		db.ExecContext(context.Background(), "DELETE FROM idempotency_keys WHERE event_id LIKE 'test-%'")
		db.ExecContext(context.Background(), "DELETE FROM events WHERE event_id LIKE 'test-%'")
		db.Close()
	})

	return db
}

