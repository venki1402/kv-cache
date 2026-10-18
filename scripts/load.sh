#!/usr/bin/env bash
# Short load test through the ingress against seeded keys.
set -euo pipefail
cd "$(dirname "$0")/.."; source scripts/lib.sh
DURATION="${DURATION:-30s}"; CONC="${CONC:-50}"
T=$(token)
log "seeding 100 keys"
for i in $(seq 1 100); do
  curl -sf -o /dev/null -X POST "$INGRESS/put" -H "Authorization: Bearer $T" -d "{\"key\":\"load$i\",\"value\":\"value $i\"}"
done
log "hey -z $DURATION -c $CONC GET $INGRESS/get?key=load42 (sha $(deployed_sha))"
hey -z "$DURATION" -c "$CONC" "$INGRESS/get?key=load42"
