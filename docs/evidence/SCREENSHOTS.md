# Screenshots

All screenshots were taken on 2026-10-18 (times are IST) and checked so that no token, password or kubeconfig is visible. They live in [`screenshots/`](screenshots/).

- [x] **1. CI run with approval**: [`01-actions-run-326a537.png`](screenshots/01-actions-run-326a537.png). Run 39144658231 for `326a537`: checks, build, e2e and release all green, coverage 92.2%, "No leaks detected", and the `production` approval by venki1402.
- [x] **2. Argo CD synced app**: [`02-argocd-synced.png`](screenshots/02-argocd-synced.png). Application `kv-cache` is Healthy and Synced to `devops`, with the full resource tree: ConfigMaps, Service, ServiceAccount, Deployment, PrometheusRule, ServiceMonitor, Ingress and both NetworkPolicies.
- [x] **3. Grafana dashboard**: [`03-grafana-dashboard.png`](screenshots/03-grafana-dashboard.png). The kv-cache dashboard under load. "Running versions" shows `baseline-15-g240ba9d`, the 1/1 pod is ready, the request-rate spike reaches ~10k req/s, latency stays far below the 50 ms line, and the hit ratio is 100%.
- [x] **4. Jaeger trace**: [`04-jaeger-trace-get.png`](screenshots/04-jaeger-trace-get.png). 20 `GET get` traces with 2 spans each: the server span plus its `cache.get` child span.
- [x] **5. Fired alert**: [`05a`](screenshots/05a-alert-inactive-before.png) → [`05b`](screenshots/05b-alert-pending.png) → [`05c`](screenshots/05c-alert-firing.png) → [`05d`](screenshots/05d-alert-resolved.png). `KvCachePodNotReady` through its whole lifecycle; see below.
- [x] **6. Fault demo in traces (extra)**: [`06-jaeger-readyz-errors-fault-demo.png`](screenshots/06-jaeger-readyz-errors-fault-demo.png). During a fault-demo run, the bad pod's `GET readyz` spans are flagged as errors in Jaeger while `GET version` traffic keeps succeeding on the old pod.

## Alert lifecycle

These were taken during `make fault-demo` re-runs made for the screenshots, after the documented run in [FAULT-DEMO.md](../FAULT-DEMO.md):

- Run A: bad commit `92f8ce5` at 22:59:14, revert `77e852e` at 23:01:34.
- Run B: bad commit `9a8e044` at 23:08:09, revert `37dc057` at 23:10:33.

The Prometheus alerts page does not auto-refresh, so a screenshot can be saved a little after the state it shows. Each step below gives both the save time and the state shown.

| Step | Screenshot | Saved | State shown |
|---|---|---|---|
| Healthy | [`05a-alert-inactive-before.png`](screenshots/05a-alert-inactive-before.png) | 23:04:40 | All `kv-cache.kubernetes` and `kv-cache.slo` rules loaded and inactive, between runs A and B. Only kube-prometheus-stack's always-on `Watchdog` is firing |
| Pending | [`05b-alert-pending.png`](screenshots/05b-alert-pending.png) | 23:09:09 | Run B: `KvCachePodNotReady` **pending** for pod `kv-cache-6c6c79bf-848gq`, active 33.75 s into its 2m `for:` window. `KvCacheRolloutStuck` is also pending |
| Firing | [`05c-alert-firing.png`](screenshots/05c-alert-firing.png) | 23:11:39 | Run B: `KvCachePodNotReady` **firing** (critical) for the same pod, active 2m 12s. Pending started at about 23:08:35, so this is the state at about 23:10:47. Argo CD had synced the 23:10:33 revert at 23:10:41, but the alert stays firing until Prometheus next evaluates the rule (every 30 s) |
| Recovered | [`05d-alert-resolved.png`](screenshots/05d-alert-resolved.png) | 23:14:43 | After the revert: all six `kv-cache.kubernetes` rules inactive again |
