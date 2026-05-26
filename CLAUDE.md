# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Before Writing Any Code

1. Read `docs/plan.md` — current status, In Progress, Next.
2. Read `docs/changelog.md` — what changed and why.

After any significant change, move completed items in `docs/plan.md` and add an entry to `docs/changelog.md` (under `[Unreleased]` or a new dated section).

## Commands

The full stack runs via Docker Compose. The Makefile is the canonical entry point.

```bash
make up        # docker compose up -d --build (postgres, rabbitmq, minio, prometheus, grafana, ingest, processor, query, alert-consumer)
make down      # docker compose down (also tears down the replay profile)
make replay    # docker compose --profile replay up -d replay  (requires ./data/transactions.csv — IEEE-CIS train_transaction.csv renamed)
make logs      # follow logs for all services
make ps        # container status
make test      # go test -v -race ./...   (DB-touching tests require `make up` first — no DB mocks)
make lint      # golangci-lint run ./...
make clean     # docker compose down -v + remove coverage artifacts
```

Run a single Go test:

```bash
go test -v -race -run TestName ./internal/processor
go test -v -race -run TestName/subtest_name ./internal/fraud
```

After editing `rules.yaml`, restart only the processor to pick up new rules — no rebuild needed:

```bash
docker compose restart processor
```

Service ports: ingest `:8080` (metrics `:9091`), query `:8083` (metrics `:9093`), processor metrics `:9092`, alert-consumer metrics `:9094`, Postgres `:5432`, RabbitMQ `:5672` / mgmt `:15672` (fluxa/fluxa_pass), MinIO `:9000` / console `:9001` (minioadmin/minioadmin123), Prometheus `:9090`, Grafana `:3000` (admin/admin).

## Architecture

Fluxa is a local fraud-detection pipeline. The flow is: **CSV replay → ingest (HTTP) → RabbitMQ events queue → processor → Postgres + alerts fanout → alert-consumer + query service**. Prometheus scrapes every service; Grafana renders a pre-provisioned dashboard.

### Code layout that requires context across files

- `internal/ports/` — interfaces (`Publisher`, `Consumer`, `Delivery`, `Storage`, `Metrics`). Service `main.go` files compose adapters that satisfy these.
- `internal/adapters/{rabbitmq,minio,prometheus}` — concrete implementations. Swap here, not in the call sites.
- `internal/domain/` — `Event`, `FraudFlag`, `QueueMessage`, `RulesConfig`, and the **error taxonomy** in `errors.go` (`NonRetryableError` vs `RetryableError`). This taxonomy is load-bearing — see "Error classification" below.
- `internal/processor/processor.go` — the pipeline. Reads a `QueueMessage`, runs idempotency → fetch payload (inline or MinIO) → SHA-256 verify → unmarshal+validate → insert event → run fraud → publish alerts → mark idempotency success. The control flow returns `nil` to ACK (including permanent failures via `failPermanent`) and a non-nil error to NACK for broker retry.
- `internal/fraud/engine.go` — loads `rules.yaml` once at startup, evaluates **all** rules per event (all-match, not first-match), returning a slice of `FraudFlag`. The engine depends on a narrow `VelocityQuerier` interface, not `*db.Client`, so it stays unit-testable.
- `internal/idempotency/` — `SELECT FOR UPDATE` on `idempotency_keys` + `ON CONFLICT DO NOTHING` on `events`. The race-condition fix (lock expiry + retry) is documented in `docs/plan.md`.
- `internal/db/db.go` — single Postgres client. New DB queries go here unless they require a new client type.
- `services/{ingest,processor,query,replay,alert-consumer}/main.go` — thin entrypoints that wire config → adapters → handler/loop. Each has its own Dockerfile built from the repo root.
- `migrations/` — numbered SQL, auto-applied by Postgres on first boot via `/docker-entrypoint-initdb.d`. Tables: `events`, `idempotency_keys`, `fraud_flags`.

### Message contract & reliability invariants

- `QueueMessage` carries either `PayloadInline` (small) or an `S3Key` into MinIO (events >256 KB). Always carries `PayloadSHA256` — the processor recomputes and rejects mismatches as non-retryable.
- Alerts are published to a RabbitMQ **fanout exchange** (`alerts`), not a routing key; the alert-consumer subscribes to that exchange.
- Fraud evaluation is **best-effort**: the event is already persisted before rules run, so fraud errors are logged but never propagated back to the queue.

## Style and Conventions

- **Error classification**: `domain.NonRetryableError` → ACK + discard (poison). All other errors → NACK + requeue. Use the constructors (`NewNonRetryableError`, `NewRetryableError`) — do not return bare errors from `process()`.
- **Logging**: structured JSON via `logging.NewLogger()`. Use fields `stage`, `event_id`, and `latency_ms` where applicable.
- **DB calls**: wrap with `context.WithTimeout(ctx, 5*time.Second)`.
- **New DB operations** go in `internal/db/db.go` unless they need a new client type.
- **Tests that touch Postgres must be real integration tests** against the local DB — no mocks. CI spins up a Postgres service and runs migrations.
- **Commits**: no `Co-authored-by` lines.
- Do not add docstrings, comments, or type annotations to code that wasn't changed.
- Do not add error handling for scenarios that cannot happen; trust framework guarantees.
