package logging

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// LogEntry represents a single log line with standardized fields
type LogEntry struct {
	Timestamp     string                 `json:"timestamp"`
	Level         string                 `json:"level"`
	Message       string                 `json:"message"`
	CorrelationID string                 `json:"correlation_id"`
	Service       string                 `json:"service,omitempty"`
	Stage         string                 `json:"stage,omitempty"`
	EventID       string                 `json:"event_id,omitempty"`
	PayloadMode   string                 `json:"payload_mode,omitempty"`
	Status        string                 `json:"status,omitempty"`
	LatencyMs     float64                `json:"latency_ms,omitempty"`
	ErrorCode     string                 `json:"error_code,omitempty"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
}

// Logger provides structured logging with context
type Logger struct {
	correlationID string
	service       string
	defaultFields map[string]interface{}
}

// NewLogger creates a new logger with service name and correlation ID
func NewLogger(service, correlationID string) *Logger {
	return &Logger{
		service:       service,
		correlationID: correlationID,
		defaultFields: make(map[string]interface{}),
	}
}

// With returns a new Logger instance with additional context fields
func (l *Logger) With(fields map[string]interface{}) *Logger {
	newFields := make(map[string]interface{})
	for k, v := range l.defaultFields {
		newFields[k] = v
	}
	for k, v := range fields {
		newFields[k] = v
	}
	return &Logger{
		service:       l.service,
		correlationID: l.correlationID,
		defaultFields: newFields,
	}
}

// Info logs an info level message
func (l *Logger) Info(message string, fields ...map[string]interface{}) {
	l.log("INFO", message, fields...)
}

// Error logs an error level message
func (l *Logger) Error(message string, err error, fields ...map[string]interface{}) {
	fieldsMap := mergeFields(fields...)
	if err != nil {
		fieldsMap["error"] = err.Error()
	}
	l.log("ERROR", message, fieldsMap)
}

// Warn logs a warning level message
func (l *Logger) Warn(message string, fields ...map[string]interface{}) {
	l.log("WARN", message, fields...)
}

// Debug logs a debug level message
func (l *Logger) Debug(message string, fields ...map[string]interface{}) {
	l.log("DEBUG", message, fields...)
}

func (l *Logger) log(level, message string, fields ...map[string]interface{}) {
	entry := LogEntry{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		Level:         level,
		Service:       l.service,
		CorrelationID: l.correlationID,
		Message:       message,
		Fields:        mergeFields(l.defaultFields, mergeFields(fields...)),
	}

	// Promote specific standard fields to top-level if present in Fields
	if val, ok := entry.Fields["stage"]; ok {
		entry.Stage = fmt.Sprint(val)
		delete(entry.Fields, "stage")
	}
	if val, ok := entry.Fields["event_id"]; ok {
		entry.EventID = fmt.Sprint(val)
		delete(entry.Fields, "event_id")
	}
	if val, ok := entry.Fields["payload_mode"]; ok {
		entry.PayloadMode = fmt.Sprint(val)
		delete(entry.Fields, "payload_mode")
	}
	if val, ok := entry.Fields["status"]; ok {
		entry.Status = fmt.Sprint(val)
		delete(entry.Fields, "status")
	}
	if val, ok := entry.Fields["latency_ms"]; ok {
		// Handle float and int types for latency
		switch v := val.(type) {
		case float64:
			entry.LatencyMs = v
		case int:
			entry.LatencyMs = float64(v)
		case int64:
			entry.LatencyMs = float64(v)
		}
		delete(entry.Fields, "latency_ms")
	}
	if val, ok := entry.Fields["error_code"]; ok {
		entry.ErrorCode = fmt.Sprint(val)
		delete(entry.Fields, "error_code")
	}

	bytes, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, `{"level":"ERROR","message":"Failed to marshal log entry: %v"}`+"\n", err)
		return
	}
	fmt.Println(string(bytes))
}

func mergeFields(fields ...map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})
	for _, f := range fields {
		for k, v := range f {
			result[k] = v
		}
	}
	return result
}
