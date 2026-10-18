# Shared helpers for the demo scripts.
NS=kv-cache
PROM=http://prometheus.localhost:8080
INGRESS=http://kv.localhost:8080
ts() { date -u +%H:%M:%S; }
log() { echo "[$(ts)] $*"; }
token() { kubectl -n "$NS" get secret kv-cache-auth -o jsonpath='{.data.token}' | base64 -d; }
deployed_sha() { kubectl -n "$NS" get deploy kv-cache -o jsonpath='{.spec.template.spec.containers[0].image}' | cut -d: -f2; }
argo_status() { kubectl -n argocd get application kv-cache -o jsonpath='sync={.status.sync.status} health={.status.health.status} revision={.status.sync.revision}'; echo; }
argo_refresh() { kubectl -n argocd annotate application kv-cache argocd.argoproj.io/refresh=normal --overwrite >/dev/null; }
prom_query() { curl -sG "$PROM/api/v1/query" --data-urlencode "query=$1"; }
alert_state() { # alertname -> firing|pending|inactive
  curl -s "$PROM/api/v1/alerts" | jq -r --arg a "$1" \
    '[.data.alerts[] | select(.labels.alertname==$a) | .state] | if length==0 then "inactive" else (if index("firing") then "firing" else .[0] end) end'
}
attribution_check() {
  if git log --format='%an %ae%n%B' origin/main..HEAD | grep -iE 'claude|anthropic|co-authored'; then
    echo "attribution check failed" >&2; exit 1
  fi
}
push() { attribution_check; git pull --rebase -q; git push -q; }
