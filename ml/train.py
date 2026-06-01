#!/usr/bin/env python
"""Train the Fluxa ML fraud scorer on the exported IEEE-CIS feature CSV.

Pipeline: temporal split -> encode (numeric passthrough, one-hot low-card,
frequency-encode high-card, all categoricals lowercased) -> XGBoost ->
export ONNX + encoder.json + metrics.json. The encoder.json schema is the
single train/serve contract (server.py reads it to encode incoming requests).

Run inside the conda env:  conda run -n fluxa-ml python ml/train.py
"""
import json
import os

import numpy as np
import pandas as pd
import xgboost as xgb
from sklearn.metrics import average_precision_score

import onnxruntime as ort
from onnxmltools import convert_xgboost
from onnxmltools.convert.common.data_types import FloatTensorType

SEED = 42
MODEL_VERSION = "v1"
DATA = "ml/data/features.csv"
ART = "ml/artifacts"

NUMERIC = ["amount", "velocity_60s", "velocity_3600s", "velocity_86400s",
           "secs_since_prev", "user_amt_sum_3600s", "user_amt_max_3600s"]
ONEHOT = ["currency", "product_code", "card_network"]
FREQ = ["merchant", "email_domain"]


def lc(series):
    return series.astype(str).str.lower()


def build_encoder(train_df):
    enc = {"model_version": MODEL_VERSION, "numeric": NUMERIC, "onehot": {}, "frequency": {}}
    for c in ONEHOT:
        vocab = sorted(lc(train_df[c]).unique().tolist())
        enc["onehot"][c] = vocab
    n = len(train_df)
    for c in FREQ:
        counts = lc(train_df[c]).value_counts()
        enc["frequency"][c] = {k: round(v / n, 8) for k, v in counts.items()}
    order = list(NUMERIC)
    for c in ONEHOT:
        order += [f"{c}={v}" for v in enc["onehot"][c]]
    order += [f"{c}_freq" for c in FREQ]
    enc["column_order"] = order
    return enc


def encode(df, enc):
    cols = [df[c].astype(float).values for c in NUMERIC]
    for c in ONEHOT:
        lcv = lc(df[c])
        for v in enc["onehot"][c]:
            cols.append((lcv == v).astype(np.float32).values)
    for c in FREQ:
        fmap = enc["frequency"][c]
        cols.append(lc(df[c]).map(fmap).fillna(0.0).astype(np.float32).values)
    return np.column_stack(cols).astype(np.float32)


def main():
    os.makedirs(ART, exist_ok=True)
    df = pd.read_csv(DATA, dtype={"currency": str, "product_code": str,
                                  "card_network": str, "merchant": str, "email_domain": str})
    df = df.fillna({c: "" for c in ONEHOT + FREQ})
    y = df["label"].astype(int).values
    n = len(df)
    # Temporal split (rows are ts-ordered by the exporter): 70 / 15 / 15.
    i1, i2 = int(n * 0.70), int(n * 0.85)
    tr, va, te = df.iloc[:i1], df.iloc[i1:i2], df.iloc[i2:]
    ytr, yva, yte = y[:i1], y[i1:i2], y[i2:]
    print(f"rows={n} train={len(tr)} val={len(va)} test={len(te)} "
          f"pos_rate_train={ytr.mean():.4f} pos_rate_test={yte.mean():.4f}")

    enc = build_encoder(tr)
    Xtr, Xva, Xte = encode(tr, enc), encode(va, enc), encode(te, enc)
    ncols = Xtr.shape[1]
    print(f"encoded n_cols={ncols}")

    pos = max(int(ytr.sum()), 1)
    neg = len(ytr) - int(ytr.sum())
    clf = xgb.XGBClassifier(
        n_estimators=300, max_depth=6, learning_rate=0.1,
        subsample=0.9, colsample_bytree=0.9,
        scale_pos_weight=neg / pos, eval_metric="aucpr",
        random_state=SEED, n_jobs=8, tree_method="hist",
    )
    clf.fit(Xtr, ytr, eval_set=[(Xva, yva)], verbose=False)

    def prauc(X, yy):
        return float(average_precision_score(yy, clf.predict_proba(X)[:, 1]))
    metrics = {"pr_auc_train": prauc(Xtr, ytr), "pr_auc_val": prauc(Xva, yva),
               "pr_auc_test": prauc(Xte, yte), "n_cols": ncols,
               "rows": n, "pos_rate_test": float(yte.mean())}
    print("PR-AUC", metrics)

    # Production tau on val: smallest threshold with val FPR <= 1%.
    pva = clf.predict_proba(Xva)[:, 1]
    negmask = yva == 0
    tau = 1.0
    for t in np.linspace(0.01, 0.99, 99):
        fpr = float(((pva >= t) & negmask).sum()) / max(int(negmask.sum()), 1)
        if fpr <= 0.01:
            tau = float(round(t, 4))
            break
    enc["tau"] = tau
    metrics["tau"] = tau
    print("tau(val FPR<=1%)", tau)

    # Export ONNX and discover the probability output (robust to converter format).
    onx = convert_xgboost(clf, initial_types=[("input", FloatTensorType([None, ncols]))])
    onnx_path = os.path.join(ART, "model.onnx")
    with open(onnx_path, "wb") as fh:
        fh.write(onx.SerializeToString())
    sess = ort.InferenceSession(onnx_path, providers=["CPUExecutionProvider"])
    in_name = sess.get_inputs()[0].name
    prob_out, prob_index = None, 1
    sample = Xte[:5]
    for o in sess.get_outputs():
        try:
            res = sess.run([o.name], {in_name: sample})[0]
        except Exception:
            continue
        arr = np.asarray(res)
        if arr.dtype.kind == "f" and arr.ndim == 2 and arr.shape[1] >= 2:
            prob_out, prob_index = o.name, 1
            break
    if prob_out is None:  # fallback: last float output
        prob_out = sess.get_outputs()[-1].name
    enc["onnx_input"] = in_name
    enc["onnx_prob_output"] = prob_out
    enc["onnx_prob_index"] = prob_index
    # sanity: onnx vs xgboost on a sample
    onnx_p = np.asarray(sess.run([prob_out], {in_name: sample})[0])
    onnx_score = onnx_p[:, prob_index] if onnx_p.ndim == 2 else onnx_p.ravel()
    xgb_score = clf.predict_proba(sample)[:, 1]
    metrics["onnx_xgb_max_abs_diff"] = float(np.max(np.abs(onnx_score - xgb_score)))
    print("onnx vs xgb max abs diff", metrics["onnx_xgb_max_abs_diff"])

    with open(os.path.join(ART, "encoder.json"), "w") as fh:
        json.dump(enc, fh, indent=2)
    with open(os.path.join(ART, "metrics.json"), "w") as fh:
        json.dump(metrics, fh, indent=2)
    print("wrote", onnx_path, "+ encoder.json + metrics.json")


if __name__ == "__main__":
    main()
