package models

import (
	"encoding/json"
	"time"
)

// Event represents a transaction event in the system
type Event struct {
	EventID   string                 `json:"event_id"`
	UserID    string                 `json:"user_id" binding:"required"`
	Amount    float64                `json:"amount" binding:"required"`
	Currency  string                 `json:"currency" binding:"required"`
	Merchant  string                 `json:"merchant" binding:"required"`
	Timestamp time.Time              `json:"timestamp" binding:"required"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
}

// Validation error codes
const (
	ErrCodeMissingField = "MISSING_FIELD"
	ErrCodeInvalidValue = "INVALID_VALUE"
)

// Validate performs basic validation on the event
func (e *Event) Validate() error {
	if e.UserID == "" {
		return ErrInvalidEvent{Field: "user_id", Reason: "cannot be empty", Code: ErrCodeMissingField}
	}
	if e.Amount <= 0 {
		return ErrInvalidEvent{Field: "amount", Reason: "must be greater than 0", Code: ErrCodeInvalidValue}
	}
	if e.Currency == "" {
		return ErrInvalidEvent{Field: "currency", Reason: "cannot be empty", Code: ErrCodeMissingField}
	}
	if e.Merchant == "" {
		return ErrInvalidEvent{Field: "merchant", Reason: "cannot be empty", Code: ErrCodeMissingField}
	}
	if e.Timestamp.IsZero() {
		return ErrInvalidEvent{Field: "timestamp", Reason: "must be set", Code: ErrCodeMissingField}
	}
	// Check for future timestamp (basic sanity check, allow 5 min drift)
	if e.Timestamp.After(time.Now().Add(5 * time.Minute)) {
		return ErrInvalidEvent{Field: "timestamp", Reason: "cannot be in the future", Code: ErrCodeInvalidValue}
	}

	// Metadata size check (simulated constraint)
	if len(e.Metadata) > 10 {
		return ErrInvalidEvent{Field: "metadata", Reason: "too many keys (max 10)", Code: ErrCodeInvalidValue}
	}

	return nil
}

// ToJSON converts the event to JSON bytes
func (e *Event) ToJSON() ([]byte, error) {
	return json.Marshal(e)
}

// ErrInvalidEvent represents a validation error
type ErrInvalidEvent struct {
	Field  string
	Reason string
	Code   string
}

func (e ErrInvalidEvent) Error() string {
	return "invalid event: [" + e.Code + "] " + e.Field + " " + e.Reason
}
