# Fluxa — Project Status Tracker

_Last updated: 2026-05-27 (Step 2 complete)_

---

## What Fluxa Is

Fluxa is a **local financial transaction fraud detection platform** that runs entirely via Docker Compose on an 8 GB Mac. It streams transaction data through a multi-service Go pipeline, applies configurable fraud rules in real time, and surfaces results in Grafana — no cloud credentials required.

**Pipeline:**
```
CSV replay (200 req/s)
    │
    ▼
POST :8080/events          GET :8083/events/:id
   ingest-svc  ──────────────────  query-svc
        │                               │
        │ RabbitMQ (events queue)        │ PostgreSQL
        ▼                               │
  processor-svc  ─────────────────────►─┘
        │   persist + fraud rules
        │   write fraud_flags table
        │
        │ RabbitMQ (alerts fanout)
        ▼
  alert-consumer  (logs FRAUD ALERT to stdout)

Prometheus :9090   Grafana :3000
```

---

## Completed

### Platform Foundation (2026-01-03)
- [x] Core domain models (`Event`, `FraudFlag`, `QueueMessage`, error types)
- [x] Structured JSON logging (`internal/logging`)
- [x] Environment-based config (`internal/config`)
- [x] PostgreSQL client with 3 schema migrations (events, idempotency_keys, fraud_flags)
- [x] Distributed idempotency via `SELECT FOR UPDATE` + `ON CONFLICT DO NOTHING`
- [x] MinIO (S3-compatible) adapter for large payloads >256 KB
- [x] All service entrypoints wired (`ingest`, `processor`, `query`, `replay`, `alert-consumer`)

### Reliability & Hardening (2026-01-03 → 2026-01-05)
- [x] Explicit error classification: `NonRetryableError` (ACK) / retryable (NACK + requeue)
- [x] SHA-256 hash verification before persist; mismatch → non-retryable discard
- [x] Strict schema validation and poison message handling in processor
- [x] Idempotency race condition fixed (concurrency lock expire + retry logic)
- [x] Integration tests against real PostgreSQL (no DB mocks)
- [x] Non-retryable error path verified by test suite

### Observability & CI (2026-01-03 → 2026-01-05)
- [x] Prometheus metrics on all services (ports 9091–9094)
- [x] GitHub Actions CI with PostgreSQL service and migration runner
- [x] Automated metrics capture script
- [x] SQL reporting artifacts for operational visibility

### AWS → Local Platform Migration (2026-04-14)
- [x] Replaced SQS + Lambda + RDS + S3/CloudWatch with Docker Compose stack
- [x] RabbitMQ as message broker (events queue + alerts fanout exchange)
- [x] MinIO as local S3-compatible object store
- [x] Prometheus + Grafana auto-provisioned at startup
- [x] YAML-driven fraud rules engine (`rules.yaml`, hot-reload via container restart)
  - Amount threshold rule
  - Velocity check (count per user per window)
  - Blocked merchants list
  - High-risk currencies list
- [x] Grafana dashboard auto-provisioned (traffic, fraud rate, DB-direct panels)
- [x] PostgreSQL datasource and UID wiring fixed in Grafana provisioning
- [x] 7-step local verification passing end-to-end
- [x] README updated for local platform; `.claude/` gitignored

---

## In Progress

### IEEE-CIS Replay Integration
- [x] Spec written and approved: `docs/specs/ieee-cis-replay.md`
- [x] Updated `services/replay/main.go`: replaced PaySim mapper with IEEE-CIS mapper
  - Column mapping: `TransactionAmt` → amount, `card1` → user_id, `TransactionDT` → timestamp
  - Merchant derivation from `ProductCD` + `card4` lookup table (8 entries + default)
  - Metadata: `P_emaildomain`, `card4`, `ProductCD`, `is_fraud_ground_truth`
- [x] Updated README: replaced PaySim dataset link with IEEE-CIS Kaggle link; updated file rename instructions and column notes
- [x] **End-to-end verified**: all three fraud rules firing on live IEEE-CIS data
  - `amount_threshold`: ~4% of transactions flagged (16k+ flags)
  - `blocked_merchant`: Amazon Marketplace, Walmart Online, Target firing
  - `velocity`: confirmed via test — 3 hits in 60s crosses threshold
- [x] Grafana dashboard fully operational — all 9 panels showing data
  - Fixed Prometheus fraud rate gauge (label mismatch — added `sum()`)
  - Fixed all Postgres panels (added `rawQuery: true`)
  - Fixed time range conflict (`now-3h` for Prometheus; hardcoded `2024-01-01` for Postgres panels)
- [x] `CountRecentEvents` bug fixed: switched from `ts` to `created_at` so velocity check works on replay data with historical timestamps
- [x] `rules.yaml` tuned for IEEE-CIS: `amount_threshold: 500`, `velocity_window_seconds: 60`, `velocity_max_count: 3`, real merchant names
- [x] Grafana dashboard screenshot saved to `docs/grafana-dashboard.png`

---

### Trifecta Step 1 — Synchronous gRPC Fraud-Eval Surface (2026-05-29, DoD passed)
- [x] `protoc-gen-go` + `protoc-gen-go-grpc` toolchain wired (`make proto-tools` / `make proto`)
- [x] Generated Go stubs at `internal/grpc/fraud/v1/`
- [x] Server impl `internal/fraudeval/server.go` (proto↔domain, status-code mapping, logging interceptor)
- [x] Entrypoint `services/fraud-grpc/main.go` + `Dockerfile` (gRPC reflection, graceful shutdown)
- [x] `docker-compose.yml` + `prometheus.yml` updated for new service
- [x] Integration tests `internal/fraudeval/server_test.go` (bufconn, 11 test functions; DB-backed tests skip cleanly without `TEST_DB_DSN`)
- [x] k6 SLO script `scripts/k6/fraud_grpc_p99.js` (500 RPS, p99 < 50ms threshold)
- [x] Static checks pass: `gofmt -l .` empty, `golangci-lint run ./...` clean, `go build ./...` clean
- [x] **DoD gate passed (2026-05-29)**: 39 tests, 0 skips, 0 fails; `grpcurl` ALLOW + FLAG verified against `rules.yaml` (`amount_threshold=500`); `make k6-fraud` p99=25.79ms @ 500 RPS (gate <50ms)

### Trifecta Step 2 — bankops-portal calls Fluxa fraud-eval (2026-05-27)

Implementation lives in the separate `bankops-portal` repo (see its `STATUS.md` and new `CLAUDE.md` for the full artefact list). Highlights:

- [x] Proto vendored into `bankops-portal/backend/src/main/proto/fraud/v1/`; `protobuf-maven-plugin` + `os-maven-plugin` generate `com.fluxa.fraud.v1.*` stubs (gRPC 1.68.1, protobuf 3.25.5)
- [x] New `com.bankops.portal.client.fluxa` package — sealed `FluxaEvalOutcome` (6 variants), `FluxaFraudClient`, `FluxaClientConfiguration`, `FluxaUnavailableException`, `FraudFlagDto`
- [x] `FluxaProperties` config (`bankops.fluxa.*`); profile overlays `application-{local,test,prod}.yaml` with directional FAIL_OPEN/FAIL_CLOSED policy
- [x] `Transaction.TransactionStatus.HELD` added; `CaseService.createForFraud` builds HIGH-severity P1 case
- [x] `TransactionService.{withdrawWithOptimisticRetry, withdrawOnce, createDeposit}` rewired: idempotency pre-check + Fluxa eval lifted above optimistic-lock retry; balance mutation reordered after tx-save on withdraw; `dispatchFluxaOutcome` handles all six outcomes
- [x] `TransactionController` returns 202 for HELD; `GlobalExceptionHandler` maps `FluxaUnavailableException` → 503
- [x] Tests green: `FluxaFraudClientTest` 6/6 (Mockito), `FraudGateIntegrationTest` 5/5 (`@SpringBootTest` + in-process gRPC), `ShadowModeFraudGateIntegrationTest` 1/1 (separate context with `shadow-mode=true`)
- [x] Fixed pre-existing `AuditEventService.recordEvent` propagation MANDATORY → REQUIRED (unblocked the legacy `TransactionIntegrationTest` as a side-effect)
- [x] `bankops-portal/CLAUDE.md` written from scratch (integration entrypoint, DoD, reliability invariants)
- [ ] Manual demo (requires fluxa stack up): `curl -X POST localhost:8080/api/accounts/1/transactions … {"type":"DEPOSIT","amount":99999,…}` → expect 202 + HELD + HIGH-severity SupportCase

### Trifecta Step A — e2e verification (2026-05-30, CLOSED)

Full chain proven: `bankops POST(99999) → Fluxa gRPC (26ms, OK) → HELD → P1 SupportCase`. All four bugs were bankops-side; Fluxa needed zero changes. See memory for details.

### Trifecta Step 4 (Console) — SSE fraud feed + ops console (2026-05-31, e2e CLOSED)

- [x] `domain.FraudEvent` — joined view of `fraud_flags` + `events`
- [x] `db.GetRecentFraudEvents(limit)` — replay on connect (DESC order, newest first)
- [x] `db.GetFraudEventsSince(since)` — 2s poll for new events (ASC order)
- [x] `GET /fraud-events` SSE handler in query service (`:8083`) — CORS enabled, `?limit=N` (default 50, max 500), replays history then polls
- [x] Integration tests for the new DB methods (6 tests, see `d5240eb`)
- [x] `trifecta-console` Next.js repo scaffold (separate repo; 5 screens incl. Fraud Review Center)
- [x] Fraud Feed tab wired to `GET :8083/fraud-events`

**e2e CLOSED (2026-05-31):** full console chain proven against a live HELD txn —
`bankops POST(47500) → Fluxa gRPC FLAG (24ms) → 202/HELD → P1 SupportCase → SSE feed (matching correlation_id) → console release (CORS, Origin :3001) → RELEASED`.
Three bankops-side blockers surfaced and fixed in the `bankops-portal` repo (Fluxa needed zero changes): security `PathPattern` 500 on `/accounts/**` (mid-`**` illegal in Spring `PathPattern`), missing CORS for the `:3001` console origin, and no local seed data (in-memory H2 wiped account 1 on every restart → added `@Profile("local")` `LocalDataSeeder`).

### Trifecta Step 5a (ML scorer core) — 2026-05-31, DONE & VERIFIED

Spec `docs/specs/2026-05-31-ml-scorer-design.md` (brainstormed + 2-round critique/patch loop → converged). Plan `docs/plans/2026-05-31-ml-scorer-5a.md`.

- [x] `mlfeatures` builder + `events(user_id, ts)` index + ts point-in-time DB queries
- [x] `cmd/export-features` (shared builder → CSV) ; IEEE-CIS replay → 216k labeled rows
- [x] `ml/train.py` XGBoost → ONNX (parity to **3.4e-8**), `ml/evaluate.py` → `docs/ML_EVALUATION.md`
- [x] `services/ml-scorer` Python ONNX gRPC service (`:9097`) + Go client adapter
- [x] `fraud.Engine.EvaluateWithScorer` blend + **fail-open**; wired into fraud-grpc + processor; `EvaluateResponse.ml_score`
- [x] Verified live: `ml_score` flows, `evaluated_by=…+ml-v1`, 7–20ms warm, cold-start fails open; integration suite green (0 SKIP)

**Honest result (H1/H15):** PR-AUC 0.132 (CI 0.116–0.151) on serve-parity features — modest by design; amount-only ablation 0.043 confirms real lift. Full-data retrain (full replay → `make export-features train`) is the documented quality lever (zero code change).

**Step 5b — DONE (2026-05-31):** `ml_score` persisted on `fraud_flags` (migration `005`) → carried through `domain.FraudFlag`/`FraudEvent`/`AlertMessage` → stamped by processor + fraud-grpc → SSE `/fraud-events` wire format → `trifecta-console` fraud feed renders an **ML** risk column (commit `8c90050`). Verified: SSE emits `"ml_score"` on freshly-scored flags.

### Trifecta Step 6a (fluxa-internal OTel tracing) — 2026-06-01, DONE & VERIFIED

Plan `docs/plans/2026-06-01-otel-tracing-6a.md` (brainstorm spec → 2-round critique/patch loop → READY → implemented).

- [x] `jaeger` all-in-one in compose (OTLP `:4317`/`:4318`, UI `:16686`)
- [x] `internal/observability` shared fail-open OTel init (OTLP/gRPC exporter + W3C propagator + shutdown flush; TDD unit test)
- [x] `fraud-grpc` server + Go scorer client instrumented with otelgrpc StatsHandlers (W3C `traceparent` propagation)
- [x] Python `ml-scorer` OTel SDK + grpc server interceptor + explicit W3C propagator + manual `Score` span
- [x] OTel Go deps pinned for Go 1.22 (`otel` v1.34.0 / `otelgrpc` v0.59.0); Python deps pinned (`opentelemetry-sdk` 1.29.0 / instrumentation-grpc 0.50b0)
- [x] **Verified live:** connected Go→Python trace in Jaeger (`EvaluateTransaction` → `Scorer/Score` client → `ml-scorer Scorer/Score` server → `Score`); fail-open holds with jaeger down; integration suite 0 SKIP; gofmt + golangci-lint clean.

Remaining Step 6: **6b** (bankops Spring OTel + `traceparent` over the bankops→fluxa gRPC — needs the bankops session) and **6c** (console browser spans, per-hop p50/p95/p99 Grafana dashboard, `BENCHMARKS.md`).

## Next

Decision agreed with bankops (2026-05-31): **Option B — new `trifecta-console` Next.js standalone repo** covering fraud feed (SSE from Fluxa), bank ops actions (REST → bankops), rate-limit telemetry (Prometheus → fluxguard). ~5-6 screens, ~1 week target. Step 5 (ML scorer) planning parallel, no implementation until console MVP is demoed.

Open Questions deferred from Step 2 plan: (a) should `CreateTransactionRequest` gain a `merchant` field so Fluxa's `blocked_merchant` rule can fire? (b) Should shadow-mode swallow `InvalidArgument` (current) or surface 400 even in observer mode?

Tactical, smaller items that remain valid regardless of the trifecta path:
- [ ] **README badges** — build status, Go version, license badges
- [ ] **Portfolio polish** — architecture diagram image, demo GIF, concise project summary for portfolio/LinkedIn
- [ ] _(Optional)_ Add Grafana panel comparing rules-detected fraud vs. `is_fraud_ground_truth` label (precision/recall overlay)
