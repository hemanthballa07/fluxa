# Fluxa — Benchmarks

Latency benchmarks for the fluxa fraud pipeline. Captured locally via the k6 SLO script under load, with server-side per-hop percentiles read from Prometheus over the run window. This is a **capacity/headroom** benchmark on a single laptop with the full Docker stack (ML scorer in the eval path), not a production SLO certification.

## Environment

- Local Docker Compose stack (`make up`), Apple Silicon (macOS), 2026-06-01.
- Load: `scripts/k6/fraud_grpc_p99.js` — arrival-rate target **500 iters/s for 30s**, `maxVUs: 200`, against `fraud-grpc:9095` (`EvaluateTransaction`), **ML scorer enabled (warm)**.

## Client-side (k6, gRPC EvaluateTransaction)

| metric | value |
|---|---|
| `grpc_req_duration` med | 596 ms |
| `grpc_req_duration` p95 | 2.40 s |
| `grpc_req_duration` p99 | 3.89 s |
| `grpc_req_duration` max | 7.94 s |
| effective throughput | ~229 completed iters/s |
| checks succeeded | 99.95% (7058 / 7061) |
| SLO `p(99) < 50ms` | **FAIL** (p99 = 3.89 s) |

The configured 500 iters/s was **not** sustained: at ~840 ms average latency, the 200-VU cap bounds throughput to ~229 completions/s, so the stack saturated at roughly **~230 RPS** on this hardware with ML scoring in the path.

## Server-side per-hop (Prometheus `histogram_quantile`, over the load window)

| hop | p50 | p95 | p99 |
|---|---|---|---|
| ingest (`ingest_latency_seconds`) | n/a | n/a | n/a |
| processor (`process_latency_seconds`) | n/a | n/a | n/a |
| fraud-grpc eval (`fraud_eval_latency_seconds`) | 0.63 s | 2.5 s* | 2.5 s* |
| ml-scorer (`scorer_score_latency_seconds`) | 2.6 ms | 5.0 ms | 20 ms |

`*` The Go latency histograms top out at a **2.5 s** bucket (`latencyBuckets` in `internal/adapters/prometheus/metrics.go`), so any value above 2.5 s is censored — the fraud-grpc eval p95/p99 are **≥ 2.5 s**, not exactly 2.5 s. ingest/processor read `n/a` because the async replay pipeline was idle during a pure-gRPC `EvaluateTransaction` run.

## Analysis

- **The ONNX scorer is not the bottleneck.** The `ml-scorer` RPC stays fast under load (p99 = 20 ms); the gRPC scorer hop is cheap.
- **The eval path dominates under load.** `EvaluateWithScorer` runs per-request point-in-time **DB feature-build queries** (`mlfeatures.Build`) plus the rules pass on every call; at ~230 RPS those serialize against the local Postgres and connection pool, pushing fraud-grpc eval p99 past the 2.5 s histogram ceiling.
- **Context vs. Step 1.** The Step-1 rules-only gRPC SLO (`p(99) = 25.79 ms @ 500 RPS`, no scorer, no feature-build) is the apples-to-apples surface SLO. This run adds ML feature-build + scoring and runs the entire stack on one laptop, so treat the numbers as a saturation/headroom signal, not an SLO regression.
- **Documented follow-ups (out of 6c scope):** widen the Go latency buckets above 2.5 s for tail resolution (`metrics.go`); cache/reuse point-in-time aggregates to cut per-request DB cost; raise the Postgres pool or move the scorer feature-build off the hot path.

## How to reproduce

1. `make up` (full stack incl. jaeger + ml-scorer).
2. `make k6-fraud` (client-side summary).
3. Query Prometheus per-hop with `histogram_quantile(0.99, sum(rate(NAME_bucket[5m])) by (le))`, substituting each `*_latency_seconds` metric name (e.g. `fraud_eval_latency_seconds`) — or open the **Per-Hop Latency** Grafana dashboard (uid `fluxa-latency-per-hop`) at `localhost:3000`.
