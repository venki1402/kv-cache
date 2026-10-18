#!/usr/bin/env bash
# Manual drift: scale to 3 and time how long Argo CD selfHeal takes to revert.
set -euo pipefail
cd "$(dirname "$0")/.."; source scripts/lib.sh
log "before: $(kubectl -n $NS get deploy kv-cache -o jsonpath='{.spec.replicas}') replica(s); $(argo_status)"
start=$(date +%s)
kubectl -n "$NS" scale deploy kv-cache --replicas=3
for i in $(seq 1 120); do
  r=$(kubectl -n "$NS" get deploy kv-cache -o jsonpath='{.spec.replicas}')
  [[ "$r" == 1 && $(( $(date +%s) - start )) -gt 1 ]] && break
  sleep 1
done
log "after: spec.replicas=$r, reverted in $(( $(date +%s) - start ))s"
kubectl -n argocd get application kv-cache -o jsonpath='{range .status.history[-1:]}last sync revision={.revision} at {.deployedAt}{"\n"}{end}'
log "$(argo_status)"
