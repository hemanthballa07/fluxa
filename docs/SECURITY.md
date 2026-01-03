# Fluxa Security

This document describes the security measures implemented in Fluxa.

## Secrets Management

### Database Password

- **Storage**: AWS Secrets Manager
- **Access**: Lambda functions retrieve password via `GetSecretValue` API call
- **Environment Variable**: `DB_PASSWORD_SECRET_ARN` contains only the secret ARN, not the password
- **Code Location**: `internal/config/secrets.go`

**Security Guarantee**: Database password is never stored in environment variables or logs.

### Secrets in Logs

**Invariant**: Secrets are never logged.

**Verification**:
- Code review confirms no secrets in log statements
- Error messages do not include sensitive data
- Only correlation IDs, event IDs, and non-sensitive metadata are logged

**Code Locations**:
- Logging: `internal/logging/logger.go`
- Error handling: All error messages reviewed for sensitive data

## Environment Variables

Lambda functions use minimal environment variables:

### Ingest Lambda
- `ENVIRONMENT` - Environment name (dev/prod)
- `SQS_QUEUE_URL` - Queue URL (non-sensitive)
- `S3_BUCKET_NAME` - Bucket name (non-sensitive)
- `LOG_LEVEL` - Logging level

### Processor Lambda
- `ENVIRONMENT` - Environment name
- `SQS_QUEUE_URL` - Queue URL
- `SQS_DLQ_URL` - DLQ URL
- `S3_BUCKET_NAME` - Bucket name
- `DB_HOST` - Database hostname
- `DB_PORT` - Database port (5432)
- `DB_NAME` - Database name
- `DB_USER` - Database username
- `DB_PASSWORD_SECRET_ARN` - Secret ARN (not password itself)
- `DB_SSL_MODE` - SSL mode (require)
- `SNS_TOPIC_ARN` - SNS topic ARN
- `LOG_LEVEL` - Logging level

### Query Lambda
- `ENVIRONMENT` - Environment name
- `DB_HOST` - Database hostname
- `DB_PORT` - Database port
- `DB_NAME` - Database name
- `DB_USER` - Database username
- `DB_PASSWORD_SECRET_ARN` - Secret ARN (not password itself)
- `DB_SSL_MODE` - SSL mode (require)
- `LOG_LEVEL` - Logging level

**Security**: No sensitive credentials in environment variables.

## SQL Injection Prevention

All SQL queries use parameterized statements (prepared statements).

### Examples

**Events Table Insert**:
```sql
INSERT INTO events (event_id, correlation_id, user_id, ...) 
VALUES ($1, $2, $3, ...)
```
Parameters are bound, never concatenated.

**Idempotency Check**:
```sql
SELECT status FROM idempotency_keys WHERE event_id = $1 FOR UPDATE
```

**Code Locations**:
- `internal/db/db.go` - All queries use `ExecContext` or `QueryRowContext` with parameters
- `internal/idempotency/idempotency.go` - All queries parameterized

**Security Guarantee**: SQL injection is not possible due to parameterized queries.

## Request Size Limits

### API Gateway

- **Default Limit**: 10MB payload size
- **Enforcement**: API Gateway automatically rejects requests >10MB

**Note**: For Fluxa, typical event payloads are <10KB. 10MB limit is more than sufficient.

### SQS Message Size

- **Limit**: 256KB per message
- **Enforcement**: 
  - Payloads ≤256KB: Inlined in SQS message
  - Payloads >256KB: Stored in S3, only S3 key in SQS message
- **Code Location**: `internal/queue/sqs.go:16` - `maxPayloadSizeBytes` constant

**Security**: Prevents SQS message size limit violations.

## S3 Bucket Security

### Public Access

**Configuration**: All public access blocked
- `block_public_acls = true`
- `block_public_policy = true`
- `ignore_public_acls = true`
- `restrict_public_buckets = true`

**Code Location**: `infra/terraform/modules/stateless/s3.tf:30-37`

### Encryption

**Encryption at Rest**: AES256 server-side encryption enabled
- All objects encrypted by default
- No additional KMS key required (uses S3 managed keys)

**Code Location**: `infra/terraform/modules/stateless/s3.tf:20-28`

### Access Control

- **IAM Policies**: Only Lambda functions have access (via IAM roles)
- **Ingest Lambda**: `s3:PutObject` permission
- **Processor Lambda**: `s3:GetObject` permission

**Code Location**: `infra/terraform/modules/stateless/iam.tf`

## IAM Least Privilege

### Separate Roles Per Lambda

Each Lambda has its own IAM role:
- `fluxa-ingest-{environment}`
- `fluxa-processor-{environment}`
- `fluxa-query-{environment}`

**Rationale**: Minimizes blast radius if one role is compromised.

### Wildcard Analysis

#### Necessary Wildcards

1. **CloudWatch Logs**: `arn:aws:logs:*:*:*`
   - **Reason**: Unavoidable - Lambda needs to write to log groups that are created dynamically
   - **Risk**: Low - only applies to CloudWatch Logs service
   - **Location**: All Lambda IAM policies

2. **CloudWatch Metrics**: `*` with namespace condition
   - **Reason**: CloudWatch PutMetricData requires wildcard resource
   - **Mitigation**: Namespace condition restricts to specific namespace (e.g., `Fluxa/Ingest`)
   - **Risk**: Low - namespace restriction limits scope
   - **Location**: All Lambda IAM policies

#### No Unnecessary Wildcards

All other permissions use specific ARNs:
- SQS queue ARNs
- S3 bucket ARNs
- SNS topic ARNs
- Secrets Manager secret ARNs

**Code Location**: `infra/terraform/modules/stateless/iam.tf`

## Network Security

### RDS

- **Public Access**: Disabled (`publicly_accessible = false`)
- **VPC**: RDS in private subnets
- **Security Groups**: Only Lambda security groups can access port 5432

**Code Location**: `infra/terraform/modules/stateful/rds.tf:93`

### Lambda VPC Configuration

- **Dev**: Uses VPC for Lambda to connect to RDS
- **Prod**: Uses VPC with proper security group restrictions
- **Security Groups**: Lambda security groups allow outbound only

**Code Location**: `infra/terraform/envs/dev/main.tf`, `infra/terraform/envs/prod/main.tf`

## Input Validation

### Schema Validation

Events are validated at ingestion:
- Required fields: `user_id`, `amount`, `currency`, `merchant`, `timestamp`
- Amount must be > 0
- Timestamp must be valid

**Code Location**: `internal/models/event.go:19-36`

### Payload Integrity

- SHA-256 hash calculated at ingest
- Hash verified at processor
- Mismatch triggers failure (no retry)

**Code Location**: 
- Ingest: `cmd/ingest/main.go:106-108`
- Processor: `cmd/processor/main.go:157-165`

## Database Security

### SSL/TLS

- **Connection**: SSL required (`DB_SSL_MODE=require`)
- **Enforcement**: PostgreSQL connection string enforces SSL

**Code Location**: `internal/config/config.go:DSN()` method

### Connection Security

- **Credentials**: Retrieved from Secrets Manager
- **Network**: RDS in private subnets, accessible only via VPC
- **Connection Pooling**: Limited connections per Lambda (10 max)

**Code Location**: `internal/db/db.go:19-35`

## Security Best Practices

1. **Regular Secret Rotation**: Rotate database password periodically (manually or via Secrets Manager rotation)
2. **Audit IAM Policies**: Regularly review IAM policies for unnecessary permissions
3. **Monitor Access**: Use CloudTrail to monitor API access
4. **Encryption in Transit**: All connections use SSL/TLS
5. **Least Privilege**: Each Lambda has minimum required permissions
6. **No Secrets in Code**: No hardcoded secrets, all via Secrets Manager

## Security Checklist

- [x] No secrets in environment variables
- [x] Secrets stored in Secrets Manager
- [x] All SQL queries parameterized
- [x] Request size limits enforced
- [x] S3 bucket private with encryption
- [x] IAM least privilege (minimal wildcards)
- [x] Separate IAM roles per Lambda
- [x] RDS not publicly accessible
- [x] SSL/TLS enforced for database
- [x] Input validation at ingest
- [x] Payload integrity verification
- [x] No secrets in logs

## Risk Assessment

| Risk | Mitigation | Status |
|------|-----------|--------|
| Secret leakage in logs | Code review, structured logging | ✅ Mitigated |
| SQL injection | Parameterized queries | ✅ Mitigated |
| Unauthorized S3 access | Private bucket, IAM policies | ✅ Mitigated |
| Database credential exposure | Secrets Manager | ✅ Mitigated |
| Unauthorized Lambda execution | IAM roles, API Gateway auth | ✅ Mitigated |
| Data in transit interception | SSL/TLS enforced | ✅ Mitigated |
| Overly permissive IAM | Least privilege, specific ARNs | ✅ Mitigated |


