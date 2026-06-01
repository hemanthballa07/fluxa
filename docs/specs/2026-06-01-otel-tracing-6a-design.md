# Step 6a — fluxa-internal OTel Tracing: Design Spec

**Status:** Approved (brainstorming; user said "run the loop"). Next step: `writing-plans`.
**Date:** 2026-06-01
**Trifecta step:** 6a (first sub-loop of Step 6) — follows Step 5 (ML scorer, CLOSED).

## Step 6 decomposition (why 6a is scoped this way)

Step 6 (OTel across Angular → Spring → Go → Python + per-hop Grafana + `BENCHMARKS.md`) is multi-repo, so it's split:
- **6a (this spec):** fluxa Go services + Python scorer OTel, trace-context propagation over the fraud-eval gRPC (Go→Python), exported to Jaeger. Self-contained in fluxa, no cross-repo coordination, immediately demonstrable.
- **6b:** bankops Spring Boot OTel + W3C `traceparent` over the bankops→fluxa gRPC (trace spans bankops → fluxa → scorer). Needs the bankops session via the bridge. Fluxa side: extract `traceparent` from gRPC metadata in `fraudeval` server (otelgrpc handles this automatically once the server is instrumented).
- **6c:** console browser spans (likely YAGNI-trimmed), per-hop p50/p95/p99 Grafana dashboard, `BENCHMARKS.md` (k6 under load).

## Goal (6a)

A connected, polyglot distributed trace visible in Jaeger: `fraud-grpc: EvaluateTransaction` (Go) with a child `ml-scorer: Score` (Python) span, proving Go↔Python trace propagation. All fluxa services emit spans to one Jaeger backend.

## Current state (verified 2026-06-01)

- **No OTel deps** in `go.mod`; **no tracing backend** in compose. Greenfield.
- Only `correlation_id` structured-logging plumbing exists (not OTel spans).
- `deploy/grafana/{provisioning,dashboards}` + `deploy/prometheus` exist.
- Services: `ingest` (:8080→host 8088), `processor`, `fraud-grpc` (:9095, reflection on, uses `grpc.NewServer` with a UnaryInterceptor already), `query` (:8083), `ml-scorer` (Python, :9097). The Go scorer client is `internal/adapters/scorer`.

## Architecture / components

| Path | Responsibility | New/Mod |
|---|---|---|
| `docker-compose.yml` | add `jaeger` (jaegertracing/all-in-one): OTLP gRPC `4317`, OTLP http `4318`, UI `16686` | Mod |
| `internal/observability/tracing.go` | shared Go OTel init: OTLP gRPC exporter → `OTEL_EXPORTER_OTLP_ENDPOINT` (default `jaeger:4317`), resource with `service.name`, `TracerProvider`, W3C propagator; returns a `shutdown(ctx)` func; **init failure logs + returns no-op (never blocks)** | New |
| `services/fraud-grpc/main.go` | call `observability.Init("fraud-grpc")`; add `otelgrpc` stats handler to `grpc.NewServer` (`grpc.StatsHandler(otelgrpc.NewServerHandler())`) | Mod |
| `internal/adapters/scorer/client.go` | add `grpc.WithStatsHandler(otelgrpc.NewClientHandler())` to the dial → injects `traceparent` into scorer gRPC metadata | Mod |
| `services/{ingest,processor,query}/main.go` | call `observability.Init("<svc>")` (broad span coverage; query/ingest HTTP via `otelhttp`, processor wraps its loop in a span) | Mod |
| `services/ml-scorer/server.py` + `requirements.txt` | OTel SDK + `opentelemetry-instrumentation-grpc` (server) + OTLP exporter (`OTEL_EXPORTER_OTLP_ENDPOINT`, default `jaeger:4317`); the `Score` span auto-parents off the incoming `traceparent` | Mod |
| `deploy/grafana/provisioning/datasources` | add Jaeger datasource (optional; Jaeger UI :16686 is the primary view for 6a) | Mod |
| `Makefile` / `go.mod` | OTel Go deps (`go.opentelemetry.io/otel`, `otel/sdk`, `otel/exporters/otlp/otlptrace/otlptracegrpc`, `otelgrpc`, `otelhttp`) | Mod |

## Propagation (load-bearing)

1. `fraud-grpc` server instrumented (otelgrpc server handler) → server span per RPC.
2. Go scorer client instrumented (otelgrpc client handler) → injects W3C `traceparent` into outgoing gRPC metadata.
3. Python scorer's grpc-server instrumentation extracts `traceparent` → `Score` span is a child of the Go span.
4. **Async path (ingest→RabbitMQ→processor):** inject `traceparent` into `QueueMessage` (new header/field), extract in processor to continue the trace. **Included only if clean; otherwise deferred to 6b** — do not let it bloat 6a.

## Error handling / reliability

- Tracer init failure → log + no-op tracer; tracing never blocks or fails the pipeline (same fail-open spirit as the scorer).
- Sampling: always-on (`AlwaysSample`) for local; note env override for prod.
- `shutdown(ctx)` flushes spans on graceful stop (wire into existing signal-shutdown paths).

## Verification / DoD

- `make up` (incl. `jaeger`); `docker compose ps` shows jaeger healthy.
- Fire `EvaluateTransaction` (grpcurl, as in Step 5) **twice** (first warms the connection).
- Jaeger UI (`localhost:16686`) → service `fraud-grpc` → the trace shows `EvaluateTransaction` with a **child `Score` span on `ml-scorer`** (Go→Python propagation proven). Verify via the Jaeger HTTP API too: `curl 'localhost:16686/api/traces?service=fraud-grpc&limit=1'` contains both spans.
- Existing integration suite green (`TEST_DB_DSN=… go test -race ./...`, 0 SKIP); `gofmt -l .` empty.

## Out of scope for 6a

bankops spans (6b), frontend/console spans (6c), per-hop Grafana latency dashboard (6c), `BENCHMARKS.md` (6c).

## Build order

jaeger in compose → `internal/observability` helper + go.mod deps → fraud-grpc server + scorer client instrumentation → Python scorer OTel → (ingest/processor/query broad instrumentation) → verify the Go→Python trace in Jaeger → docs.
