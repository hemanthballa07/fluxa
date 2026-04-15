# Fluxa

A local financial transaction fraud detection platform. Streams PaySim transaction data through a multi-service pipeline, applies configurable fraud rules in real time, and surfaces results in Grafana.

No cloud credentials required — runs entirely via Docker Compose on an 8 GB Mac.

## Architecture

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

| Service | Port(s) | Description |
|---------|---------|-------------|
| ingest | 8080, 9091 | HTTP ingest + Prometheus metrics |
| processor | 9092 | RabbitMQ consumer, fraud engine |
| query | 8083, 9093 | HTTP query + Prometheus metrics |
| alert-consumer | 9094 | Logs fraud alerts from RabbitMQ fanout |
| replay | — | Streams PaySim CSV at ~200 req/s |
| postgres | 5432 | Events, idempotency keys, fraud flags |
| rabbitmq | 5672, 15672 | Message broker + management UI |
| minio | 9000, 9001 | S3-compatible storage for large payloads |
| prometheus | 9090 | Metrics scraper |
| grafana | 3000 | Dashboard (admin/admin) |

## Quick Start

```bash
# 1. Start everything
make up

# 2. Smoke test
curl http://localhost:8080/health   # {"status":"ok"}
curl http://localhost:8083/health   # {"status":"ok"}

# 3. Ingest an event
curl -X POST http://localhost:8080/events \
  -H "Content-Type: application/json" \
  -d '{"user_id":"u1","amount":50.00,"currency":"USD","merchant":"Amazon","timestamp":"2026-01-01T12:00:00Z"}'

# 4. Open Grafana
open http://localhost:3000   # admin/admin → Dashboards → Fluxa Overview
```

## Fraud Detection Replay

Download the [IEEE-CIS Fraud Detection dataset](https://www.kaggle.com/c/ieee-fraud-detection/data) from Kaggle, rename `train_transaction.csv` to `transactions.csv`, and place it at `./data/transactions.csv`, then:

```bash
make replay
```

Streams ~590 000 transactions at 200 req/s. Watch fraud flags accumulate in Grafana in real time.

Key columns used: `TransactionAmt` (amount), `card1` (user ID), `TransactionDT` (timestamp offset), `ProductCD` + `card4` (merchant derivation), `P_emaildomain`, `isFraud` (ground-truth label forwarded as metadata).

## Fraud Rules

Rules are loaded from `rules.yaml` at processor startup — edit and restart the processor container, no rebuild needed:

```yaml
amount_threshold: 10000.00        # flag transactions above this USD amount
velocity_window_seconds: 300      # velocity check window
velocity_max_count: 10            # max transactions per user in window
blocked_merchants:                # exact-match merchant names
  - "FraudMerchant1"
high_risk_currencies:             # flag these currency codes
  - "XMR"
  - "ZEC"
```

All rules are evaluated independently (all-match, not first-match).

## API

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/events` | Ingest a transaction event → `202 {"event_id":"…","status":"enqueued"}` |
| `GET` | `/events/:id` | Retrieve a persisted event → `200` or `404` |
| `GET` | `/health` | Liveness check → `{"status":"ok"}` |
| `GET` | `/metrics` | Prometheus scrape endpoint (on port 9091–9094) |

## Makefile

```bash
make up       # Build images and start all services
make down     # Stop and remove containers
make replay   # Start dataset replay (requires ./data/transactions.csv)
make logs     # Follow logs for all services
make ps       # Show container status
make test     # Run Go unit tests
make lint     # Run golangci-lint
make clean    # Stop containers and remove volumes
```

## Observability

**Prometheus metrics** (scraped every 15s from each service):

| Metric | Type | Description |
|--------|------|-------------|
| `events_ingested_total` | Counter | Accepted ingest requests |
| `events_processed_total{status}` | Counter | Processor outcomes (success/failure) |
| `fraud_flags_total{rule}` | Counter | Fraud flags by rule name |
| `query_total{status}` | Counter | Query outcomes |
| `alerts_consumed_total` | Counter | Alerts consumed |
| `ingest_latency_seconds` | Histogram | End-to-end ingest latency |
| `process_latency_seconds` | Histogram | Per-message processor latency |

**Grafana dashboard** (auto-provisioned at startup):
- Row 1 — Traffic: ingested rate, processed rate, p99 latency
- Row 2 — Fraud: fraud rate %, flags by rule (bar), flags over time (line)
- Row 3 — DB-direct: top flagged merchants, tx volume (5-min buckets), active flags by rule (pie)

## Reliability

- **Idempotency** — `SELECT FOR UPDATE` on `idempotency_keys` + `ON CONFLICT DO NOTHING` on `events`
- **Hash verification** — SHA-256 checked before persisting; mismatch → non-retryable, message ACKed and discarded
- **Error classification** — `NonRetryableError` → ACK; all other errors → NACK with requeue
- **Large payloads** — events >256 KB are stored in MinIO; inline reference in RabbitMQ message

## Project Structure

```
fluxa/
├── services/               Long-running Go services
│   ├── ingest/             HTTP ingest server (:8080)
│   ├── processor/          RabbitMQ consumer + fraud engine
│   ├── query/              HTTP query server (:8083)
│   ├── replay/             PaySim CSV streamer
│   └── alert-consumer/     Fraud alert logger
├── internal/
│   ├── ports/              Publisher, Consumer, Storage, Metrics interfaces
│   ├── adapters/           RabbitMQ, MinIO, Prometheus implementations
│   ├── domain/             Event, FraudFlag, QueueMessage, errors
│   ├── fraud/              Rules engine (YAML-driven, all-match)
│   ├── config/             Environment-based config
│   ├── db/                 PostgreSQL client
│   ├── idempotency/        Exactly-once processing
│   └── logging/            Structured JSON logger
├── migrations/             001 events, 002 idempotency_keys, 003 fraud_flags
├── deploy/
│   ├── prometheus/         Scrape config
│   └── grafana/            Dashboard JSON + auto-provisioning
├── rules.yaml              Fraud rules (hot-reload via container restart)
└── docker-compose.yml      Full local stack
```

## License

MIT
