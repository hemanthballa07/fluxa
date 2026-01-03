# Fluxa Runbook

## Operational Procedures

This runbook provides step-by-step procedures for common operational tasks, incident response, and troubleshooting scenarios in the Fluxa platform.

## Table of Contents

- [Common Failure Scenarios](#common-failure-scenarios)
- [Monitoring and Alarms](#monitoring-and-alarms)
- [DLQ Investigation](#dlq-investigation)
- [Lambda Failures](#lambda-failures)
- [Database Issues](#database-issues)
- [Performance Tuning](#performance-tuning)
- [Disaster Recovery](#disaster-recovery)

## Common Failure Scenarios

### Scenario 1: Messages in DLQ

**Symptoms**: CloudWatch alarm `fluxa-dlq-depth-dev` triggers

**Root Causes**:

- Permanent validation errors (invalid JSON, missing fields)
- Payload hash mismatch (data corruption)
- Database connection failures (after max retries)

**Resolution**:

1. Check DLQ messages (see [DLQ Investigation](#dlq-investigation))
2. Review CloudWatch Logs for correlation IDs
3. Fix root cause and redrive valid messages

### Scenario 2: Database Connection Failures

**Symptoms**: `processed_failure` metric increases, Lambda error logs show connection errors

**Root Causes**:

- RDS instance down or rebooting
- Security group misconfiguration
- Connection pool exhausted
- Network connectivity issues (VPC)

**Resolution**:

1. Check RDS instance status in AWS Console
2. Verify security groups allow Lambda → RDS (port 5432)
3. Check connection pool settings (max connections per Lambda)
4. Review CloudWatch logs for specific error messages

### Scenario 3: Payload Hash Mismatch

**Symptoms**: `processed_failure` with `hash_mismatch` error in logs

**Root Causes**:

- Payload corrupted in S3
- Payload modified between ingest and processing
- SHA-256 calculation error

**Resolution**:

1. Verify S3 object integrity
2. Check S3 versioning (if enabled)
3. Review ingest logs for original hash
4. Re-process message manually if payload is valid

### Scenario 4: Idempotency Violations

**Symptoms**: Duplicate events in database (same event_id, multiple rows)

**Root Causes**:

- Race condition in idempotency check (should not happen with transaction-based implementation)
- Database constraint missing

**Resolution**:

1. Verify `idempotency_keys` table has PRIMARY KEY on `event_id`
2. Check `events` table has PRIMARY KEY on `event_id`
3. Review processor logs for idempotency check failures
4. Clean up duplicate rows manually if needed

### Scenario 5: Lambda Throttling

**Symptoms**: CloudWatch alarm `fluxa-*-throttles-dev` triggers

**Root Causes**:

- Burst of traffic exceeding Lambda concurrency limits
- Reserved concurrency set too low

**Resolution**:

1. Check CloudWatch Metrics → Lambda → Throttles
2. Increase reserved concurrency if needed
3. Review SQS queue depth (messages queued)
4. Consider increasing Lambda memory (affects CPU)

## Monitoring and Alarms

### CloudWatch Alarms

Monitor the following alarms in CloudWatch:

1. **DLQ Depth Alarm**

   - **Trigger**: When DLQ has messages
   - **Action**: Investigate failed messages immediately
   - **Severity**: High

2. **Lambda Error Rate**

   - **Trigger**: Error rate > 5% over 5 minutes
   - **Action**: Check Lambda logs for error patterns
   - **Severity**: High

3. **Lambda Throttles**

   - **Trigger**: Throttle count > 0 over 5 minutes
   - **Action**: Consider increasing reserved concurrency or optimizing Lambda execution time
   - **Severity**: Medium

4. **API Gateway 5xx Errors**
   - **Trigger**: 5xx error rate > 1% over 5 minutes
   - **Action**: Check API Gateway logs and downstream Lambda errors
   - **Severity**: High

### Key Metrics to Monitor

- **Ingest Lambda**:

  - `ingest_success` / `ingest_failure`: Success/failure rates
  - `s3_puts`: Number of large payloads stored in S3
  - `sqs_sent`: Messages successfully enqueued
  - Duration and Memory usage

- **Processor Lambda**:

  - `processed_success` / `processed_failure`: Success/failure rates
  - `db_latency_ms`: Database operation latency
  - Duration, Memory usage, and Concurrency

- **Query Lambda**:

  - Duration and Error rate
  - API Gateway latency

- **SQS Queue**:
  - Approximate number of messages visible
  - Approximate number of messages in flight
  - DLQ depth

## DLQ Investigation

### When Messages Appear in DLQ

1. **Check DLQ Message Count**:

   ```bash
   aws sqs get-queue-attributes \
     --queue-url <DLQ_URL> \
     --attribute-names ApproximateNumberOfMessages
   ```

2. **Receive and Inspect Messages**:

   ```bash
   aws sqs receive-message \
     --queue-url <DLQ_URL> \
     --max-number-of-messages 10 \
     --message-attribute-names All
   ```

3. **Identify Failure Pattern**:

   - Check message body for `event_id` and `correlation_id`
   - Review CloudWatch Logs for that `correlation_id`
   - Check idempotency table for processing attempts:
     ```sql
     SELECT * FROM idempotency_keys WHERE event_id = '<event_id>';
     ```

4. **Common Failure Causes**:

   - **Validation Errors**: Invalid event schema (permanent failure)
   - **Database Connection**: Transient network issues (should retry)
   - **Large Payload S3 Access**: Missing permissions or S3 key not found
   - **Idempotency Conflicts**: Concurrent processing race condition (should be handled)

5. **Remediation Steps**:
   - **For Transient Errors**: Redrive messages back to main queue:
     ```bash
     aws sqs start-message-move-task \
       --source-arn <DLQ_ARN> \
       --destination-arn <MAIN_QUEUE_ARN> \
       --max-number-of-messages-per-second 10
     ```
   - **For Permanent Errors**:
     - Fix the root cause (schema validation, data format)
     - Manually process valid messages or archive invalid ones
     - Update validation logic if needed

### Manual Message Redrive

```bash
# Receive messages from DLQ
MESSAGES=$(aws sqs receive-message --queue-url $DLQ_URL --max-number-of-messages 10)

# For each message, send to main queue
# (Script this or use AWS Console SQS redrive feature)
```

## Lambda Failures

### Ingest Lambda Failures

**Symptoms**: API returns 500 errors, `ingest_failure` metric increases

**Investigation**:

1. Check CloudWatch Logs for Ingest Lambda:
   ```bash
   aws logs tail /aws/lambda/fluxa-ingest --follow
   ```
2. Look for correlation IDs in logs
3. Common issues:
   - SQS send permission errors
   - S3 put permission errors (for large payloads)
   - Invalid request format
   - Queue is full/throttled

**Resolution**:

- Check IAM role permissions
- Verify queue exists and is accessible
- Review request format and schema validation
- Check SQS queue visibility timeout and DLQ configuration

### Processor Lambda Failures

**Symptoms**: Messages stuck in SQS, `processed_failure` metric increases

**Investigation**:

1. Check CloudWatch Logs for Processor Lambda:
   ```bash
   aws logs tail /aws/lambda/fluxa-processor --follow
   ```
2. Query idempotency table for failed events:
   ```sql
   SELECT event_id, status, attempts, error_reason, last_seen_at
   FROM idempotency_keys
   WHERE status = 'failed'
   ORDER BY last_seen_at DESC
   LIMIT 100;
   ```
3. Common issues:
   - Database connection failures
   - S3 access errors (for large payloads)
   - Idempotency key conflicts
   - Payload hash mismatch

**Resolution**:

- Verify RDS connectivity and credentials
- Check database connection pool settings
- Review S3 bucket permissions and object existence
- Inspect payload hash calculation logic
- Check for database deadlocks or lock timeouts

### Query Lambda Failures

**Symptoms**: GET /events/{id} returns 500 errors

**Investigation**:

1. Check CloudWatch Logs for Query Lambda
2. Verify RDS connectivity
3. Check query performance (slow queries)

**Resolution**:

- Ensure RDS is accessible
- Add database index on `event_id` if missing
- Optimize query if performance is poor

## Database Issues

### Connection Pool Exhaustion

**Symptoms**: Lambda timeouts, "too many connections" errors

**Resolution**:

1. Check connection pool size in Lambda code
2. Monitor RDS connection count:
   ```sql
   SELECT count(*) FROM pg_stat_activity;
   ```
3. Reduce connection pool size or increase RDS `max_connections`
4. Implement connection retry with exponential backoff

### Slow Queries

**Symptoms**: High `db_latency_ms` metric, Lambda timeouts

**Resolution**:

1. Enable PostgreSQL slow query log
2. Analyze query plans:
   ```sql
   EXPLAIN ANALYZE SELECT * FROM events WHERE event_id = '<id>';
   ```
3. Add indexes as needed:
   ```sql
   CREATE INDEX IF NOT EXISTS idx_events_event_id ON events(event_id);
   CREATE INDEX IF NOT EXISTS idx_events_user_id ON events(user_id);
   CREATE INDEX IF NOT EXISTS idx_events_ts ON events(ts);
   ```

### Database Failover

**Symptoms**: Connection errors, RDS status changes

**Resolution**:

1. Check RDS event history in AWS Console
2. Verify Multi-AZ configuration for automatic failover
3. Update Lambda environment variables if endpoint changes (should be automatic with RDS endpoint DNS)
4. Monitor application recovery after failover

## Performance Tuning

### Lambda Optimization

1. **Memory Allocation**:

   - Profile Lambda memory usage
   - Adjust memory (affects CPU proportionally)
   - Test optimal memory size for cost/performance

2. **Concurrency**:

   - Monitor Lambda concurrency metrics
   - Set reserved concurrency if needed to avoid throttling
   - Use provisioned concurrency for consistent low-latency (cost consideration)

3. **Cold Starts**:
   - Minimize initialization code
   - Use Lambda SnapStart for Java (if applicable)
   - Consider provisioned concurrency for critical paths

### SQS Optimization

1. **Batch Size**:

   - Adjust Processor Lambda batch size (up to 10)
   - Balance between throughput and error handling granularity

2. **Visibility Timeout**:

   - Set visibility timeout to 6x Lambda timeout (processor)
   - Prevents message becoming visible during processing

3. **Message Retention**:
   - Default 4 days is usually sufficient
   - Increase if processing delays are expected

### Database Optimization

1. **Connection Pooling**:

   - Use appropriate pool size (10-20 connections per Lambda)
   - Monitor connection usage

2. **Indexes**:

   - Ensure indexes on frequently queried columns
   - Monitor index usage and remove unused indexes

3. **Vacuum and Analyze**:
   - Schedule regular VACUUM and ANALYZE operations
   - Monitor table bloat

## Disaster Recovery

### Backup and Restore

1. **RDS Automated Backups**:

   - Verify automated backups are enabled (7-day retention default)
   - Test restore procedure in non-production environment

2. **Point-in-Time Recovery**:

   ```bash
   aws rds restore-db-instance-to-point-in-time \
     --source-db-instance-identifier fluxa-prod \
     --target-db-instance-identifier fluxa-prod-restored \
     --restore-time 2024-01-01T12:00:00Z
   ```

3. **S3 Payload Backup**:
   - Enable S3 versioning for payload bucket
   - Consider cross-region replication for critical data

### Incident Response Playbook

1. **Severity 1 (Service Down)**:

   - Check CloudWatch alarms
   - Review recent deployments
   - Check AWS service health dashboard
   - Rollback deployment if needed
   - Escalate to on-call engineer

2. **Severity 2 (Degraded Performance)**:

   - Monitor metrics and logs
   - Check for throttling or resource limits
   - Review recent changes
   - Consider scaling adjustments

3. **Severity 3 (DLQ Messages)**:
   - Investigate DLQ messages
   - Fix root cause
   - Redrive valid messages
   - Update documentation

### Contact Information

- **On-Call**: [Configure PagerDuty/OpsGenie]
- **Team Slack**: #fluxa-alerts
- **AWS Support**: Enterprise Support case if needed
