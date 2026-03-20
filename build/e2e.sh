#!/bin/sh
set -eu

BASE_URL="${BASE_URL:-http://localhost:19090}"
PASS=0
FAIL=0

request() {
    method=$1 path=$2 body=$3 header=${4:-}
    if [ -n "$header" ]; then
        curl -s -w "\n%{http_code}" -H "$header" -X "$method" -d "$body" "$BASE_URL$path"
    else
        curl -s -w "\n%{http_code}" -X "$method" -d "$body" "$BASE_URL$path"
    fi
}

assert() {
    name=$1 expected_code=$2
    shift 2
    output=$("$@")
    code=$(echo "$output" | tail -1)

    if [ "$code" = "$expected_code" ]; then
        printf "\033[32mPASS\033[0m %s (HTTP %s)\n" "$name" "$code"
        PASS=$((PASS + 1))
    else
        body=$(echo "$output" | sed '$d')
        printf "\033[31mFAIL\033[0m %s (expected %s, got %s)\n" "$name" "$expected_code" "$code"
        [ -n "$body" ] && printf "     %s\n" "$body"
        FAIL=$((FAIL + 1))
    fi
}

lock()   { request POST /v1/pre-reboot   "{\"client_params\":{\"id\":\"$1\"}}" "fleet-lock-protocol: true"; }
unlock() { request POST /v1/steady-state "{\"client_params\":{\"id\":\"$1\"}}" "fleet-lock-protocol: true"; }

echo "Waiting for fleetlock to be ready..."
i=0
while [ $i -lt 30 ]; do
    if curl -s -o /dev/null -w "%{http_code}" "$BASE_URL/v1/pre-reboot" 2>/dev/null | grep -q '400'; then
        break
    fi
    sleep 1
    i=$((i + 1))
done

echo ""
echo "=== FleetLock E2E Tests ==="
echo ""

# Basic lock/unlock
assert "lock-node-1"             200 lock node-1
assert "lock-node-1-idempotent"  200 lock node-1
assert "lock-node-2-blocked"     409 lock node-2
assert "unlock-node-1"           200 unlock node-1
sleep 2
assert "lock-node-2-after-unlock" 200 lock node-2
assert "unlock-node-2"           200 unlock node-2

# Unlock non-existent lock (should succeed)
sleep 2
assert "unlock-no-existing-lock" 200 unlock node-3

# Missing header
assert "missing-header" 400 request POST /v1/pre-reboot '{"client_params":{"id":"node-1"}}'

# Missing ID
assert "missing-id" 400 request POST /v1/pre-reboot '{"client_params":{}}' "fleet-lock-protocol: true"

# Invalid JSON
assert "invalid-json" 400 request POST /v1/pre-reboot 'not-json' "fleet-lock-protocol: true"

echo ""
echo "=== Results: $PASS passed, $FAIL failed ==="
exit $FAIL
