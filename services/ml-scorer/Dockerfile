# Build from the repo root: docker compose builds with context=.
FROM python:3.12-slim
WORKDIR /app

COPY services/ml-scorer/requirements.txt requirements.txt
RUN pip install --no-cache-dir -r requirements.txt

# Generate gRPC stubs from the scorer proto.
COPY proto/scorer/v1/scorer.proto proto/scorer/v1/scorer.proto
COPY services/ml-scorer/gen_stubs.sh gen_stubs.sh
RUN sh gen_stubs.sh

# App + model artifacts (baked into the image; retrain => rebuild or mount).
COPY services/ml-scorer/server.py server.py
COPY ml/artifacts ml/artifacts

ENV ARTIFACTS_DIR=/app/ml/artifacts SCORER_PORT=9097 METRICS_PORT=9098
EXPOSE 9097 9098
CMD ["python", "server.py"]
