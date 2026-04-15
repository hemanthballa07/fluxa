-- Migration 003: Create fraud_flags table
-- Stores results of fraud rule evaluation for each flagged event.

CREATE TABLE IF NOT EXISTS fraud_flags (
    flag_id    VARCHAR(255) PRIMARY KEY,
    event_id   VARCHAR(255) NOT NULL REFERENCES events(event_id),
    user_id    VARCHAR(255) NOT NULL,
    rule_name  VARCHAR(100) NOT NULL,
    rule_value TEXT         NOT NULL,
    flagged_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- For Grafana time-series: fraud rate over time
CREATE INDEX IF NOT EXISTS idx_fraud_flags_flagged_at
    ON fraud_flags(flagged_at DESC);

-- For rule breakdown panel
CREATE INDEX IF NOT EXISTS idx_fraud_flags_rule_name
    ON fraud_flags(rule_name);

-- For per-user queries and "top fraudulent users" panel
CREATE INDEX IF NOT EXISTS idx_fraud_flags_user_id
    ON fraud_flags(user_id);

-- For Grafana composite: user + time window queries
CREATE INDEX IF NOT EXISTS idx_fraud_flags_user_flagged
    ON fraud_flags(user_id, flagged_at DESC);

COMMENT ON TABLE fraud_flags IS 'Fraud rule evaluation results for flagged events';
COMMENT ON COLUMN fraud_flags.rule_name IS 'amount_threshold | velocity | blocked_merchant | high_risk_currency';
COMMENT ON COLUMN fraud_flags.rule_value IS 'Human-readable explanation of why the rule fired';
