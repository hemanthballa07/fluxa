# Fluxa — Changelog

All significant changes, in reverse chronological order.

---

## [Unreleased]

### Added
- `docs/PORTFOLIO_NARRATIVE.md` — strategic plan for the fintech infrastructure trifecta (`fluxa` + `bankops-portal` + `fluxguard`). Includes existing-asset inventory, 8-step build sequence with time-boxes, pre-written resume bullets with bracketed measurement targets, honest tradeoffs, and stop conditions. Also captures the `bankops-portal/backend/src` audit result (verdict: production-shape — Spring Boot 3.2, optimistic-lock retry on withdrawals, real concurrency tests via `CountDownLatch`/`ExecutorService`, 12 test classes including resilience and idempotency suites). Integration plan for step 2 (Fluxa gRPC fraud-eval called from `TransactionService`) confirmed additive — no rewrites required.

### Changed
- `docs/plan.md` "Next" section reframed: trifecta is now the major initiative, with tactical items (README badges, demo GIF) reclassified as smaller-scope follow-ons that hold regardless of the trifecta path.

---

## 2026-04-15 — IEEE-CIS Replay Integration + Grafana Fix

**Commits:** `cb5052a` (replay mapper), this commit (rules + dashboard + velocity fix)

### Added
- IEEE-CIS replay mapper (`services/replay/main.go`): `TransactionAmt`/`card1`/`TransactionDT`/`ProductCD`+`card4` → ingest event; `isFraud` forwarded as `is_fraud_ground_truth` metadata
- `docs/grafana-dashboard.png` — dashboard screenshot for portfolio

### Fixed
- `internal/db/db.go` `CountRecentEvents`: switched `ts` → `created_at` for velocity window query; `ts` holds simulated transaction time so velocity checks would never fire on replay data with historical timestamps
- Grafana dashboard Postgres panels: added `"rawQuery": true` to all three targets (panels 7, 8, 9) — without it Grafana ignores `rawSql` and returns no data
- Grafana Fraud Rate gauge: wrapped both sides of division in `sum()` to resolve label mismatch between `fraud_flags_total{rule=...}` and `events_processed_total{service=...,status=...}`
- Dashboard time range: `now-1h` → `now-3h` for Prometheus panels; Postgres panels now use hardcoded `'2024-01-01'` lower bound instead of `$__timeFilter` to decouple from the time picker; day-level bucketing for transaction volume panel

### Changed
- `rules.yaml`: `amount_threshold` 10000 → 500 (fires on ~4% of IEEE-CIS transactions); `velocity_window_seconds` 300 → 60; `velocity_max_count` 5 → 3; replaced PaySim placeholder merchants with real IEEE-CIS merchant names: `Amazon Marketplace`, `Walmart Online`, `Target`

### Verified end-to-end
- 500k+ events processed; 16k+ `amount_threshold` flags; `blocked_merchant` firing on top-3 merchants; `velocity` confirmed via test (3 transactions in <60s triggers flag)

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
