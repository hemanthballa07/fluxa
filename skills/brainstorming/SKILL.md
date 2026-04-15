---
name: brainstorming
description: Structured design-first skill. Enforces project exploration, clarifying questions, approach comparison, section-by-section approval, and a written spec before any implementation begins.
trigger: explicit
---

# Brainstorming Skill

You are in **design mode**. Follow this protocol precisely. Do not write implementation code until the user explicitly approves the final design.

---

## Phase 0 — Explore Project Context

Before asking anything, silently read the codebase to ground yourself:

- Skim `docs/` for architecture, invariants, and tradeoffs documents.
- Identify the relevant packages under `internal/` and `cmd/` that the request touches.
- Note existing patterns: error types, logging conventions, metric emission, idempotency usage, DB access style.

Only after completing this exploration do you speak.

---

## Phase 1 — Clarifying Questions (one at a time)

Ask clarifying questions **one at a time**. Wait for the answer before asking the next.

Start with the most load-bearing unknown — the one whose answer most changes the shape of the solution. Common first questions:

- What is the trigger or entry point for this feature?
- Is this on the hot path (ingest/processor) or an offline/async operation?
- Are there latency, throughput, or cost constraints I should know about?
- Does this touch the idempotency contract or the existing DB schema?

Do not ask questions you can answer by reading the code. Do not ask more than 4 clarifying questions total.

---

## Phase 2 — Propose 2–3 Approaches

Present exactly **2 or 3 distinct approaches**. For each, provide:

```
### Approach N — [Short Name]

**Summary:** One sentence.

**How it works:** 2–4 sentences describing the mechanism.

**Tradeoffs:**
- Pro: ...
- Pro: ...
- Con: ...
- Con: ...

**Best when:** The scenario where this approach wins.
```

End with: "Which approach would you like to pursue, or should I blend elements of multiple?"

Do not proceed until the user selects an approach.

---

## Phase 3 — Present Design Sections for Approval

Break the design into sections. Present **one section at a time** and wait for explicit approval ("looks good", "approved", "yes", "proceed") before moving to the next.

Sections (use only what applies to the feature):

1. **Data Model** — new or changed DB columns, structs, constants
2. **API Contract** — new endpoints, request/response shapes, error codes
3. **Processing Logic** — step-by-step flow with error classification (retryable vs. non-retryable)
4. **Idempotency** — how duplicate messages or requests are handled
5. **Observability** — metrics names/units, log fields, alert thresholds
6. **Infrastructure** — new AWS resources, IAM permissions, Terraform changes
7. **Migration** — schema changes, backward compatibility, rollout order

For each section:
```
## [Section Name]

[Design content]

---
Ready to proceed to the next section, or do you want to revise anything here?
```

Do not skip to the next section without approval.

---

## Phase 4 — Write the Spec Document

Once all sections are approved, write a spec to `docs/specs/{feature-name}.md` before any code.

Use this template:

```markdown
# Spec: {Feature Name}

**Status:** Draft  
**Author:** Hemanth Balla  
**Date:** {today}

## Problem

[1–3 sentences: what gap or need this addresses]

## Chosen Approach

[Name of the selected approach and a brief rationale]

## Data Model

[Finalized structs, DB columns, constants]

## API Contract

[Endpoints, request/response, error codes — if applicable]

## Processing Logic

[Step-by-step, with error classification]

## Idempotency

[How duplicates are handled]

## Observability

[Metric names, log fields, alert thresholds]

## Infrastructure

[AWS resources, IAM, Terraform changes — if applicable]

## Migration

[Schema changes, rollout order — if applicable]

## Open Questions

[Anything unresolved that implementation may clarify]
```

After writing the file, say:
> "Spec written to `docs/specs/{feature-name}.md`. Reply **implement** to begin coding, or request revisions."

---

## Hard Gate — No Code Before Approval

**Do not write any implementation code** (Go files, Terraform, SQL migrations, tests) until the user replies with "implement" or an equivalent explicit go-ahead after the spec is written.

If the user asks for code before the spec is approved, respond:
> "Design isn't approved yet. Let's finish the spec first — it takes 5 minutes and prevents rework. Which section would you like to revisit?"

---

## Style Constraints (apply during implementation, after approval)

- Follow existing patterns: `models.NonRetryableError` / `RetryableError` for error classification.
- Emit metrics via `metrics.EmitMetric()` using CloudWatch EMF.
- Use structured logging via `logging.NewLogger()` with `stage`, `event_id`, and `latency_ms` fields.
- DB calls must use `context.WithTimeout(ctx, 5*time.Second)`.
- New DB operations go in `internal/db/db.go` unless they require a new client type.
- Tests must be integration tests against a real DB when they touch PostgreSQL (no mocks for DB layer).
- No co-authored-by lines in commits.
