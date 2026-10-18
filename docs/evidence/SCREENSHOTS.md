# Screenshots checklist

Save each screenshot in `docs/evidence/screenshots/` with the file name given below. Before saving, check that no token, password or kubeconfig is visible.

| # | File | What to capture | Where |
|---|---|---|---|
| 1 | `01-actions-approval.png` | A CI run where `release` waits for, or has passed, the `production` approval. The `checks → build → e2e → release` graph should be visible. | GitHub → Actions → ci → the run for the released commit |
| 2 | `02-argocd-synced.png` | Application `kv-cache` showing Synced and Healthy, with the revision and the resource tree. | http://argocd.localhost:8080/applications/argocd/kv-cache |
| 3 | `03-grafana-dashboard.png` | The kv-cache dashboard during or just after `make load`: request rate, latency percentiles, hit ratio and the "Running versions" table showing the deployed commit. | http://grafana.localhost:8080/d/kv-cache |
| 4 | `04-jaeger-trace.png` | One `kv-cache` trace with the `POST put` server span and its `cache.put` child span. | http://jaeger.localhost:8080, service `kv-cache` |
| 5 | `05-alert-fired.png` | `KvCachePodNotReady` firing during `make fault-demo`, in Alertmanager or in Prometheus → Alerts. | http://alertmanager.localhost:8080 or http://prometheus.localhost:8080/alerts |
| 6 (optional) | `06-log-to-trace.png` | Grafana Explore → Loki, query `{namespace="kv-cache"}`, a log line expanded with its "View trace" link. | http://grafana.localhost:8080/explore |

Credentials:

- Grafana: `kubectl -n monitoring get secret grafana-admin -o jsonpath='{.data.admin-password}' | base64 -d` (user `admin`)
- Argo CD: `kubectl -n argocd get secret argocd-initial-admin-secret -o jsonpath='{.data.password}' | base64 -d` (user `admin`)
