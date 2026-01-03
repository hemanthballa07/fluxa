# Fluxa Operational Runbook

This runbook provides actionable steps for troubleshooting and operating the Fluxa system.

## 1. System Overview

- **Services**: `ingest` (API -> SQS), `processor` (SQS -> DB/S3), `query` (API -> DB).
- **Core Dependencies**: PostgreSQL (`events`, `idempotency_keys`), S3 (`fluxa-payloads`), SQS.

## 2. Troubleshooting Guide

### 2.1 DLQ Triage (Dead Letter Queue)

When the `processor` fails to process a message safely, it (configured via AWS Redrive Policy) moves to the DLQ.

**Diagnosis:**
1. Check CloudWatch Alarms for `ApproximateNumberOfMessagesVisible` > 0 on `Fluxa-DLQ`.
2. Inspect the messages:
   ```bash
   aws sqs receive-message \
     --queue-url https://sqs.us-east-1.amazonaws.com/123456789012/Fluxa-DLQ \
     --max-number-of-messages 1 \
     --attribute-names All
   ```
3. Identify the failure reason. Common causes:
   - `HashMismatch`: Payload hash check failed. (Action: Investigate producer/tampering. **Do NOT retry**).
   - `DBConnectionError`: Transient DB issue. (Action: Safe to retry).
   - `SchemaValidation`: Invalid JSON structure. (Action: Fix producer).

### 2.2 Replay Procedure (Safe Retries)

Fluxa supports **idempotent** replays. It is safe to re-drive messages from DLQ back to the Source Queue unless they are fundamentally invalid (Hash/Schema errors).

**Command**:
```bash
aws sqs start-message-move-task \
  --source-arn arn:aws:sqs:us-east-1:123456789012:Fluxa-DLQ \
  --destination-arn arn:aws:sqs:us-east-1:123456789012:Fluxa-InputQueue
```

**Verification**:
 Check `Fluxa-InputQueue` metrics for `NumberOfMessagesReceived` and `Fluxa/Processor` logs for "Successfully processed event".

### 2.3 Common Alarms & Actions

| Alarm | Likely Cause | First Action |
|-------|--------------|--------------|
| `ProcessorErrorRate > 1%` | Bad deployment or DB connectivity | Check Logs: `fields.error_code`. Rollback if deployment related. |
| `DLQDepth > 0` | Poison pill message | Run generic DLQ Triage (Sec 2.1). |
| `APIGateway5xx > 1%` | Ingest/Query Lambda timeout or crash | Check `ingest` or `query` logs for panics or timeouts. |
| `DBHighCPU > 80%` | Inefficient Query or Load Spike | Check `pg_stat_activity` for stuck queries. |

### 2.4 Logging & Tracing

All services output structured JSON logs. Locate a trace using `correlation_id` or `event_id`.

**Log Fields**:
- `service`: `ingest`, `processor`, `query`
- `correlation_id`: Unique trace ID (propagates end-to-end)
- `stage`: `validate` -> `enqueue` -> `process` -> `persist`
- `status`: `success`, `failure`
- `error_code`: Machine-readable error type

**CloudWatch Logs Insights Query**:
```sql
fields @timestamp, service, stage, status, error_code, latency_ms
| filter correlation_id = "YOUR_CORRELATION_ID"
| sort @timestamp desc
```
