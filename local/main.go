package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
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
	ctx := context.Background()

	// Connect to database
	fmt.Println("Connecting to database...")
	dbClient, err := db.NewClient(dbDSN, 10)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to connect to database: %v\n", err)
		os.Exit(1)
	}
	defer dbClient.Close()

	// Run migrations
	fmt.Println("Running migrations...")
	if err := runMigrations(dbClient.GetDB()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to run migrations: %v\n", err)
		os.Exit(1)
	}

	// Test idempotency client
	idempotencyClient := idempotency.NewClient(dbClient.GetDB())

	// Create test event
	eventID := uuid.New().String()
	correlationID := uuid.New().String()
	testEvent := &models.Event{
		EventID:   eventID,
		UserID:    "test_user_123",
		Amount:    99.99,
		Currency:  "USD",
		Merchant:  "Test Merchant",
		Timestamp: time.Now().UTC(),
		Metadata: map[string]interface{}{
			"source": "local_test",
		},
	}

	fmt.Printf("Testing with event_id: %s\n", eventID)

	// Test 1: CheckAndMark should return false for new event
	fmt.Println("\n1. Testing CheckAndMark for new event...")
	alreadyProcessed, err := idempotencyClient.CheckAndMark(eventID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CheckAndMark failed: %v\n", err)
		os.Exit(1)
	}
	if alreadyProcessed {
		fmt.Fprintf(os.Stderr, "ERROR: Expected alreadyProcessed=false for new event\n")
		os.Exit(1)
	}
	fmt.Println("   ✓ New event marked as processing")

	// Test 2: Insert event
	fmt.Println("\n2. Inserting event into database...")
	err = dbClient.InsertEvent(testEvent, correlationID, models.PayloadModeInline, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "InsertEvent failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("   ✓ Event inserted")

	// Test 3: Mark as success
	fmt.Println("\n3. Marking event as successful...")
	err = idempotencyClient.MarkSuccess(eventID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MarkSuccess failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("   ✓ Event marked as successful")

	// Test 4: CheckAndMark again should return true (already processed)
	fmt.Println("\n4. Testing CheckAndMark for already-processed event...")
	alreadyProcessed2, err := idempotencyClient.CheckAndMark(eventID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "CheckAndMark failed: %v\n", err)
		os.Exit(1)
	}
	if !alreadyProcessed2 {
		fmt.Fprintf(os.Stderr, "ERROR: Expected alreadyProcessed=true for already-processed event (IDEMPOTENCY BUG!)\n")
		os.Exit(1)
	}
	fmt.Println("   ✓ Already-processed event correctly detected")

	// Test 5: Query event
	fmt.Println("\n5. Querying event from database...")
	record, err := dbClient.GetEventByID(eventID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "GetEventByID failed: %v\n", err)
		os.Exit(1)
	}
	if record == nil {
		fmt.Fprintf(os.Stderr, "ERROR: Event not found\n")
		os.Exit(1)
	}
	if record.EventID != eventID {
		fmt.Fprintf(os.Stderr, "ERROR: Retrieved event_id mismatch\n")
		os.Exit(1)
	}
	fmt.Printf("   ✓ Event retrieved: user_id=%s, amount=%.2f, merchant=%s\n", record.UserID, record.Amount, record.Merchant)

	// Test 6: Verify only one event exists (idempotency)
	fmt.Println("\n6. Verifying idempotency (should only have one event)...")
	var count int
	err = dbClient.GetDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE event_id = $1", eventID).Scan(&count)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Count query failed: %v\n", err)
		os.Exit(1)
	}
	if count != 1 {
		fmt.Fprintf(os.Stderr, "ERROR: Expected exactly 1 event, found %d (IDEMPOTENCY VIOLATION!)\n", count)
		os.Exit(1)
	}
	fmt.Printf("   ✓ Exactly 1 event found (idempotency verified)\n")

	// Test 7: Try to insert same event again (should be idempotent)
	fmt.Println("\n7. Attempting to insert same event again (idempotency test)...")
	err = dbClient.InsertEvent(testEvent, correlationID, models.PayloadModeInline, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "InsertEvent failed: %v\n", err)
		os.Exit(1)
	}
	var count2 int
	err = dbClient.GetDB().QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE event_id = $1", eventID).Scan(&count2)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Count query failed: %v\n", err)
		os.Exit(1)
	}
	if count2 != 1 {
		fmt.Fprintf(os.Stderr, "ERROR: Expected exactly 1 event after duplicate insert, found %d (IDEMPOTENCY VIOLATION!)\n", count2)
		os.Exit(1)
	}
	fmt.Printf("   ✓ Still exactly 1 event (duplicate insert handled correctly)\n")

	// Print final status
	fmt.Println("\n============================================================")
	fmt.Println("ALL TESTS PASSED ✓")
	fmt.Println("============================================================")
	fmt.Printf("\nEvent ID: %s\n", eventID)
	fmt.Printf("Correlation ID: %s\n", correlationID)
	fmt.Println("\nIdempotency verified:")
	fmt.Println("  - Same event_id processed twice = one row only")
	fmt.Println("  - CheckAndMark correctly detects already-processed events")
}

func runMigrations(db *sql.DB) error {
	// Read and execute migrations
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS events (
			event_id VARCHAR(36) PRIMARY KEY,
			correlation_id VARCHAR(36) NOT NULL,
			user_id VARCHAR(255) NOT NULL,
			amount DECIMAL(18, 2) NOT NULL,
			currency VARCHAR(3) NOT NULL,
			merchant VARCHAR(255) NOT NULL,
			ts TIMESTAMP WITH TIME ZONE NOT NULL,
			metadata_json JSONB,
			payload_mode VARCHAR(10) NOT NULL CHECK (payload_mode IN ('INLINE', 'S3')),
			s3_key VARCHAR(500),
			created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS idempotency_keys (
			event_id VARCHAR(36) PRIMARY KEY,
			status VARCHAR(20) NOT NULL CHECK (status IN ('processing', 'success', 'failed')),
			first_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
			attempts INTEGER NOT NULL DEFAULT 1,
			error_reason TEXT
		)`,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for i, migration := range migrations {
		if _, err := db.ExecContext(ctx, migration); err != nil {
			return fmt.Errorf("migration %d failed: %w", i+1, err)
		}
	}

	return nil
}

