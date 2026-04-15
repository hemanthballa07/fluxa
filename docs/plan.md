# Fluxa — Project Status Tracker

_Last updated: 2026-04-15_

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

## Next

- [ ] **README badges** — build status, Go version, license badges
- [ ] **Portfolio polish** — architecture diagram image, demo GIF, concise project summary for portfolio/LinkedIn
- [ ] _(Optional)_ Add Grafana panel comparing rules-detected fraud vs. `is_fraud_ground_truth` label (precision/recall overlay)
