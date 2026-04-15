# Fluxa — Project Status Tracker

_Last updated: 2026-04-14_

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
- [ ] Verify end-to-end: fraud flags accumulate in Grafana during replay

---

## Next

- [ ] **Replay verified** — confirm IEEE-CIS replay drives fraud flags in Grafana as expected
- [ ] **README badges** — build status, Go version, license badges
- [ ] **Portfolio polish** — architecture diagram image, demo GIF or screenshot, concise project summary for portfolio/LinkedIn
- [ ] _(Optional)_ Forward `isFraud` ground-truth label in metadata for precision/recall analysis
- [ ] _(Optional)_ Tune fraud rules thresholds against IEEE-CIS distribution (e.g., amount threshold calibration)
