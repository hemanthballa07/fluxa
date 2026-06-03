-- Fluxa Operational Reporting Queries
-- Use these views/queries to generate customer-facing or internal dashboards

-- 1. Events by Status (Success vs Failure Rates)
SELECT 
    status,
    COUNT(*) as total_events,
    ROUND(COUNT(*) * 100.0 / SUM(COUNT(*)) OVER (), 2) as percentage
FROM idempotency_keys
GROUP BY status;

-- 2. Failure Reasons (Pareto Analysis of DLQ Candidates)
-- Identifies top causes for non-retryable failures (Poison Messages)
SELECT 
    error_reason,
    COUNT(*) as frequency
FROM idempotency_keys
WHERE status = 'failed'
GROUP BY error_reason
ORDER BY frequency DESC;

-- 3. Ingestion Latency Stats (End-to-End Performance)
-- Measures time from 'created_at' in DB (ingest) to 'last_seen_at' (process completion)
-- Note: Requires successfully processed events
SELECT
    percentile_cont(0.50) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (last_seen_at - created_at))) as p50_latency_sec,
    percentile_cont(0.95) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (last_seen_at - created_at))) as p95_latency_sec,
    MAX(EXTRACT(EPOCH FROM (last_seen_at - created_at))) as max_latency_sec
FROM idempotency_keys
WHERE status = 'success';

-- 4. Recent Processing Activity (Last 1 Hour)
SELECT 
    date_trunc('minute', last_seen_at) as minute_bucket,
    status,
    COUNT(*) as events_processed
FROM idempotency_keys
WHERE last_seen_at > NOW() - INTERVAL '1 hour'
GROUP BY 1, 2
ORDER BY 1 DESC;
