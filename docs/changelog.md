# Fluxa — Changelog

All significant changes, in reverse chronological order.

---

## [Unreleased] — In Progress

### IEEE-CIS Replay Integration
- Spec written and approved (`docs/specs/ieee-cis-replay.md`)
- `services/replay/main.go`: replaced PaySim `mapCSVRowToEvent` with IEEE-CIS mapper
  - `TransactionAmt` → `amount`; `card1` → `user_id`
  - `TransactionDT` anchored to `2024-01-01T00:00:00Z` + seconds offset
  - Merchant derived from `ProductCD` + `card4` lookup table (8 named entries + `"merchant_<P>_<C>"` default)
  - Metadata: `email_domain` (`P_emaildomain`), `card_network` (`card4`), `product_code` (`ProductCD`), `is_fraud_ground_truth` (`isFraud` as `"0"` or `"1"`)
- `README.md`: replaced PaySim dataset section with IEEE-CIS Kaggle link and updated file rename instructions
- Pending: end-to-end verification with Grafana

---

## 2026-04-14 — AWS → Local Platform Migration

**Commits:** `d4dba1c`, `a55a0bb`, `9322323`

### Removed
- All AWS infrastructure: SQS queues, Lambda functions, API Gateway, RDS, S3, CloudWatch, Terraform configs
- GitHub Actions CI/CD pipeline (no longer applicable to local platform)
- PaySim replay dependency on `nameOrig`/`nameDest` merchant semantics (still PaySim, but context changed)

### Added
- **RabbitMQ** (`rabbitmq:3.13-management`) as message broker
  - `events` queue: ingest → processor
  - `alerts` fanout exchange: processor → alert-consumer
- **MinIO** (`minio/minio:latest`) as local S3-compatible object store for payloads >256 KB
- **Prometheus** (`prom/prometheus:v2.53.0`) scraping all services every 15s
- **Grafana** (`grafana/grafana:11.1.0`) with auto-provisioned dashboard
  - Row 1: Traffic (ingested rate, processed rate, p99 latency)
  - Row 2: Fraud (fraud rate %, flags by rule bar chart, flags over time line chart)
  - Row 3: DB-direct (top flagged merchants, tx volume 5-min buckets, active flags by rule pie)
- **YAML-driven fraud rules engine** (`rules.yaml`) loaded at processor startup, no rebuild needed
  - `amount_threshold`: flag transactions above USD threshold
  - `velocity_window_seconds` + `velocity_max_count`: per-user rate limiting
  - `blocked_merchants`: exact-match blocklist
  - `high_risk_currencies`: currency code blocklist
- **`alert-consumer` service**: dedicated RabbitMQ consumer that logs `FRAUD ALERT` to stdout
- **`replay` service**: streams PaySim CSV at 200 req/s via Docker Compose profile `replay`
- Full Docker Compose stack with health checks and service dependencies
- Grafana datasource UID wiring and PostgreSQL datasource auto-provisioning

### Changed
- Ingest service: HTTP→RabbitMQ publish (was HTTP→SQS)
- Processor service: RabbitMQ consumer (was SQS Lambda handler)
- Query service: direct PostgreSQL reads (unchanged pattern, new infra)
- README: rewritten for local platform; architecture diagram, quick-start, Makefile docs

---

## 2026-01-05 — Reliability Hardening

**Commits:** `648b856`, `a809008`, `dcf8779`, `41fb272`, `9e01e4c`

### Added
- Explicit error classification types: `NonRetryableError` and `RetryableError`
  - Non-retryable: message ACKed and discarded (poison messages, schema violations, hash mismatch)
  - Retryable: message NACKed with requeue (transient failures)
- Strict schema validation in processor; invalid messages rejected as non-retryable
- Poison message handling: malformed JSON, missing required fields → non-retryable discard
- Integration test: verify non-retryable errors are ACKed and not requeued
- SQL reporting artifacts for operational visibility (`docs/METRICS_CAPTURE.md`)

### Fixed
- Test harness schema aligned to match production definitions

---

## 2026-01-03 — Core Platform Build, CI, and Observability

**Commits:** `9f153d3` through `877deb1`

### Added — Core Services
- `ingest` service: HTTP server on `:8080`, validates and enqueues events
- `processor` service: queue consumer, persists events, evaluates fraud rules, publishes alerts
- `query` service: HTTP server on `:8083`, retrieves persisted events from PostgreSQL
- `replay` service: streams CSV dataset at configurable req/s

### Added — Infrastructure
- PostgreSQL schema migrations:
  - `001_events.sql`: `events` table
  - `002_idempotency_keys.sql`: distributed dedup keys
  - `003_fraud_flags.sql`: per-event fraud flag records
- Distributed idempotency: `SELECT FOR UPDATE` on `idempotency_keys` + `ON CONFLICT DO NOTHING` on `events`
- MinIO adapter for large payload offload (>256 KB threshold)
- SHA-256 hash verification: computed at ingest, verified at processor; mismatch → non-retryable

### Added — Observability
- Prometheus metrics on all services:
  - `events_ingested_total`, `events_processed_total{status}`, `fraud_flags_total{rule}`
  - `query_total{status}`, `alerts_consumed_total`
  - `ingest_latency_seconds`, `process_latency_seconds` (histograms)
- Automated metrics capture script

### Added — CI
- GitHub Actions workflows: Go lint, unit tests, integration tests (with real PostgreSQL)
- Integration tests require real DB; no mocks for the DB layer

### Fixed
- Concurrency race in idempotency check (lock expiry + retry path)
- Config missing variables for local harness
- Binary naming for `provided.al2` target

---

## 2025-12-16 — Initial Commit

**Commit:** `76f0c9d`

- Initial project scaffold
