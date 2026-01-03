package logging

import (
	"encoding/json"
	"os"
	"time"
)

// Logger provides structured JSON logging
type Logger struct {
	correlationID string
}

// LogEntry represents a structured log entry
type LogEntry struct {
	Timestamp     string                 `json:"timestamp"`
	Level         string                 `json:"level"`
	CorrelationID string                 `json:"correlation_id,omitempty"`
	Message       string                 `json:"message"`
	Fields        map[string]interface{} `json:"fields,omitempty"`
}

// NewLogger creates a new logger with optional correlation ID
func NewLogger(correlationID string) *Logger {
	return &Logger{
		correlationID: correlationID,
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
		CorrelationID: l.correlationID,
		Message:       message,
		Fields:        mergeFields(fields...),
	}

	bytes, err := json.Marshal(entry)
	if err != nil {
		// Fallback to simple log if JSON marshaling fails
		os.Stdout.WriteString(level + ": " + message + "\n")
		return
	}

	os.Stdout.Write(append(bytes, '\n'))
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

