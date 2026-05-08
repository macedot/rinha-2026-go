#!/bin/bash
set -e
cd "$(dirname "$0")"

echo "=== Building with profile support ==="
docker compose down -v 2>/dev/null || true

# Start containers with profiling enabled
PROFILE=/tmp/cpu.pprof docker compose up -d --build 2>/dev/null

# Wait for startup
sleep 8

# Check if containers are healthy
if ! docker ps | grep -q rinha-api1; then
    echo "ERROR: api1 not running"
    docker logs rinha-api1 2>/dev/null || true
    exit 1
fi

echo "=== Running load test ==="
export K6_NO_USAGE_REPORT=true
k6 run ../test/test.js > /dev/null 2>&1 || true

echo "=== Stopping containers to flush profiles ==="
docker compose stop 2>/dev/null || true

# Give profiles time to flush
sleep 2

PPROF_FILE="$(pwd)/cpu.pprof"

echo "=== Copying profile ==="
docker cp rinha-api1:/tmp/cpu.pprof "$PPROF_FILE" 2>/dev/null || echo "No profile"

echo "=== Analyzing profile ==="
if [ -f "$PPROF_FILE" ]; then
    echo "--- Top 20 by CPU time ---"
    go tool pprof -top -cum -n 20 "$PPROF_FILE" 2>/dev/null || echo "pprof failed"
    echo ""
    echo "--- Top 20 flat ---"
    go tool pprof -top -n 20 "$PPROF_FILE" 2>/dev/null || echo "pprof failed"
else
    echo "No profile found"
fi

echo ""
echo "=== Results ==="
jq '{p99, final_score: .scoring.final_score}' ../test/results.json 2>/dev/null || echo "No results"
