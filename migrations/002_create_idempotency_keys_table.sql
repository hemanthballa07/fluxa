-- Create idempotency_keys table
CREATE TABLE IF NOT EXISTS idempotency_keys (
    event_id VARCHAR(255) PRIMARY KEY,
    status VARCHAR(20) NOT NULL CHECK (status IN ('processing', 'success', 'failed')),
    first_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
    attempts INTEGER NOT NULL DEFAULT 1,
    error_reason TEXT
);

-- Create index on status for monitoring queries
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_status ON idempotency_keys(status);
CREATE INDEX IF NOT EXISTS idx_idempotency_keys_last_seen_at ON idempotency_keys(last_seen_at);

-- Add comment
COMMENT ON TABLE idempotency_keys IS 'Tracks event processing status for idempotency';
COMMENT ON COLUMN idempotency_keys.status IS 'processing, success, or failed';
COMMENT ON COLUMN idempotency_keys.attempts IS 'Number of processing attempts';
COMMENT ON COLUMN idempotency_keys.error_reason IS 'Error reason if status is failed';


