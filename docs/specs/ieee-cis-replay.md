# Spec: IEEE-CIS Replay Dataset Migration

**Status:** Approved  
**Author:** Hemanth Balla  
**Date:** 2026-04-14

## Problem

The replay service currently streams PaySim synthetic data, which uses unrealistic merchant names (`nameOrig`/`nameDest`) and a simple hour-based timestamp step. Switching to the IEEE-CIS Fraud Detection dataset (`train_transaction.csv`) provides richer, more realistic transaction signals — real card networks, product categories, and email domains — which better exercises the fraud rules engine and produces more meaningful Grafana dashboards.

## Chosen Approach

Replace `mapCSVRowToEvent` with an IEEE-CIS-aware mapper. No new abstractions: the same function signature, same ticker loop, same HTTP POST. Only the column mapping logic changes.

## Data Model

### IEEE-CIS Column → Ingest Event Field

| IEEE-CIS Column | Ingest Field | Transform |
|-----------------|--------------|-----------|
| `TransactionAmt` | `amount` | `strconv.ParseFloat`; default `1.0` if missing/zero |
| `card1` | `user_id` | String as-is; default `"unknown"` if empty |
| `TransactionDT` | `timestamp` | `2024-01-01T00:00:00Z` + `TransactionDT` seconds |
| _(derived)_ | `merchant` | Lookup table on `ProductCD` + `card4` (see below) |
| _(constant)_ | `currency` | `"USD"` always |
| `P_emaildomain`, `card4`, `ProductCD` | `metadata.*` | Pass-through strings |
| `isFraud` | `metadata.is_fraud_ground_truth` | Forward as `"0"` or `"1"` string; default `"0"` if missing |

### Merchant Lookup Table

Keyed by `"<ProductCD>+<card4>"` (both lowercased before lookup):

| Key | Merchant Name |
|-----|---------------|
| `w+visa` | `Amazon Marketplace` |
| `w+mastercard` | `Walmart Online` |
| `c+mastercard` | `Walmart` |
| `c+visa` | `Target` |
| `r+discover` | `Target RedCard` |
| `h+amex` | `Best Buy` |
| `s+visa` | `Steam Games` |
| `s+mastercard` | `PlayStation Store` |
| _(default)_ | `"merchant_" + ProductCD + "_" + card4` |

### Metadata Fields

```json
{
  "email_domain":           "<P_emaildomain>",
  "card_network":           "<card4>",
  "product_code":           "<ProductCD>",
  "is_fraud_ground_truth":  "0" | "1"
}
```

`is_fraud_ground_truth` enables a future Grafana panel comparing rules-detected fraud vs. ground-truth labeled fraud — a precision/recall overlay useful for portfolio demos.

### Timestamp Derivation

```
epoch_offset = 2024-01-01T00:00:00Z (Unix: 1704067200)
timestamp    = epoch_offset + time.Duration(TransactionDT) * time.Second
```

`TransactionDT` is an integer number of seconds since the dataset's simulation start. If the column is missing or unparseable, fall back to `time.Now().UTC()`.

## API Contract

No changes. The mapper output must satisfy the existing ingest POST body shape:

```json
{
  "user_id":   "string",
  "amount":    float64,
  "currency":  "USD",
  "merchant":  "string",
  "timestamp": "RFC3339",
  "metadata":  { "email_domain": "...", "card_network": "...", "product_code": "...", "is_fraud_ground_truth": "0" }
}
```

## Processing Logic

1. Open `CSV_FILE` (default `/data/transactions.csv`).
2. Read header row; build `colIdx` map (unchanged).
3. For each row, call updated `mapCSVRowToEvent`:
   a. Extract `TransactionAmt` → `amount`; clamp to `1.0` if `≤ 0`.
   b. Extract `card1` → `user_id`; default `"unknown"` if empty.
   c. Parse `TransactionDT` as `float64` → derive `timestamp` from fixed epoch offset.
   d. Derive `merchant` via lookup table (lowercase `ProductCD + "+" + card4`; fall back to `"merchant_<ProductCD>_<card4>"`).
   e. Set `currency = "USD"`.
   f. Populate `metadata` with `P_emaildomain`, `card4`, `ProductCD`, and `isFraud` as `is_fraud_ground_truth` (`"0"` or `"1"`; default `"0"` if column absent).
4. Marshal to JSON, POST to ingest. Throttle via ticker (unchanged).
5. Progress log every 1 000 rows; final summary on EOF (unchanged).

**Error handling** (unchanged from current service):
- Row parse error → `failed++`, continue.
- HTTP error or non-202 → `failed++`, log every 100 failures, continue.
- No retries; replay is best-effort throughput.

## Idempotency

Unchanged. The downstream ingest service enforces idempotency via `idempotency_keys`. The replay service itself is stateless and makes no idempotency guarantees on re-run.

## Observability

No new metrics or log fields required. Existing progress logs (`sent`, `failed` counts) are sufficient. The `user_id` values (`card1`) are integers like `13926`; Grafana "top flagged users" panel will display them as-is.

## Infrastructure

- `CSV_FILE` env var in `docker-compose.yml` replay service stays `/data/transactions.csv` — no path change needed as long as the user places `train_transaction.csv` renamed to `transactions.csv` under `./data/`.
- README must be updated to point to the IEEE-CIS Kaggle dataset instead of PaySim.
- No new Docker volumes, env vars, or service dependencies.

## Migration

No schema changes. No other services are affected. The change is entirely contained within `services/replay/main.go` and `README.md`:

**`services/replay/main.go`**
- Delete `mapCSVRowToEvent` (PaySim version).
- Add `mapIEEECISRowToEvent` (or rename in-place).

**`README.md`**
- Replace the "Fraud Detection Replay" section dataset reference:
  - Remove: PaySim link (`https://www.kaggle.com/datasets/ealaxi/paysim1`) and `transactions.csv` instruction tied to PaySim
  - Add: IEEE-CIS Fraud Detection dataset link `https://www.kaggle.com/c/ieee-fraud-detection/data`
  - Update file rename instruction: download `train_transaction.csv` from the competition, rename to `transactions.csv`, place at `./data/transactions.csv`
  - Update column description note (remove PaySim column references; note IEEE-CIS fields: `TransactionAmt`, `card1`, `ProductCD`, `card4`, `P_emaildomain`, `isFraud`)

## Open Questions

_(Both resolved — no open questions remain.)_

- ~~Should `isFraud` be forwarded in metadata?~~ **Resolved:** Yes. Forward as `is_fraud_ground_truth: "0"|"1"`. Enables future Grafana precision/recall panel.
- ~~Are ~13 000 unique `card1` values sufficient for velocity checks?~~ **Resolved:** Yes. At 200 req/s, card1 values repeat frequently enough to trigger velocity rules.
