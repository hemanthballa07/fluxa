# Fluxa Observability

This document describes the observability implementation in Fluxa, including metrics, logs, alarms, and tracing.

## Metrics

All metrics use CloudWatch Embedded Metric Format (EMF) and are emitted to CloudWatch Logs, where CloudWatch automatically extracts and creates metrics.

### Metric Namespaces

- `Fluxa/Ingest` - Ingest Lambda metrics
- `Fluxa/Processor` - Processor Lambda metrics
- `Fluxa/Query` - Query Lambda metrics

### Ingest Lambda Metrics

| Metric Name | Unit | Description | When Emitted |
|-------------|------|-------------|--------------|
| `ingest_success` | Count | Successful event ingestion | After successful SQS send |
| `ingest_failure` | Count | Failed event ingestion | On validation, SQS, or S3 errors |
| `s3_puts` | Count | Large payloads stored in S3 | When payload >256KB stored in S3 |
| `sqs_sent` | Count | Messages sent to SQS | After successful SQS send |

**Code Location**: `cmd/ingest/main.go:137, 148, 159`

### Processor Lambda Metrics

| Metric Name | Unit | Description | When Emitted |
|-------------|------|-------------|--------------|
| `processed_success` | Count | Successfully processed events | After event persisted and SNS published |
| `processed_failure` | Count | Failed processing attempts | On various failure scenarios (with error dimension) |
| `db_latency_ms` | Milliseconds | Database operation latency | After DB insert operation |

**Error Dimensions for `processed_failure`**:
- `parse_error` - Failed to parse SQS message
- `idempotency_error` - Idempotency check failed
- `missing_payload` - Payload missing for INLINE mode
- `missing_s3_key` - S3 key missing for S3 mode
- `s3_fetch_error` - Failed to fetch from S3
- `invalid_payload_mode` - Unknown payload mode
- `hash_mismatch` - Payload hash verification failed
- `unmarshal_error` - Failed to unmarshal event JSON
- `db_error` - Database insert failed

**Code Location**: `cmd/processor/main.go:102, 111, 128, 137, 146, 152, 162, 171, 185, 190, 228`

### Query Lambda Metrics

| Metric Name | Unit | Description | When Emitted |
|-------------|------|-------------|--------------|
| `query_success` | Count | Successful queries | After successful event retrieval |
| `query_failure` | Count | Failed queries | On database errors |
| `query_not_found` | Count | 404 responses | When event not found |

**Code Location**: `cmd/query/main.go:70, 78, 88`

## CloudWatch Alarms

All alarms are defined in `infra/terraform/modules/stateless/cloudwatch.tf`.

### DLQ Depth Alarm

- **Name**: `fluxa-dlq-depth-{environment}`
- **Metric**: `AWS/SQS.ApproximateNumberOfMessagesVisible` for DLQ
- **Threshold**: > 0 messages
- **Period**: 5 minutes
- **Action**: Investigate failed messages (see RUNBOOK.md)

**Failure Mode**: Messages in DLQ indicate permanent failures that require manual intervention.

### Lambda Error Rate Alarms

- **Ingest**: `fluxa-ingest-errors-{environment}`
- **Processor**: `fluxa-processor-errors-{environment}`
- **Query**: `fluxa-query-errors-{environment}`

**Configuration**:
- **Metric**: `AWS/Lambda.Errors`
- **Threshold**: > 5 errors over 5 minutes
- **Evaluation Periods**: 2
- **Action**: Check CloudWatch Logs for error patterns

**Failure Mode**: High error rate indicates system issues requiring investigation.

### Lambda Throttle Alarms

- **Ingest**: `fluxa-ingest-throttles-{environment}`
- **Processor**: `fluxa-processor-throttles-{environment}`

**Configuration**:
- **Metric**: `AWS/Lambda.Throttles`
- **Threshold**: > 0 throttles over 5 minutes
- **Evaluation Periods**: 1
- **Action**: Consider increasing reserved concurrency or optimizing Lambda execution time

**Failure Mode**: Throttling indicates Lambda concurrency limits being reached.

### API Gateway 5xx Errors

- **Name**: `fluxa-api-5xx-{environment}`
- **Metric**: `AWS/ApiGateway.5XXError`
- **Threshold**: > 10 errors over 5 minutes
- **Evaluation Periods**: 2
- **Action**: Check API Gateway logs and downstream Lambda errors

**Failure Mode**: API 5xx errors indicate downstream service failures (Lambda errors).

## Structured Logging

All logs are emitted in JSON format with the following structure:

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "level": "INFO",
  "correlation_id": "550e8400-e29b-41d4-a716-446655440000",
  "message": "Successfully enqueued event",
  "fields": {
    "event_id": "550e8400-e29b-41d4-a716-446655440000",
    "payload_mode": "INLINE"
  }
}
```

### Log Levels

- **INFO**: Normal operations, successful processing
- **ERROR**: Errors that are logged and handled
- **WARN**: Warnings (not currently used extensively)
- **DEBUG**: Detailed debugging information (limited use)

**Code Location**: `internal/logging/logger.go`

### Sample Log Lines

#### Ingest Lambda - Success

```json
{
  "timestamp": "2024-01-15T10:30:00Z",
  "level": "INFO",
  "correlation_id": "550e8400-e29b-41d4-a716-446655440000",
  "message": "Successfully enqueued event",
  "fields": {
    "event_id": "550e8400-e29b-41d4-a716-446655440000",
    "payload_mode": "INLINE"
  }
}
```

**Explanation**: Event successfully validated, payload hash calculated, message sent to SQS. Payload mode indicates whether payload was inlined (≤256KB) or stored in S3.

#### Processor Lambda - Idempotency Skip

```json
{
  "timestamp": "2024-01-15T10:30:05Z",
  "level": "INFO",
  "correlation_id": "550e8400-e29b-41d4-a716-446655440000",
  "message": "Event already processed, skipping",
  "fields": {
    "event_id": "550e8400-e29b-41d4-a716-446655440000"
  }
}
```

**Explanation**: Event was already successfully processed (idempotency check detected 'success' status). Lambda exits safely without duplicate processing.

#### Processor Lambda - Hash Mismatch

```json
{
  "timestamp": "2024-01-15T10:30:05Z",
  "level": "ERROR",
  "correlation_id": "550e8400-e29b-41d4-a716-446655440000",
  "message": "Payload hash mismatch",
  "fields": {
    "error": "expected abc123..., got def456..."
  }
}
```

**Explanation**: Calculated SHA-256 hash of payload doesn't match hash in message. Indicates data corruption. Event marked as failed, sent to DLQ (no retry).

## Correlation ID Tracing

Correlation IDs enable end-to-end tracing across all services:

1. **API Gateway → Ingest Lambda**: Correlation ID extracted from `X-Correlation-ID` header or generated
2. **Ingest → SQS**: Correlation ID included in SQS message attributes
3. **SQS → Processor Lambda**: Correlation ID extracted from message attributes
4. **Processor → Database**: Correlation ID stored in `events.correlation_id` column
5. **Processor → Logs**: Correlation ID included in all log entries
6. **API Gateway → Query Lambda**: Correlation ID extracted from request headers
7. **Query → Logs**: Correlation ID included in log entries

### Tracing Example

To trace an event across the system:

1. Extract `correlation_id` from API response or database
2. Search CloudWatch Logs for `correlation_id` across all Lambda log groups
3. Filter logs by correlation_id to see complete request flow

**Code Locations**:
- Ingest: `cmd/ingest/main.go:57-59, 148-151`
- Processor: `cmd/processor/main.go:85-91, 183`
- Query: `cmd/query/main.go:42-45`

## CloudWatch Log Groups

Log groups are automatically created by Lambda:

- `/aws/lambda/fluxa-ingest-{environment}`
- `/aws/lambda/fluxa-processor-{environment}`
- `/aws/lambda/fluxa-query-{environment}`

Log retention: Default (never expire) - consider setting retention policy for cost optimization.

## Metric Extraction

CloudWatch automatically extracts metrics from EMF logs. Metrics appear in CloudWatch Metrics under:

- `Fluxa/Ingest` namespace
- `Fluxa/Processor` namespace
- `Fluxa/Query` namespace

**Code Location**: `internal/metrics/metrics.go` - EmitMetric function

## Unused Metrics

All defined metrics are actively used. There are no dead observability code paths.

## Monitoring Best Practices

1. **Set up CloudWatch Dashboard**: Create dashboard with key metrics (success rates, latencies, DLQ depth)
2. **Configure SNS subscriptions for alarms**: Receive notifications when alarms trigger
3. **Use correlation IDs for debugging**: Search logs by correlation_id to trace request flow
4. **Review DLQ regularly**: Check DLQ depth metric and investigate messages
5. **Monitor Lambda concurrency**: Watch for throttling alarms

## Cost Considerations

- CloudWatch Logs: $0.50 per GB ingested, $0.03 per GB stored
- CloudWatch Metrics: First 10 custom metrics free, then $0.30 per metric per month
- Consider setting log retention policy (e.g., 30 days) to reduce storage costs


