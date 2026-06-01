package domain

import "time"

// FraudFlag is persisted to the fraud_flags table when a rule fires.
type FraudFlag struct {
	FlagID    string // UUID primary key
	EventID   string // FK → events.event_id
	UserID    string
	RuleName  string  // "amount_threshold" | "velocity" | "blocked_merchant" | "high_risk_currency" | "ml_risk"
	RuleValue string  // human-readable: e.g. "amount=15000.00 > threshold=10000.00"
	MlScore   float64 // blended ML fraud probability for the event (0 when scorer unavailable)
	FlaggedAt time.Time
}

// AlertMessage is published to the RabbitMQ alerts exchange when a fraud flag is created.
type AlertMessage struct {
	FlagID    string    `json:"flag_id"`
	EventID   string    `json:"event_id"`
	UserID    string    `json:"user_id"`
	RuleName  string    `json:"rule_name"`
	RuleValue string    `json:"rule_value"`
	MlScore   float64   `json:"ml_score"`
	FlaggedAt time.Time `json:"flagged_at"`
}

// FraudEvent is a joined view of fraud_flags + events, used by the SSE stream.
type FraudEvent struct {
	FlagID        string    `json:"flag_id"`
	EventID       string    `json:"event_id"`
	CorrelationID string    `json:"correlation_id"`
	UserID        string    `json:"user_id"`
	Amount        float64   `json:"amount"`
	Currency      string    `json:"currency"`
	Merchant      string    `json:"merchant"`
	RuleName      string    `json:"rule_name"`
	RuleValue     string    `json:"rule_value"`
	MlScore       float64   `json:"ml_score"`
	FlaggedAt     time.Time `json:"flagged_at"`
}
