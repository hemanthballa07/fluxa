package idempotency

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	"github.com/fluxa/fluxa/internal/models"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
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

	cleanup := func() {
		if _, err := db.ExecContext(context.Background(), "DELETE FROM idempotency_keys WHERE event_id LIKE 'test-%'"); err != nil {
			t.Logf("cleanup failed: %v", err)
		}
		if _, err := db.ExecContext(context.Background(), "DELETE FROM events WHERE event_id LIKE 'test-%'"); err != nil {
			t.Logf("cleanup failed: %v", err)
		}
	}
	t.Cleanup(cleanup)

	return db
}

func TestCheckAndMark_NewEvent(t *testing.T) {
	db := getTestDB(t)
	client := NewClient(db)

	eventID := "test-" + uuid.New().String()

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

	eventID := "test-" + uuid.New().String()

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

	eventID := "test-" + uuid.New().String()

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
	eventID := "test-" + uuid.New().String()

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

func TestCheckAndMark_ConcurrentCalls_OnlyOneSucceeds(t *testing.T) {
	db := getTestDB(t)
	client := NewClient(db)

	eventID := "test-" + uuid.New().String()
	concurrency := 50

	// Channel to coordinate start of all goroutines
	startCh := make(chan struct{})
	// Channel to collect results
	resultsCh := make(chan bool, concurrency)

	for i := 0; i < concurrency; i++ {
		go func() {
			<-startCh // Wait for signal to start

			// Try to acquire lock
			alreadyProcessed, err := client.CheckAndMark(eventID)
			if err != nil {
				// In a real race, some DB errors (serialization failure) might occur
				// But our logic handles locking, so we expect mostly success or alreadyProcessed
				// We log error but don't fail test immediately to avoid race on t.Fail
				t.Logf("CheckAndMark error: %v", err)
				resultsCh <- false // Treat error as "didn't succeed in claiming"
				return
			}

			if !alreadyProcessed {
				resultsCh <- true // I claimed it!
			} else {
				resultsCh <- false // Already taken
			}
		}()
	}

	// Release the hounds!
	close(startCh)

	// Collect results
	successCount := 0
	for i := 0; i < concurrency; i++ {
		if <-resultsCh {
			successCount++
		}
	}

	if successCount != 1 {
		t.Errorf("Expected exactly 1 client to succeed, got %d", successCount)
	}

	// Verify DB state has exactly 1 record
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM idempotency_keys WHERE event_id = $1", eventID).Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query DB: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected 1 idempotency record, found %d", count)
	}
}
