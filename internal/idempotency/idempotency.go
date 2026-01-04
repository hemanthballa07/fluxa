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

	// First, check if record exists and lock it
	var currentStatus sql.NullString
	checkQuery := `SELECT status FROM idempotency_keys WHERE event_id = $1 FOR UPDATE`
	err = tx.QueryRowContext(ctx, checkQuery, eventID).Scan(&currentStatus)

	if err == sql.ErrNoRows {
		// New event - insert with 'processing' status
		insertQuery := `
			INSERT INTO idempotency_keys (event_id, status, first_seen_at, last_seen_at, attempts)
			VALUES ($1, $2, $3, $4, 1)
		`
		_, err = tx.ExecContext(ctx, insertQuery, eventID, string(models.IdempotencyStatusProcessing), now, now)
		if err != nil {
			return false, fmt.Errorf("failed to insert idempotency key: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return false, fmt.Errorf("failed to commit transaction: %w", err)
		}
		return false, nil // Not already processed
	}
	if err != nil {
		return false, fmt.Errorf("failed to check idempotency key: %w", err)
	}

	// Record exists - check if already successfully processed
	if currentStatus.Valid && currentStatus.String == string(models.IdempotencyStatusSuccess) {
		// Already processed successfully - update last_seen_at but keep status as 'success'
		updateQuery := `
			UPDATE idempotency_keys
			SET last_seen_at = $1
			WHERE event_id = $2
		`
		_, err = tx.ExecContext(ctx, updateQuery, now, eventID)
		if err != nil {
			return false, fmt.Errorf("failed to update idempotency key: %w", err)
		}
		if err = tx.Commit(); err != nil {
			return false, fmt.Errorf("failed to commit transaction: %w", err)
		}
		return true, nil // Already processed
	}

	// Record exists but not successful - update to 'processing' and increment attempts
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
	return false, nil // Not already processed, marked as processing
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
