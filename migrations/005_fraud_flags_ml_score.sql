-- 005_fraud_flags_ml_score.sql
-- Per-flag ML risk score (denormalized from the event's blended eval) so the SSE
-- fraud feed can render it without a second lookup. 0 when the scorer was unavailable.
ALTER TABLE fraud_flags ADD COLUMN IF NOT EXISTS ml_score DOUBLE PRECISION NOT NULL DEFAULT 0;
