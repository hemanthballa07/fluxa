# Integration Spec — bankops-portal → Fluxa (synchronous fraud eval)

_Status: bankops-side locked after `/understand` + direct source read on 2026-05-26. Open questions all closed; see end of doc._
_Owner: trifecta build, step 1 + step 2_

## Goal

Bankops-portal calls Fluxa over gRPC before committing any transaction (deposit, withdrawal, future transfers). If Fluxa returns `DECISION_FLAG`, bankops-portal marks the transaction `HELD` and auto-creates a `SupportCase`. The existing Fluxa async RabbitMQ pipeline is unchanged and continues to serve CSV replay + analytics.

## SLOs

| Metric | Target |
|---|---|
| Rules-only eval p99 | < 50 ms |
| Rules+ML eval p99 (step 5 onward) | < 100 ms |
| Bankops transaction p99 (with eval in hot path) | < 200 ms |
| Fluxa availability (sync surface) | "best effort" — caller handles `UNAVAILABLE` per failure-mode policy below |

## Proto contract

Defined in [`proto/fraud/v1/fraud_eval.proto`](../../proto/fraud/v1/fraud_eval.proto). Key decisions:

- **`Decision` enum has only `ALLOW` / `FLAG`, not `DENY`** — Fluxa is detection, not enforcement.
- **`user_id` is an opaque string** — bankops's `Long account_id` becomes a string; no ID-type coupling.
- **`event_id` is supplied by the caller** (bankops's transaction `correlation_id`) so OTel traces align across services.
- **`metadata` is `map<string,string>`** — JSON-encode the value if nested structure is needed.

## Fluxa-side changes (step 1)

### New files

| Path | Purpose |
|---|---|
| `proto/fraud/v1/fraud_eval.proto` | written |
| `proto/buf.yaml`, `proto/buf.gen.yaml` | Buf workspace + Go codegen config |
| `internal/grpc/fraud/v1/*.pb.go` | Generated — do not edit |
| `internal/grpc/fraud/v1/*_grpc.pb.go` | Generated — do not edit |
| `internal/fraudeval/server.go` | `FraudEvalServer` impl — wraps existing `fraud.Engine` |
| `services/fraud-grpc/main.go` | Service entrypoint, port `:9095` |
| `services/fraud-grpc/Dockerfile` | Container build |
| `internal/fraudeval/server_test.go` | Integration tests against a real DB |

### Modified files

| Path | Change |
|---|---|
| `go.mod` | Add `google.golang.org/grpc`, `google.golang.org/protobuf`, `google.golang.org/genproto/googleapis/protobuf/timestamp` |
| `Makefile` | New target `proto` → `buf generate`; new target `fraud-grpc` → `docker compose up -d fraud-grpc` |
| `docker-compose.yml` | Add `fraud-grpc` service (port `9095:9095`, depends on postgres + rabbitmq); add `9096:9096` for metrics |
| `deploy/prometheus/prometheus.yml` | Add scrape job for fraud-grpc on `:9096` |
| `docs/plan.md` | Move "trifecta step 1" to In Progress |

### Server impl sketch

```go
// internal/fraudeval/server.go
package fraudeval

import (
    "context"
    "time"

    fraudv1 "github.com/fluxa/fluxa/internal/grpc/fraud/v1"
    "github.com/fluxa/fluxa/internal/db"
    "github.com/fluxa/fluxa/internal/domain"
    "github.com/fluxa/fluxa/internal/fraud"
    "github.com/fluxa/fluxa/internal/logging"
    "github.com/fluxa/fluxa/internal/ports"
)

type Server struct {
    fraudv1.UnimplementedFraudEvalServer
    Engine  *fraud.Engine    // reused — same rules.yaml as async pipeline
    DB      *db.Client       // for velocity queries + event persistence
    Metrics ports.Metrics
    Logger  *logging.Logger
    Version string           // "fluxa-rules-v1.2"
}

func (s *Server) EvaluateTransaction(ctx context.Context, req *fraudv1.EvaluateRequest) (*fraudv1.EvaluateResponse, error) {
    start := time.Now()

    // 1. Translate proto → domain.Event
    event := protoToEvent(req)
    if err := event.Validate(); err != nil {
        return nil, status.Errorf(codes.InvalidArgument, "validation: %v", err)
    }

    // 2. Persist the event (so velocity check on subsequent calls sees it,
    //    and so async analytics has a record). Idempotent on event_id.
    if err := s.DB.InsertEvent(&event, req.EventId /* correlation */, domain.PayloadModeInline, nil); err != nil {
        // Idempotency conflict is fine — the event already exists.
        // Other errors → retryable, surface as Unavailable.
        if !isDuplicate(err) {
            return nil, status.Errorf(codes.Unavailable, "db: %v", err)
        }
    }

    // 3. Evaluate rules — reuses the exact same engine the async pipeline uses
    flags, err := s.Engine.Evaluate(&event, s.DB)
    if err != nil {
        return nil, status.Errorf(codes.Internal, "eval: %v", err)
    }

    // 4. Persist flags (best-effort — eval result is the source of truth even
    //    if write fails; same pattern as the async pipeline's evaluateFraud)
    for _, f := range flags {
        _ = s.DB.InsertFraudFlag(&f)
        s.Metrics.IncCounter("fraud_flags_total", "rule", f.RuleName, "surface", "grpc")
    }

    // 5. Build response
    resp := &fraudv1.EvaluateResponse{
        Decision:    decisionFromFlags(flags),
        Flags:       toProtoFlags(flags),
        LatencyMs:   float64(time.Since(start).Milliseconds()),
        EvaluatedBy: s.Version,
    }
    s.Metrics.ObserveHistogram("fraud_eval_latency_seconds",
        time.Since(start).Seconds(), "surface", "grpc")
    return resp, nil
}
```

The crucial architectural property: the sync RPC and the async pipeline **share the same `fraud.Engine` instance**, so `rules.yaml` remains the single source of truth and the two surfaces can never drift on rule logic.

## Bankops-portal-side changes (step 2 — refine after `/understand`)

### New files (planned)

| Path | Purpose |
|---|---|
| `backend/src/main/java/com/bankops/portal/fraud/FluxaFraudClient.java` | Spring-injected gRPC client wrapping the generated stub |
| `backend/src/main/java/com/bankops/portal/fraud/FraudGateway.java` | Higher-level facade: builds request, calls client, translates response, applies fail-open/closed policy |
| ~~`backend/src/main/resources/db/migration/V7__add_held_status.sql`~~ | **No migration needed.** `Transaction.status` is `@Enumerated(EnumType.STRING)` and JPA runs `ddl-auto: update` (H2 dev) / accepts new string values directly (MSSQL prod). Adding `HELD` to the Java enum is sufficient. |
| `backend/src/test/.../fraud/FraudGatewayTest.java` | Tests for translation + failure-mode handling |
| `backend/src/test/.../service/HeldTransactionIntegrationTest.java` | End-to-end: flagged eval → HELD txn → SupportCase auto-created |

### Modified files (planned)

| Path | Change |
|---|---|
| `backend/pom.xml` | Add `grpc-spring-boot-starter`, `protobuf-maven-plugin`; point at the same `.proto` source |
| `backend/src/main/.../service/TransactionService.java` | Inject `FraudGateway`; call `gateway.evaluate(...)` at the top of `withdrawOnce` and `createDeposit`; on `FLAG`, branch to `persistHeldTransaction` + `caseService.createForFraud(...)` |
| `backend/src/main/.../entity/Transaction.java` | Add `HELD` to `TransactionStatus` enum |
| `backend/src/main/.../service/CaseService.java` | New method `createForFraud(Transaction, List<FraudFlag>)` |
| `backend/src/main/resources/application.yaml` | New `fluxa.fraud.grpc.endpoint` config (default `localhost:9095`) |
| `frontend/src/app/.../transactions/transaction-detail.component.html` | Show "HELD — fraud review" badge when status is HELD |

Spring integration sketch (already in `PORTFOLIO_NARRATIVE.md` but pinned here too):

```java
@Service
@RequiredArgsConstructor
public class TransactionService {
    private final FraudGateway fraudGateway;   // NEW
    // ... existing fields

    @Transactional(isolation = READ_COMMITTED, ...)
    protected TransactionDto withdrawOnce(Long accountId, CreateTransactionRequest request, String idempotencyKey) {
        Account account = accountRepository.findById(accountId).orElseThrow(...);

        // NEW — fraud eval before any state change
        FraudDecision decision = fraudGateway.evaluate(account, request, idempotencyKey);
        if (decision.isFlagged()) {
            return persistHeldTransaction(account, request, idempotencyKey, decision);
        }

        // existing logic continues unchanged
    }
}
```

## Failure modes

| Fluxa response | Bankops behavior | Rationale |
|---|---|---|
| `DECISION_ALLOW` | Proceed with transaction normally. | Happy path. |
| `DECISION_FLAG` | Set `HELD`, persist, auto-create case, return 202-ish response to client. | Detection without enforcement — case must be reviewed. |
| gRPC `UNAVAILABLE` (Fluxa down, network) | **Fail-open** for deposits, **fail-closed** for withdrawals. | Symmetric with fluxguard's ADR-003 in spirit (fail-open under uncertainty), but withdrawals are higher risk and warrant the conservative side. Justify in a new ADR. |
| gRPC `DEADLINE_EXCEEDED` (>100 ms) | Same as UNAVAILABLE. | Latency budget is part of the contract. |
| gRPC `INVALID_ARGUMENT` | Reject the bank transaction with 400. | Caller bug; should never happen in prod. Alarm. |

Configurable per environment via `application.yaml` (`fluxa.fraud.failure-mode: open|closed|per-type`).

## Rollout plan (step 2)

1. **Feature flag**: `bankops.fluxa.enabled` (default false). When false, `FraudGateway.evaluate` returns `ALLOW` immediately. Lets the integration ship behind a flag, get reviewed, then toggle on.
2. **Shadow mode first**: with flag on but `bankops.fluxa.shadow-mode=true`, bankops calls Fluxa and logs the decision but does not act on it. Compare decisions against null hypothesis (all ALLOW) for a week of dev traffic.
3. **Enforcement mode**: flip `shadow-mode` to false. Withdrawals now actually go to HELD on flag.
4. **k6 load test**: 1000 RPS of mixed deposits/withdrawals, half flagged via amount_threshold rule. Confirm bankops p99 stays <200 ms with eval in the path.

## Closed answers (post-`/understand` + source read, 2026-05-26)

**Q1: `TransactionStatus` shape?**
Current enum is `PENDING, COMPLETED, FAILED` — no `HELD`. Add `HELD` to the Java enum (`Transaction.java:73-75`). No SQL migration required: `@Enumerated(EnumType.STRING)` + `ddl-auto: update` (H2 dev) handles it; MSSQL `varchar` columns accept the new value with no schema change.

**Q2: `CaseService` surface and existing fraud path?**
`CaseService.createCase(CreateCaseRequest)` exists and already supports linking to a `Transaction` via `request.getTransactionId()`. **No fraud-derived path yet.** Add `CaseService.createForFraud(Transaction tx, List<FraudFlag> flags)` that:
1. Reuses `tx.getCorrelationId()` as the case's `correlationId` — single thread of correlation across bankops + fluxa.
2. Sets `severity = HIGH` for fraud cases (or derive from rule count).
3. Builds the `SupportCase` directly (skipping the DTO indirection — internal callsite).
4. Calls the existing `slaService.initializeSla(supportCase, SlaPriority.P1)` — fraud cases are P1.
5. Calls the existing `assignmentService.autoAssign(supportCase.getId(), correlationId)` — same auto-routing every other case gets.
6. Records the audit event via the existing `auditEventService.recordEvent(...)`.

That's ~30 lines added to `CaseService.java`. Every collaborator it needs is already injected into the class.

**Q3: Correlation ID flow?**
Yes — exactly as planned. `Transaction.correlationId` is a unique-constrained UUID string (`@Column(name = "correlation_id", nullable = false, unique = true, length = 36)`) generated in `@PrePersist`. Bankops sends this as `EvaluateRequest.event_id`; Fluxa uses it as the primary key in its `events` table; `LoggingService.logEvent(correlationId, ...)` puts it in MDC for structured logs (`%X{correlationId}` in the logging pattern, line 39 of `application.yaml`). One UUID threads through bankops + fluxa + their respective DBs. OTel spans line up natively.

**Q4: Spring `@Async` event bus for shadow-mode logging?**
No — only `@EnableScheduling` is on the application class (for `SlaScheduler`); no `@EnableAsync`. That's fine. Shadow-mode logging uses `LoggingService.logEvent` synchronously inside the existing transaction context. Bonus design note: `LoggingService` already uses `PROPAGATION_REQUIRES_NEW` for log persistence, so log writes never roll back the business transaction. We get the same isolation property for free.

**Q5: Angular status-badge component?**
No dedicated `transaction-detail` component or status-badge component exists — transactions render inside `account-detail` and `transaction-form`. Adding HELD is one new CSS class + one branch in whatever template currently shows the status string. **Trivial change, not a new component.** Worst case ~15 minutes including the CSS.

## Other discoveries worth pinning

- **Swagger UI is wired** at `http://localhost:8080/api/swagger-ui/index.html` — useful for manually testing the integration before bringing up Fluxa.
- **No active Flyway despite `V5`/`V6` SQL files in the tree.** JPA `ddl-auto: update` is doing schema management instead. The SQL files appear to be seed/reference. Confirm with `pom.xml` next session before assuming.
- **`LoggingService` uses `PROPAGATION_REQUIRES_NEW`** — log writes never participate in business transaction rollback. Mirror this pattern in any future cross-cutting service.
- **`Transaction.cases` is a `@OneToMany`** — once `CaseService.createForFraud` persists the `SupportCase` with a `transaction` reference, it appears in `tx.getCases()` automatically. No manual back-reference required.
- **Three profiles**: `local` (H2 in-memory), `prod` (Azure SQL via env vars), `test` (separate H2 instance with `create-drop`). The integration test in step 2.3 should add a fourth `integration` profile that points at a real Postgres container if we want fluxa + bankops to talk to the same DB family.

## Build sequence within step 1+2

| # | Action | Owner | Verifies |
|---|---|---|---|
| 1.1 | `buf generate` produces Go stubs cleanly | fluxa | proto is well-formed |
| 1.2 | `fraudeval.Server` unit tests pass against real DB | fluxa | rules logic untouched |
| 1.3 | `services/fraud-grpc` runs in docker-compose, `grpcurl` smoke test returns ALLOW | fluxa | wiring works |
| 1.4 | k6 against the gRPC endpoint hits p99 <50 ms | fluxa | SLO is real |
| 2.1 | `protobuf-maven-plugin` generates Java stubs from same `.proto` source | bankops | wire compatibility |
| 2.2 | `FraudGateway` unit tests with mocked gRPC | bankops | translation correct |
| 2.3 | Integration test: real Fluxa container + bankops talking to it | both | end-to-end |
| 2.4 | Toggle feature flag on in shadow mode for one day of dev traffic | bankops | no false positives |
| 2.5 | Flip to enforcement | bankops | done |
