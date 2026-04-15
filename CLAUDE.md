# Claude Code Instructions — Fluxa

## Before Writing Any Code

1. Read `docs/plan.md` — understand current project status, what's in progress, and what's next.
2. Read `docs/changelog.md` — understand what has changed and when.

## After Any Significant Change

Update both files:
- `docs/plan.md` — move completed items to the Completed section; update In Progress and Next accordingly.
- `docs/changelog.md` — add an entry under `[Unreleased]` or a new dated section describing what changed and why.

## Style and Conventions

- Error classification: `models.NonRetryableError` (ACK + discard) / retryable errors (NACK + requeue).
- Structured logging via `logging.NewLogger()` with `stage`, `event_id`, and `latency_ms` fields.
- DB calls must use `context.WithTimeout(ctx, 5*time.Second)`.
- New DB operations go in `internal/db/db.go` unless they require a new client type.
- Tests must be integration tests against a real DB when they touch PostgreSQL — no mocks for the DB layer.
- No co-authored-by lines in commits.
- Do not add docstrings, comments, or type annotations to code that wasn't changed.
- Do not add error handling for scenarios that cannot happen; trust framework guarantees.
