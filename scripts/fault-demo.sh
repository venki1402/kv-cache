#!/usr/bin/env bash
# Bad release through GitOps, then recovery by git revert.
# The bad change makes /readyz fail; maxUnavailable 0 keeps the old pod serving.
set -euo pipefail
cd "$(dirname "$0")/.."; source scripts/lib.sh
VALUES=deploy/helm/kv-cache/values-local.yaml
OUT=docs/evidence/fault-demo.txt
exec > >(tee "$OUT") 2>&1

wait_for() { # description timeout-seconds command...
  local desc=$1 timeout=$2; shift 2
  local s=$(date +%s)
  until "$@" >/dev/null 2>&1; do
    (( $(date +%s) - s > timeout )) && { log "TIMEOUT waiting for: $desc"; return 1; }
    sleep 3
  done
  log "$desc (after $(( $(date +%s) - s ))s)"
}
is_alert() { [[ $(alert_state "$1") == "$2" ]]; }
argo_health() { [[ $(kubectl -n argocd get application kv-cache -o jsonpath='{.status.health.status}') == "$1" ]]; }
argo_synced_to() { [[ $(kubectl -n argocd get application kv-cache -o jsonpath='{.status.sync.revision}') == "$1" ]]; }

log "== baseline"
log "deployed image sha: $(deployed_sha)"; log "argo: $(argo_status)"
kubectl -n "$NS" get pods -o wide

# Probe the public path once per second for the whole demo.
PROBE=$(mktemp)
( while true; do echo "$(date +%s) $(curl -s -o /dev/null -m 2 -w '%{http_code}' "$INGRESS/version")"; sleep 1; done ) > "$PROBE" &
PROBE_PID=$!
trap 'kill $PROBE_PID 2>/dev/null || true' EXIT

log "== 1. bad release: failReadiness=true via a values commit"
yq -i '.config.failReadiness = true' "$VALUES"
git add "$VALUES"
git commit -q -m "chore(helm): enable failing readiness drill"
BAD=$(git rev-parse HEAD); push; t0=$(date +%s)
log "pushed bad commit $BAD"
argo_refresh
wait_for "argo synced bad commit" 300 argo_synced_to "$BAD"

log "== 2. symptoms"
wait_for "new pod created and not ready" 120 bash -c "kubectl -n $NS get pods --no-headers | grep -q '0/1'"
kubectl -n "$NS" get pods -o wide
kubectl -n "$NS" rollout status deploy/kv-cache --timeout=20s || log "rollout stalled as expected"
log "old pod still serving: $(curl -s "$INGRESS/version")"
wait_for "argo health Degraded" 240 argo_health Degraded
log "argo: $(argo_status)"
wait_for "KvCachePodNotReady firing" 360 is_alert KvCachePodNotReady firing
t_alert=$(date +%s); log "alert fired $(( t_alert - t0 ))s after the bad push"
curl -s "$PROM/api/v1/alerts" | jq -c '.data.alerts[] | select(.labels.namespace=="kv-cache" or (.labels.alertname|startswith("KvCache"))) | {alertname: .labels.alertname, state, severity: .labels.severity, activeAt}'

log "== 3. recover: git revert"
git revert --no-commit "$BAD"
git commit -q -m "fix(helm): revert failing readiness drill" -m "This reverts commit $BAD."
GOOD=$(git rev-parse HEAD); push; t_rev=$(date +%s)
log "pushed revert $GOOD"
argo_refresh
wait_for "argo synced revert" 300 argo_synced_to "$GOOD"
kubectl -n "$NS" rollout status deploy/kv-cache --timeout=180s
t_ready=$(date +%s); log "rollout complete $(( t_ready - t_rev ))s after the revert push"
wait_for "argo health Healthy" 180 argo_health Healthy
wait_for "KvCachePodNotReady resolved" 360 is_alert KvCachePodNotReady inactive
log "alert resolved $(( $(date +%s) - t_rev ))s after the revert push"

log "== 4. verify"
kubectl -n "$NS" get pods -o wide
log "argo: $(argo_status)"
AUTH_TOKEN=$(token) EXPECTED_SHA=$(deployed_sha) BASE_URL=$INGRESS scripts/smoke.sh
log "5xx rate (Prometheus): $(prom_query 'sum(rate(kvcache_http_requests_total{code=~"5.."}[5m])) or vector(0)' | jq -r '.data.result[0].value[1]')"

kill $PROBE_PID; trap - EXIT
total=$(wc -l < "$PROBE"); bad=$(awk '$2!="200"' "$PROBE" | wc -l)
log "== public probe during the whole demo: $total requests, $bad non-200"
awk '$2!="200"' "$PROBE" | head -20
log "summary: alert fired after $(( t_alert - t0 ))s; rollout healthy $(( t_ready - t_rev ))s after revert push"
