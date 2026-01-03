package models

import (
	"encoding/json"
	"testing"
	"time"
)

func TestEvent_Validate(t *testing.T) {
	tests := []struct {
		name    string
		event   Event
		wantErr bool
	}{
		{
			name: "valid event",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: time.Now(),
			},
			wantErr: false,
		},
		{
			name: "missing user_id",
			event: Event{
				Amount:    100.50,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "zero amount",
			event: Event{
				UserID:    "user123",
				Amount:    0,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "negative amount",
			event: Event{
				UserID:    "user123",
				Amount:    -10.00,
				Currency:  "USD",
				Merchant:  "Amazon",
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing currency",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Merchant:  "Amazon",
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing merchant",
			event: Event{
				UserID:    "user123",
				Amount:    100.50,
				Currency:  "USD",
				Timestamp: time.Now(),
			},
			wantErr: true,
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
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.event.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Event.Validate() error = %v, wantErr %v", err, tt.wantErr)
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

