# Fluxa System Design Interview Explanation

## 1-2 Minute Speaking Outline

### Introduction (15 seconds)
"Fluxa is an event-driven data platform I built on AWS that ingests, processes, and persists transaction events. It's designed as a serverless architecture using API Gateway, Lambda, SQS, RDS, and S3, with a focus on reliability, scalability, and observability."

### Architecture Flow (30 seconds)
"The flow starts with API Gateway receiving events. The ingest Lambda validates the schema, generates correlation IDs for tracing, and handles payload size intelligently—payloads under 256KB go inline in SQS, larger ones go to S3 with just the key in the message. Messages then flow to SQS, which triggers the processor Lambda in batches. The processor implements idempotency checks via a database table to prevent duplicates, fetches payloads from S3 if needed, persists to PostgreSQL, and publishes notifications via SNS."

### Key Design Decisions (30 seconds)
"I chose SQS over Kinesis because we needed exactly-once semantics with DLQ support, not ordered streams. PostgreSQL over DynamoDB for SQL query flexibility and relational capabilities. The payload strategy balances latency and cost—small payloads avoid S3 API calls, large ones keep SQS messages small. Idempotency is database-backed for persistence across retries."

### Reliability Features (20 seconds)
"The system includes schema validation at ingest, idempotency to prevent duplicates, exponential backoff retries via SQS, Dead Letter Queue for poison messages, correlation IDs for end-to-end tracing, and SHA-256 hash verification for payload integrity."

### Observability (15 seconds)
"All logs are structured JSON with correlation IDs. Custom CloudWatch metrics track success rates, latencies, and business metrics. Alarms monitor DLQ depth, Lambda errors, throttles, and API 5xx responses for proactive alerting."

### Tradeoffs (20 seconds)
"Serverless gives us auto-scaling and pay-per-use but has cold starts—mitigated with provisioned concurrency if needed. RDS requires connection pooling and has operational overhead but provides SQL flexibility. The dual payload strategy adds complexity but optimizes for both cost and latency."

### Failure Modes and Mitigations (20 seconds)
"Transient database failures trigger SQS retries. Permanent validation errors go to DLQ for manual review. Database connection exhaustion is prevented with connection pooling. S3 failures for large payloads are retried. Lambda throttling is handled by SQS queuing, with alarms to scale up if needed."

## Key Tradeoffs to Discuss

### 1. Serverless vs. Containers
- **Chosen**: Serverless (Lambda)
- **Why**: Auto-scaling, pay-per-use, built-in AWS integrations
- **Tradeoff**: Cold starts (can add provisioned concurrency), 15-minute execution limit

### 2. SQS vs. Kinesis
- **Chosen**: SQS
- **Why**: Exactly-once delivery, DLQ support, simpler for our use case
- **Tradeoff**: No strict ordering (not needed), 256KB message limit (mitigated with S3)

### 3. PostgreSQL vs. DynamoDB
- **Chosen**: PostgreSQL (RDS)
- **Why**: SQL queries, relational capabilities, ACID transactions
- **Tradeoff**: Connection pooling needed, operational overhead, cost at scale

### 4. Inline vs. S3 Payload Strategy
- **Chosen**: Hybrid (≤256KB inline, >256KB S3)
- **Why**: Optimize for both cost (avoid S3 API calls) and SQS message size
- **Tradeoff**: Code complexity with two paths, but clear benefit

### 5. Idempotency Implementation
- **Chosen**: Database table
- **Why**: Persistent across Lambda invocations, handles concurrent attempts
- **Tradeoff**: Additional database write, but necessary for correctness

### 6. API Gateway REST vs. HTTP API
- **Chosen**: REST API
- **Why**: More features (request validation, API keys, usage plans)
- **Tradeoff**: Slightly higher cost, more configuration

## Failure Modes and Handling

### 1. API Gateway Failures
- **Impact**: Users cannot submit events
- **Mitigation**: AWS SLA guarantees, multi-region deployment possible
- **Monitoring**: CloudWatch alarm on 5xx errors

### 2. Ingest Lambda Failures
- **Impact**: Events not enqueued
- **Mitigation**: Returns 500 to client (client should retry), CloudWatch logs with correlation IDs
- **Monitoring**: Lambda error rate alarm

### 3. SQS Queue Full/Throttled
- **Impact**: Events cannot be enqueued
- **Mitigation**: SQS auto-scales, Lambda throttles handled by queuing
- **Monitoring**: Lambda throttle alarm, SQS queue depth metrics

### 4. Processor Lambda Failures
- **Impact**: Events not processed
- **Mitigation**: SQS retries with exponential backoff, DLQ after max receives
- **Monitoring**: Lambda error rate alarm, DLQ depth alarm

### 5. Database Connection Exhaustion
- **Impact**: Processor Lambda cannot persist events
- **Mitigation**: Connection pooling (10 connections per Lambda), connection retry logic
- **Monitoring**: Database connection metrics, Lambda error logs

### 6. Database Failures/Outages
- **Impact**: Events cannot be persisted
- **Mitigation**: Multi-AZ RDS in production, automatic failover, SQS retains messages during outage
- **Monitoring**: RDS instance metrics, CloudWatch alarms

### 7. S3 Failures (for large payloads)
- **Impact**: Large payloads cannot be retrieved
- **Mitigation**: S3 retries in Lambda, error logged, message goes to DLQ after retries
- **Monitoring**: Lambda error logs, DLQ depth alarm

### 8. Idempotency Key Conflicts
- **Impact**: Potential duplicate processing
- **Mitigation**: Database-level unique constraint, idempotency check before processing
- **Monitoring**: Idempotency table query logs

### 9. Payload Hash Mismatch
- **Impact**: Data integrity concerns
- **Mitigation**: SHA-256 verification, failed events go to DLQ
- **Monitoring**: Lambda error logs with hash mismatch details

### 10. Correlation ID Loss
- **Impact**: Difficult to trace events end-to-end
- **Mitigation**: Correlation IDs in SQS message attributes (guaranteed delivery), logged in all steps
- **Monitoring**: Log analysis for missing correlation IDs

## Scalability Considerations

### Current Limits
- **API Gateway**: 10,000 req/sec (can request increase)
- **Lambda**: 1,000 concurrent executions (can request increase)
- **SQS**: Virtually unlimited
- **RDS**: Depends on instance size

### Scaling Strategies
1. **Horizontal**: Lambda auto-scales, SQS handles queuing
2. **Database**: Read replicas for queries, connection pooling, instance scaling
3. **Regional**: Deploy to multiple regions with regional databases
4. **Partitioning**: Shard events table by date or user_id if needed

### Bottlenecks
- **Likely**: Database for high throughput (consider read replicas, connection pooling)
- **Unlikely**: Lambda or SQS (auto-scaling)
- **Optimization**: Batch processing in processor Lambda (up to 10 messages)

## Performance Characteristics

- **Ingestion Latency**: ~50-100ms (cold start: +500ms)
- **Processing Latency**: ~200-300ms end-to-end (async)
- **Query Latency**: ~10-50ms (warm connection pool)
- **Throughput**: Scales automatically with load


