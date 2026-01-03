# Metrics Capture Guide

This document provides step-by-step instructions for capturing real metrics from Fluxa deployments to populate resume bullets with defensible numbers.

## Prerequisites

- Fluxa deployed to AWS (dev or prod environment)
- AWS CLI configured with appropriate credentials
- Access to AWS Console (CloudWatch)
- `jq` (optional, for JSON parsing)

## Metric Names

Fluxa emits the following custom metrics via CloudWatch Embedded Metric Format (EMF):

### Ingest Lambda Metrics (Namespace: `Fluxa/Ingest`)

- `ingest_latency_ms` - Latency in milliseconds for ingest operations
- `ingest_success` - Count of successful ingestions
- `payload_inline_count` - Count of payloads stored inline (≤256KB)
- `payload_s3_count` - Count of payloads stored in S3 (>256KB)

### Processor Lambda Metrics (Namespace: `Fluxa/Processor`)

- `process_latency_ms` - Latency in milliseconds for processing operations
- `processed_success` - Count of successfully processed events
- `db_latency_ms` - Database operation latency

## Running Load Tests

### Step 1: Run Load Test

```bash
# Set API endpoint (or let script read from Terraform)
export API_ENDPOINT=https://your-api-gateway-url.execute-api.us-east-1.amazonaws.com/dev

# Run load test with defaults (1000 events, concurrency 20)
./scripts/load_test.sh

# Or customize
NUM_EVENTS=2000 CONCURRENCY=50 ./scripts/load_test.sh
```

The script will:

- Send N events to the API endpoint
- Save all event IDs to `tmp/load_test_event_ids_TIMESTAMP.txt`
- Print summary statistics (events/sec, events/min)
- Save detailed stats to `tmp/load_test_stats_TIMESTAMP.txt`

### Step 2: Wait for Processing

Wait 2-5 minutes after load test completes for all events to be processed by the processor Lambda. This ensures metrics are available in CloudWatch.

## Capturing Metrics

### Method 1: AWS Console (Recommended for Screenshots)

#### Capture ingest_p95_ms (Ingest Latency p95)

1. Open AWS Console → CloudWatch → Metrics
2. Navigate to: **Custom Namespaces** → **Fluxa/Ingest**
3. Select metric: `ingest_latency_ms`
4. Set time range: **Last 1 hour** (or your load test window)
5. Set statistic: **p95** (95th percentile)
6. **Screenshot**: Take a screenshot showing the p95 value
7. **Record value**: Note the p95 value in milliseconds

**Alternative**: Use period-based aggregation:

- Period: 1 minute
- Statistic: p95
- View as "Number" to see exact value

#### Capture process_p95_ms (Processor Latency p95)

1. Navigate to: **Custom Namespaces** → **Fluxa/Processor**
2. Select metric: `process_latency_ms`
3. Set time range: **Last 1 hour**
4. Set statistic: **p95**
5. **Screenshot**: Take a screenshot showing the p95 value
6. **Record value**: Note the p95 value in milliseconds

#### Capture throughput_events_per_min

**Option A: From Load Test Script Output**

The load test script prints `events/min` directly in its output. Record this value.

**Option B: From CloudWatch Metrics**

1. Navigate to: **Custom Namespaces** → **Fluxa/Ingest**
2. Select metric: `ingest_success`
3. Set time range: **Last 1 hour**
4. Set statistic: **Sum**
5. Set period: **1 minute**
6. **Calculate**: Sum of all 1-minute periods, then divide by number of minutes
7. **Record value**: Events per minute

Example: If sum is 1000 events over 10 minutes = 100 events/min

### Method 2: AWS CLI

#### Step 1: List Available Metrics (Discover Dimensions)

First, list available metrics to discover if dimensions are required:

```bash
REGION=us-east-1  # Change to your AWS region

# List Ingest metrics
aws cloudwatch list-metrics \
  --namespace Fluxa/Ingest \
  --metric-name ingest_latency_ms \
  --region "$REGION"

# List Processor metrics
aws cloudwatch list-metrics \
  --namespace Fluxa/Processor \
  --metric-name process_latency_ms \
  --region "$REGION"
```

If dimensions are listed, include them in the get-metric-statistics command. If no dimensions are shown, omit the `--dimensions` parameter.

#### Step 2: Capture ingest_p95_ms

```bash
# Set environment variables
REGION=us-east-1  # Change to your AWS region

# Calculate time range (1 hour ago to now)
# macOS:
START_TIME=$(date -u -v-1H +%Y-%m-%dT%H:%M:%S)
END_TIME=$(date -u +%Y-%m-%dT%H:%M:%S)

# Linux:
# START_TIME=$(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%S)
# END_TIME=$(date -u +%Y-%m-%dT%H:%M:%S)

# Get p95 latency (using extended-statistics, NOT statistics)
aws cloudwatch get-metric-statistics \
  --namespace Fluxa/Ingest \
  --metric-name ingest_latency_ms \
  --start-time "${START_TIME}" \
  --end-time "${END_TIME}" \
  --period 300 \
  --extended-statistics p95 \
  --region "$REGION" \
  | jq '.Datapoints | map(.ExtendedStatistics.p95) | sort | .[-1]'

# If you need to include dimensions (from Step 1), add:
# --dimensions Name=DimensionName,Value=DimensionValue
```

#### Step 3: Capture process_p95_ms

```bash
aws cloudwatch get-metric-statistics \
  --namespace Fluxa/Processor \
  --metric-name process_latency_ms \
  --start-time "${START_TIME}" \
  --end-time "${END_TIME}" \
  --period 300 \
  --extended-statistics p95 \
  --region "$REGION" \
  | jq '.Datapoints | map(.ExtendedStatistics.p95) | sort | .[-1]'
```

**Important Notes**:

- Use `--extended-statistics p95` (NOT `--statistics p95`) for percentile statistics
- Extract from `.ExtendedStatistics.p95` (NOT `.p95`) in jq
- If metrics have dimensions, include them from the `list-metrics` output
- Period 300 (5 minutes) provides good granularity for percentile stats

### Capture terraform_apply_minutes

Time the `terraform apply` command:

```bash
cd infra/terraform/envs/dev

# Time the apply operation
time terraform apply

# Or more precise timing:
START=$(date +%s)
terraform apply
END=$(date +%s)
MINUTES=$(( (END - START) / 60 ))
echo "Terraform apply took ${MINUTES} minutes"
```

**Record**: Total minutes for `terraform apply` to complete.

### Capture end_to_end_async_p95_ms (Optional)

This measures the time from event ingestion to event being queryable.

1. Use event IDs from load test: `tmp/load_test_event_ids_TIMESTAMP.txt`
2. For a sample of events (e.g., 100), record:
   - Ingest timestamp (from load test script start time + offset)
   - Query timestamp (when GET /events/{id} first returns 200)
3. Calculate latency for each event
4. Compute p95 of latencies

**Script approach** (create custom script):

```bash
# Pseudo-code
for event_id in $(head -100 tmp/load_test_event_ids_*.txt); do
  ingest_time=$(grep "$event_id" load_test_log.txt | extract_timestamp)
  query_time=$(poll_until_found "$event_id" | extract_timestamp)
  latency=$((query_time - ingest_time))
  echo "$latency"
done | sort -n | tail -n 5% | head -n 1  # p95
```

This is more complex and optional for resume bullets.

## Computing Throughput

### From Load Test Script

The script prints:

```
Throughput: X events/sec
Throughput: Y events/min
```

**Use the events/min value** for `throughput_events_per_min`.

### From CloudWatch

1. Navigate to: **Custom Namespaces** → **Fluxa/Ingest**
2. Select metric: `ingest_success`
3. Time range: **Last 1 hour** (or your load test window)
4. Period: **1 minute**
5. Statistic: **Sum**
6. View graph: Each bar represents events in that 1-minute period
7. **Average**: Sum all bars, divide by number of minutes
8. **Peak**: Maximum value across all bars

## Resume Bullets Template

After capturing all metrics, replace placeholders in these bullets:

### Bullet 1: Latency Metrics

```
Built serverless event pipeline (API Gateway → Lambda → SQS → Lambda → RDS) with p95 latency of [ingest_p95_ms]ms for ingestion and [process_p95_ms]ms for processing, handling [throughput_events_per_min] events/min at peak load.
```

### Bullet 2: Infrastructure & Reliability

```
Implemented idempotency guarantees and payload integrity verification (SHA-256) for a serverless event-driven platform, deploying infrastructure via Terraform in [terraform_apply_minutes] minutes with zero-downtime updates and automatic DLQ handling for permanent failures.
```

### Bullet 3: Observability & Scale

```
Instrumented CloudWatch metrics (EMF) and alarms for [throughput_events_per_min] events/min throughput, achieving [ingest_p95_ms]ms p95 ingestion latency with intelligent payload handling (inline ≤256KB, S3 for larger payloads) optimizing cost and latency.
```

### Bullet 4: Production Engineering

```
Designed and deployed production-grade serverless architecture on AWS with idempotency, DLQ retries, and comprehensive observability, processing [throughput_events_per_min] events/min with [process_p95_ms]ms p95 processing latency and [ingest_p95_ms]ms p95 ingestion latency.
```

## Example with Real Numbers (Fill In)

After capturing metrics, your bullets might look like:

```
Built serverless event pipeline (API Gateway → Lambda → SQS → Lambda → RDS) with p95 latency of 245ms for ingestion and 890ms for processing, handling 120 events/min at peak load.

Implemented idempotency guarantees and payload integrity verification (SHA-256) for a serverless event-driven platform, deploying infrastructure via Terraform in 8 minutes with zero-downtime updates and automatic DLQ handling for permanent failures.

Instrumented CloudWatch metrics (EMF) and alarms for 120 events/min throughput, achieving 245ms p95 ingestion latency with intelligent payload handling (inline ≤256KB, S3 for larger payloads) optimizing cost and latency.

Designed and deployed production-grade serverless architecture on AWS with idempotency, DLQ retries, and comprehensive observability, processing 120 events/min with 890ms p95 processing latency and 245ms p95 ingestion latency.
```

## Validation Checklist

Before using metrics in resume:

- [ ] Metrics captured during load test (not idle system)
- [ ] Time range matches load test window
- [ ] p95 values are from consistent time period
- [ ] Throughput matches load test script output
- [ ] Terraform apply time recorded during actual deployment
- [ ] Screenshots saved for verification (optional but recommended)
- [ ] All numbers are defensible if asked in interviews

## Troubleshooting

### Metrics Not Appearing in CloudWatch

1. Wait 2-5 minutes after load test (CloudWatch metrics can take time to appear)
2. Verify Lambda functions are executing (check CloudWatch Logs)
3. Check metric namespace matches exactly: `Fluxa/Ingest` or `Fluxa/Processor`
4. Verify time range includes your load test window

### Missing p95 Statistic

1. Ensure sufficient data points (need multiple invocations)
2. Try shorter time window (e.g., 15 minutes) if p95 unavailable
3. Use average or max as fallback if p95 not available

### Inconsistent Throughput

1. Ensure load test completed fully
2. Check for throttling (Lambda or API Gateway)
3. Verify all events were successful (check load test stats file)
