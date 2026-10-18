#!/usr/bin/env bash
# User-journey check: authenticated PUT, unauthenticated PUT rejected, GET
# returns the value, and /version reports the expected commit.
# Usage: BASE_URL=http://kv.localhost:8080 AUTH_TOKEN=... EXPECTED_SHA=<sha> scripts/smoke.sh
set -euo pipefail

BASE_URL="${BASE_URL:-http://kv.localhost:8080}"
AUTH_TOKEN="${AUTH_TOKEN:?AUTH_TOKEN is required}"
EXPECTED_SHA="${EXPECTED_SHA:-}"
key="smoke-$(date +%s)-$RANDOM"
fail() { echo "FAIL: $*" >&2; exit 1; }

echo "target: $BASE_URL"

code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE_URL/put" \
  -H 'Content-Type: application/json' -d "{\"key\":\"$key\",\"value\":\"v1\"}")
[[ "$code" == 401 ]] || fail "PUT without token returned $code, want 401"
echo "ok   PUT without token -> 401"

body=$(curl -sf -X POST "$BASE_URL/put" -H "Authorization: Bearer $AUTH_TOKEN" \
  -H 'Content-Type: application/json' -d "{\"key\":\"$key\",\"value\":\"v1\"}") || fail "PUT with token failed"
[[ "$body" == *'"status":"OK"'* ]] || fail "unexpected PUT body: $body"
echo "ok   PUT with token -> $body"

body=$(curl -sf "$BASE_URL/get?key=$key") || fail "GET failed"
[[ "$body" == "{\"status\":\"OK\",\"key\":\"$key\",\"value\":\"v1\"}" ]] || fail "unexpected GET body: $body"
echo "ok   GET -> $body"

code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE_URL/get?key=missing-$key")
[[ "$code" == 404 ]] || fail "GET missing key returned $code, want 404"
echo "ok   GET missing key -> 404"

headers=$(curl -sf -D - -o /tmp/kv-version.$$ "$BASE_URL/version") || fail "/version failed"
version_body=$(cat /tmp/kv-version.$$); rm -f /tmp/kv-version.$$
echo "ok   /version -> $version_body"
echo "     $(grep -i '^x-app-version' <<<"$headers" | tr -d '\r')"
if [[ -n "$EXPECTED_SHA" ]]; then
  [[ "$version_body" == *"\"commit\":\"$EXPECTED_SHA\""* ]] || fail "/version commit does not match $EXPECTED_SHA"
  echo "ok   /version commit matches $EXPECTED_SHA"
fi

echo "PASS"
