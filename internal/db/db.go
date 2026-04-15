package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fluxa/fluxa/internal/domain"
	_ "github.com/lib/pq"
)

// Client wraps database operations
type Client struct {
	db *sql.DB
}

// NewClient creates a new database client
func NewClient(dsn string, maxConnections int) (*Client, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db.SetMaxOpenConns(maxConnections)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return &Client{db: db}, nil
}

// Close closes the database connection
func (c *Client) Close() error {
	return c.db.Close()
}

// GetDB returns the underlying database connection (for idempotency client)
func (c *Client) GetDB() *sql.DB {
	return c.db
}

// InsertEvent inserts an event into the events table
// Uses ON CONFLICT DO NOTHING to handle duplicate event_id gracefully (idempotency)
func (c *Client) InsertEvent(event *domain.Event, correlationID string, payloadMode domain.PayloadMode, s3Key *string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	metadataJSON := "{}"
	if event.Metadata != nil {
		bytes, err := json.Marshal(event.Metadata)
		if err != nil {
			return fmt.Errorf("failed to marshal metadata: %w", err)
		}
		metadataJSON = string(bytes)
	}

	query := `
		INSERT INTO events (
			event_id, correlation_id, user_id, amount, currency, merchant, 
			ts, metadata_json, payload_mode, s3_key, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (event_id) DO NOTHING
	`

	_, err := c.db.ExecContext(
		ctx,
		query,
		event.EventID,
		correlationID,
		event.UserID,
		event.Amount,
		event.Currency,
		event.Merchant,
		event.Timestamp,
		metadataJSON,
		string(payloadMode),
		s3Key,
		time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("failed to insert event: %w", err)
	}

	return nil
}

// GetEventByID retrieves an event by event_id
func (c *Client) GetEventByID(eventID string) (*domain.EventRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		SELECT
			event_id, correlation_id, user_id, amount, currency, merchant,
			ts, metadata_json, payload_mode, s3_key, created_at
		FROM events
		WHERE event_id = $1
	`

	var record domain.EventRecord
	var metadataJSON sql.NullString
	var s3Key sql.NullString

	err := c.db.QueryRowContext(ctx, query, eventID).Scan(
		&record.EventID,
		&record.CorrelationID,
		&record.UserID,
		&record.Amount,
		&record.Currency,
		&record.Merchant,
		&record.Timestamp,
		&metadataJSON,
		&record.PayloadMode,
		&s3Key,
		&record.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to query event: %w", err)
	}

	if metadataJSON.Valid {
		if err := json.Unmarshal([]byte(metadataJSON.String), &record.Metadata); err != nil {
			return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
		}
	}

	if s3Key.Valid {
		record.S3Key = &s3Key.String
	}

	return &record, nil
}

// ErrNotFound is returned when an event is not found
var ErrNotFound = fmt.Errorf("event not found")

// InsertFraudFlag inserts a fraud flag into the fraud_flags table.
// Uses ON CONFLICT DO NOTHING so repeated calls with the same flag_id are safe.
func (c *Client) InsertFraudFlag(flag *domain.FraudFlag) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		INSERT INTO fraud_flags (flag_id, event_id, user_id, rule_name, rule_value, flagged_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (flag_id) DO NOTHING
	`

	_, err := c.db.ExecContext(ctx, query,
		flag.FlagID,
		flag.EventID,
		flag.UserID,
		flag.RuleName,
		flag.RuleValue,
		flag.FlaggedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert fraud flag: %w", err)
	}
	return nil
}

// CountRecentEvents returns the number of events for a user within the last windowSeconds seconds.
// Used by the fraud engine for velocity checks.
func (c *Client) CountRecentEvents(userID string, windowSeconds int) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	query := `
		SELECT COUNT(*)
		FROM events
		WHERE user_id = $1
		  AND ts >= NOW() - ($2 * INTERVAL '1 second')
	`

	var count int
	err := c.db.QueryRowContext(ctx, query, userID, windowSeconds).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count recent events: %w", err)
	}
	return count, nil
}
