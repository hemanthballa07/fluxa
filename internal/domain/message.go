package domain

import (
	"time"
)

// PayloadMode indicates how the payload is stored.
type PayloadMode string

const (
	PayloadModeInline PayloadMode = "INLINE"
	PayloadModeS3     PayloadMode = "S3"
)

// QueueMessage represents the message envelope published to and consumed from the queue.
// S3Bucket is not included — the bucket is a service configuration detail, not message data.
type QueueMessage struct {
	EventID       string      `json:"event_id"`
	CorrelationID string      `json:"correlation_id"`
	PayloadMode   PayloadMode `json:"payload_mode"`

	// For INLINE mode
	PayloadInline *string `json:"payload_inline,omitempty"`
	PayloadSHA256 string  `json:"payload_sha256"`

	// For S3 mode — only the key is needed; bucket comes from service config
	S3Key *string `json:"s3_key,omitempty"`

	ReceivedAt time.Time `json:"received_at"`
}

// EventRecord represents a persisted event in the database.
type EventRecord struct {
	EventID       string                 `json:"event_id" db:"event_id"`
	CorrelationID string                 `json:"correlation_id" db:"correlation_id"`
	UserID        string                 `json:"user_id" db:"user_id"`
	Amount        float64                `json:"amount" db:"amount"`
	Currency      string                 `json:"currency" db:"currency"`
	Merchant      string                 `json:"merchant" db:"merchant"`
	Timestamp     time.Time              `json:"timestamp" db:"ts"`
	MetadataJSON  string                 `json:"-" db:"metadata_json"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
	PayloadMode   PayloadMode            `json:"payload_mode" db:"payload_mode"`
	S3Key         *string                `json:"s3_key,omitempty" db:"s3_key"`
	CreatedAt     time.Time              `json:"created_at" db:"created_at"`
}

// IdempotencyKeyRecord represents an idempotency key in the database.
type IdempotencyKeyRecord struct {
	EventID     string    `db:"event_id"`
	Status      string    `db:"status"`
	FirstSeenAt time.Time `db:"first_seen_at"`
	LastSeenAt  time.Time `db:"last_seen_at"`
	Attempts    int       `db:"attempts"`
	ErrorReason *string   `db:"error_reason"`
}

// IdempotencyStatus represents the processing status.
type IdempotencyStatus string

const (
	IdempotencyStatusProcessing IdempotencyStatus = "processing"
	IdempotencyStatusSuccess    IdempotencyStatus = "success"
	IdempotencyStatusFailed     IdempotencyStatus = "failed"
)
