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

## Next

After Step 2 the **fintech infrastructure trifecta** continues with **Step 3**: frontend HELD badge + ops-action UI + HELD → RELEASED/REJECTED state-machine transitions. Full sequence in **[`docs/PORTFOLIO_NARRATIVE.md`](PORTFOLIO_NARRATIVE.md)**.

Open Questions deferred from Step 2 plan: (a) should `CreateTransactionRequest` gain a `merchant` field so Fluxa's `blocked_merchant` rule can fire? (b) Should shadow-mode swallow `InvalidArgument` (current) or surface 400 even in observer mode?

Tactical, smaller items that remain valid regardless of the trifecta path:
- [ ] **README badges** — build status, Go version, license badges
- [ ] **Portfolio polish** — architecture diagram image, demo GIF, concise project summary for portfolio/LinkedIn
- [ ] _(Optional)_ Add Grafana panel comparing rules-detected fraud vs. `is_fraud_ground_truth` label (precision/recall overlay)
