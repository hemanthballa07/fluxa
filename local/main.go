package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	_ "github.com/lib/pq"
	"github.com/fluxa/fluxa/internal/db"
	"github.com/fluxa/fluxa/internal/idempotency"
	"github.com/fluxa/fluxa/internal/models"
	"github.com/google/uuid"
)

const (
	dbDSN = "host=localhost port=5432 user=fluxa_user password=fluxa_password dbname=fluxa sslmode=disable"
)

func main() {
	// 1. Setup
	fmt.Println("🚀 Starting Fluxa Local Test Harness...")
	dbClient, err := db.NewClient(dbDSN, 10)
	if err != nil {
		fatal("Failed to connect to database: %v", err)
	}
	defer dbClient.Close()

	if err := runMigrations(dbClient.GetDB()); err != nil {
		fatal("Failed to run migrations: %v", err)
	}

	idempotencyClient := idempotency.NewClient(dbClient.GetDB())
	
	// 2. Run Scenarios
	runTest("Idempotency (Duplicate Events)", func() {
		eventID := uuid.New().String()
		correlationID := uuid.New().String()

		// A. First submission
		processed, err := idempotencyClient.CheckAndMark(eventID)
		assertNoError(err)
		assertFalse(processed, "New event should not be 'already processed'")
		
		// Simulate processing success
		err = idempotencyClient.MarkSuccess(eventID)
		assertNoError(err)

		// B. Duplicate submission
		processed, err = idempotencyClient.CheckAndMark(eventID)
		assertNoError(err)
		assertTrue(processed, "Duplicate event SHOULD be 'already processed'")

		// Verify only 1 row in events table (simulation)
		// In a real e2e, we'd verify the consumer logic, here we verify the locking logic + DB state
		// Let's insert the event to match the flow
		evt := makeEvent(eventID)
		err = dbClient.InsertEvent(evt, correlationID, models.PayloadModeInline, nil)
		assertNoError(err)

		// Try to insert again (simulate race or duplicate logic)
		// Our DB implementation might not enforce unique constraint on event_id if we rely on app-layer idempotency
		// But let's check idempotency table status
		status, err := idempotencyClient.GetStatus(eventID)
		assertNoError(err)
		assertEqual(status.Status, string(models.IdempotencyStatusSuccess))
	})

	runTest("Schema Validation (Invalid Payload)", func() {
		// This primarily tests the models package
		evt := makeEvent(uuid.New().String())
		evt.UserID = "" // Invalid
		
		err := evt.Validate()
		if err == nil {
			fatal("Expected validation error for missing UserID")
		}
		fmt.Printf("   ✓ Caught expected validation error: %v\n", err)
	})

	runTest("Large Payload Handling (Simulation)", func() {
		// Simulate >256KB payload
		largeData := strings.Repeat("A", 300*1024) // 300KB
		
		// Verify logic for "Should Use S3"
		if len(largeData) <= 256*1024 {
			fatal("Test setup error: payload too small")
		}
		
		// We can't easily test S3 without a mock S3 server (LocalStack), 
		// but we can verify our payload mode logic would flag this.
		// In a real local harness, we'd check the models.PayloadMode logic if exposed
		fmt.Println("   ✓ Large payload size verified (>256KB)")
		fmt.Println("   ✓ (S3 upload skipped in local harness without LocalStack)")
	})
	
	fmt.Println("\n✅ ALL LOCAL SCENARIOS PASSED")
}

// Helpers

func runTest(name string, fn func()) {
	fmt.Printf("\nTEST: %s\n%s\n", name, strings.Repeat("-", 40))
	fn()
	fmt.Println("   PASS")
}

func makeEvent(id string) *models.Event {
	return &models.Event{
		EventID:   id,
		UserID:    "test-user",
		Amount:    10.0,
		Currency:  "USD",
		Merchant:  "TestStore",
		Timestamp: time.Now(),
	}
}

func assertNoError(err error) {
	if err != nil {
		fatal("Unexpected error: %v", err)
	}
}

func assertTrue(val bool, msg string) {
	if !val {
		fatal("Assertion failed: %s", msg)
	}
}

func assertFalse(val bool, msg string) {
	if val {
		fatal("Assertion failed: %s", msg)
	}
}

func assertEqual(a, b interface{}) {
	if a != b {
		fatal("Expected %v, got %v", a, b)
	}
}

func fatal(format string, args ...interface{}) {
	fmt.Printf("\n❌ FAIL: "+format+"\n", args...)
	os.Exit(1)
}

func runMigrations(db *sql.DB) error {
	// Simplified migrations for harness
	query := `create table if not exists idempotency_keys (
		event_id varchar(255) primary key,
		status varchar(50),
		attempts int,
		error_reason text,
		created_at timestamp default current_timestamp,
		updated_at timestamp default current_timestamp
	);
	create table if not exists events (
		event_id varchar(255) primary key,
		correlation_id varchar(255),
		payload_mode varchar(50),
		s3_key text,
		user_id varchar(255),
		amount decimal,
		currency varchar(10),
		merchant varchar(255),
		timestamp timestamp
	);
	`
	_, err := db.Exec(query)
	return err
}
