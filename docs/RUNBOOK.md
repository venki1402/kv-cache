# kv-cache runbook

Operational guide for the `kv-cache` service (namespace `kv-cache`), deployed by
Argo CD from the Helm chart in `deploy/helm/kv-cache`.

| Tool | URL |
| --- | --- |
| Grafana (dashboard `kv-cache`) | http://grafana.localhost:8080/d/kv-cache |
| Prometheus | http://prometheus.localhost:8080 |
| Alertmanager | http://alertmanager.localhost:8080 |
| Argo CD | http://argocd.localhost:8080 |
| Jaeger | http://jaeger.localhost:8080 |

Things to keep in mind:

- The cache is **in-memory only**. Any pod restart or reschedule loses all keys
  on that pod. This is by design, not data loss to recover from. Expect a hit
  ratio dip and higher miss rate after restarts.
- The cluster state is owned by Argo CD with `selfHeal` on. Do **not** fix
  things with `kubectl edit/patch/scale/set image`: Argo CD will revert them.
  Change Git instead.
- **Rollback** = `git revert` the commit that changed the release (image tag or
  values), push, and let Argo CD sync:

  ```sh
  git log --oneline -- deploy/helm/kv-cache
  git revert <sha>
  git push
  argocd app sync kv-cache      # optional, speeds up the sync
  argocd app wait kv-cache --health --timeout 300
  ```

## SLOs

| SLO | Target | Window | SLI |
| --- | --- | --- | --- |
| Availability | 99.5% of requests are non-5xx | 30 days | `kvcache_http_requests_total` |
| Latency | 99% of `get` requests complete in < 50 ms | 30 days | `kvcache_http_request_duration_seconds` |

Kubelet probe routes (`healthz`, `readyz`) are excluded from the availability
SLI, since they are not user traffic.

Availability over 30 days:

```promql
1 - (
  sum(increase(kvcache_http_requests_total{job="kv-cache",namespace="kv-cache",route!~"healthz|readyz",code=~"5.."}[30d]))
  /
  sum(increase(kvcache_http_requests_total{job="kv-cache",namespace="kv-cache",route!~"healthz|readyz"}[30d]))
)
```

Latency: share of `get` requests under 50 ms (`le="0.05"` is a histogram bucket):

```promql
sum(increase(kvcache_http_request_duration_seconds_bucket{job="kv-cache",namespace="kv-cache",route="get",le="0.05"}[30d]))
/
sum(increase(kvcache_http_request_duration_seconds_count{job="kv-cache",namespace="kv-cache",route="get"}[30d]))
```

Recording rules used by alerts and the dashboard:

| Rule | Meaning |
| --- | --- |
| `kvcache:http_requests:rate5m` | Request rate by route |
| `kvcache:http_error_ratio:rate5m` / `rate30m` / `rate1h` / `rate6h` | 5xx ratio per window (no series when there is no traffic) |
| `kvcache:get_latency_seconds:p50_5m` / `p95_5m` / `p99_5m` | `get` latency quantiles |
| `kvcache:cache_hit_ratio:rate5m` | hits / (hits + misses) |

## Error budget

99.5% over 30 days leaves a 0.5% error budget:
30 d x 24 h x 60 min x 0.005 = **216 minutes, about 3h36m** of full outage
(or the equivalent number of failed requests spread out).

Burn rate = observed error ratio / 0.005. A burn rate of 1 uses exactly the
budget in 30 days.

| Alert | Burn rate | Windows | Budget used before firing | Budget gone in |
| --- | --- | --- | --- | --- |
| KvCacheHighErrorRatioFastBurn | 14.4x (ratio > 7.2%) | 1h and 5m | 2% | ~2 days |
| KvCacheHighErrorRatioSlowBurn | 6x (ratio > 3%) | 6h and 30m | 5% | ~5 days |

The short window makes the alert resolve quickly once errors stop.

## General triage

1. Open the Grafana `kv-cache` dashboard and check Overview (versions, ready
   pods, restarts), then Traffic and Cache rows.
2. Cluster state:

   ```sh
   kubectl -n kv-cache get deploy,rs,pods -o wide
   kubectl -n kv-cache describe deploy kv-cache
   kubectl -n kv-cache get events --sort-by=.lastTimestamp | tail -20
   argocd app get kv-cache
   argocd app history kv-cache
   ```

3. Logs:

   ```sh
   kubectl -n kv-cache logs deploy/kv-cache --tail=200
   kubectl -n kv-cache logs <pod> --previous      # last crashed container
   ```

   Or in Grafana Explore with Loki: `{namespace="kv-cache"}`, narrowed with
   `{namespace="kv-cache"} |= "error"` or `| json | level="error"`.
4. Traces: copy the `trace_id` from a log line and open it in Jaeger
   (http://jaeger.localhost:8080, service `kv-cache`).
5. Firing alerts: http://alertmanager.localhost:8080 or Prometheus `/alerts`.
6. Was there a recent change? Compare the commit in "Running versions" with
   `argocd app history kv-cache` and `git log -- deploy/helm/kv-cache`.

## Alerts

### KvCacheHighErrorRatioFastBurn

- **Severity:** critical
- **Symptom:** More than 7.2% of requests return 5xx over both the last 1h and
  the last 5m. Clients are seeing errors right now.
- **Likely causes:** bad release, a code path panicking/returning 500, pods
  not ready so the remaining pods are overloaded, memory pressure.
- **What to check:**
  - Which route and code:
    `sum by (route, code) (rate(kvcache_http_requests_total{job="kv-cache",code=~"5.."}[5m]))`
  - Did it start with a rollout? Dashboard "Running versions", then
    `argocd app history kv-cache`.
  - Error logs: Loki `{namespace="kv-cache"} |= "error"`; follow a `trace_id`
    to Jaeger.
  - `kubectl -n kv-cache get pods` for not-ready or restarting pods.
- **Action:** If it lines up with a release, roll back with `git revert` of the
  release/values commit and let Argo CD sync. Otherwise fix forward through Git.
- **Verify recovery:** `kvcache:http_error_ratio:rate5m` drops below 0.005;
  the alert resolves within a few minutes.

### KvCacheHighErrorRatioSlowBurn

- **Severity:** warning
- **Symptom:** More than 3% 5xx over both the last 6h and 30m. Not an outage,
  but the monthly budget will be gone in about 5 days.
- **Likely causes:** a specific route failing intermittently, partial failure
  of one pod, a slow regression from a recent release.
- **What to check:**
  - `kvcache:http_error_ratio:rate6h` and `kvcache:http_error_ratio:rate30m`
  - Errors per pod:
    `sum by (pod, route) (rate(kvcache_http_requests_total{job="kv-cache",code=~"5.."}[30m]))`
  - Remaining budget (30d availability query in [SLOs](#slos)).
- **Action:** Find the failing route/pod, open an issue, fix via Git. Roll back
  if a recent release introduced it.
- **Verify recovery:** 30m ratio below 0.005 and trending down on the
  "Error ratio (5xx) vs SLO" panel.

### KvCacheGetLatencyHigh

- **Severity:** warning
- **Symptom:** p99 of `get` requests above 50 ms for 5 minutes (only evaluated
  while there is `get` traffic).
- **Likely causes:** CPU throttling, GC pressure from a large cache, lock
  contention under load, node pressure.
- **What to check:**
  - `kvcache:get_latency_seconds:p99_5m` and per pod:
    `histogram_quantile(0.99, sum by (le, pod) (rate(kvcache_http_request_duration_seconds_bucket{job="kv-cache",route="get"}[5m])))`
  - CPU throttling:
    `sum by (pod) (rate(container_cpu_cfs_throttled_periods_total{namespace="kv-cache",container="kv-cache"}[5m]))`
  - GC: `rate(go_gc_duration_seconds_sum{job="kv-cache"}[5m])`,
    `go_memstats_heap_inuse_bytes{job="kv-cache"}`
  - `kubectl -n kv-cache top pods`
  - Slow traces in Jaeger for operation `get`.
- **Action:** If throttled, raise CPU limits or replicas in values via Git. If
  tied to a release, roll back.
- **Verify recovery:** p99 back under 50 ms on the latency panel.

### KvCachePodNotReady

- **Severity:** critical
- **Symptom:** A `kv-cache-*` pod has not been Ready for 2 minutes. This is the
  usual signal of a bad release: new pods fail readiness while old pods keep
  serving.
- **Likely causes:** bad image tag (`ImagePullBackOff`), crash on start
  (`CrashLoopBackOff`), failing `/readyz`, wrong config/env in values,
  insufficient resources (Pending).
- **What to check:**

  ```sh
  kubectl -n kv-cache get pods -o wide
  kubectl -n kv-cache describe pod <pod>          # Events, probe failures
  kubectl -n kv-cache logs <pod> --previous
  argocd app get kv-cache                         # Synced? Healthy/Progressing/Degraded?
  ```

  PromQL: `kube_pod_status_ready{namespace="kv-cache",condition="true"} == 0`
  and `kube_pod_container_status_waiting_reason{namespace="kv-cache"}`.
- **Action:** If it follows a release, `git revert` the release/values commit
  and let Argo CD sync. Do not delete or patch pods by hand. If the pod is
  Pending on resources, fix requests or cluster capacity.
- **Verify recovery:** all pods `1/1 Running`, "Ready pods" panel shows
  ready = total, `argocd app get kv-cache` reports Healthy.

### KvCacheRolloutStuck

- **Severity:** warning
- **Symptom:** Deployment `kv-cache` has unavailable replicas, or updated
  replicas differ from desired, for 3 minutes.
- **Likely causes:** same as KvCachePodNotReady (new ReplicaSet never becomes
  ready), quota or scheduling limits, `progressDeadlineSeconds` exceeded.
- **What to check:**

  ```sh
  kubectl -n kv-cache rollout status deploy/kv-cache --timeout=10s
  kubectl -n kv-cache get rs -l app.kubernetes.io/name=kv-cache
  kubectl -n kv-cache describe deploy kv-cache    # Conditions
  argocd app get kv-cache
  ```

  PromQL: `kube_deployment_status_replicas_unavailable{namespace="kv-cache"}`,
  `kube_deployment_spec_replicas{namespace="kv-cache"}` vs
  `kube_deployment_status_replicas_updated{namespace="kv-cache"}`.
- **Action:** Roll back through Git (`git revert`, Argo CD sync). Do not use
  `kubectl rollout undo`; selfHeal would re-apply the bad spec.
- **Verify recovery:** `rollout status` reports success; unavailable = 0 and
  updated = desired.

### KvCachePodRestarting

- **Severity:** warning
- **Symptom:** The `kv-cache` container restarted at least once in the last
  15 minutes. Each restart empties that pod's cache.
- **Likely causes:** OOMKilled (cache filled past the memory limit), panic,
  liveness probe failures under load.
- **What to check:**

  ```sh
  kubectl -n kv-cache get pods
  kubectl -n kv-cache describe pod <pod> | grep -A5 'Last State'
  kubectl -n kv-cache logs <pod> --previous
  ```

  PromQL:
  `kube_pod_container_status_last_terminated_reason{namespace="kv-cache",container="kv-cache"}`
  and the "Container memory working set vs limit" panel.
- **Action:** OOMKilled: lower the cache max bytes or raise the memory limit
  in values (Git). Panic: roll back the release. Liveness failures: check
  latency and CPU.
- **Verify recovery:** no new restarts for 15 minutes; the alert clears on its
  own. Keys repopulate as clients write again.

### KvCacheMemoryHigh

- **Severity:** warning
- **Symptom:** Container working set above 90% of its memory limit for 5
  minutes. An OOMKill (and loss of all keys on that pod) is likely soon.
- **Likely causes:** configured cache ceiling (`kvcache_max_bytes`) too close
  to the container limit, Go heap overhead on top of cached bytes, a leak.
- **What to check:**
  - `kvcache_bytes_used` vs `kvcache_max_bytes` ("Cache bytes used vs limit").
  - `go_memstats_heap_inuse_bytes{job="kv-cache"}` vs `kvcache_bytes_used`.
  - Evictions: `rate(kvcache_evictions_total{job="kv-cache"}[5m])`. Zero
    evictions with memory still climbing suggests a leak.
  - `kubectl -n kv-cache top pods`
- **Action:** Keep the cache ceiling well below the container limit (leave
  room for Go runtime overhead): lower max bytes or raise
  `resources.limits.memory` in values via Git.
- **Verify recovery:** working set ratio below 0.9; memory panel flat below
  the limit line.

### KvCacheTargetDown

- **Severity:** critical
- **Symptom:** Either a kv-cache target fails to scrape for 2 minutes
  (`reason=scrape_failed`), or Prometheus has no kv-cache targets at all for 5
  minutes (`reason=no_targets`). Metric-based alerts are blind while this fires.
- **Likely causes:** pods down or crashing, metrics port `metrics` (9091) not
  served, Service/ServiceMonitor missing or labels changed, app not synced in
  Argo CD.
- **What to check:**

  ```sh
  kubectl -n kv-cache get pods,svc,endpoints,servicemonitor
  kubectl -n kv-cache port-forward deploy/kv-cache 9091:9091 &
  curl -s localhost:9091/metrics | head
  argocd app get kv-cache
  ```

  Prometheus Targets page: http://prometheus.localhost:8080/targets (search
  `kv-cache`), and `up{job="kv-cache"}`.
- **Action:** If pods are down, follow KvCachePodNotReady. If the
  ServiceMonitor or Service is missing or mislabeled, fix the chart in Git and
  let Argo CD sync.
- **Verify recovery:** `up{job="kv-cache",namespace="kv-cache"} == 1` for every
  pod; targets page shows them UP.
