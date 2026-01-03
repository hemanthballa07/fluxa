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

// Validate performs basic validation on the event
func (e *Event) Validate() error {
	if e.UserID == "" {
		return ErrInvalidEvent{Field: "user_id", Reason: "cannot be empty"}
	}
	if e.Amount <= 0 {
		return ErrInvalidEvent{Field: "amount", Reason: "must be greater than 0"}
	}
	if e.Currency == "" {
		return ErrInvalidEvent{Field: "currency", Reason: "cannot be empty"}
	}
	if e.Merchant == "" {
		return ErrInvalidEvent{Field: "merchant", Reason: "cannot be empty"}
	}
	if e.Timestamp.IsZero() {
		return ErrInvalidEvent{Field: "timestamp", Reason: "must be set"}
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
}

func (e ErrInvalidEvent) Error() string {
	return "invalid event: " + e.Field + " " + e.Reason
}

