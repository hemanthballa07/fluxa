#!/usr/bin/env python
"""Fluxa ML scorer — gRPC service serving the ONNX fraud model.

Loads model.onnx + encoder.json once at startup. Score() encodes the incoming
raw features EXACTLY as ml/train.py did (numeric passthrough, one-hot, frequency,
all categoricals lowercased; column order = encoder.json["column_order"]) and runs
onnxruntime inference. Fail-open is handled caller-side (the Go engine), so this
service just returns ml_score + model_version.
"""
import argparse
import json
import os
import time
from concurrent import futures

import grpc
import numpy as np
import onnxruntime as ort
from prometheus_client import Counter, Histogram, start_http_server

import scorer_pb2
import scorer_pb2_grpc

SCORE_TOTAL = Counter("scorer_score_total", "Score RPCs", ["status"])
SCORE_LAT = Histogram("scorer_score_latency_seconds", "Score latency seconds")


class Encoder:
    def __init__(self, enc):
        self.numeric = enc["numeric"]
        self.onehot = enc["onehot"]
        self.frequency = enc["frequency"]
        self.column_order = enc["column_order"]
        self.model_version = enc.get("model_version", "v1")
        self.onnx_input = enc.get("onnx_input", "input")
        self.onnx_prob_output = enc["onnx_prob_output"]
        self.onnx_prob_index = int(enc.get("onnx_prob_index", 1))

    def vector(self, req):
        vals = []
        for c in self.numeric:
            vals.append(float(getattr(req, c)))
        for c, vocab in self.onehot.items():
            v = str(getattr(req, c)).lower()
            for u in vocab:
                vals.append(1.0 if v == u else 0.0)
        for c, fmap in self.frequency.items():
            v = str(getattr(req, c)).lower()
            vals.append(float(fmap.get(v, 0.0)))
        return np.array([vals], dtype=np.float32)


class ScorerServicer(scorer_pb2_grpc.ScorerServicer):
    def __init__(self, sess, enc):
        self.sess = sess
        self.enc = enc

    def Score(self, request, context):
        start = time.time()
        try:
            x = self.enc.vector(request)
            out = self.sess.run([self.enc.onnx_prob_output], {self.enc.onnx_input: x})[0]
            arr = np.asarray(out)
            score = float(arr[0, self.enc.onnx_prob_index]) if arr.ndim == 2 else float(arr.ravel()[0])
            SCORE_TOTAL.labels(status="ok").inc()
            return scorer_pb2.ScoreResponse(ml_score=score, model_version=self.enc.model_version)
        except Exception as e:  # noqa: BLE001 — surface as gRPC error; caller fails open
            SCORE_TOTAL.labels(status="error").inc()
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return scorer_pb2.ScoreResponse()
        finally:
            SCORE_LAT.observe(time.time() - start)


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--artifacts", default=os.getenv("ARTIFACTS_DIR", "ml/artifacts"))
    ap.add_argument("--port", type=int, default=int(os.getenv("SCORER_PORT", "9097")))
    ap.add_argument("--metrics-port", type=int, default=int(os.getenv("METRICS_PORT", "9098")))
    args = ap.parse_args()

    with open(os.path.join(args.artifacts, "encoder.json")) as fh:
        enc = Encoder(json.load(fh))
    sess = ort.InferenceSession(os.path.join(args.artifacts, "model.onnx"),
                                providers=["CPUExecutionProvider"])

    start_http_server(args.metrics_port)
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=8))
    scorer_pb2_grpc.add_ScorerServicer_to_server(ScorerServicer(sess, enc), server)
    server.add_insecure_port(f"[::]:{args.port}")
    server.start()
    print(f"ml-scorer listening on :{args.port} (metrics :{args.metrics_port}), "
          f"model_version={enc.model_version}, n_cols={len(enc.column_order)}", flush=True)
    server.wait_for_termination()


if __name__ == "__main__":
    main()
