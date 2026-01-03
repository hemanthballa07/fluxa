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

// getTestDB returns a test database connection (requires TEST_DB_DSN env var)
// If not set, tests are skipped
func getTestDB(t *testing.T) *sql.DB {
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

	// Clean up test data
	cleanup := func() {
		db.ExecContext(context.Background(), "DELETE FROM idempotency_keys WHERE event_id LIKE 'test-%'")
		db.ExecContext(context.Background(), "DELETE FROM events WHERE event_id LIKE 'test-%'")
	}
	t.Cleanup(cleanup)

	return db
}

func TestCheckAndMark_NewEvent(t *testing.T) {
	db := getTestDB(t)
	client := NewClient(db)

	eventID := "test-new-event-" + time.Now().Format("20060102150405")

	alreadyProcessed, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("CheckAndMark failed: %v", err)
	}

	if alreadyProcessed {
		t.Error("Expected alreadyProcessed to be false for new event")
	}

	// Verify status is 'processing'
	status, err := client.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status == nil {
		t.Fatal("Expected status record to exist")
	}
	if status.Status != string(models.IdempotencyStatusProcessing) {
		t.Errorf("Expected status 'processing', got '%s'", status.Status)
	}
	if status.Attempts != 1 {
		t.Errorf("Expected attempts to be 1, got %d", status.Attempts)
	}
}

func TestCheckAndMark_AlreadyProcessed(t *testing.T) {
	db := getTestDB(t)
	client := NewClient(db)

	eventID := "test-already-processed-" + time.Now().Format("20060102150405")

	// First, mark as processing and then success
	alreadyProcessed1, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("First CheckAndMark failed: %v", err)
	}
	if alreadyProcessed1 {
		t.Error("Expected alreadyProcessed to be false on first call")
	}

	// Mark as successful
	err = client.MarkSuccess(eventID)
	if err != nil {
		t.Fatalf("MarkSuccess failed: %v", err)
	}

	// Now check again - should detect as already processed
	alreadyProcessed2, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("Second CheckAndMark failed: %v", err)
	}

	if !alreadyProcessed2 {
		t.Error("Expected alreadyProcessed to be true for already-successful event")
	}

	// Verify status is still 'success'
	status, err := client.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Status != string(models.IdempotencyStatusSuccess) {
		t.Errorf("Expected status to remain 'success', got '%s'", status.Status)
	}
}

func TestCheckAndMark_RetryAfterFailure(t *testing.T) {
	db := getTestDB(t)
	client := NewClient(db)

	eventID := "test-retry-" + time.Now().Format("20060102150405")

	// First attempt - mark as processing
	alreadyProcessed1, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("First CheckAndMark failed: %v", err)
	}
	if alreadyProcessed1 {
		t.Error("Expected alreadyProcessed to be false on first call")
	}

	// Mark as failed
	err = client.MarkFailed(eventID, "test error")
	if err != nil {
		t.Fatalf("MarkFailed failed: %v", err)
	}

	// Retry - should allow retry (not already processed)
	alreadyProcessed2, err := client.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("Second CheckAndMark failed: %v", err)
	}

	if alreadyProcessed2 {
		t.Error("Expected alreadyProcessed to be false for failed event (allows retry)")
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
		t.Errorf("Expected status to be 'processing' after retry, got '%s'", status.Status)
	}
}

func TestIdempotency_EndToEnd(t *testing.T) {
	db := getTestDB(t)
	idempotencyClient := NewClient(db)

	// This test simulates the full processor flow to prove idempotency
	eventID := "test-e2e-" + time.Now().Format("20060102150405")

	// Simulate first processing attempt
	alreadyProcessed1, err := idempotencyClient.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("CheckAndMark failed: %v", err)
	}
	if alreadyProcessed1 {
		t.Fatal("Expected event to not be already processed")
	}

	// Simulate successful processing
	err = idempotencyClient.MarkSuccess(eventID)
	if err != nil {
		t.Fatalf("MarkSuccess failed: %v", err)
	}

	// Simulate duplicate/retry attempt
	alreadyProcessed2, err := idempotencyClient.CheckAndMark(eventID)
	if err != nil {
		t.Fatalf("Second CheckAndMark failed: %v", err)
	}

	if !alreadyProcessed2 {
		t.Fatal("CRITICAL: Event was not detected as already processed - idempotency broken!")
	}

	// Verify final state
	status, err := idempotencyClient.GetStatus(eventID)
	if err != nil {
		t.Fatalf("GetStatus failed: %v", err)
	}
	if status.Status != string(models.IdempotencyStatusSuccess) {
		t.Errorf("Expected final status to be 'success', got '%s'", status.Status)
	}
}


