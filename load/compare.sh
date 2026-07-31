#!/usr/bin/env bash
#
# Run the k6 load test twice: once with Redis OFF (cache disabled),
# once with Redis ON (cache enabled). Print a comparison.
#
# Usage:
#   ./load/compare.sh
#
# Prerequisites:
#   - PostgreSQL and the API are running (docker compose up -d, go run ./cmd/api)
#   - k6 is installed
#   - The worker is running (go run ./cmd/worker) — optional, but
#     without it click messages queue up in RabbitMQ.
set -euo pipefail

BASE_URL="${BASE_URL:-http://localhost:3000}"
DB_URL="${DATABASE_URL:-postgres://shortener:shortener@localhost:5432/shortener?sslmode=disable}"
REDIS_NAME="${REDIS_NAME:-shortener-redis}"

echo "=== Seeding a short URL into the database ==="
SEED_RESP=$(curl -s -X POST "$BASE_URL/shorten" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://example.com/load-test-target"}')
echo "$SEED_RESP"
CODE=$(echo "$SEED_RESP" | grep -o '"code":"[^"]*"' | cut -d'"' -f4)

if [ -z "$CODE" ]; then
  echo "ERROR: could not extract code from response"
  exit 1
fi
echo "Using code: $CODE"

# Warm the cache once so the ON test measures steady-state HITs.
curl -s -o /dev/null "$BASE_URL/$CODE"

echo ""
echo "=========================================="
echo "  RUN 1: Redis OFF (no cache, DB only)"
echo "=========================================="
podman stop "$REDIS_NAME" 2>/dev/null || docker stop "$REDIS_NAME" 2>/dev/null || true
sleep 2

# Also flush so the next ON run starts clean.
RUN1_OUTPUT=$(k6 run --quiet load/load.js -e CODE="$CODE" -e BASE_URL="$BASE_URL" 2>&1)
echo "$RUN1_OUTPUT"

# Extract key metrics from k6 output.
P99_OFF=$(echo "$RUN1_OUTPUT" | grep -o 'p\(99\)[[:space:]]*[0-9.]*' | head -1 | awk '{print $2}')
RPS_OFF=$(echo "$RUN1_OUTPUT" | grep -o 'http_reqs[[:space:]]*:' -A1 | tail -1 | awk '{print $1}')
ITER_OFF=$(echo "$RUN1_OUTPUT" | grep -o 'iterations[[:space:]]*:' -A1 | tail -1 | awk '{print $1}')

echo ""
echo "=========================================="
echo "  RUN 2: Redis ON (cache enabled)"
echo "=========================================="
podman start "$REDIS_NAME" 2>/dev/null || docker start "$REDIS_NAME" 2>/dev/null || true
sleep 3

# Warm the cache so the ON test measures HITs.
curl -s -o /dev/null "$BASE_URL/$CODE"

RUN2_OUTPUT=$(k6 run --quiet load/load.js -e CODE="$CODE" -e BASE_URL="$BASE_URL" 2>&1)
echo "$RUN2_OUTPUT"

P99_ON=$(echo "$RUN2_OUTPUT" | grep -o 'p\(99\)[[:space:]]*[0-9.]*' | head -1 | awk '{print $2}')
RPS_ON=$(echo "$RUN2_OUTPUT" | grep -o 'http_reqs[[:space:]]*:' -A1 | tail -1 | awk '{print $1}')
ITER_ON=$(echo "$RUN2_OUTPUT" | grep -o 'iterations[[:space:]]*:' -A1 | tail -1 | awk '{print $1}')

echo ""
echo "=========================================="
echo "  COMPARISON"
echo "=========================================="
printf "  Metric              | Redis OFF     | Redis ON\n"
printf "  --------------------|---------------|----------\n"
printf "  p99 latency         | %-13s | %s\n" "${P99_OFF:-N/A}" "${P99_ON:-N/A}"
printf "  total requests      | %-13s | %s\n" "${ITER_OFF:-N/A}" "${ITER_ON:-N/A}"
echo ""
echo "Write the p99 numbers on your CV."
echo "For example: 'With Redis, p99 latency went from ${P99_OFF} to ${P99_ON}."
