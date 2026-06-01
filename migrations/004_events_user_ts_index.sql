-- 004_events_user_ts_index.sql
-- Supports ML feature point-in-time aggregates: same-user events as-of a ts
-- (internal/mlfeatures CountUserEventsAsOf / UserAmountStatsAsOf).
CREATE INDEX IF NOT EXISTS idx_events_user_ts ON events (user_id, ts);
