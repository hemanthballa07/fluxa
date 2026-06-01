# Step 6c — Per-Hop Latency Dashboard + BENCHMARKS: Design Spec

**Status:** Approved (brainstorming; user said "go with the recommendations"). Next step: `writing-plans`.
**Date:** 2026-06-01
**Trifecta step:** 6c (final sub-loop of Step 6) — follows 6a (fluxa OTel tracing, DONE). Runs in parallel with 6b (bankops cross-service OTel).

## Goal

Complete the Step 6 observability arc on the fluxa side: a **per-hop p50/p95/p99 latency dashboard** in Grafana and a committed **`BENCHMARKS.md`** capturing the pipeline's latency under k6 load. Jaeger (6a) answers "what does one request's path look like?"; this dashboard answers "what are the latency percentiles at each hop, over time?".

## Scope decisions (locked in brainstorming)

- **Per-hop percentiles come from Prometheus** `histogram_quantile()` over the existing `_latency_seconds` histograms — not from Jaeger/OTel span-metrics. Reuses metrics fluxa already emits; standard for percentile timeseries. Jaeger stays the trace explorer from 6a.
- **Dashboard is a new dedicated file** `deploy/grafana/dashboards/fluxa-latency-per-hop.json`, leaving `fluxa-overview.json` untouched.
- **Console browser spans: DROPPED** (YAGNI — separate Next.js repo, low portfolio value).
- **Out of scope:** Jaeger span-metrics connector; a Jaeger Grafana datasource (Jaeger UI :16686 remains the trace surface).

## Current state (verified 2026-06-01)

- Existing Go histograms (buckets `0.001…2.5`s): `ingest_latency_seconds`, `process_latency_seconds`, `fraud_eval_latency_seconds` (labeled `{service}`). Python `ml-scorer` emits `scorer_score_latency_seconds` on `:9098`.
- **Gap:** `deploy/prometheus/prometheus.yml` scrapes ingest/processor/query/alert-consumer/fraud-grpc — but **not `ml-scorer:9098`**, so the scorer hop is uncollected. Must be added or the scorer panel is blank.
- Grafana dashboards auto-provision from `deploy/grafana/dashboards/` (provider `path: /var/lib/grafana/dashboards`, mounted from that dir). Existing: `fluxa-overview.json`.
- One k6 script exists: `scripts/k6/fraud_grpc_p99.js` (500 RPS, 30s, `p(99)<50ms` threshold against fraud-grpc).

## Architecture / components

| Path | Responsibility | New/Mod |
|---|---|---|
| `deploy/prometheus/prometheus.yml` | add scrape job `fluxa-ml-scorer` → `ml-scorer:9098` (closes the scorer-hop gap) | Mod |
| `deploy/grafana/dashboards/fluxa-latency-per-hop.json` | new dashboard: one row per hop (ingest → processor → fraud-grpc eval → ml-scorer), each with p50/p95/p99 panels via `histogram_quantile` | New |
| `scripts/k6/fraud_grpc_p99.js` | reuse as the load generator for the benchmark run (extend only if a second scenario is needed) | (reuse) |
| `BENCHMARKS.md` | new doc: method + captured per-hop p50/p95/p99 under load, plus the k6 client-side summary | New |

## Per-hop panels (PromQL)

Each hop gets three series (p50/p95/p99). Template (5-minute rate window):

```promql
histogram_quantile(0.50, sum(rate(<metric>_bucket[5m])) by (le))
histogram_quantile(0.95, sum(rate(<metric>_bucket[5m])) by (le))
histogram_quantile(0.99, sum(rate(<metric>_bucket[5m])) by (le))
```

| Hop | `<metric>` |
|---|---|
| Ingest (HTTP accept) | `ingest_latency_seconds` |
| Processor (per-message pipeline) | `process_latency_seconds` |
| Fraud-grpc (end-to-end gRPC eval) | `fraud_eval_latency_seconds` |
| ML scorer (Python ONNX inference RPC) | `scorer_score_latency_seconds` |

`fraud_eval_latency_seconds` carries a `{service}` label; `sum(...) by (le)` aggregates across it. Panels use the existing Prometheus datasource (the one `fluxa-overview.json` already references). Units: seconds (Grafana `s` unit). Panel legends label the quantile (`p50`/`p95`/`p99`).

## BENCHMARKS.md (method)

1. `make up` (full stack incl. jaeger + ml-scorer).
2. Run the load generator: `make k6-fraud` (`scripts/k6/fraud_grpc_p99.js`, 500 RPS / 30s against fraud-grpc `:9095`).
3. Capture two latency views for the run window:
   - **Client-side** (k6 summary): the script's `grpc_req_duration` p95/p99 + the existing `p(99)<50ms` threshold pass/fail.
   - **Server-side per-hop** (Prometheus, queried over the load window): `fraud_eval_latency_seconds` and `scorer_score_latency_seconds` p50/p95/p99 via the `histogram_quantile` template above (`curl` Prometheus `/api/v1/query`).
4. Record the numbers, the env (local Docker, machine), and the date in `BENCHMARKS.md`, noting that the scorer hop is the dominant cost on warm calls and that rules-only fail-open keeps p99 bounded when the scorer is cold/slow.

## Verification / DoD

- `prometheus.yml` valid; `ml-scorer` target shows **UP** in Prometheus `/targets` (or `/api/v1/targets`).
- New dashboard loads in Grafana (`localhost:3000`) and all four hop rows render non-empty data after generating traffic (e.g., a short `make k6-fraud` or a few `grpcurl` calls).
- Dashboard JSON is valid (Grafana loads it without a provisioning error in `docker compose logs grafana`).
- `BENCHMARKS.md` committed with real captured numbers (not placeholders).
- No regression: existing `fluxa-overview.json` still loads; Go build/tests unaffected (this step touches only YAML/JSON/Markdown + an optional k6 tweak — no Go changes expected).

## Parallel-safety (with bankops 6b)

- This step is **Prometheus/Grafana-only** and touches no proto, no Go service code, and no shared port beyond what's already running — so it cannot conflict with bankops' Spring-side 6b work.
- The one cross-session coordination item (shared Jaeger backend vs. separate, and the `:16686`/`:4317` host-port question) belongs to **6b** and is already raised with bankops (bridge msg 32); 6c does not depend on its resolution.

## Out of scope for 6c

Console/browser OTel spans (YAGNI); Jaeger span-metrics connector; a Grafana Jaeger datasource; any bankops-side work (6b).

## Build order

scrape `ml-scorer` → generate traffic → author dashboard JSON (verify panels render) → run k6 + capture per-hop numbers → write `BENCHMARKS.md` → docs (plan/changelog).
