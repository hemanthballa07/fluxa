package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvent_Validate(t *testing.T) {
	futureTime := time.Now().Add(24 * time.Hour)
	validTime := time.Now()

	tests := []struct {
		name     string
		event    Event
		wantErr  bool
		errCode  string // Optional: check specific error code
		errField string // Optional: check specific error field
	}{
		{
			name: "valid event",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: validTime,
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			event: Event{
				Amount:    100.50,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: validTime,
			},
			wantErr:  true,
			errCode:  ErrCodeMissingField,
			errField: "user_id",
		},
		{
			name: "zero amount",
			event: Event{
				UserID:    "user123",
				Amount:    0,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: validTime,
			},
			wantErr:  true,
			errCode:  ErrCodeInvalidValue,
			errField: "amount",
		},
		{
			name: "negative amount",
			event: Event{
				UserID:    "user123",
				Amount:    -10.00,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: validTime,
			},
			wantErr:  true,
			errCode:  ErrCodeInvalidValue,
			errField: "amount",
		},
		{
			name: "missing currency",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Merchant:  "Amazon",
				Timestamp: validTime,
			},
			wantErr:  true,
			errCode:  ErrCodeMissingField,
			errField: "currency",
		},
		{
			name: "missing merchant",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Currency:  "USD",
				Timestamp: validTime,
			},
			wantErr:  true,
			errCode:  ErrCodeMissingField,
			errField: "merchant",
		},
		{
			name: "zero timestamp",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: time.Time{},
			},
			wantErr:  true,
			errCode:  ErrCodeMissingField,
			errField: "timestamp",
		},
		{
			name: "future timestamp",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: futureTime,
			},
			wantErr:  true,
			errCode:  ErrCodeInvalidValue,
			errField: "timestamp",
		},
		{
			name: "metadata too large",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: validTime,
				Metadata: map[string]interface{}{
					"k1": "v1", "k2": "v2", "k3": "v3", "k4": "v4", "k5": "v5",
					"k6": "v1", "k7": "v2", "k8": "v3", "k9": "v4", "k10": "v5",
					"k11": "v6", // 11th key, limit is 10
				},
			},
			wantErr:  true,
			errCode:  ErrCodeInvalidValue,
			errField: "metadata",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Event.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil {
				// Assert structured error fields
				validationErr, ok := err.(ErrInvalidEvent)
				if !ok {
					t.Errorf("Expected ErrInvalidEvent type, got %T", err)
				} else {
					if tt.errCode != "" && validationErr.Code != tt.errCode {
						t.Errorf("Expected error code %s, got %s", tt.errCode, validationErr.Code)
					}
					if tt.errField != "" && validationErr.Field != tt.errField {
						t.Errorf("Expected error field %s, got %s", tt.errField, validationErr.Field)
					}
				}
			}
		})
	}
}

func TestEvent_ToJSON(t *testing.T) {
	event := Event{
		EventID:   "test-id",
		UserID:    "user123",
		Amount:    100.50,
		Currency:  "USD",
		Merchant:  "Amazon",
		Timestamp: time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
		Metadata: map[string]interface{}{
			"key": "value",
		},
	}

	jsonBytes, err := event.ToJSON()
	if err != nil {
		t.Fatalf("Event.ToJSON() error = %v", err)
	}

	if len(jsonBytes) == 0 {
		t.Error("Event.ToJSON() returned empty bytes")
	}

	// Verify it's valid JSON by unmarshaling
	var decoded Event
	if err := json.Unmarshal(jsonBytes, &decoded); err != nil {
		t.Errorf("Event.ToJSON() produced invalid JSON: %v", err)
	}
}
