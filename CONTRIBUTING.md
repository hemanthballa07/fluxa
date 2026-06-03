# Contributing to Fluxa

Fluxa runs entirely locally via Docker Compose — no cloud credentials required.

## Prerequisites

- Docker + Docker Compose
- Go 1.22+ (to run tests/linters outside containers)
- (Optional) the IEEE-CIS Fraud Detection dataset for replay — see the README

## Local development

```bash
make up        # build + start the full stack
make logs      # follow logs
make ps        # container status
make down      # stop everything
```

After editing `rules.yaml`, restart only the processor (no rebuild needed):

```bash
docker compose restart processor
```

## Tests

```bash
make test      # go test -v -race ./...
```

DB integration tests skip silently unless `TEST_DB_DSN` is set. To run the full suite
with **zero skips**:

```bash
make up
TEST_DB_DSN=postgres://fluxa_user:fluxa_password@localhost:5432/fluxa?sslmode=disable make test
```

## Definition of Done

A change is done when:

- Integration tests pass with `TEST_DB_DSN` set — **zero `--- SKIP:` lines**
- `golangci-lint run ./...` is clean (`make lint`)
- `gofmt -l .` is empty
- `docs/changelog.md` is updated

## Conventions

- **Error classification:** return `domain.NonRetryableError` (ACK + discard) vs
  `domain.RetryableError` (NACK + requeue) via the constructors — never bare errors
  from `process()`.
- **Logging:** structured JSON via `logging.NewLogger()`; use fields `stage`,
  `event_id`, `latency_ms` where applicable.
- **DB calls:** wrap with `context.WithTimeout(ctx, 5*time.Second)`.
- New DB queries go in `internal/db/db.go`; new adapters in `internal/adapters/`.
- Commits: no `Co-authored-by` lines.
