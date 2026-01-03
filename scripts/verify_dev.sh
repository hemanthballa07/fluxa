#!/bin/bash
# Deployment verification script for Fluxa dev environment
# Verifies end-to-end functionality: health check, event ingestion, and query

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TERRAFORM_DIR="$REPO_ROOT/infra/terraform/envs/dev"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Function to print colored messages
info() {
  echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
  echo -e "${YELLOW}[WARN]${NC} $1"
}

error() {
  echo -e "${RED}[ERROR]${NC} $1" >&2
}

# Get API endpoint
if [ -n "${API_ENDPOINT:-}" ]; then
  info "Using API_ENDPOINT from environment: $API_ENDPOINT"
  ENDPOINT="$API_ENDPOINT"
else
  info "API_ENDPOINT not set, attempting to read from Terraform output..."
  if [ ! -d "$TERRAFORM_DIR" ]; then
    error "Terraform directory not found: $TERRAFORM_DIR"
    error "Please set API_ENDPOINT environment variable or run from correct directory"
    exit 1
  fi
  
  cd "$TERRAFORM_DIR"
  if ! ENDPOINT=$(terraform output -raw api_endpoint 2>/dev/null); then
    error "Failed to read api_endpoint from Terraform output"
    error "Please run 'terraform apply' first, or set API_ENDPOINT environment variable"
    exit 1
  fi
  info "Using API endpoint from Terraform: $ENDPOINT"
fi

# Remove trailing slash if present
ENDPOINT="${ENDPOINT%/}"

# Verify endpoint is a valid URL
if [[ ! "$ENDPOINT" =~ ^https?:// ]]; then
  error "Invalid API endpoint format: $ENDPOINT"
  exit 1
fi

START_TIME=$(date +%s)
SUCCESS_COUNT=0
FAILED_EVENTS=()

# Test 1: Health check
info "Testing health endpoint: $ENDPOINT/health"
HTTP_CODE=$(curl -s -o /tmp/health_response.json -w "%{http_code}" "$ENDPOINT/health" || echo "000")

if [ "$HTTP_CODE" != "200" ]; then
  error "Health check failed with HTTP $HTTP_CODE"
  if [ -f /tmp/health_response.json ]; then
    error "Response: $(cat /tmp/health_response.json)"
  fi
  exit 1
fi
info "✓ Health check passed"

# Test 2: Ingest and query events
info "Starting event ingestion and verification..."

EVENT_IDS=()

# Generate event payloads with varying sizes
# 8 small events (<1KB each)
for i in {1..8}; do
  EVENT_IDS+=("small_$i")
done

# 1 medium-large event (~250KB - should be INLINE)
EVENT_IDS+=("large_inline")

# 1 very large event (~300KB - should be S3)
EVENT_IDS+=("large_s3")

TOTAL_EVENTS=${#EVENT_IDS[@]}
info "Will ingest and verify $TOTAL_EVENTS events"

# Function to generate event payload
generate_event() {
  local event_type=$1
  local event_id=$2
  
  case "$event_type" in
    small_*)
      cat <<EOF
{
  "event_id": "verify-$(date +%s)-$event_id",
  "user_id": "verify_user_${event_id}",
  "amount": $((RANDOM % 1000 + 10)).$((RANDOM % 100)),
  "currency": "USD",
  "merchant": "Verification Test Merchant $event_id",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "metadata": {
    "source": "verify_dev_script",
    "test_id": "$event_id",
    "iteration": "${event_id#small_}"
  }
}
EOF
      ;;
    large_inline)
      # Generate ~250KB payload
      "$SCRIPT_DIR/gen_large_payload.sh" inline | jq --arg eid "verify-$(date +%s)-inline" '. + {event_id: $eid}'
      ;;
    large_s3)
      # Generate ~300KB payload
      "$SCRIPT_DIR/gen_large_payload.sh" s3 | jq --arg eid "verify-$(date +%s)-s3" '. + {event_id: $eid}'
      ;;
    *)
      error "Unknown event type: $event_type"
      return 1
      ;;
  esac
}

# Function to query event until found or timeout
query_event_until_found() {
  local event_id=$1
  local max_wait=30
  local elapsed=0
  local interval=2
  
  while [ $elapsed -lt $max_wait ]; do
    HTTP_CODE=$(curl -s -o /tmp/query_response.json -w "%{http_code}" "$ENDPOINT/events/$event_id" || echo "000")
    
    if [ "$HTTP_CODE" == "200" ]; then
      return 0
    elif [ "$HTTP_CODE" == "404" ]; then
      sleep $interval
      elapsed=$((elapsed + interval))
    else
      error "Query failed with HTTP $HTTP_CODE for event $event_id"
      return 1
    fi
  done
  
  return 1
}

# Ingest events
info "Ingesting $TOTAL_EVENTS events..."
for event_type in "${EVENT_IDS[@]}"; do
  info "  Ingesting event: $event_type"
  
  EVENT_PAYLOAD=$(generate_event "$event_type" "$event_type")
  CORRELATION_ID="verify-$(date +%s)-$RANDOM"
  
  HTTP_CODE=$(curl -s -o /tmp/ingest_response.json -w "%{http_code}" \
    -X POST "$ENDPOINT/events" \
    -H "Content-Type: application/json" \
    -H "X-Correlation-ID: $CORRELATION_ID" \
    -d "$EVENT_PAYLOAD" || echo "000")
  
  if [ "$HTTP_CODE" != "202" ]; then
    error "Ingest failed with HTTP $HTTP_CODE for event $event_type"
    if [ -f /tmp/ingest_response.json ]; then
      error "Response: $(cat /tmp/ingest_response.json)"
    fi
    FAILED_EVENTS+=("$event_type")
    continue
  fi
  
  # Extract event_id from response
  EVENT_ID=$(jq -r '.event_id' /tmp/ingest_response.json 2>/dev/null || echo "")
  if [ -z "$EVENT_ID" ] || [ "$EVENT_ID" == "null" ]; then
    error "Failed to extract event_id from ingest response for $event_type"
    FAILED_EVENTS+=("$event_type")
    continue
  fi
  
  info "    ✓ Ingested: $EVENT_ID"
  
  # Query event until found
  info "    Querying event $EVENT_ID..."
  if query_event_until_found "$EVENT_ID"; then
    info "    ✓ Event found in database"
    SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
  else
    error "    ✗ Event not found after 30s timeout"
    FAILED_EVENTS+=("$event_type")
  fi
  
  # Small delay between events
  sleep 1
done

# Summary
END_TIME=$(date +%s)
TOTAL_TIME=$((END_TIME - START_TIME))

echo ""
echo "=========================================="
info "Verification Summary"
echo "=========================================="
info "Total events tested: $TOTAL_EVENTS"
info "Successful: $SUCCESS_COUNT"
if [ ${#FAILED_EVENTS[@]} -gt 0 ]; then
  error "Failed: ${#FAILED_EVENTS[@]}"
  error "Failed events: ${FAILED_EVENTS[*]}"
else
  info "Failed: 0"
fi
info "Total time: ${TOTAL_TIME}s"
echo "=========================================="

if [ $SUCCESS_COUNT -eq $TOTAL_EVENTS ]; then
  info "✓ All verifications passed!"
  exit 0
else
  error "✗ Some verifications failed"
  exit 1
fi


