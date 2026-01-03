# Fluxa Interview Cheatsheet

This document provides talking points for discussing Fluxa in technical interviews.

## 30-Second Explanation

"Fluxa is a production-grade serverless event-driven data platform I built on AWS. It ingests transaction events via API Gateway, processes them asynchronously through SQS, and persists them to PostgreSQL. The system implements idempotency to prevent duplicates, uses intelligent payload handling where small payloads go inline in SQS and large ones go to S3, and includes comprehensive observability with CloudWatch metrics and alarms. Everything is Infrastructure as Code with Terraform, and I validated correctness with integration tests and failure injection scenarios."

## 2-Minute System Walkthrough

"Let me walk you through the architecture. Events come in through API Gateway's REST API to the ingest Lambda. The ingest Lambda validates the schema, generates correlation IDs for tracing, and handles payload size intelligently—if it's under 256KB, we inline it in the SQS message with a SHA-256 hash. If it's larger, we store it in S3 and just put the S3 key in the message to avoid hitting SQS size limits.

Messages flow to SQS, which triggers the processor Lambda in batches of up to 10. The processor implements idempotency using a database-backed transaction pattern—we use SELECT FOR UPDATE to atomically check if an event was already processed, which prevents duplicate processing even with concurrent retries. If it's already processed, we skip safely. Otherwise, we fetch the payload from inline or S3, verify the hash for integrity, persist to PostgreSQL, and publish an SNS notification.

The query Lambda handles GET requests to retrieve events by ID. Everything is instrumented with structured JSON logging using correlation IDs for end-to-end tracing, and CloudWatch metrics using Embedded Metric Format. We have alarms for DLQ depth, Lambda errors, throttles, and API 5xx responses.

The infrastructure is Terraform with separate modules for stateful resources like RDS and stateless resources like Lambda and SQS. I implemented proper security with Secrets Manager for database passwords, least-privilege IAM roles, and parameterized SQL queries."

## 3 Strongest Design Decisions (with Tradeoffs)

### 1. Database-Backed Idempotency vs. SQS Deduplication

**Decision**: Use PostgreSQL table with transaction-based idempotency checks instead of SQS message deduplication.

**Rationale**: 
- SQS deduplication only works for a 5-minute window and doesn't handle retries well
- Database approach provides persistence across Lambda invocations and handles concurrent attempts correctly
- Transaction with SELECT FOR UPDATE ensures atomicity

**Tradeoff**: 
- Adds database write per event (overhead)
- Requires transaction handling complexity
- SQS deduplication would be simpler but less reliable

**Why Strong**: This decision ensures correctness even under failure scenarios. The idempotency guarantee is critical for financial transaction events.

### 2. Hybrid Payload Strategy (Inline vs. S3)

**Decision**: Inline payloads ≤256KB in SQS messages, store larger payloads in S3 with only the key in the message.

**Rationale**:
- Avoids S3 API calls for the common case (small payloads) - reduces latency and cost
- Prevents SQS message size limit violations for large payloads
- SHA-256 hash verification ensures integrity in both cases

**Tradeoff**:
- Adds code complexity with two code paths
- Requires hash calculation and verification
- Alternative: Always use S3 would be simpler but less efficient

**Why Strong**: This optimizes for both cost and latency while maintaining correctness. Most events are small, so we optimize the hot path.

### 3. Serverless Architecture vs. Containers/EC2

**Decision**: Use Lambda functions instead of ECS containers or EC2 instances.

**Rationale**:
- Auto-scaling without infrastructure management
- Pay-per-use pricing model
- Built-in integration with AWS services (API Gateway, SQS, SNS)
- Reduced operational overhead

**Tradeoff**:
- Cold start latency (mitigated with connection pooling and warm starts)
- 15-minute execution limit (sufficient for our use case)
- Less control over runtime environment

**Why Strong**: Serverless fits the event-driven, bursty workload perfectly. The tradeoffs are acceptable given the operational simplicity and cost efficiency.

## 3 Failure Scenarios + Handling

### 1. Duplicate SQS Message Delivery

**Scenario**: SQS delivers the same message twice (at-least-once delivery semantics).

**Handling**: 
- Idempotency check in processor: `CheckAndMark` uses transaction with `SELECT FOR UPDATE` to atomically check if event was already processed
- If status is 'success', skip processing and return success
- Database PRIMARY KEY on `event_id` provides final safeguard against duplicate inserts

**Prevention**: Cannot prevent duplicate delivery (SQS at-least-once semantics), but handle gracefully with idempotency.

**Code**: `internal/idempotency/idempotency.go:22-91`, `internal/db/db.go:68` (ON CONFLICT DO NOTHING)

### 2. Database Connection Failure

**Scenario**: RDS instance is down or network connectivity issues prevent database access.

**Handling**:
- Processor Lambda returns error (not nil) to trigger SQS retry
- SQS automatically retries with exponential backoff
- After 3 retries (maxReceiveCount), message goes to DLQ
- CloudWatch alarm on DLQ depth alerts on-call engineer
- Connection pooling with context timeouts (5s) prevents hanging

**Prevention**: 
- RDS Multi-AZ in production for automatic failover
- Health checks and monitoring
- Connection pool limits prevent resource exhaustion

**Code**: `cmd/processor/main.go:183-186`, `internal/db/db.go:51` (context timeout)

### 3. Payload Hash Mismatch

**Scenario**: Calculated SHA-256 hash of payload doesn't match hash in SQS message, indicating data corruption.

**Handling**:
- Hash verification in processor detects mismatch
- Event marked as failed in idempotency table with error reason 'hash_mismatch'
- Lambda returns nil (no retry) - permanent failure
- Message goes to DLQ after max receives
- On-call engineer investigates DLQ message to determine if payload is recoverable

**Prevention**:
- Hash calculated at ingest time before S3 storage or SQS send
- Hash stored in SQS message attributes
- Verification prevents corrupted data from being processed

**Code**: `cmd/processor/main.go:157-165`

## 2 Things to Improve with More Time

### 1. Individual Message Error Handling in Processor Batch

**Current**: If one message in a batch of 10 fails, the entire batch is retried (processor returns error).

**Improvement**: Implement per-message success/failure handling:
- Process each message individually
- Delete successful messages from SQS immediately
- Only return error for messages that should retry
- This improves throughput and prevents unnecessary reprocessing of successful messages

**Impact**: Higher throughput, more efficient resource usage, better error isolation.

### 2. Distributed Tracing with AWS X-Ray

**Current**: Correlation IDs in logs enable manual tracing across services.

**Improvement**: Integrate AWS X-Ray for automatic distributed tracing:
- X-Ray SDK for Lambda provides automatic instrumentation
- Visual service map showing request flow
- Automatic performance analysis and bottleneck detection
- Integration with CloudWatch for unified observability

**Impact**: Easier debugging, automatic performance insights, better production visibility.

**Tradeoff**: Additional cost (~$5 per million traces) and slight latency overhead.

## Additional Talking Points

### Scalability
- Lambda auto-scales based on SQS queue depth
- SQS handles virtually unlimited throughput
- RDS can be scaled vertically (instance size) or horizontally (read replicas for queries)
- Connection pooling limits prevent database connection exhaustion

### Cost Optimization
- Payload strategy minimizes S3 API calls (most events inline)
- Serverless means pay only for actual usage
- S3 lifecycle policies archive old payloads after 90 days
- CloudWatch log retention could be set to reduce storage costs

### Testing Strategy
- Unit tests for core logic (idempotency, validation, queue logic)
- Integration tests with local PostgreSQL (docker-compose)
- Failure injection tests simulate various failure scenarios
- No AWS credentials required for local testing

### Production Readiness
- Infrastructure as Code (Terraform) for repeatable deployments
- Comprehensive monitoring and alerting
- Security best practices (Secrets Manager, least privilege IAM, parameterized SQL)
- Documentation (architecture, runbook, tradeoffs, invariants)


