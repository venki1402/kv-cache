#!/usr/bin/env bash
# Reliability tests: pod-delete | drain | readonly-fs. Output is meant to be
# redirected into docs/evidence/.
set -euo pipefail
cd "$(dirname "$0")/.."; source scripts/lib.sh

probe_start() { PROBE=$(mktemp); ( while true; do echo "$(date +%s.%N | cut -c1-14) $(curl -s -o /dev/null -m 1 -w '%{http_code}' "$INGRESS/version")"; sleep 0.2; done ) > "$PROBE" & PROBE_PID=$!; }
probe_stop() {
  kill "$PROBE_PID"; wait "$PROBE_PID" 2>/dev/null || true
  local total bad first last
  total=$(wc -l < "$PROBE"); bad=$(awk '$2!="200"' "$PROBE" | wc -l | tr -d ' ')
  log "probe every 0.2s: $total requests, $bad non-200"
  if (( bad > 0 )); then
    first=$(awk '$2!="200"{print $1; exit}' "$PROBE"); last=$(awk '$2!="200"{l=$1} END{print l}' "$PROBE")
    log "unavailable window: ~$(echo "$last - $first + 0.2" | bc)s (codes: $(awk '$2!="200"{print $2}' "$PROBE" | sort | uniq -c | tr '\n' ' '))"
  fi
}
put_key() { curl -s -X POST "$INGRESS/put" -H "Authorization: Bearer $(token)" -d "{\"key\":\"$1\",\"value\":\"$2\"}"; echo; }
get_key() { curl -s -w ' (%{http_code})' "$INGRESS/get?key=$1"; echo; }
wait_ready() {
  local s=$(date +%s)
  until [[ $(kubectl -n "$NS" get pods -l app.kubernetes.io/name=kv-cache -o jsonpath='{range .items[*]}{.status.conditions[?(@.type=="Ready")].status}{"\n"}{end}' | grep -c True) -ge 1 ]] && \
        [[ -z $(kubectl -n "$NS" get pods -l app.kubernetes.io/name=kv-cache --no-headers | grep -v '1/1 *Running') ]]; do
    sleep 0.5
  done
  echo $(( $(date +%s) - s ))
}

case "${1:-}" in
pod-delete)
  log "== delete the pod"
  put_key reliability "survives?"; get_key reliability
  kubectl -n "$NS" get pods -o wide
  probe_start; s=$(date +%s)
  kubectl -n "$NS" delete pod -l app.kubernetes.io/name=kv-cache --wait=false
  sleep 2; secs=$(wait_ready)
  log "replacement pod Ready $(( $(date +%s) - s ))s after delete"
  sleep 3; probe_stop
  kubectl -n "$NS" get pods -o wide
  log "GET after restart (expected 404: the cache is in-memory and starts cold by design)"
  get_key reliability
  ;;
drain)
  NODE=$(kubectl -n "$NS" get pods -l app.kubernetes.io/name=kv-cache -o jsonpath='{.items[0].spec.nodeName}')
  log "== drain $NODE (the node running kv-cache)"
  kubectl get nodes; kubectl -n "$NS" get pods -o wide
  probe_start; s=$(date +%s)
  kubectl drain "$NODE" --ignore-daemonsets --delete-emptydir-data --timeout=180s 2>&1 | grep -E 'kv-cache|drained|error' || true
  wait_ready >/dev/null
  log "kv-cache Ready on another node $(( $(date +%s) - s ))s after drain started"
  sleep 3; probe_stop
  kubectl -n "$NS" get pods -o wide
  kubectl uncordon "$NODE"
  ;;
readonly-fs)
  POD=$(kubectl -n "$NS" get pods -l app.kubernetes.io/name=kv-cache -o jsonpath='{.items[0].metadata.name}')
  log "== read-only root filesystem on $POD"
  kubectl -n "$NS" get pod "$POD" -o jsonpath='{.spec.containers[0].securityContext}'; echo
  log "exec into the app container (distroless: no shell, so exec itself fails)"
  kubectl -n "$NS" exec "$POD" -- sh -c 'echo x > /x' 2>&1 || true
  log "ephemeral debug container as the same UID, writing into the app's root fs via /proc/1/root"
  cat > /tmp/kv-debug-profile.json <<'JSON'
{"securityContext":{"runAsUser":65532,"runAsGroup":65532,"runAsNonRoot":true,"allowPrivilegeEscalation":false,"capabilities":{"drop":["ALL"]},"seccompProfile":{"type":"RuntimeDefault"}}}
JSON
  kubectl -n "$NS" debug "$POD" --image=busybox:1.37.0 --target=kv-cache --profile=restricted \
    --custom=/tmp/kv-debug-profile.json -i --quiet -- \
    sh -c 'ls /proc/1/root/; touch /proc/1/root/probe-write && echo "WRITE SUCCEEDED" || echo "write to / failed as expected"' 2>&1 || true
  ;;
*) echo "usage: $0 pod-delete|drain|readonly-fs" >&2; exit 2 ;;
esac
