# Metrics Capture Guide

This guide explains how to capture "resume-grade" performance metrics from the Fluxa system using the provided automation scripts.

## Prerequisites

- AWS CLI configured with `AdministratorAccess` (or sufficient permissions for CloudWatch/Lambda/API Gateway).
- `jq` installed (for JSON parsing).
- A deployed Fluxa environment (Dev or Prod).

## Key Metrics

We capture the following key performance indicators (KPIs) to demonstrate system scale and reliability:

| Metric | Description | Source |
|--------|-------------|--------|
| **Ingest Latency (p95)** | Time for API Gateway + Ingest Lambda to accept an event. | CloudWatch: `Fluxa/Ingest` -> `ingest_latency_ms` |
| **Process Latency (p95)** | End-to-end processing time (SQS -> Lambda -> DB). | CloudWatch: `Fluxa/Processor` -> `process_latency_ms` |
| **Throughput** | Events successfully ingested per minute. | CloudWatch: `Fluxa/Ingest` -> `ingest_success` (Sum) |
| **Error Rate** | Percentage of failed requests vs. total requests. | CloudWatch: `Fluxa/Ingest` -> `ingest_failure` / `ingest_success` |

## Automated Capture

Run the `capture_metrics.sh` script to automatically load test the system and generate a report.

```bash
# 1. Export your API Endpoint (from terraform output)
export API_ENDPOINT="https://xxxxxx.execute-api.us-east-1.amazonaws.com/dev"

# 2. Run the capture script
#    - n: Number of events
#    - c: Concurrency level
./scripts/capture_metrics.sh -n 1000 -c 10
```

### Script Output

The script will generate artifacts in the `out/` directory:

- `out/metrics.json`: Raw metrics data.
- `out/metrics.md`: A markdown summary table ready for copy-pasting.

## Manual verification

You can verifying the numbers in the AWS Console:

1. Go to **CloudWatch > Metrics > All metrics**.
2. Select **Fluxa/Ingest** namespace.
3. Select `ingest_latency_ms` metric.
4. Set **Statistic** to `p95` and **Period** to `1 Minute`.
