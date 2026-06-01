# Step 5 — ML Fraud Scorer: Design Spec

**Status:** Approved (brainstorming converged, H1–H18 resolved)
**Date:** 2026-05-31
**Author:** Hemanth Balla
**Trifecta step:** 5 (ML scorer) — follows Step 4 (console e2e, CLOSED 2026-05-31)

## Goal

Add a machine-learning fraud scorer to Fluxa that blends with the existing YAML rules engine, trained and evaluated on the IEEE-CIS dataset. Demonstrate that a gradient-boosted model adds measurable lift over hand-tuned rules **on the surface where the model actually has signal**, served through a Python gRPC service (ONNX), wired into Fluxa's shared `fraud.Engine`, and surfaced in the console fraud feed.

Deliverable / acceptance (from `PORTFOLIO_NARRATIVE.md` step 5): PR-AUC with bootstrap CI on the IEEE-CIS held-out split, documented in `docs/ML_EVALUATION.md`.

## Scope

**In scope (5a — ML core):**
- A single authoritative feature builder (`internal/mlfeatures`) computing transaction-time, point-in-time aggregates, used **both** online and by the offline training export.
- Offline training pipeline (conda Python 3.12): temporal split, XGBoost, categorical encoding, ONNX export, evaluation.
- A Python gRPC scorer service (`services/ml-scorer`) serving the ONNX model.
- Blend logic in `fraud.Engine` (rules OR ml≥τ), fail-open on scorer unavailability.
- `docs/ML_EVALUATION.md` generated from the eval run.

**In scope (5b — surface the score):**
- Persist `ml_score` + `model_version` on fraud results.
- Plumb `ml_score` through the SSE `/fraud-events` wire format and the console fraud feed.

**Non-goals:**
- Making the ML model the **decider** on the bankops sync path (it stays rules-primary there — see H1).
- Rich IEEE-CIS feature blocks (V/C/D/M) that can't be produced at serve time.
- Cross-service OTel tracing of the scorer hop (that is Step 6).
- Online/continuous retraining; the model is trained offline and shipped as an artifact.

## Key decisions (H1–H18)

| # | Decision |
|---|---|
| H1 | **Surface:** shared scorer in `fraud.Engine`, called by both async processor and sync gRPC. Headline PR-AUC is measured on the **IEEE-CIS replay distribution** (the async stream that carries labels + features). On the bankops sync path the scorer runs but inputs are feature-poor, so **rules stay the effective decider and the score is advisory**. Stated honestly in `ML_EVALUATION.md`. |
| H2 | **Feature parity by shared code:** features are defined once in `internal/mlfeatures` (Go) and used for both online serving and offline training export. No Python reimplementation of aggregates. |
| H3 | **Temporal split** on `TransactionDT` (train/val/test, oldest→newest), `scale_pos_weight` for the ~3.5% positive rate, PR-AUC (not ROC-AUC) as the headline metric with bootstrap CI. |
| H4 | **Blend:** `FLAG if (any rule fires) OR (ml_score ≥ τ)`. τ chosen on the **validation** split so blended precision ≥ rules-only precision; a synthetic `ml_risk` flag is appended when `score ≥ τ`. |
| H5 | **Proto:** add `double ml_score = 5;` and `string model_version = 6;` to `EvaluateResponse` (new field numbers → backward compatible; bankops ignores unknown fields). |
| H6 | **Fail-open + latency:** scorer is called **after** rules; on timeout (~40 ms)/UNAVAILABLE/error → log and return the rules-only result with `model_version="unavailable"`. Comfortably inside p99 < 100 ms. |
| H7 | **Encoding/OOV:** low-cardinality categoricals (`ProductCD`, `card4`, `currency`) one-hot; high-cardinality (`merchant`, `email_domain`) frequency-encoded. Encoder shipped as `encoder.json`; unseen/missing values → `unknown` bucket (which is what bankops traffic hits → low signal, by design). |
| H8 | **Both surfaces** call the shared scorer; value demonstrated async, advisory sync (follows from H1). |
| H9 | **ONNX:** train XGBoost → export via `onnxmltools` → serve via `onnxruntime` in the Python gRPC service (keeps the Java↔Go↔Python polyglot story; framework-independent, fast inference). |
| H10 | **Env/repro:** conda-managed **Python 3.12** env (`ml/environment.yml`); `make train` with fixed seed; scorer container `python:3.12-slim`. Artifacts (`model.onnx`, `encoder.json`, `metrics.json`) versioned under `ml/artifacts/`. |
| H11 | **Tests:** Go blend + fail-open unit tests; integration test (compose brings up scorer, asserts `ml_score` present + blended decision); feature-parity test (offline export == online compute on a sample); Python eval test with a PR-AUC floor as a regression guard. |
| H12 | **Beats amount itself:** `ML_EVALUATION.md` includes an ablation (amount-only vs full features) + feature importances proving lift comes from aggregates/categoricals, not relearning the amount rule. |
| H13 | **Surface the score (5b):** migration adds `ml_score`/`model_version` to `fraud_flags`; `GetRecentFraudEvents`/`GetFraudEventsSince` select them; SSE `FraudEvent` gains `ml_score`; console renders it. |
| H14 | **Transaction-time aggregates:** ML aggregates use a new point-in-time query over the event's `ts` (not the rules' `created_at`/`NOW()` velocity), so they are meaningful and reproducible on both replay and live traffic. Requires a `(user_id, ts)` index. |
| H15 | **Eval honesty:** primary signal is the ML-standalone PR curve + PR-AUC w/ bootstrap CI; blended-vs-rules lift is secondary, with an explicit note that the rules baseline is weak (no overclaiming). |
| H16 | **Python 3.12** via conda (3.14 host avoided); exact wheel versions pinned at env-setup time. |
| H17 | (Retracted — `amount` is a real `events` column.) |
| H18 | **Decomposition:** build 5a (ML core) first, then 5b (surface the score). |

## Architecture / data flow

```
Training (offline, one-time):
  data/transactions.csv (IEEE-CIS) → replay → ingest → processor → events table (incl. is_fraud_ground_truth)
       → `make export-features` (Go batch, internal/mlfeatures, ts point-in-time) → ml/data/features.parquet
       → ml/train.py (conda py3.12, XGBoost, temporal split) → model.onnx + encoder.json + metrics.json
       → ml/evaluate.py → docs/ML_EVALUATION.md

Serving (online):
  [async] processor ─┐
  [sync]  fraud-grpc ─┴─► fraud.Engine.Evaluate(event, velocityQ, scorer)
                            │  rules (existing) ─────────► flags
                            │  mlfeatures.Build(event) ─── ts aggregates + categoricals
                            └► scorer.Score(features) ──gRPC──► ml-scorer (Py, ONNX) ──► ml_score
                            blend: FLAG if (any rule) OR (ml_score ≥ τ)
                                   fail-open: scorer error/timeout → rules-only result
```

## Components

### `internal/mlfeatures/` (Go, NEW) — authoritative feature builder
- `Build(ctx, event, q FeatureQuerier) (Features, error)` — produces the exact feature vector for one event.
- Owns the **transaction-time point-in-time** aggregate queries (H14):
  - `CountUserEventsAsOf(user_id, ts, window)` → count of same-user events with `ts ≤ event.ts AND ts ≥ event.ts - window`.
  - Recency: seconds since the user's previous event (by `ts`).
  - Rolling user amount stats (sum/avg/max within window).
- Categorical extraction: `merchant`, `currency`, `ProductCD`/`card4`/`email_domain` from `metadata_json`.
- Used by online serving **and** the offline batch export → guarantees parity (H2).
- New migration: index on `events(user_id, ts)`.

### `services/ml-scorer/` (Python 3.12, NEW) — ONNX gRPC server
- Loads `model.onnx` (onnxruntime) + `encoder.json` at startup; one RPC `Score(FeatureVector) → {ml_score, model_version}`.
- Applies the same categorical encoding (from `encoder.json`) to incoming features; unseen → `unknown` bucket.
- Port `:9097`, Prometheus metrics `:9098`, compose service, Prometheus scrape job, `python:3.12-slim` Dockerfile.
- Its own `proto/scorer/v1/scorer.proto`; Go client generated via buf, Python stubs via grpcio-tools.

### `internal/fraud/engine.go` (Go, MODIFIED)
- After rules, call `mlfeatures.Build` then the injected `Scorer` (narrow interface, like `VelocityQuerier`), apply the blend (H4), fail-open (H6).
- Engine stays unit-testable behind the `Scorer` interface (a fake in tests).

### proto (MODIFIED + NEW)
- `EvaluateResponse`: add `ml_score` (5), `model_version` (6).
- New `scorer.proto` for Go↔Python scorer RPC.

### `ml/` (NEW) — training + evaluation
- `environment.yml` (conda py3.12: xgboost, onnxmltools, skl2onnx, onnxruntime, scikit-learn, pandas, pyarrow).
- `train.py` (temporal split, scale_pos_weight, fixed seed → `model.onnx` + `encoder.json` + `metrics.json`).
- `evaluate.py` (PR-AUC + bootstrap CI, ML-standalone PR curve, blended-vs-rules, amount-only ablation → `docs/ML_EVALUATION.md`).
- `artifacts/` (versioned model + encoder + metrics).

### 5b surfacing (MODIFIED)
- Migration: add `ml_score DECIMAL`, `model_version VARCHAR` to `fraud_flags`.
- `internal/db/db.go`: persist on insert; select in `GetRecentFraudEvents`/`GetFraudEventsSince`.
- `domain.FraudEvent` + SSE wire format: add `ml_score`.
- Console: render `ml_score` in the fraud feed / review.

## Feature definition (authoritative)

| Feature | Source | Notes |
|---|---|---|
| `amount` | `events.amount` | also a rule; ML must add lift beyond it |
| `velocity_count_{60s,3600s,86400s}` | `CountUserEventsAsOf` over `ts` | point-in-time, reproducible (H14) |
| `secs_since_prev_user_event` | `ts` of prior user event | recency |
| `user_amt_sum_{window}` / `user_amt_max_{window}` | rolling over `ts` | spend pattern |
| `currency` | `events.currency` | one-hot |
| `product_code` (ProductCD) | `metadata_json` | one-hot |
| `card_network` (card4) | `metadata_json` | one-hot |
| `merchant` | `events.merchant` | frequency-encoded |
| `email_domain` (P_emaildomain) | `metadata_json` | frequency-encoded |

Label: `metadata_json.is_fraud_ground_truth` ("0"/"1"). No feature derives from the label (no leakage; frequency encoding fit on train only).

## Model & training

- **Model:** XGBoost binary classifier, `scale_pos_weight = neg/pos`, fixed `random_state`.
- **Split:** temporal by `TransactionDT` — train (oldest 70%), val (next 15%), test (newest 15%).
- **Threshold τ:** chosen on val so blended precision ≥ rules-only precision (H4).
- **Export:** `onnxmltools.convert_xgboost` → `model.onnx`; `encoder.json` bundled under one `model_version`.

## Evaluation (`docs/ML_EVALUATION.md`)

1. **ML-standalone PR curve + PR-AUC with bootstrap CI** on the held-out test split (primary).
2. **Blended vs rules-only** at the val-chosen operating point (secondary), with an explicit caveat that the rules baseline (`amount>500` etc.) is weak.
3. **Ablation:** amount-only vs full feature set; feature importances (H12/H15).
4. Honest scope note: eval is on the IEEE-CIS replay distribution; bankops sync traffic is feature-poor and rules-primary (H1).

## Error handling / reliability

- Scorer call is **best-effort** and **after** rules; any error/timeout (~40 ms budget) → rules-only result, `model_version="unavailable"`, metric incremented. The fraud decision is never blocked by the scorer (mirrors fluxguard ADR-003 fail-open).
- The async processor failing-open means an event may carry no `ml_score`; the console renders that gracefully (blank/"—").

## Testing

- **Go unit:** blend truth table; fail-open (scorer fake returns error → decision == rules-only).
- **Go integration:** compose brings up `ml-scorer`; `fraud-grpc` eval returns a populated `ml_score` and correct blended decision.
- **Parity test:** offline feature export == online `mlfeatures.Build` for a sampled set of events.
- **Python:** training smoke + eval asserts PR-AUC ≥ a documented floor (CI regression guard).

## Environment / reproducibility

- Conda env `fluxa-ml` (Python 3.12) from `ml/environment.yml`; host 3.14 untouched.
- `make train` / `make export-features` / `make ml-eval` targets; fixed seeds; data snapshot = the committed `data/transactions.csv` (IEEE-CIS).
- Scorer served from `python:3.12-slim`.

## Honest risks / caveats (carried into ML_EVALUATION.md)

- The model's measured lift is on the **IEEE-CIS replay distribution**; on the **bankops** path it sees feature-poor requests (no categoricals, cold user, merchant="UNSPECIFIED") → near-zero added signal, rules-primary. This is intentional and stated.
- The rules baseline is weak; the headline is the ML-standalone PR curve, not "beats the toy rule."
- Replay-derived `created_at` is wall-clock; we deliberately use `ts`/`TransactionDT` for ML aggregates to stay meaningful and reproducible.

## Build order

**5a:** `mlfeatures` (+ index migration) → batch export → conda env + `train.py` → ONNX/encoder artifacts → `ml-scorer` service + proto → engine blend + fail-open → `evaluate.py` + `ML_EVALUATION.md` → tests.
**5b:** `fraud_flags` migration → db persist/select → SSE `FraudEvent` + wire format → console rendering.
