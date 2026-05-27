# Fluxa — Portfolio Narrative

_Last updated: 2026-05-26_

The strategic plan for turning Fluxa into the headline project of a Plaid-grade fintech portfolio, by integrating it with existing repos rather than building a new bank from scratch.

---

## The frame

The previous plan was "build a neobank prototype with Fluxa as the fraud gateway." That plan was wrong. The repo audit (see _Existing Inventory_ below) showed that the bank app, the rate limiter, and the multi-React-dashboard frontend skill **already exist** in `hemanthballa07`'s GitHub.

The new plan is consolidation, not invention: unify Fluxa with `bankops-portal` (the bank backend) and `fluxguard` (the rate limiter) into a **fintech infrastructure trifecta** with a single unified ops console on top. This produces a Plaid-shaped story (infrastructure plugging into a bank backend) in ~6-7 focused weeks rather than ~10, and degrades gracefully if interrupted because each component stands alone.

---

## Trifecta architecture

```
                ┌──────────────────────────────────┐
                │  Unified Ops Console (Next.js)   │
                │  bank ops + fraud + rate limits  │
                └─────────────────┬────────────────┘
                                  │
                       ┌──────────▼──────────┐
                       │  API Gateway        │
                       │  + JWT/RBAC         │
                       └──────────┬──────────┘
                                  │
              ┌───────────────────┼───────────────────┐
              ▼                   ▼                   ▼
       ┌────────────┐     ┌────────────┐     ┌──────────────┐
       │ fluxguard  │     │ bankops-   │     │   fluxa      │
       │ (Java)     │     │ portal     │     │   (Go)       │
       │ rate limit │     │ (Java)     │     │   fraud      │
       │ Redis Lua  │     │ accounts,  │     │   detection  │
       └────────────┘     │ transfers, │     │   gRPC sync  │
                          │ cases      │     │   + async    │
                          └─────┬──────┘     │   pipeline   │
                                │             └──────┬───────┘
                       calls fraud sync             │
                       on every transfer            │
                       ───────────────────────────►─┘
                                                    │
                                                    ▼
                                         ┌────────────────────┐
                                         │  ML Scorer (Py)    │
                                         │  gRPC + ONNX       │
                                         └────────────────────┘

  All three services emit to one Prometheus + Grafana + Jaeger stack.
  Polyglot: Java ↔ Go ↔ Python via gRPC — proves infra is language-agnostic.
```

The Plaid-shaped framing: "I built fintech infrastructure (fraud + rate limiting) that integrates with a bank backend via gRPC, with end-to-end OpenTelemetry tracing across all three services."

---

## Existing inventory (what we don't have to build)

| Repo | Stack | What it already provides | Reuse plan |
|---|---|---|---|
| `fluxa` (this repo) | Go, RabbitMQ, MinIO, Postgres, Grafana | Async fraud pipeline, rules engine, idempotency, MinIO large-payload offload, IEEE-CIS replay with ground-truth labels | **Headline repo.** Add sync gRPC eval + ML scorer. |
| `bankops-portal` | Java/Spring Boot 3.2 + Angular 17 + Azure | Customers, Accounts, Transactions (with overdraft + idempotency), incident/case workflows, audit timeline, RBAC, correlation IDs, Azure CI/CD | **Bank consumer.** Modify `TransactionService` to call Fluxa via gRPC before commit; flagged → `HELD` + auto case. |
| `fluxguard` | Java/Spring Boot + Redis Lua + k6 | Token bucket + sliding window, atomic Redis Lua, k6 benchmarks (p99 <10ms across 5 scenarios), 4 ADRs, OTel | **Edge service.** Sits in front of bankops-portal API. Adapt existing k6 scripts. |
| `SupportOps-…-Dashboard` | React 18 + TS + Vite + Postgres | Internal dashboard pattern, RBAC, ticket workflow | **Pattern source for ops console.** Copy structure, not code. |
| `uf-research-metrics-dashboard` | TypeScript monorepo + Grafana + Makefile | Production-shape repo layout (`/apps`, alert rules, prom/grafana provisioning) | **Pattern source for repo layout.** |
| `HALO-RAG`, `parkinsons-cnnlstm`, `medical-xray-triage` | Python (PyTorch) | ML modeling background, evaluation discipline | **Pattern source for ML scorer.** |

What's missing entirely: the **unified Next.js + TS ops console**, the **ML fraud scorer**, the **gRPC sync surface on Fluxa**, the **cross-service tracing**.

---

## Build sequence

Each step is independently shippable. **Discipline rule: step N must be merged to main with a screenshot in the README before step N+1 starts.**

| # | Step | Focused time | Deliverable / acceptance |
|---|---|---|---|
| 0 | **Audit & consolidate.** Archive zombie repos (`amfoss-tasks`, `first-kotlin-project-calculator`, `Go-Local-App`, `my-aws-capstone-project`, `portfolio-`, `reactjs-todo`, etc.). Pin 5 repos: `fluxa`, `bankops-portal`, `fluxguard`, `mediflow`, one ML. Audit `bankops-portal/backend/src` for production-shape (this doc's companion task B). | 3 days | GitHub shows 5 pinned, ~15 archived; `bankops-portal` audit report in this file. |
| 1 | **Synchronous fraud-eval gRPC in Fluxa.** New `FraudEval.EvaluateTransaction` RPC. Existing async RabbitMQ pipeline keeps running for replay/analytics. Same `fraud_flags` table. | 1 wk | gRPC server on `:9095`; integration test calling from Java client; p99 <50ms under k6. |
| 2 | **Wire bankops-portal → Fluxa.** Spring `TransactionService` calls Fluxa gRPC before commit. Flagged → `HELD` state, auto-creates case in incident console. Threads correlation ID through. | 1 wk | End-to-end demo: deposit a $99,999 USD txn in bankops-portal → blocked → case appears. |
| 3 | **fluxguard in front of bankops-portal.** Token bucket on `/transfers`, sliding window on `/login`. Adapt fluxguard's k6 scripts to the bank API. | 3-4 days | k6 burst test against `/transfers` returns 429 once bucket drains; recovers as expected. |
| 4 | **Unified Next.js + TS ops console.** Three tabs: bank ops (REST → bankops-portal), fraud feed (WebSocket from Fluxa), rate limits (Prom query). JWT/RBAC. TanStack Query + Virtual. | 1 wk | New `apps/console/` in monorepo or new repo; renders 100K rows at 60fps; live flag arrival visible. |
| 5 | **ML scorer.** Python gRPC service, ONNX-served, blended with rules in Fluxa. Fail-open on model unavailable (mirrors fluxguard ADR-003). | 1.5 wk | PR-AUC with bootstrap CI on IEEE-CIS held-out split documented in `docs/ML_EVALUATION.md`. |
| 6 | **Cross-service tracing + benchmarks.** OTel traces spanning Angular → Spring → Go → Python and back. One Grafana dashboard with per-hop p50/p95/p99. `BENCHMARKS.md` at trifecta level. | 4 days | Click any txn in the console → see the trace. End-to-end p99 under load measured and recorded. |
| 7 | **Narrative & polish.** Top-level README rewrite on `fluxa`. Architecture diagram (this one). Demo GIF. 3-5 short ADRs. Cross-links between the three repos. | 3 days | Recruiter can grok the project in 90 seconds. |

**Total focused: ~6-7 weeks. Calendar at 20 hrs/wk: ~3 months.**

---

## Resume bullets (target — measure before publishing)

Bracketed numbers are targets to measure and swap in, not figures to ship before the work exists.

1. **Architected a polyglot fintech infrastructure stack** integrating three production-style services: Spring Boot/Angular bank operations portal, Go-based real-time fraud detection service, Java-based distributed rate limiter. Every banking transaction flows through both services synchronously via gRPC before commit, with end-to-end OpenTelemetry traces spanning Angular → Spring → Go → Python.

2. **Built the Fluxa fraud detection service in Go** with a RabbitMQ async pipeline and synchronous gRPC eval (p99 <[X]ms), `SELECT FOR UPDATE` advisory-lock idempotency, and an ONNX-served gradient-boosted scorer blended with a YAML rules engine — PR-AUC [0.XX] on IEEE-CIS held-out split, +[Y]pts recall over rules-only at equal precision (bootstrap CI [Z]).

3. **Shipped a unified Next.js + TypeScript ops console** aggregating bank operations, fraud flags (live via WebSocket), and rate-limit telemetry from three backend services behind one JWT/RBAC gateway; rendered [N]K transactions at 60fps via windowed virtualization, Lighthouse [95+] on performance and accessibility.

4. **Designed fail-open rate limiting** in front of the bank API with Redis Lua atomic scripts (token bucket + sliding window), validated via k6 to sustain [X] RPS at p99 <10ms across burst, sustained, and threshold scenarios with full tracing enabled.

---

## Honest tradeoffs

1. **Polyglot maintenance cost.** Java in two repos, Go in one, Python in the ML service, TypeScript in the console. Asset for the resume (proves cross-stack integration), but a real cognitive cost.
2. **`bankops-portal` audit is load-bearing.** If the Spring code is tutorial-quality (H2 in-memory only, weak tests, no real concurrency), step 2 expands to "harden bankops-portal first." Task B (next) confirms or invalidates this assumption.
3. **The ops console is the longest single-stack stretch.** If frontend velocity is rusty, the 1-week estimate becomes 2.
4. **Cross-service tracing (step 6) is the unsexy piece that makes the numbers believable.** Do not skip; every benchmark claim references a trace.
5. **"Really good numbers" is a stopping problem.** Define the number before measuring; ship at the defined number even if more is possible.

---

## Stop conditions / discipline

- **Resume bullet before code.** If a step's bullet doesn't impress when read cold, the step is wrong-shaped — fix the design before writing code.
- **Hard time-box each step at 1.5× the estimate.** If a step blows past that, stop and reassess scope (not just push harder).
- **No benchmarks before step 1 ships.** No ML before step 4 ships. No polish before step 5 ships.
- **One repo, one README, one diagram is the goal of step 7.** If a recruiter has to click into multiple READMEs to understand the project, the narrative failed.
- **Archive aggressively at step 0.** Five visible repos is the right number.

---

## Open questions (answer before step 0)

- Interview runway: how many weeks until you're actively talking to companies? If <8 weeks, this plan is wrong — fall back to the standalone ops console.
- Frontend sustained pace: can you commit 20+ hrs/week for ~10 weeks?
- Monorepo or polyrepo for the unified console? Recommend monorepo absorption into `fluxa` (`/apps/console`) for narrative coherence — three pinned repos still tell the trifecta story.
- Hosted demo vs local-only? Default to local + GIF; revisit at step 7.

---

## bankops-portal audit (Task B)

**Verdict: production-shape. The trifecta plan is viable as designed.** Read on 2026-05-26.

### What's real

- **Spring Boot 3.2 + Java 17** in `backend/pom.xml`. H2 for local, Azure SQL (`mssql-jdbc`) for prod. Spring Security, Validation, Actuator all wired.
- **Domain depth**: 13 entities (`Account`, `Transaction`, `Customer`, `SupportCase`, `AuditEvent`, `Agent`, `AssignmentEvent`, `FeatureFlag`, `LogEvent`, `SlaStatusChangeEvent`, `StateTransitionEvent`, `CaseNote`, `Transaction`), 12 repositories, 8 services. A `CaseStateMachine` (not a TODO), an `SlaScheduler`, an `AssignmentService` — far beyond CRUD.
- **`TransactionService.java` is the file step 2 modifies. ~330 lines, real concurrency code:**
  - `withdrawWithOptimisticRetry`: 3-attempt retry loop on `ObjectOptimisticLockingFailureException` / `OptimisticLockException`, with structured logging on each retry. Throws after `maxRetries`.
  - `withdrawOnce` is `@Transactional(isolation=READ_COMMITTED, propagation=REQUIRED, rollbackFor=Exception.class)`.
  - Idempotency: pre-check via `findByAccount_IdAndIdempotencyKeyAndType`, post-write `DataIntegrityViolationException` race-handler that returns the duplicate. Two layers — exactly right.
  - Correlation ID per transaction, threaded into `LoggingService` and `AuditEventService` calls.
- **Tests are integration + concurrency**: `WithdrawalIdempotencyTest` uses `CountDownLatch` + `ExecutorService` + `AtomicInteger` to simulate concurrent withdrawals — real contention, not mocked. Plus `TransactionResilienceTest`, `CriticalFinancialWorkflowTest`, `StateTransitionAuditIntegrationTest`. 12 test classes total.
- **Migrations versioned** Flyway-style (`V5__add_sla_fields.sql`, `V6__add_assignment_tables.sql` visible — V1-V4 squashed or in classpath baseline).
- **DTOs are separated from entities** — 30+ DTOs, no leaking JPA into the API.

### Integration plan adjustments for step 2

| Concern | Resolution |
|---|---|
| No `HELD` state on `Transaction.TransactionStatus` enum | Add `HELD` to the enum + a migration. Small change. |
| Deposit path (`createDeposit`) is not idempotent — only withdrawals are | Fraud-eval at the **top** of both `createDeposit` and `withdrawOnce`, before the optimistic-lock work. If flagged → set status `HELD`, persist, return — skip the balance update and audit. |
| `bankops-portal` uses `Long` account IDs; Fluxa uses UUID strings | Already aligned: bankops generates `String correlationId = UUID.randomUUID().toString()` per transaction. Use that as Fluxa's `event_id`. No translation layer needed. |
| No OpenTelemetry agent visible in `pom.xml` | Step 6 adds the OTel Java agent + Spring Boot starter. Actuator is already present, so spans will piggyback existing instrumentation. |
| `LoggingService` already exists with structured context maps | Fraud-eval call result (decision, latency_ms, rule names) goes through it. Reuses existing observability. |

### Integration shape (step 2 sketch)

```java
@Service
@RequiredArgsConstructor
public class TransactionService {
    private final FluxaFraudClient fraudClient;   // NEW — gRPC stub
    // ... existing fields

    @Transactional(isolation = READ_COMMITTED, ...)
    protected TransactionDto withdrawOnce(Long accountId, CreateTransactionRequest request, String idempotencyKey) {
        // 0) NEW — fraud check before any state change
        FraudDecision decision = fraudClient.evaluate(buildFraudRequest(accountId, request, idempotencyKey));
        if (decision.isFlagged()) {
            return persistHeldTransaction(accountId, request, idempotencyKey, decision);
            // auto-creates a SupportCase via CaseService
        }
        // 1-8) existing logic unchanged
    }
}
```

**Estimated step-2 effort holds at ~1 week.** No surprise rewrites required.

### Small concerns (none blocking)

1. `TransactionService` is already ~330 lines — adding fraud-eval pushes it toward "extract a `FraudGatedTransactionExecutor`" territory. Resist for now; extract only if it crosses 450.
2. No observability beyond Actuator — no Prometheus micrometer config visible in the snippet. Step 6 will add it; harmless until then.
3. Some methods catch `IllegalArgumentException` from `Enum.valueOf` and default to `OTHER` (e.g., category parsing) — fine for category, but the equivalent pattern on `TransactionType` throws. Inconsistency, not a bug.
4. Migrations starting at V5 suggests V1-V4 may have been squashed. Worth confirming on local clone, but not blocking.

### Bottom line

The trifecta plan stands. `bankops-portal` is a credible bank backend to integrate against. The integration is **additive** — a new gRPC client field, a fraud-eval call at the top of both transaction paths, a `HELD` enum value, a small migration. The existing optimistic-lock + idempotency machinery is untouched and continues to do its job.
