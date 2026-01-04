package idempotency

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/fluxa/fluxa/internal/models"
)

// Client handles idempotency checks
type Client struct {
	db *sql.DB
}

// NewClient creates a new idempotency client
func NewClient(db *sql.DB) *Client {
	return &Client{db: db}
}

// CheckAndMark attempts to mark an event as processing, returns true if already processed
// Uses a transaction with SELECT FOR UPDATE to atomically check and update status
func (c *Client) CheckAndMark(eventID string) (alreadyProcessed bool, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()

	// Loop to handle race conditions during insert
	for i := 0; i < 3; i++ {
		// 1. Try to fetch and lock existing record
		var currentStatus sql.NullString
		var lastSeenAt sql.NullTime
		checkQuery := `SELECT status, last_seen_at FROM idempotency_keys WHERE event_id = $1 FOR UPDATE`
		err = tx.QueryRowContext(ctx, checkQuery, eventID).Scan(&currentStatus, &lastSeenAt)

		if err == sql.ErrNoRows {
			// 2. New event - attempt insert
			insertQuery := `
				INSERT INTO idempotency_keys (event_id, status, first_seen_at, last_seen_at, attempts)
				VALUES ($1, $2, $3, $4, 1)
			`
			_, err = tx.ExecContext(ctx, insertQuery, eventID, string(models.IdempotencyStatusProcessing), now, now)
			if err != nil {
				// If duplicate key error (race condition), continue loop to find the record
				// pq error code 23505 is unique_violation, but checking string is safer cross-driver/mock
				continue
			}
			if err = tx.Commit(); err != nil {
				return false, fmt.Errorf("failed to commit transaction: %w", err)
			}
			return false, nil // Successfully claimed new event
		} else if err != nil {
			return false, fmt.Errorf("failed to check idempotency key: %w", err)
		}

		// 3. Record exists - check state
		if currentStatus.Valid && currentStatus.String == string(models.IdempotencyStatusSuccess) {
			// Already processed successfully
			if err = tx.Commit(); err != nil {
				return false, fmt.Errorf("failed to commit transaction: %w", err)
			}
			return true, nil
		}

		if currentStatus.Valid && currentStatus.String == string(models.IdempotencyStatusProcessing) {
			// If currently processing and "active" (seen recently), consider it locked/deduplicated.
			// This prevents concurrent execution race where B thinks it's a retry while A is still working.
			// Assumption: A process won't take longer than 1 minute without updating status/heartbeat.
			if lastSeenAt.Valid && now.Sub(lastSeenAt.Time) < 1*time.Minute {
				if err = tx.Commit(); err != nil {
					return false, fmt.Errorf("failed to commit transaction: %w", err)
				}
				return true, nil // Considered "already processed" (or being processed)
			}
			// If stale, fall through to retry logic
		}

		// 4. Retry Logic (Status is 'failed' OR 'processing' but stale)
		updateQuery := `
			UPDATE idempotency_keys
			SET status = $1, last_seen_at = $2, attempts = attempts + 1
			WHERE event_id = $3
		`
		_, err = tx.ExecContext(ctx, updateQuery, string(models.IdempotencyStatusProcessing), now, eventID)
		if err != nil {
			return false, fmt.Errorf("failed to update idempotency key: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return false, fmt.Errorf("failed to commit transaction: %w", err)
		}
		return false, nil // Allowed to retry
	}
	return false, fmt.Errorf("failed to process idempotency check after retries")
}

// MarkSuccess marks an event as successfully processed
func (c *Client) MarkSuccess(eventID string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		UPDATE idempotency_keys
		SET status = $1, last_seen_at = $2
		WHERE event_id = $3
	`

	_, err := c.db.ExecContext(ctx, query, string(models.IdempotencyStatusSuccess), time.Now().UTC(), eventID)
	if err != nil {
		return fmt.Errorf("failed to mark success: %w", err)
	}

	return nil
}

// MarkFailed marks an event as failed with error reason
func (c *Client) MarkFailed(eventID string, errorReason string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Truncate error reason to safe length (500 chars to fit in TEXT field comfortably)
	if len(errorReason) > 500 {
		errorReason = errorReason[:500]
	}

	query := `
		UPDATE idempotency_keys
		SET status = $1, last_seen_at = $2, error_reason = $3
		WHERE event_id = $4
	`

	_, err := c.db.ExecContext(ctx, query, string(models.IdempotencyStatusFailed), time.Now().UTC(), errorReason, eventID)
	if err != nil {
		return fmt.Errorf("failed to mark failed: %w", err)
	}

	return nil
}

// GetStatus retrieves the idempotency status for an event
func (c *Client) GetStatus(eventID string) (*models.IdempotencyKeyRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		SELECT event_id, status, first_seen_at, last_seen_at, attempts, error_reason
		FROM idempotency_keys
		WHERE event_id = $1
	`

	var record models.IdempotencyKeyRecord
	var errorReason sql.NullString

	err := c.db.QueryRowContext(ctx, query, eventID).Scan(
		&record.EventID,
		&record.Status,
		&record.FirstSeenAt,
		&record.LastSeenAt,
		&record.Attempts,
		&errorReason,
	)
	if err == sql.ErrNoRows {
		return nil, nil // Not found, means it's new
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query idempotency key: %w", err)
	}

	if errorReason.Valid {
		record.ErrorReason = &errorReason.String
	}

	return &record, nil
}
