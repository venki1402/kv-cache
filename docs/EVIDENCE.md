# Evidence

Raw outputs are in [`docs/evidence/`](evidence/). Screenshots are listed in [`evidence/SCREENSHOTS.md`](evidence/SCREENSHOTS.md).

## Tested version

| Item | Value |
|---|---|
| Baseline | `8c563c08056dc1e9a69196ef5bab6fd3bdacde23` (tag `baseline`) |
| Application commit tested | `326a537352014ec8451531b0544d42b1ca949a2c` (`fix(ci): retrigger ci and fail the release step if its push fails`) |
| Image | `ghcr.io/venki1402/kv-cache:326a537352014ec8451531b0544d42b1ca949a2c`, index digest `sha256:41e4c8e1aaebbc942f23fb4d6436709bb5f535b22a23eeeea646c9466c047c2e` (amd64 + arm64) |
| Release commit | `a963826f2acd2b12e933735fe5cdd607c4117675` `chore(release): deploy 326a537 [skip ci]` by github-actions[bot], approved by venki1402 in the `production` environment |
| CI run | https://github.com/venki1402/kv-cache/actions/runs/39144658231 (checks, build, e2e and release all succeeded) |
| kind / Kubernetes | kind v0.33.0 / v1.37.0 (`kindest/node:v1.37.0`), 1 control-plane + 1 worker |
| Date | 2026-10-18 |

**Commits after the tested release:**

- Fault-demo pairs, each a bad values change and its revert, which together leave `values-local.yaml` unchanged:
  - `67faf39` / `fa0d8bf`: the documented run.
  - `92f8ce5` / `77e852e` and `9a8e044` / `37dc057`: re-runs made for the screenshots.
- Docs and evidence: `240ba9d`, `78b5784` and the final docs commit.
- Release commit `4cfd5e1` deployed image `240ba9d`. Its source differs from `326a537` only in `docs/evidence/` and the `values-local.yaml` tag, so the Go code and Dockerfile are unchanged.
- Captures up to 16:44 UTC show `326a537` because Argo CD had not yet synced `4cfd5e1`. The cluster now runs the `240ba9d` image, with identical app code.
- Nothing after `326a537` changes the app, the chart templates or CI behaviour.

## Source to running pod

Raw output: [`evidence/traceability.txt`](evidence/traceability.txt). Screenshots: [CI run with approval](evidence/screenshots/01-actions-run-326a537.png) and [Argo CD synced app](evidence/screenshots/02-argocd-synced.png).

| Step | Evidence |
|---|---|
| 1. Source commit | `326a537` by venki1402 on `devops` |
| 2. CI run | [run 39144658231](https://github.com/venki1402/kv-cache/actions/runs/39144658231), push event, `headSha` = `326a537…`. `checks`, `build`, `e2e` and `release` all succeeded |
| 3. Image | `ghcr.io/venki1402/kv-cache:326a537…`. OCI label `org.opencontainers.image.revision` = `326a537…` on both linux/amd64 and linux/arm64 |
| 4. e2e | The same run loaded the image it had just built into a throwaway kind cluster, installed the chart and ran `scripts/smoke.sh` with `EXPECTED_SHA=326a537…` |
| 5. Approval | Environment `production`, approved by venki1402 |
| 6. Release commit | `a963826` by github-actions[bot] changes only `values-local.yaml`: `tag: "9cb6b31…"` → `tag: "326a537…"`. Its tested source is its parent, `326a537` |
| 7. Argo CD | Application `kv-cache` is Synced and Healthy at revision `a963826` |
| 8. Pod | Image `ghcr.io/venki1402/kv-cache:326a537…`. The image ID resolves to the same index digest `sha256:41e4c8e1…` |
| 9. `/version` | `{"commit":"326a537…","version":"baseline-13-g326a537"}`, header `X-App-Version: baseline-13-g326a537` |
| 10. Prometheus | `kvcache_build_info{commit="326a537…",version="baseline-13-g326a537"} 1` |

## Pipeline gates that caught real problems

| Run / where | What failed | Fix commit |
|---|---|---|
| Local `govulncheck` (same command and version as CI), before the first push of CI | GO-2026-6617, a reachable HPACK encoder race (HTTP/2 server crash) in `golang.org/x/net` v0.59.0, pulled in by the OTLP gRPC exporter. Output: [`govulncheck-before.txt`](evidence/govulncheck-before.txt), [`govulncheck-after.txt`](evidence/govulncheck-after.txt) | `735afb3` fix(security): bump golang.org/x/net for GO-2026-6617 |
| Local `golangci-lint` v2.15.0 (same version as CI) | SA1019 on the **baseline** code: `fasthttp.Server.MaxKeepaliveDuration` has been a no-op since fasthttp deprecated it, so the intended 60s keep-alive never applied. Also errcheck on unchecked `Put` errors in the baseline tests | `4fd5663` fix(app): address staticcheck and test findings in baseline code |
| Workflow trigger, commit `b8401d3` | No run started: the commit body quoted the bot's release message, including the skip-ci marker, which GitHub honours anywhere in a commit message. This was a pipeline mistake, not a code defect | `326a537` fix(ci): retrigger ci and fail the release step if its push fails (it also makes the release step fail when all push retries fail) |
| Local `gitleaks dir` before committing evidence | Two `generic-api-key` hits: the smoke test's random cache keys (`"key":"smoke-<time>-<random>"`) in captured output. False positives, but CI's full-history gitleaks scan would have failed | `78b5784` adds a `.gitleaks.toml` allowlist for exactly that pattern. I checked that a real-looking API key is still detected |
| `trivy config` (CI gates on HIGH/CRITICAL) | No HIGH or CRITICAL findings. The MEDIUM/LOW findings are accepted with reasons in [DECISIONS.md](DECISIONS.md#19-accepted-trivy-config-findings) | none needed |

No CI run has failed on a code or security problem: the local checks above, which use the same tools and versions, caught those before each push. Superseded PR runs show as cancelled because PR runs cancel older ones in their concurrency group.

## User journey

Raw output: [`evidence/smoke.txt`](evidence/smoke.txt) (through the ingress) and the `e2e` job log of the CI run (on the throwaway kind cluster).

```
target: http://kv.localhost:8080
ok   PUT without token -> 401
ok   PUT with token -> {"status":"OK","message":"Key inserted/updated"}
ok   GET -> {"status":"OK","key":"smoke-…","value":"v1"}
ok   GET missing key -> 404
ok   /version -> {"buildTime":"2026-10-18T21:51:45+05:30","commit":"326a537352014ec8451531b0544d42b1ca949a2c","version":"baseline-13-g326a537"}
     X-App-Version: baseline-13-g326a537
ok   /version commit matches 326a537352014ec8451531b0544d42b1ca949a2c
PASS
```

The container was also smoke-tested before Kubernetes: [`evidence/docker-smoke.txt`](evidence/docker-smoke.txt).

## Observability

Raw output: [`evidence/observability.txt`](evidence/observability.txt), captured against the released pod.

- **Targets up:** the `kv-cache` ServiceMonitor target is `up` with no scrape error. All other active targets are listed in [`evidence/prometheus.txt`](evidence/prometheus.txt).
- **Rules loaded:** three groups, `kv-cache.recording` (9), `kv-cache.slo` (3) and `kv-cache.kubernetes` (6), all with health `ok`.
- **Alert fired and resolved:** `KvCachePodNotReady` fired during the fault demo and resolved after the revert. Timings are in the reliability table below and in [FAULT-DEMO.md](FAULT-DEMO.md). Screenshots of the lifecycle from a re-run: [inactive](evidence/screenshots/05a-alert-inactive-before.png) → [pending](evidence/screenshots/05b-alert-pending.png) → [firing](evidence/screenshots/05c-alert-firing.png) → [resolved](evidence/screenshots/05d-alert-resolved.png); details are in [SCREENSHOTS.md](evidence/SCREENSHOTS.md#alert-lifecycle).
- **Trace:** a PUT sent through the ingress with a fixed `traceparent` produced a server span `POST put`, whose parent is the caller's span id, and a child span `cache.put` with the shard number. Jaeger lists service `kv-cache` with `service.version` and `vcs.revision` resource attributes. Screenshot: [`GET get` traces, 2 spans each](evidence/screenshots/04-jaeger-trace-get.png).
- **Log → trace:** the app's JSON log line for that request carries the same `trace_id`. Loki has the line with labels `namespace`, `pod`, `container` and `app`. Grafana's Loki data source has a `TraceID` derived field that opens the trace in the Jaeger data source.
- **Dashboard:** the chart provisions the `kv-cache` dashboard through the Grafana sidecar. Every panel query was evaluated against Prometheus and returned data. Screenshot: [dashboard under load](evidence/screenshots/03-grafana-dashboard.png).

## Reliability tests

| Test | Result | Raw output |
|---|---|---|
| Bad release → revert | Rollout stalled, old pod kept serving (169/169 probes 200), Argo CD Degraded after 101 s, `KvCachePodNotReady` fired **140 s** after the bad push. After the revert push: healthy in **3 s**, alert resolved in **22 s**. User-facing 5xx: **0** throughout | [`fault-demo.txt`](evidence/fault-demo.txt), [FAULT-DEMO.md](FAULT-DEMO.md), screenshots [05c](evidence/screenshots/05c-alert-firing.png) and [06](evidence/screenshots/06-jaeger-readyz-errors-fault-demo.png) |
| Delete the pod | Replacement Ready **8 s** after the delete; about **5 s of 503** at the ingress (no ready endpoint). A key written before the delete returns **404** afterwards: the cache is in-memory and starts cold by design (DECISIONS #1) | [`pod-delete.txt`](evidence/pod-delete.txt) |
| Drain the worker node | kv-cache evicted from `kvcache-worker` and Ready on the control-plane **8 s** after the drain started; about **1.8 s of 503** (8 of 45 probes). This is the cost of one replica (DECISIONS #1, #3). The file also keeps an invalid first attempt, where Traefik was still pulling its image on the other node, with an explanation | [`node-drain.txt`](evidence/node-drain.txt) |
| Drift (manual scale) | `kubectl scale --replicas=3` reverted to 1 by Argo CD selfHeal in **2 s**. Side effect: the scale-down kept a new pod and terminated the original, so the cache was emptied | [`drift-demo.txt`](evidence/drift-demo.txt) |
| Load test through the ingress | `hey -z 30s -c 50`: **699,532** requests, **0 errors**, 23.3k req/s, client-side p95 **4.2 ms**, p99 **6.6 ms** (SLO: 99% of `get` under 50 ms: **met**). Server-side p99 under 0.5 ms. Pod peaked at 1.16 CPU (no CPU limit) and 57 Mi | [`load-test.txt`](evidence/load-test.txt) |
| Ingress exposure | `/version`, `/health` and `/get` routed (200/404). `/metrics`, `/healthz` and `/readyz` return **404** from outside. The only host ports are `127.0.0.1:8080` (Traefik) and `127.0.0.1` for the API server | [`ingress-exposure.txt`](evidence/ingress-exposure.txt) |
| NetworkPolicy | From `default` → `kv-cache:7171`: **timed out**. From `traefik` → 7171: **200**. From `traefik` → metrics port 9091: **timed out**. kindnet enforces NetworkPolicy | [`networkpolicy.txt`](evidence/networkpolicy.txt) |
| Read-only filesystem | `kubectl exec` fails (distroless has no shell). An ephemeral debug container running as UID 65532 tried `touch /proc/1/root/probe-write` → **"Read-only file system"** | [`readonly-fs.txt`](evidence/readonly-fs.txt) |

## Limitations of this evidence

- One cluster on one laptop. Timings for rescheduling, Argo sync and alert firing reflect kind on Docker Desktop, not a real cloud cluster.
- The load test runs from the same machine as the cluster, so client and server share CPU, and the ~23k req/s figure is bounded by the laptop. Server-side p99 is below the lowest histogram bucket (0.5 ms), so Prometheus can only say it is under 0.5 ms.
- The 30-day availability SLO cannot be observed in an evening. The burn-rate rules are loaded and evaluated, but only the alerts triggered by the fault demo (listed above) were exercised live.
- Prometheus, Loki and Jaeger keep data for hours, in memory or on node-local storage, so this evidence exists only as the text captured here and in the screenshots.
- `kubectl` on the host is v1.34 against a v1.37 server, outside the supported version skew. Every command used here worked.
- The pod-delete and drift tests ran back to back, so the drift test cannot show the loss of a specific key; the pod names show that the original pod was replaced.
