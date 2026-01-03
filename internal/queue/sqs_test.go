package queue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/fluxa/fluxa/internal/models"
)

func TestShouldUseS3(t *testing.T) {
	tests := []struct {
		name         string
		payloadSize  int
		wantUseS3    bool
	}{
		{
			name:        "small payload inline",
			payloadSize: 100 * 1024, // 100KB
			wantUseS3:   false,
		},
		{
			name:        "exactly 256KB inline",
			payloadSize: 256 * 1024, // 256KB
			wantUseS3:   false,
		},
		{
			name:        "large payload use S3",
			payloadSize: 257 * 1024, // 257KB
			wantUseS3:   true,
		},
		{
			name:        "very large payload use S3",
			payloadSize: 1024 * 1024, // 1MB
			wantUseS3:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldUseS3(tt.payloadSize)
			if got != tt.wantUseS3 {
				t.Errorf("ShouldUseS3(%d) = %v, want %v", tt.payloadSize, got, tt.wantUseS3)
			}
		})
	}
}

func TestParseSQSEventMessage(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{
			name: "valid inline message",
			body: func() string {
				msg := models.SQSEventMessage{
					EventID:       "event-123",
					CorrelationID: "corr-123",
					PayloadMode:   models.PayloadModeInline,
					PayloadInline: stringPtr("{\"test\": \"data\"}"),
					PayloadSHA256: "abc123",
					ReceivedAt:    time.Now(),
				}
				bytes, _ := json.Marshal(msg)
				return string(bytes)
			}(),
			wantErr: false,
		},
		{
			name: "valid S3 message",
			body: func() string {
				bucket := "test-bucket"
				key := "raw/2024-01-01/event-123.json"
				msg := models.SQSEventMessage{
					EventID:       "event-123",
					CorrelationID: "corr-123",
					PayloadMode:   models.PayloadModeS3,
					S3Bucket:      &bucket,
					S3Key:         &key,
					PayloadSHA256: "abc123",
					ReceivedAt:    time.Now(),
				}
				bytes, _ := json.Marshal(msg)
				return string(bytes)
			}(),
			wantErr: false,
		},
		{
			name:    "missing event_id",
			body:    `{"correlation_id": "corr-123", "payload_sha256": "abc123"}`,
			wantErr: true,
		},
		{
			name:    "missing correlation_id",
			body:    `{"event_id": "event-123", "payload_sha256": "abc123"}`,
			wantErr: true,
		},
		{
			name:    "missing payload_sha256",
			body:    `{"event_id": "event-123", "correlation_id": "corr-123"}`,
			wantErr: true,
		},
		{
			name:    "invalid JSON",
			body:    `{invalid json}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSQSEventMessage(tt.body)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSQSEventMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func stringPtr(s string) *string {
	return &s
}


