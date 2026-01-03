#!/bin/bash
# Load test script for Fluxa API endpoint
# Sends N events with configurable concurrency and reports throughput metrics

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
TERRAFORM_DIR="$REPO_ROOT/infra/terraform/envs/dev"

# Defaults
NUM_EVENTS="${NUM_EVENTS:-1000}"
CONCURRENCY="${CONCURRENCY:-20}"
OUTPUT_DIR="${REPO_ROOT}/tmp"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

info() {
  echo -e "${GREEN}[INFO]${NC} $1"
}

warn() {
  echo -e "${YELLOW}[WARN]${NC} $1"
}

# Get API endpoint
if [ -n "${API_ENDPOINT:-}" ]; then
  info "Using API_ENDPOINT from environment: $API_ENDPOINT"
  ENDPOINT="$API_ENDPOINT"
else
  info "API_ENDPOINT not set, attempting to read from Terraform output..."
  if [ ! -d "$TERRAFORM_DIR" ]; then
    echo "Error: Terraform directory not found: $TERRAFORM_DIR" >&2
    echo "Please set API_ENDPOINT environment variable or run from correct directory" >&2
    exit 1
  fi
  
  cd "$TERRAFORM_DIR"
  if ! ENDPOINT=$(terraform output -raw api_endpoint 2>/dev/null); then
    echo "Error: Failed to read api_endpoint from Terraform output" >&2
    echo "Please run 'terraform apply' first, or set API_ENDPOINT environment variable" >&2
    exit 1
  fi
  info "Using API endpoint from Terraform: $ENDPOINT"
fi

# Remove trailing slash if present
ENDPOINT="${ENDPOINT%/}"

# Verify endpoint is a valid URL
if [[ ! "$ENDPOINT" =~ ^https?:// ]]; then
  echo "Error: Invalid API endpoint format: $ENDPOINT" >&2
  exit 1
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Generate timestamped output file
TIMESTAMP=$(date +%Y%m%d_%H%M%S)
EVENT_IDS_FILE="$OUTPUT_DIR/load_test_event_ids_${TIMESTAMP}.txt"
STATS_FILE="$OUTPUT_DIR/load_test_stats_${TIMESTAMP}.txt"

info "Load test configuration:"
info "  Endpoint: $ENDPOINT"
info "  Events: $NUM_EVENTS"
info "  Concurrency: $CONCURRENCY"
info "  Event IDs file: $EVENT_IDS_FILE"
echo ""

# Function to send a single event
send_event() {
  local event_num=$1
  local correlation_id="load-$(date +%s)-$RANDOM-$event_num"
  
  # Generate event payload
  local payload=$(cat <<EOF
{
  "user_id": "load_test_user_$event_num",
  "amount": $((RANDOM % 1000 + 10)).$((RANDOM % 100)),
  "currency": "USD",
  "merchant": "Load Test Merchant $event_num",
  "timestamp": "$(date -u +%Y-%m-%dT%H:%M:%SZ)",
  "metadata": {
    "source": "load_test",
    "event_num": $event_num,
    "correlation_id": "$correlation_id"
  }
}
EOF
)
  
  # Send event and capture response
  local response=$(curl -s -w "\n%{http_code}" \
    -X POST "$ENDPOINT/events" \
    -H "Content-Type: application/json" \
    -H "X-Correlation-ID: $correlation_id" \
    -d "$payload" 2>&1 || echo "ERROR\n000")
  
  local http_code=$(echo "$response" | tail -n 1)
  local body=$(echo "$response" | head -n -1)
  
  if [ "$http_code" == "202" ]; then
    # Extract event_id
    if command -v jq >/dev/null 2>&1; then
      local event_id=$(echo "$body" | jq -r '.event_id // empty')
    else
      # Fallback: extract with grep/sed
      local event_id=$(echo "$body" | grep -o '"event_id"[[:space:]]*:[[:space:]]*"[^"]*"' | sed 's/.*"event_id"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/')
    fi
    
    if [ -n "$event_id" ] && [ "$event_id" != "null" ]; then
      echo "$event_id"
      return 0
    fi
  fi
  
  echo "FAILED:$http_code" >&2
  return 1
}

# Export function and variables for parallel execution
export -f send_event
export ENDPOINT

# Create temporary file for event numbers
SEQ_FILE=$(mktemp)
seq 1 "$NUM_EVENTS" > "$SEQ_FILE"

info "Starting load test..."
START_TIME=$(date +%s)

# Run parallel requests using xargs
# Use xargs -P for concurrency control
SUCCESS_COUNT=0
FAIL_COUNT=0

if command -v xargs >/dev/null 2>&1 && xargs --version 2>&1 | grep -q "GNU"; then
  # GNU xargs supports -P and parallel processing
  info "Using xargs for parallel execution..."
  
  # Run in parallel and capture event IDs
  cat "$SEQ_FILE" | xargs -P "$CONCURRENCY" -I {} bash -c 'send_event {}' > "$EVENT_IDS_FILE.tmp" 2>"$STATS_FILE.tmp"
  
  # Count successes and failures
  SUCCESS_COUNT=$(grep -v "^FAILED:" "$EVENT_IDS_FILE.tmp" 2>/dev/null | wc -l | tr -d ' ')
  FAIL_COUNT=$(grep -c "^FAILED:" "$STATS_FILE.tmp" 2>/dev/null || echo "0")
  
  # Filter out failures from event IDs file
  grep -v "^FAILED:" "$EVENT_IDS_FILE.tmp" > "$EVENT_IDS_FILE" 2>/dev/null || true
  
else
  # Fallback: sequential execution (slower)
  warn "xargs not available or not GNU version, running sequentially..."
  
  while IFS= read -r event_num; do
    if send_event "$event_num" >> "$EVENT_IDS_FILE" 2>>"$STATS_FILE"; then
      SUCCESS_COUNT=$((SUCCESS_COUNT + 1))
    else
      FAIL_COUNT=$((FAIL_COUNT + 1))
    fi
  done < "$SEQ_FILE"
fi

END_TIME=$(date +%s)
ELAPSED=$((END_TIME - START_TIME))

# Cleanup temp files
rm -f "$SEQ_FILE" "$EVENT_IDS_FILE.tmp" "$STATS_FILE.tmp" 2>/dev/null || true

# Calculate metrics
if [ $ELAPSED -gt 0 ]; then
  EVENTS_PER_SEC=$((SUCCESS_COUNT / ELAPSED))
  EVENTS_PER_MIN=$((SUCCESS_COUNT * 60 / ELAPSED))
else
  EVENTS_PER_SEC=0
  EVENTS_PER_MIN=0
fi

# Print summary
echo ""
echo "=========================================="
info "Load Test Summary"
echo "=========================================="
info "Total events requested: $NUM_EVENTS"
info "Successful: $SUCCESS_COUNT"
if [ $FAIL_COUNT -gt 0 ]; then
  warn "Failed: $FAIL_COUNT"
fi
info "Elapsed time: ${ELAPSED}s"
info "Throughput: ${EVENTS_PER_SEC} events/sec"
info "Throughput: ${EVENTS_PER_MIN} events/min"
info "Event IDs saved to: $EVENT_IDS_FILE"
echo "=========================================="

# Save stats to file
cat > "$STATS_FILE" <<EOF
Load Test Statistics
====================
Timestamp: $(date -u +%Y-%m-%dT%H:%M:%SZ)
Endpoint: $ENDPOINT
Total events requested: $NUM_EVENTS
Concurrency: $CONCURRENCY
Successful: $SUCCESS_COUNT
Failed: $FAIL_COUNT
Elapsed time (seconds): $ELAPSED
Throughput (events/sec): $EVENTS_PER_SEC
Throughput (events/min): $EVENTS_PER_MIN
Event IDs file: $EVENT_IDS_FILE
EOF

info "Statistics saved to: $STATS_FILE"
echo ""

if [ $SUCCESS_COUNT -eq $NUM_EVENTS ]; then
  info "✓ All events sent successfully"
  exit 0
elif [ $SUCCESS_COUNT -gt 0 ]; then
  warn "Some events failed ($FAIL_COUNT failed, $SUCCESS_COUNT succeeded)"
  exit 0  # Non-zero exit only if complete failure
else
  echo "Error: All events failed" >&2
  exit 1
fi
