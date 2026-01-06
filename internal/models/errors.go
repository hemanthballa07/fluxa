package models

import "fmt"

// Error types for explicit failure handling (Poison Message Strategy)

// NonRetryableError indicates a failure that will not be fixed by retrying
// (e.g. Validation errors, Hash mismatches, Business rule violations).
// These should be ACKed and sent to DLQ/Failed Status.
type NonRetryableError struct {
	Reason string
	Err    error
}

func (e *NonRetryableError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("non-retryable: %s: %v", e.Reason, e.Err)
	}
	return fmt.Sprintf("non-retryable: %s", e.Reason)
}

func (e *NonRetryableError) Unwrap() error {
	return e.Err
}

// NewNonRetryableError creates a new NonRetryableError
func NewNonRetryableError(reason string, err error) error {
	return &NonRetryableError{Reason: reason, Err: err}
}

// RetryableError indicates a transient failure (e.g. DB Timeout, Network).
// These should be NACKed (returning error to SQS) to trigger retry policies.
type RetryableError struct {
	Reason string
	Err    error
}

func (e *RetryableError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("retryable: %s: %v", e.Reason, e.Err)
	}
	return fmt.Sprintf("retryable: %s", e.Reason)
}

func (e *RetryableError) Unwrap() error {
	return e.Err
}

func NewRetryableError(reason string, err error) error {
	return &RetryableError{Reason: reason, Err: err}
}
