#!/usr/bin/env bash
# Captures text evidence into docs/evidence/. Tokens and passwords are never
# printed; Secret objects are listed by name only.
set -euo pipefail
cd "$(dirname "$0")/.."; source scripts/lib.sh
E=docs/evidence; mkdir -p "$E"
run() { echo "\$ $*"; "$@" 2>&1 || true; echo; }

{
  echo "# captured $(date -u +%FT%TZ)"
  run kind version
  run kubectl version
  run kubectl get nodes -o wide
  run kubectl top nodes
  run kubectl -n "$NS" top pods
} > "$E/cluster.txt"

{
  echo "# captured $(date -u +%FT%TZ)"
  run kubectl -n "$NS" get deploy,rs,pods,svc,ingress,cm,sa,networkpolicy,servicemonitor,prometheusrule -o wide
  run kubectl -n "$NS" get secret
  run kubectl -n "$NS" get pods -o jsonpath='{range .items[*]}{.metadata.name}{"  image="}{.spec.containers[0].image}{"  imageID="}{.status.containerStatuses[0].imageID}{"\n"}{end}'
  run kubectl get namespace "$NS" --show-labels
} > "$E/k8s-resources.txt"

ARGO_PW=$(kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d)
argocd login argocd.localhost:8080 --plaintext --insecure --grpc-web --username admin --password "$ARGO_PW" >/dev/null
unset ARGO_PW
{
  echo "# captured $(date -u +%FT%TZ)"
  run argocd app get kv-cache --grpc-web
  run argocd app history kv-cache --grpc-web
} > "$E/argocd-app.txt"

{
  echo "# captured $(date -u +%FT%TZ)"
  run curl -si "$INGRESS/version"
} > "$E/version.txt"

{
  echo "# captured $(date -u +%FT%TZ)"
  AUTH_TOKEN=$(token) EXPECTED_SHA=$(deployed_sha) BASE_URL=$INGRESS scripts/smoke.sh 2>&1
} > "$E/smoke.txt"

{
  echo "# captured $(date -u +%FT%TZ)"
  echo "## kvcache_build_info"; prom_query 'kvcache_build_info' | jq -c '.data.result[] | {metric, value: .value[1]}'
  echo "## scrape targets (kv-cache)"; curl -s "$PROM/api/v1/targets?state=active" | jq -c '.data.activeTargets[] | select(.labels.namespace=="kv-cache") | {job: .labels.job, scrapeUrl, health, lastScrape}'
  echo "## all targets health"; curl -s "$PROM/api/v1/targets?state=active" | jq -r '.data.activeTargets[] | "\(.health) \(.labels.job)"' | sort | uniq -c
  echo "## kv-cache rule groups loaded"; curl -s "$PROM/api/v1/rules" | jq -c '.data.groups[] | select(.file | test("kv-cache")) | {name, rules: (.rules | length), health: ([.rules[].health] | unique)}'
  echo "## currently firing kv-cache alerts"; curl -s "$PROM/api/v1/alerts" | jq -c '[.data.alerts[] | select(.labels.alertname | startswith("KvCache")) | {alertname: .labels.alertname, state}]'
  echo "## request rate by route (5m)"; prom_query 'sum by (route, code) (rate(kvcache_http_requests_total[5m]))' | jq -c '.data.result[] | {route: .metric.route, code: .metric.code, rps: .value[1]}'
  echo "## get latency p99 (5m)"; prom_query 'histogram_quantile(0.99, sum by (le) (rate(kvcache_http_request_duration_seconds_bucket{route="get"}[5m])))' | jq -c '.data.result'
} > "$E/prometheus.txt"

{
  echo "# captured $(date -u +%FT%TZ)"
  echo "## from namespace default (expected: blocked by NetworkPolicy)"
  run kubectl run np-test-default -n default --rm -i --restart=Never --image=busybox:1.37.0 -- \
    wget -q -T 3 -O - http://kv-cache.kv-cache.svc.cluster.local:7171/version
  echo "## from namespace traefik (expected: allowed)"
  run kubectl run np-test-traefik -n traefik --rm -i --restart=Never --image=busybox:1.37.0 -- \
    wget -q -T 3 -O - http://kv-cache.kv-cache.svc.cluster.local:7171/version
  echo "## metrics port from namespace traefik (expected: blocked, only monitoring may scrape)"
  run kubectl run np-test-metrics -n traefik --rm -i --restart=Never --image=busybox:1.37.0 -- \
    wget -q -T 3 -O /dev/null http://kv-cache.kv-cache.svc.cluster.local:9091/metrics
  echo "## through the ingress (Traefik -> app)"
  run curl -s -o /dev/null -w '%{http_code}\n' "$INGRESS/version"
} > "$E/networkpolicy.txt"

{
  echo "# captured $(date -u +%FT%TZ)"
  echo "## app paths through the ingress"
  for p in /version /health "/get?key=does-not-exist"; do echo "$p -> $(curl -s -o /dev/null -w '%{http_code}' "$INGRESS$p")"; done
  echo "## /metrics and other paths through the ingress (expected 404: not routed)"
  for p in /metrics /healthz /readyz; do echo "$p -> $(curl -s -o /dev/null -w '%{http_code}' "$INGRESS$p")"; done
  echo "## host port exposure (expected: only 127.0.0.1:8080)"
  docker port "kvcache-control-plane" 2>&1 || true
} > "$E/ingress-exposure.txt"

log "evidence written to $E/"
ls -1 "$E"
