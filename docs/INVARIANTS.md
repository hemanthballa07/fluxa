# Fluxa System Invariants

This document defines the critical invariants that Fluxa maintains and how they are enforced.

## 1. Event ID Uniqueness

**Invariant**: An `event_id` is persisted at most once in the `events` table.

**Enforcement**:
- Database: PRIMARY KEY constraint on `event_id` column (`migrations/001_create_events_table.sql:2`)
- Application: `ON CONFLICT (event_id) DO NOTHING` in `InsertEvent` (`internal/db/db.go:62`)

**Test Validation**:
- Local test harness: Duplicate insert test (`local/main.go:195-210`)
- Integration test: `TestIdempotency_EndToEnd` (`internal/idempotency/idempotency_test.go:126`)

**Failure Mode**: If violated, database constraint will reject duplicate insert. Application gracefully handles conflict.

---

## 2. Processor Idempotency

**Invariant**: Processor is idempotent across retries and duplicate messages. Same `event_id` processed multiple times results in at most one persisted event.

**Enforcement**:
- Idempotency check before processing: `CheckAndMark` with transaction + `SELECT FOR UPDATE` (`internal/idempotency/idempotency.go:22-81`)
- Status tracking: `idempotency_keys` table tracks processing state (`migrations/002_create_idempotency_keys_table.sql:2-8`)
- Atomic status transition: Transaction ensures only one attempt processes at a time (`internal/idempotency/idempotency.go:31-45`)

**Test Validation**:
- `TestCheckAndMark_AlreadyProcessed` (`internal/idempotency/idempotency_test.go:76`)
- `TestIdempotency_EndToEnd` (`internal/idempotency/idempotency_test.go:126`)
- Local test harness end-to-end verification (`local/main.go:140-210`)

**Failure Mode**: If violated, duplicate events could be persisted. Mitigated by database PRIMARY KEY constraint on `event_id`.

---

## 3. Payload Hash Integrity

**Invariant**: Payload hash (SHA-256) must match stored payload. Any mismatch indicates data corruption and processing must fail.

**Enforcement**:
- Hash verification in processor: `cmd/processor/main.go:157-165`
- Hash calculation at ingest: `cmd/ingest/main.go:106-108`
- Mismatch handling: Mark as failed, send to DLQ (no retry) (`cmd/processor/main.go:163-164`)

**Test Validation**:
- Unit test: Hash calculation correctness (implicit in payload processing tests)
- Integration: Payload verification in processor flow

**Failure Mode**: If violated, corrupted payloads could be processed. Mitigated by hash verification and DLQ for permanent failures.

---

## 4. Inline Payload Constraint

**Invariant**: Payloads ≤256KB are always inlined in SQS messages. They never hit S3.

**Enforcement**:
- Size check: `queue.ShouldUseS3(len(payloadBytes))` (`cmd/ingest/main.go:118`)
- Threshold constant: `maxPayloadSizeBytes = 256 * 1024` (`internal/queue/sqs.go:16`)
- Conditional S3 storage: Only if size > 256KB (`cmd/ingest/main.go:120-137`)

**Test Validation**:
- Unit test: `TestShouldUseS3` (`internal/queue/sqs_test.go:11-39`)

**Failure Mode**: If violated, small payloads might unnecessarily use S3, increasing cost and latency.

---

## 5. S3 Payload Constraint

**Invariant**: Payloads >256KB stored in S3 are never inlined in SQS messages. Only S3 key is included.

**Enforcement**:
- Size check forces S3 mode: `cmd/ingest/main.go:118-137`
- SQS message contains only `s3_bucket` and `s3_key`, not `payload_inline` (`cmd/ingest/main.go:132-134`)
- Processor fetches from S3 when `payload_mode == "S3"` (`cmd/processor/main.go:134-148`)

**Test Validation**:
- Unit test: `TestShouldUseS3` verifies threshold (`internal/queue/sqs_test.go:11-39`)
- Message parsing: `TestParseSQSEventMessage` validates S3 vs inline modes (`internal/queue/sqs_test.go:41-107`)

**Failure Mode**: If violated, SQS message size limit (256KB) could be exceeded, causing send failures.

---

## 6. DLQ Message Semantics

**Invariant**: DLQ messages represent permanent failures only. Transient errors trigger retries and never reach DLQ.

**Enforcement**:
- SQS redrive policy: `maxReceiveCount = 3` (`infra/terraform/modules/stateless/sqs.tf:17`)
- Error classification in processor:
  - Permanent (no retry): Parse errors, validation errors, hash mismatch (`cmd/processor/main.go:100-105, 127-130, 160-164`)
  - Transient (retry): DB errors, S3 fetch errors (`cmd/processor/main.go:146-147, 183-186`)
- Return error for transient failures to trigger SQS retry (`cmd/processor/main.go:186`)

**Test Validation**:
- Manual: DLQ investigation procedures (`docs/RUNBOOK.md:142-190`)
- Integration: Error handling in processor logic

**Failure Mode**: If violated, transient errors could be sent to DLQ unnecessarily, or permanent errors could retry forever.

---

## 7. Query Lambda Read-Only

**Invariant**: Query Lambda never mutates state. It only reads from the database.

**Enforcement**:
- Query Lambda only implements `GetEventByID` (`cmd/query/main.go`)
- No write operations in query handler
- Database client only uses `SELECT` queries (`internal/db/db.go:86-131`)

**Test Validation**:
- Code review: No mutation operations in query Lambda
- Integration: Query endpoint testing

**Failure Mode**: If violated, query operations could corrupt data or create duplicate records.

---

## 8. Lambda Panic Handling

**Invariant**: No Lambda panics propagate silently. All errors are logged and handled gracefully.

**Enforcement**:
- Error handling in all Lambda handlers:
  - Ingest: Returns HTTP error responses (`cmd/ingest/main.go:89-95, 122-129`)
  - Processor: Logs errors, returns error for retry or nil for DLQ (`cmd/processor/main.go:100-104, 183-186`)
  - Query: Returns HTTP error responses (`cmd/query/main.go:70-85`)
- Structured logging with correlation IDs for all errors (`internal/logging/logger.go:28-40`)
- CloudWatch metrics for failures (`cmd/*/main.go` - various `EmitMetric` calls)

**Test Validation**:
- Code review: All error paths return appropriate responses
- Integration: Error handling tests

**Failure Mode**: If violated, panics could cause silent failures or unhandled errors.

---

## 9. Correlation ID Propagation

**Invariant**: Correlation ID is propagated end-to-end: API → SQS → Logs → Database.

**Enforcement**:
- Ingest: Generates or extracts correlation ID, includes in SQS message attributes (`cmd/ingest/main.go:57-59, 148-151`)
- SQS: Correlation ID in message attributes (`cmd/ingest/main.go:148-151`)
- Processor: Extracts from message attributes, includes in logs and DB (`cmd/processor/main.go:85-91, 183`)
- Query: Extracts from request headers, includes in logs (`cmd/query/main.go:42-45`)
- Database: Stored in `events.correlation_id` column (`internal/db/db.go:68`)

**Test Validation**:
- Integration: End-to-end correlation ID tracking
- Log inspection: Verify correlation IDs in CloudWatch logs

**Failure Mode**: If violated, tracing across services becomes impossible, making debugging difficult.

---

## 10. Secrets Never Logged

**Invariant**: Secrets (passwords, keys, tokens) are never logged or exposed in error messages.

**Enforcement**:
- Secrets Manager integration: DB password fetched from Secrets Manager, never in env vars (`internal/config/secrets.go`)
- Error messages: No sensitive data in error strings (`internal/*/*.go` - error handling)
- Logging: Only correlation IDs, event IDs, not sensitive payload data (`internal/logging/logger.go`)

**Test Validation**:
- Code review: No secrets in log statements or error messages
- Security audit: `docs/SECURITY.md`

**Failure Mode**: If violated, secrets could leak to CloudWatch logs, exposing system credentials.

---

## Verification Summary

| Invariant | Code Enforcement | Test Coverage | Risk if Violated |
|-----------|------------------|---------------|------------------|
| Event ID Uniqueness | DB PK + ON CONFLICT | ✓ Unit + Integration | Medium |
| Processor Idempotency | Transaction + Status Check | ✓ Unit + Integration | High |
| Payload Hash Match | Hash Verification | ✓ Integration | Medium |
| Inline ≤256KB | Size Check | ✓ Unit | Low |
| S3 >256KB | Size Check + Mode | ✓ Unit | Medium |
| DLQ = Permanent Only | Error Classification | Manual | Medium |
| Query Read-Only | Code Review | ✓ Integration | Low |
| No Silent Panics | Error Handling | Code Review | Medium |
| Correlation ID Flow | End-to-End | ✓ Integration | Low |
| Secrets Never Logged | Code Review | Security Audit | High |


