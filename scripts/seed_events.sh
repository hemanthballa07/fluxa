#!/bin/bash
# Seed events script - sends sample events to the API

set -e

API_ENDPOINT="${API_ENDPOINT:-http://localhost:3000}"
NUM_EVENTS="${NUM_EVENTS:-10}"

if [ -z "$API_ENDPOINT" ]; then
    echo "Error: API_ENDPOINT environment variable is required"
    exit 1
fi

echo "Seeding $NUM_EVENTS events to $API_ENDPOINT/events..."

for i in $(seq 1 $NUM_EVENTS); do
    EVENT_ID=$(uuidgen)
    USER_ID="user_$(shuf -i 1000-9999 -n 1)"
    AMOUNT=$(awk -v min=10 -v max=1000 'BEGIN{srand(); printf "%.2f", min+rand()*(max-min)}')
    MERCHANTS=("Amazon" "Walmart" "Target" "Best Buy" "Costco" "Starbucks" "McDonald's")
    MERCHANT=${MERCHANTS[$RANDOM % ${#MERCHANTS[@]}]}
    
    curl -X POST "$API_ENDPOINT/events" \
        -H "Content-Type: application/json" \
        -H "X-Correlation-ID: seed-$(uuidgen)" \
        -d "{
            \"event_id\": \"$EVENT_ID\",
            \"user_id\": \"$USER_ID\",
            \"amount\": $AMOUNT,
            \"currency\": \"USD\",
            \"merchant\": \"$MERCHANT\",
            \"timestamp\": \"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",
            \"metadata\": {
                \"source\": \"seed_script\",
                \"iteration\": $i
            }
        }" \
        -w "\nHTTP Status: %{http_code}\n" \
        -s -o /dev/null
    
    if [ $((i % 10)) -eq 0 ]; then
        echo "Sent $i events..."
    fi
done

echo "Completed seeding $NUM_EVENTS events"


