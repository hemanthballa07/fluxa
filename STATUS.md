# Fluxa — Status

Source of truth for where this project stands. Read this first when resuming work.

## Current phase

Feature-complete local fraud-detection pipeline with an ML scorer and distributed
tracing. Part of the "Trifecta" portfolio (fluxa fraud · bankops-portal ops ·
fluxguard rate limiting), integrated via gRPC + shared observability.

## Done

- **Core pipeline:** CSV replay → ingest (HTTP) → RabbitMQ → processor → Postgres +
  alerts fanout → alert-consumer + query. Idempotency (`SELECT FOR UPDATE` +
  `ON CONFLICT DO NOTHING`), SHA-256 payload verification, MinIO offload for payloads
  >256 KB, retryable/non-retryable error taxonomy.
- **Fraud engine:** YAML-driven rules (`rules.yaml`), all-match evaluation (amount
  threshold, velocity, blocked merchant, high-risk currency). Hot-reload via processor
  restart.
- **SSE fraud feed:** `GET :8083/fraud-events` streams flags with `correlation_id`.
- **gRPC fraud-eval:** `fraud-grpc` service (`:9095`) — synchronous `EvaluateTransaction`
  for the bankops-portal HELD-transaction gate.
- **ML scorer:** XGBoost → ONNX model served by a Python gRPC service (`ml-scorer`);
  `EvaluateWithScorer` blends `FLAG if (any rule) OR (ml_score ≥ τ)`, fail-open to
  rules-only. Train/serve parity via the shared feature builder.
- **Observability:** Prometheus metrics per service + auto-provisioned Grafana
  dashboards (overview + per-hop p50/p95/p99 latency). OpenTelemetry tracing to Jaeger
  (W3C trace-context across Go → Python). See `BENCHMARKS.md` for load behavior.

## In progress

- None — the fluxa side of the roadmap is complete.

## Next

- Latency hardening surfaced by `BENCHMARKS.md`: widen latency histogram buckets and
  cache point-in-time feature aggregates (under load the per-request DB feature-build
  dominates p99, not the ONNX scorer).

## Open decisions

- None currently.

## Reference

- Architecture & design: `docs/ARCHITECTURE.md`, `docs/SYSTEM_DESIGN.md`, `docs/specs/`
- Reliability invariants: `docs/INVARIANTS.md`
- Runbook: `docs/RUNBOOK.md`
- Benchmarks: `BENCHMARKS.md`
- Changelog: `docs/changelog.md`
- Trifecta narrative: `docs/PORTFOLIO_NARRATIVE.md`
