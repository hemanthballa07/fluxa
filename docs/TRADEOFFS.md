# Fluxa Design Tradeoffs

This document outlines key architectural decisions, tradeoffs, and alternatives considered during the design of Fluxa.

## Architecture Decisions

### 1. Serverless vs. Containers/EC2

**Decision**: Serverless (Lambda) for compute

**Rationale**:

- ✅ Auto-scaling without infrastructure management
- ✅ Pay-per-use pricing model
- ✅ Built-in integration with other AWS services
- ✅ Reduced operational overhead
- ❌ Cold start latency (mitigated with provisioned concurrency if needed)
- ❌ 15-minute maximum execution time (sufficient for batch processing)

**Alternative**: ECS Fargate containers

- More control over runtime environment
- Better for long-running processes
- Higher operational complexity

### 2. SQS vs. Kinesis vs. EventBridge

**Decision**: SQS for message queue

**Rationale**:

- ✅ Exactly-once delivery guarantees with deduplication
- ✅ Built-in DLQ support
- ✅ Simple integration with Lambda
- ✅ Cost-effective for moderate throughput
- ❌ Limited ordering guarantees (not required for our use case)
- ❌ 256KB message size limit (mitigated with S3 for large payloads)

**Alternatives**:

- **Kinesis**: Better for ordered, high-throughput streams (overkill for this use case)
- **EventBridge**: Better for event routing and filtering (we need queue semantics with DLQ)

### 3. Inline vs. S3 Payload Strategy

**Decision**: Inline for ≤256KB, S3 for >256KB

**Rationale**:

- ✅ Minimizes S3 API calls for small payloads (cost and latency)
- ✅ Reduces SQS message size for large payloads
- ✅ Allows processing without S3 fetch for most messages
- ❌ Adds complexity with two code paths
- ❌ Requires hash verification for integrity

**Alternative**: Always use S3

- Simpler code
- Consistent payload storage
- Higher latency and cost for small payloads

### 4. Idempotency Implementation

**Decision**: Database table (`idempotency_keys`) with event_id as key

**Rationale**:

- ✅ Persistent across Lambda invocations
- ✅ Handles concurrent processing attempts
- ✅ Stores error history for debugging
- ✅ Can track processing attempts
- ❌ Additional database write per event
- ❌ Requires table cleanup strategy for old keys

**Alternatives**:

- **SQS Deduplication ID**: Only works for 5-minute window, doesn't handle retries well
- **DynamoDB**: Lower latency but additional service dependency
- **In-memory cache**: Not persistent, doesn't work across Lambda instances

### 5. RDS PostgreSQL vs. DynamoDB

**Decision**: RDS PostgreSQL

**Rationale**:

- ✅ SQL queries for complex event retrieval
- ✅ ACID transactions if needed for future features
- ✅ Familiar query interface
- ✅ Better for relational queries (e.g., events by user_id)
- ❌ Requires connection pooling
- ❌ Higher operational complexity (backups, patching)
- ❌ Higher cost for low throughput

**Alternative**: DynamoDB

- Serverless, auto-scaling
- Lower latency
- Better for high-throughput, simple key-value lookups
- More expensive for complex queries
- Less flexible query patterns

### 6. API Gateway REST vs. HTTP API

**Decision**: REST API

**Rationale**:

- ✅ More features (request validation, API keys, usage plans)
- ✅ Better for production APIs
- ✅ Fine-grained IAM integration
- ❌ Slightly higher cost
- ❌ More configuration complexity

**Alternative**: HTTP API

- Lower latency
- Lower cost
- Simpler configuration
- Fewer features

### 7. Correlation ID Propagation

**Decision**: SQS message attributes + logs

**Rationale**:

- ✅ Preserved through SQS message attributes (guaranteed delivery)
- ✅ Included in all structured logs
- ✅ Enables end-to-end tracing
- ✅ Simple implementation
- ❌ Requires consistent logging format
- ❌ Doesn't span external service calls (future: X-Ray)

**Alternative**: AWS X-Ray

- Full distributed tracing
- Automatic instrumentation
- Additional cost and complexity
- Overkill for current use case

### 8. Error Handling Strategy

**Decision**: Transient vs. Permanent error classification

**Rationale**:

- ✅ Prevents infinite retries for permanent failures
- ✅ Allows DLQ for manual intervention
- ✅ Reduces unnecessary retry costs
- ❌ Requires careful error classification
- ❌ May need adjustment based on real-world patterns

**Alternative**: Retry all errors

- Simpler logic
- Wastes resources on permanent failures
- Fills DLQ with unrecoverable messages

### 9. Lambda Batch Processing

**Decision**: Process batch of up to 10 messages

**Rationale**:

- ✅ Better throughput than single-message processing
- ✅ Reduced Lambda invocations (cost savings)
- ✅ Efficient use of database connections
- ❌ Partial batch failures require careful handling
- ❌ One slow message delays batch

**Alternative**: Single message per invocation

- Simpler error handling
- Lower latency per message
- Higher Lambda invocation cost
- Lower throughput

### 10. Monitoring Strategy

**Decision**: CloudWatch Metrics (Embedded Metric Format) + Logs + Alarms

**Rationale**:

- ✅ Native AWS integration
- ✅ No additional dependencies
- ✅ Structured logs for debugging
- ✅ Cost-effective for moderate scale
- ❌ Limited visualization (basic dashboards)
- ❌ No advanced analytics

**Alternative**: Third-party APM (Datadog, New Relic)

- Better visualization and analytics
- Cross-service correlation
- Additional cost
- Additional dependency

## Performance Considerations

### Latency Budget

- **API Gateway → Ingest Lambda**: ~50-100ms (cold start: +500ms)
- **Ingest Lambda → SQS**: ~50ms
- **SQS → Processor Lambda**: <100ms (trigger delay)
- **Processor Lambda → RDS**: 10-50ms (warm connection pool)
- **Total end-to-end (async)**: ~200-300ms (excluding cold starts)

### Throughput Considerations

- **SQS**: Virtually unlimited (AWS handles scaling)
- **Lambda**: Default 1000 concurrent executions (can request increase)
- **RDS**: Depends on instance size and connection pool
- **Bottleneck**: Likely RDS for high throughput (consider read replicas for queries)

## Cost Optimization

### Estimated Monthly Costs (Dev Environment, Low-Medium Load)

- **Lambda**: ~$5-20 (depends on invocations and memory)
- **SQS**: ~$1-5 (first 1M requests free)
- **S3**: ~$1-10 (depends on payload size and access patterns)
- **RDS**: ~$15-50 (db.t3.micro to db.t3.small)
- **API Gateway**: ~$3.50 per million requests
- **CloudWatch**: ~$5-15 (logs and metrics)
- **Total**: ~$30-100/month for dev environment

### Cost Optimization Strategies

1. **S3 Lifecycle Policies**: Move old payloads to Glacier after 30 days
2. **RDS Reserved Instances**: For production (1-3 year commitment)
3. **Lambda Provisioned Concurrency**: Only for critical paths (adds cost)
4. **CloudWatch Log Retention**: Reduce retention period for non-production
5. **SQS Long Polling**: Reduces API calls (default behavior)

## Scalability Limits

### Current Design Limits

- **API Gateway**: 10,000 requests/second (can request increase)
- **Lambda**: 1000 concurrent executions (can request increase)
- **SQS**: Virtually unlimited
- **RDS**: Depends on instance size (consider read replicas and connection pooling)

### Scaling Strategies

1. **Horizontal**: Add more Lambda concurrency (automatic)
2. **Database**: Read replicas for query Lambda, connection pooling
3. **Regional**: Deploy to multiple regions with regional databases
4. **Partitioning**: Shard events table by date or user_id if needed

## Security Tradeoffs

### Secrets Management: SSM Parameter Store vs. Secrets Manager

**Decision**: Support both (SSM Parameter Store preferred for cost)

**Rationale**:

- ✅ SSM Parameter Store: Free for standard parameters
- ✅ Secrets Manager: Automatic rotation, but costs $0.40/secret/month
- ✅ Both integrate with Lambda environment variables

### Network Isolation: VPC vs. Public

**Decision**: VPC for RDS (if needed), public endpoints for Lambda-SQS-S3

**Rationale**:

- ✅ VPC adds latency (NAT Gateway cost: ~$32/month + data transfer)
- ✅ SQS, S3, API Gateway are already secure with IAM
- ✅ RDS can use public endpoint with security groups (or VPC for compliance)

**Alternative**: Full VPC deployment

- Better network isolation
- Higher latency and cost
- More complex networking

## Future Considerations

### Potential Enhancements

1. **Event Sourcing**: Add event stream for audit and replay
2. **CQRS**: Separate read models optimized for queries
3. **Multi-Region**: Active-active deployment for disaster recovery
4. **X-Ray Integration**: Distributed tracing across services
5. **Event Schema Evolution**: Versioned schemas with compatibility checks
6. **Real-time Processing**: Kinesis for low-latency stream processing
7. **Caching Layer**: ElastiCache for frequently accessed events
8. **Data Warehouse Integration**: S3 → Redshift/Athena for analytics

