# Spec: Local Fraud Detection Platform

**Status:** Implemented
**Author:** Hemanth Balla
**Date:** 2026-04-14

## Problem

Fluxa was a production-quality AWS-serverless event pipeline (API Gateway → Lambda → SQS → PostgreSQL) with no path to deployment due to zero AWS credits. The codebase had strong invariants — idempotency, hash verification, structured error classification — but no way to run or demonstrate them locally.

## Chosen Approach

**Clean Service Restructure** — introduce `internal/ports/` interfaces, `internal/adapters/` implementations, rename `models/` → `domain/`, and replace `cmd/` Lambda handlers with `services/` long-running Go services. Each AWS service is replaced with a local equivalent that satisfies the same interface contract.

| AWS service | Local replacement |
|---|---|
| SQS | RabbitMQ (amqp091-go) |
| S3 | MinIO (minio-go/v7) |
| Lambda | Long-running Go HTTP servers + worker goroutines |
| API Gateway | `net/http` multiplexer |
| CloudWatch EMF | Prometheus (client_golang) + Grafana |
| Secrets Manager | Plain env vars |

## Data Model

**New table: `fraud_flags`** (migration 003)

```sql
fraud_flags (
    flag_id    VARCHAR(255) PRIMARY KEY,
    event_id   VARCHAR(255) NOT NULL REFERENCES events(event_id),
    user_id    VARCHAR(255) NOT NULL,
    rule_name  VARCHAR(100) NOT NULL,   -- amount_threshold | velocity | blocked_merchant | high_risk_currency
    rule_value TEXT         NOT NULL,   -- human-readable explanation
    flagged_at TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP
)
```

Indexes: `flagged_at DESC`, `rule_name`, `user_id`, composite `(user_id, flagged_at DESC)`.

**New domain types:**
- `domain.QueueMessage` — replaces `SQSEventMessage`; drops `S3Bucket` (bucket is config)
- `domain.FraudFlag`, `domain.AlertMessage` — fraud evaluation output
- `domain.RulesConfig` — maps to `rules.yaml`

## API Contract

| Endpoint | Service | Port | Description |
|---|---|---|---|
| `POST /events` | ingest | 8080 | Accept transaction event → 202 Accepted |
| `GET /events/{id}` | query | 8083 | Retrieve persisted event → 200 or 404 |
| `GET /health` | ingest, query | 8080, 8083 | Liveness check → `{"status":"ok"}` |
| `GET /metrics` | all services | 9091–9094 | Prometheus scrape endpoint |

## Processing Logic

The processor consumes `QueueMessage` from RabbitMQ `events` queue with `autoAck=false`:

1. **Idempotency check** — `SELECT FOR UPDATE` on `idempotency_keys`; skip if already success
2. **Fetch payload** — inline from message, or `Storage.Get(s3Key)` from MinIO
3. **Hash verification** — SHA-256 mismatch → `NonRetryableError` (ACK + mark failed)
4. **Parse + validate** — JSON unmarshal + `Event.Validate()` → `NonRetryableError` on failure
5. **Persist** — `INSERT INTO events ON CONFLICT DO NOTHING` → `RetryableError` on DB failure
6. **Fraud evaluation** — `fraud.Engine.Evaluate()` runs all rules; any matching flags are:
   - Written to `fraud_flags` table
   - Published to RabbitMQ `alerts` (fanout) exchange
   - Errors are logged but do not abort the pipeline (best-effort)
7. **Mark success** — `MarkSuccess(eventID)` in `idempotency_keys`

Error routing: `NonRetryableError` → `d.Ack()` (message discarded); all other errors → `d.Nack(requeue=true)`.

## Idempotency

Preserved unchanged from the AWS version:
- `SELECT FOR UPDATE` transaction in PostgreSQL prevents concurrent duplicate processing
- `ON CONFLICT DO NOTHING` on `events` INSERT as a DB-level backstop
- `idempotency_keys.status` state machine: `processing → success | failed`
- Stale lock detection: if `last_seen_at < 1 min ago` and status = `processing`, considered safe to retry

## Observability

**Prometheus metrics** (scraped every 15s):

| Metric | Type | Labels |
|---|---|---|
| `events_ingested_total` | Counter | `service` |
| `events_processed_total` | Counter | `service`, `status` |
| `fraud_flags_total` | Counter | `rule` |
| `query_total` | Counter | `status` |
| `alerts_consumed_total` | Counter | — |
| `ingest_latency_seconds` | Histogram | `service` |
| `process_latency_seconds` | Histogram | `service` |

**Grafana dashboard** (`deploy/grafana/dashboards/fluxa-overview.json`):
- Row 1 (Traffic): ingested rate, processed rate, p99 latency
- Row 2 (Fraud): fraud rate %, flags by rule (bar), flags over time (stacked line)
- Row 3 (DB-direct): top flagged merchants, tx volume buckets, active flags by rule

Structured JSON logging is preserved via `internal/logging` throughout.

## Infrastructure

All services run under `docker-compose.yml` at the repo root.

| Container | Image | Port(s) |
|---|---|---|
| postgres | postgres:15-alpine | 5432 |
| rabbitmq | rabbitmq:3.13-management-alpine | 5672, 15672 |
| minio | minio/minio | 9000, 9001 |
| prometheus | prom/prometheus:v2.53.0 | 9090 |
| grafana | grafana/grafana:11.1.0 | 3000 |
| ingest | build | 8080, 9091 |
| processor | build | 9092 |
| query | build | 8083, 9093 |
| alert-consumer | build | 9094 |
| replay | build (profile: replay) | — |

Migrations auto-run via `./migrations:/docker-entrypoint-initdb.d` mount on first postgres start.
Fraud rules are mounted as `./rules.yaml:/app/rules.yaml:ro` into the processor container.

## Migration

- Migrations 001 and 002 are unchanged.
- Migration 003 adds `fraud_flags` table with four indexes.
- The PostgreSQL schema is backward-compatible with the previous version.

## Open Questions

- The Grafana PostgreSQL panels (panels 7–9) require a PostgreSQL datasource to be configured manually in Grafana UI (or via additional provisioning YAML). Prometheus panels auto-provision.
- The `replay` service uses a Docker Compose profile (`profiles: [replay]`) so it does not start by default — run `docker compose --profile replay up -d replay` to start it.
- For the PaySim dataset specifically: merchants named `nameDest` may be user IDs rather than merchant names. The `blocked_merchants` list in `rules.yaml` should be tuned accordingly.
