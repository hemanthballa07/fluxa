#!/bin/bash
# Generate large JSON payloads for testing payload size thresholds
# Usage: ./gen_large_payload.sh <size_type>
# size_type: "inline" (250KB) or "s3" (300KB)

set -euo pipefail

SIZE_TYPE="${1:-inline}"

case "$SIZE_TYPE" in
  inline)
    # Generate ~250KB payload (just under 256KB threshold for INLINE mode)
    TARGET_SIZE=250000
    ;;
  s3)
    # Generate ~300KB payload (over 256KB threshold for S3 mode)
    TARGET_SIZE=300000
    ;;
  *)
    echo "Error: Invalid size type. Use 'inline' or 's3'" >&2
    exit 1
    ;;
esac

# Base event structure (size ~180 bytes)
BASE_EVENT='{
  "user_id": "test_user_large",
  "amount": 1999.99,
  "currency": "USD",
  "merchant": "Large Payload Test Merchant",
  "timestamp": "2024-01-15T10:30:00Z",
  "metadata": {
    "source": "verification_test",
    "test_type": "large_payload",
    "payload_size": "SIZE_PLACEHOLDER",
    "large_data": "DATA_PLACEHOLDER"
  }
}'

# Calculate padding needed (subtract base size and JSON overhead for data field)
# Base JSON structure is ~180 bytes, need padding in large_data field
BASE_SIZE=180
JSON_OVERHEAD=20  # Quotes, commas, etc for the data field
PADDING_SIZE=$((TARGET_SIZE - BASE_SIZE - JSON_OVERHEAD))

# Generate padding data (repeated pattern to fill size)
# Use Python if available for reliable padding generation, otherwise use bash loop
if command -v python3 >/dev/null 2>&1; then
  PADDING_DATA=$(python3 -c "print('A' * $PADDING_SIZE, end='')")
elif command -v python >/dev/null 2>&1; then
  PADDING_DATA=$(python -c "print('A' * $PADDING_SIZE, end='')")
else
  # Fallback: bash loop (slower but works)
  PADDING_DATA=""
  CHUNK_SIZE=1000
  FULL_CHUNKS=$((PADDING_SIZE / CHUNK_SIZE))
  REMAINDER=$((PADDING_SIZE % CHUNK_SIZE))
  
  # Generate full chunks
  for i in $(seq 1 $FULL_CHUNKS); do
    PADDING_DATA="${PADDING_DATA}$(printf 'A%.0s' $(seq 1 $CHUNK_SIZE))"
  done
  
  # Add remainder
  if [ $REMAINDER -gt 0 ]; then
    PADDING_DATA="${PADDING_DATA}$(printf 'A%.0s' $(seq 1 $REMAINDER))"
  fi
fi

# Replace placeholders
RESULT="${BASE_EVENT//SIZE_PLACEHOLDER/$TARGET_SIZE}"
RESULT="${RESULT//DATA_PLACEHOLDER/$PADDING_DATA}"

echo "$RESULT"

