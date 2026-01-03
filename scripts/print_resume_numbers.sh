#!/bin/bash
# Print resume-ready metrics from load test and deployment
# Helps populate resume bullets with real numbers

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
STATS_DIR="$REPO_ROOT/tmp"

# Colors
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

info() {
  echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
  echo -e "${YELLOW}[WARN]${NC} $1"
}

header() {
  echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
  echo -e "${CYAN}$1${NC}"
  echo -e "${CYAN}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
}

header "Fluxa Resume Metrics Summary"

# 1. Load Test Throughput
echo ""
info "1. Load Test Throughput"

if [ ! -d "$STATS_DIR" ]; then
  warn "  tmp/ directory not found. Run load test first: ./scripts/load_test.sh"
  THROUGHPUT="[TBD - run ./scripts/load_test.sh]"
else
  LATEST_STATS=$(ls -t "$STATS_DIR"/load_test_stats_*.txt 2>/dev/null | head -1)
  
  if [ -z "$LATEST_STATS" ]; then
    warn "  No load test stats found. Run load test first: ./scripts/load_test.sh"
    THROUGHPUT="[TBD - run ./scripts/load_test.sh]"
  else
    info "  Reading from: $(basename "$LATEST_STATS")"
    THROUGHPUT=$(grep "Throughput (events/min):" "$LATEST_STATS" | awk '{print $4}' || echo "[TBD]")
    if [ -n "$THROUGHPUT" ] && [ "$THROUGHPUT" != "[TBD]" ]; then
      echo "  ✓ Throughput: ${THROUGHPUT} events/min"
    else
      warn "  Could not extract throughput from stats file"
      THROUGHPUT="[TBD]"
    fi
  fi
fi

# 2. Terraform Apply Time
echo ""
info "2. Terraform Apply Time"

TERRAFORM_TIME_FILE="$STATS_DIR/terraform_apply_time.txt"
if [ -f "$TERRAFORM_TIME_FILE" ]; then
  TERRAFORM_TIME=$(cat "$TERRAFORM_TIME_FILE")
  echo "  ✓ Terraform apply: ${TERRAFORM_TIME} minutes"
else
  warn "  terraform_apply_time.txt not found"
  info "  To capture, run:"
  echo "    START=\$(date +%s) && terraform apply && END=\$(date +%s) && echo \$(( (END - START) / 60 )) > tmp/terraform_apply_time.txt"
  TERRAFORM_TIME="[TBD]"
fi

# 3. CloudWatch Metrics (placeholders)
echo ""
info "3. CloudWatch Latency Metrics (p95)"

if [ -n "${INGEST_P95_MS:-}" ]; then
  echo "  ✓ Ingest p95 latency: ${INGEST_P95_MS}ms (from INGEST_P95_MS env var)"
else
  INGEST_P95_MS="[TBD]"
  echo "  ⚠ Ingest p95 latency: ${INGEST_P95_MS}"
  echo "     To capture, run:"
  echo "       aws cloudwatch get-metric-statistics \\"
  echo "         --namespace Fluxa/Ingest \\"
  echo "         --metric-name ingest_latency_ms \\"
  echo "         --start-time \$(date -u -v-1H +%Y-%m-%dT%H:%M:%S) \\"
  echo "         --end-time \$(date -u +%Y-%m-%dT%H:%M:%S) \\"
  echo "         --period 300 \\"
  echo "         --extended-statistics p95 \\"
  echo "         --region us-east-1 | jq '.Datapoints | map(.ExtendedStatistics.p95) | sort | .[-1]'"
  echo ""
  echo "     Or export: export INGEST_P95_MS=<value>"
fi

if [ -n "${PROCESS_P95_MS:-}" ]; then
  echo "  ✓ Process p95 latency: ${PROCESS_P95_MS}ms (from PROCESS_P95_MS env var)"
else
  PROCESS_P95_MS="[TBD]"
  echo "  ⚠ Process p95 latency: ${PROCESS_P95_MS}"
  echo "     To capture, run:"
  echo "       aws cloudwatch get-metric-statistics \\"
  echo "         --namespace Fluxa/Processor \\"
  echo "         --metric-name process_latency_ms \\"
  echo "         --start-time \$(date -u -v-1H +%Y-%m-%dT%H:%M:%S) \\"
  echo "         --end-time \$(date -u +%Y-%m-%dT%H:%M:%S) \\"
  echo "         --period 300 \\"
  echo "         --extended-statistics p95 \\"
  echo "         --region us-east-1 | jq '.Datapoints | map(.ExtendedStatistics.p95) | sort | .[-1]'"
  echo ""
  echo "     Or export: export PROCESS_P95_MS=<value>"
fi

# 4. Resume Bullets
echo ""
header "Resume Bullets (Ready to Copy)"

echo ""
echo "Bullet 1: Latency & Throughput"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Built serverless event pipeline (API Gateway → Lambda → SQS → Lambda → RDS) with p95 latency of ${INGEST_P95_MS}ms for ingestion and ${PROCESS_P95_MS}ms for processing, handling ${THROUGHPUT} events/min at peak load."
echo ""

echo "Bullet 2: Infrastructure & Reliability"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Implemented idempotency guarantees and payload integrity verification (SHA-256) for a serverless event-driven platform, deploying infrastructure via Terraform in ${TERRAFORM_TIME} minutes with zero-downtime updates and automatic DLQ handling for permanent failures."
echo ""

echo "Bullet 3: Observability & Scale"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Instrumented CloudWatch metrics (EMF) and alarms for ${THROUGHPUT} events/min throughput, achieving ${INGEST_P95_MS}ms p95 ingestion latency with intelligent payload handling (inline ≤256KB, S3 for larger payloads) optimizing cost and latency."
echo ""

echo "Bullet 4: Production Engineering"
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Designed and deployed production-grade serverless architecture on AWS with idempotency, DLQ retries, and comprehensive observability, processing ${THROUGHPUT} events/min with ${PROCESS_P95_MS}ms p95 processing latency and ${INGEST_P95_MS}ms p95 ingestion latency."
echo ""

# 5. Instructions
echo ""
header "Next Steps"

echo ""
info "To complete resume bullets:"
echo "  1. Replace [TBD] placeholders with actual CloudWatch metrics"
echo "  2. See docs/METRICS_CAPTURE.md for detailed instructions"
echo "  3. Use CloudWatch Dashboard (see docs/DASHBOARD.md) for visual proof"
echo "  4. Copy bullets above into your resume"
echo ""


