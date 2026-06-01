# Fluxa — Changelog

All significant changes, in reverse chronological order.

---

## [Unreleased]

### Added (2026-05-31 — Trifecta Step 5a, ML fraud scorer)
- `internal/mlfeatures` — authoritative feature builder (transaction-time point-in-time aggregates), used by both online serving and the offline export, guaranteeing train/serve parity (no skew).
- `db.CountUserEventsAsOf` / `db.UserAmountStatsAsOf` — ts-based point-in-time aggregate queries (migration `004` indexes `events(user_id, ts)`).
- `cmd/export-features` — batch export of labeled events → `ml/data/features.csv` via the shared builder.
- `ml/` — conda env (Python 3.12), `train.py` (XGBoost → ONNX, temporal split, frequency/one-hot encoder), `evaluate.py` → `docs/ML_EVALUATION.md`. **ONNX serving matches XGBoost to 3.4e-8.**
- `services/ml-scorer` — Python gRPC service (`:9097`, metrics `:9098`) serving the ONNX model; `proto/scorer/v1/scorer.proto` + Go client `internal/adapters/scorer`.
- `fraud.Engine.EvaluateWithScorer` — blends `FLAG if (any rule) OR (ml_score ≥ τ)`; **fail-open** to rules-only on any scorer error. Wired into `fraud-grpc` (sync) and the async processor.
- `EvaluateResponse.ml_score` (proto field 5); `evaluated_by` gains a `+ml-<version>` suffix when the scorer contributes.
- **Verified live:** `EvaluateTransaction` returns `ml_score` with `evaluated_by=fluxa-rules-v1.0+ml-v1` at 7–20ms warm; cold-start first call fails open correctly. Headline PR-AUC 0.132 (CI 0.116–0.151) vs amount-only 0.043 — honest serve-parity result (H1/H15).
- **Step 5b — surfaced the score:** migration `005` adds `ml_score` to `fraud_flags`; `domain.FraudFlag`/`FraudEvent`/`AlertMessage` carry it; the processor + fraud-grpc stamp each flag with the event's score; the SSE `/fraud-events` wire format gains `ml_score`; the `trifecta-console` fraud feed renders an **ML** risk column. Verified: SSE now emits `"ml_score":0.028…` on freshly-scored flags.

### Added (2026-05-31 — Trifecta Step 4, SSE fraud feed)
- `domain.FraudEvent` — new type joining `fraud_flags` + `events` for the SSE wire format (flag_id, event_id, user_id, amount, currency, merchant, rule_name, rule_value, flagged_at).
- `db.GetRecentFraudEvents(limit int)` — returns the `limit` most recent fraud events (JOIN fraud_flags + events, DESC order) for cold-load replay on SSE connect.
- `db.GetFraudEventsSince(since time.Time)` — returns fraud events with `flagged_at > since` (ASC order) for incremental polling.
- `GET /fraud-events` SSE endpoint on the query service (`:8083`): replays last N events on connect, then polls every 2s for new flags. CORS enabled (`*`), `?limit=N` param (default 50, max 500), `X-Accel-Buffering: no` for nginx compatibility.

### Verified (2026-05-31 — Trifecta Step 4 console e2e CLOSED)
- Full ops-console chain proven end-to-end against a live HELD transaction: `bankops POST → Fluxa gRPC FLAG → 202/HELD → P1 SupportCase → SSE fraud feed (matching correlation_id) → console release under CORS → RELEASED`. Fluxa needed no changes; three bankops-side blockers were fixed in the `bankops-portal` repo: security `PathPattern` 500 on `/accounts/**`, missing CORS for the `:3001` console origin, and no local seed data (added a `@Profile("local")` seeder so account 1 survives restarts).

### Added
- **Trifecta Step 2 — bankops-portal now calls Fluxa fraud-eval (2026-05-27).** Implementation lives in the separate `bankops-portal` repo; this changelog tracks the cross-cutting integration. Highlights:
  - Proto vendored into `bankops-portal/backend/src/main/proto/fraud/v1/`; `protobuf-maven-plugin` generates Java stubs at `com.fluxa.fraud.v1.*` (gRPC 1.68.1, protobuf 3.25.5).
  - New `com.bankops.portal.client.fluxa` package: sealed `FluxaEvalOutcome` (6 variants), `FluxaFraudClient`, `FluxaClientConfiguration`, `FluxaUnavailableException`, `FraudFlagDto`.
  - `TransactionService` rewired so the Fluxa eval lifts above the optimistic-lock retry (one event per logical transaction); balance mutation reorders after transaction save so the FLAG branch leaves no dirty `Account` entity.
  - FLAG → `Transaction.TransactionStatus.HELD` (new enum value) + auto-created HIGH-severity `SupportCase` with P1 SLA override (the existing `CaseService.createCase` hardcodes P2).
  - UNAVAILABLE/DEADLINE_EXCEEDED routed through directional FAIL_OPEN/FAIL_CLOSED policy; `FluxaUnavailableException` → HTTP 503 via `GlobalExceptionHandler`.
  - 12 new tests green: `FluxaFraudClientTest` (Mockito 6), `FraudGateIntegrationTest` (in-process gRPC 5), `ShadowModeFraudGateIntegrationTest` (separate `shadow-mode=true` context 1).
  - Cross-repo proto sync script `bankops-portal/scripts/sync-proto.sh` (developer-only).
- **Synchronous gRPC fraud-eval surface** (`fraud-grpc` service): new Go service at `:9095` (metrics `:9096`) implementing `fluxa.fraud.v1.FraudEval/EvaluateTransaction`. Reuses the existing `fraud.Engine` + `db.Client` against the committed proto contract in `proto/fraud/v1/fraud_eval.proto`. Step 1 of the trifecta build sequence.
  - New package `internal/fraudeval/` (server impl + bufconn-backed integration tests covering Allow, three single-rule FLAG paths, MultipleFlags, idempotent re-insert, four `InvalidArgument` paths, latency-reported, and a no-DB proto↔domain unit test).
  - New entrypoint `services/fraud-grpc/main.go` + `services/fraud-grpc/Dockerfile`; gRPC reflection registered (plaintext-only, localhost; no TLS in Step 1 per `docs/specs/integration-bankops-fluxa.md`); SIGINT/SIGTERM → `GracefulStop` with 10s force-stop fallback.
  - Generated stubs `internal/grpc/fraud/v1/fraud_eval.pb.go` + `fraud_eval_grpc.pb.go` checked in (so downstream consumers don't need `protoc`).
  - New Prometheus metrics: histogram `fraud_eval_latency_seconds{service}` + counter `fraud_flags_grpc_total{rule}` (separate from the async pipeline's `fraud_flags_total` to avoid breaking existing registrations).
  - New `docker-compose.yml` block `fraud-grpc` (depends only on `postgres`; mounts `rules.yaml`); new Prometheus scrape job `fluxa-fraud-grpc` targeting `fraud-grpc:9096`.
  - New `Makefile` targets: `proto-tools` (installs `protoc-gen-go@v1.35.2` + `protoc-gen-go-grpc@v1.5.1`, both pinned to versions compatible with Go 1.22), `grpc-tools` (macOS Homebrew install of `grpcurl`), `proto` (codegen, guarded), `k6-fraud` (runs the SLO script).
  - New k6 script `scripts/k6/fraud_grpc_p99.js`: 500 RPS for 30s with a `p(99)<50` threshold gate.
- `docs/PORTFOLIO_NARRATIVE.md` — strategic plan for the fintech infrastructure trifecta (`fluxa` + `bankops-portal` + `fluxguard`). Includes existing-asset inventory, 8-step build sequence with time-boxes, pre-written resume bullets with bracketed measurement targets, honest tradeoffs, and stop conditions. Also captures the `bankops-portal/backend/src` audit result (verdict: production-shape — Spring Boot 3.2, optimistic-lock retry on withdrawals, real concurrency tests via `CountDownLatch`/`ExecutorService`, 12 test classes including resilience and idempotency suites). Integration plan for step 2 (Fluxa gRPC fraud-eval called from `TransactionService`) confirmed additive — no rewrites required.

### Changed
- `go.mod`: added direct deps `google.golang.org/grpc v1.69.4` (last release supporting Go 1.22) and promoted `google.golang.org/protobuf` from indirect → direct.
- `internal/adapters/prometheus/metrics.go`: registered the two new metrics above.
- Divergence from `docs/specs/integration-bankops-fluxa.md`: toolchain is `protoc` direct, not `buf` — single `.proto`, single Go consumer, no breaking-change CI needed.
- `docs/plan.md` "Next" section reframed: trifecta is now the major initiative, with tactical items (README badges, demo GIF) reclassified as smaller-scope follow-ons that hold regardless of the trifecta path.
- `internal/adapters/prometheus/metrics.go`: documented `NewMetrics` as non-idempotent on the global default Prometheus registry (calling twice in one process panics).
- `internal/fraudeval/server_test.go`: shared `Metrics` instance via `sync.Once` helper so package tests stay collision-free.

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
