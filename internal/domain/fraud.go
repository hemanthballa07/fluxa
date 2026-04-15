package domain

import "time"

// FraudFlag is persisted to the fraud_flags table when a rule fires.
type FraudFlag struct {
	FlagID    string    // UUID primary key
	EventID   string    // FK → events.event_id
	UserID    string
	RuleName  string    // "amount_threshold" | "velocity" | "blocked_merchant" | "high_risk_currency"
	RuleValue string    // human-readable: e.g. "amount=15000.00 > threshold=10000.00"
	FlaggedAt time.Time
}

// AlertMessage is published to the RabbitMQ alerts exchange when a fraud flag is created.
type AlertMessage struct {
	FlagID    string    `json:"flag_id"`
	EventID   string    `json:"event_id"`
	UserID    string    `json:"user_id"`
	RuleName  string    `json:"rule_name"`
	RuleValue string    `json:"rule_value"`
	FlaggedAt time.Time `json:"flagged_at"`
}
