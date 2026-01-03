-- Create events table
CREATE TABLE IF NOT EXISTS events (
    event_id VARCHAR(36) PRIMARY KEY,
    correlation_id VARCHAR(36) NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    amount DECIMAL(18, 2) NOT NULL,
    currency VARCHAR(3) NOT NULL,
    merchant VARCHAR(255) NOT NULL,
    ts TIMESTAMP WITH TIME ZONE NOT NULL,
    metadata_json JSONB,
    payload_mode VARCHAR(10) NOT NULL CHECK (payload_mode IN ('INLINE', 'S3')),
    s3_key VARCHAR(500),
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for common queries
CREATE INDEX IF NOT EXISTS idx_events_correlation_id ON events(correlation_id);
CREATE INDEX IF NOT EXISTS idx_events_user_id ON events(user_id);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
CREATE INDEX IF NOT EXISTS idx_events_created_at ON events(created_at);

-- Add comment
COMMENT ON TABLE events IS 'Stores processed transaction events';
COMMENT ON COLUMN events.payload_mode IS 'INLINE for payloads <= 256KB, S3 for larger payloads';
COMMENT ON COLUMN events.s3_key IS 'S3 key for payload if payload_mode is S3';


