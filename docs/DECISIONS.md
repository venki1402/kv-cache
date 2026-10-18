# Design decisions

Short records of the choices I made when taking kv-cache from a single binary to
a deployed, observable service. Each entry follows the same shape: what I chose,
what else I considered, why, and what would change my mind.

## 1. One replica in a Deployment, not a StatefulSet

**Choice:** A Deployment with `replicas: 1`, fixed in the template (not a value).

**Alternatives:** StatefulSet with a PVC; several replicas behind one Service.

**Why:** The cache is in memory and has no persistence or replication. A
StatefulSet buys stable identity and storage, and I use neither. A restart
starts with a cold cache, and I treat that as expected behaviour for a cache. Several
replicas behind one ClusterIP Service would each hold a different key set, so a
`PUT` to one pod and a `GET` routed to another would miss.

**When I'd choose differently:** If cached data had to survive restarts, I would
add periodic snapshots to a PVC and move to a StatefulSet. To scale out, I would
shard by key: client-side consistent hashing against a headless Service, so each
client talks to the pod that owns the key.

## 2. No HorizontalPodAutoscaler

**Choice:** No HPA.

**Alternatives:** HPA on CPU or request rate.

**Why:** Adding replicas does not add capacity here, it splits the key space and
breaks reads (see 1). An HPA would make correctness depend on load. metrics-server
is installed for `kubectl top`, not for autoscaling.

**When I'd choose differently:** Once keys are partitioned (consistent hashing),
scaling becomes a resharding problem, and I would handle it deliberately, not
with an autoscaler reacting to CPU.

## 3. No PodDisruptionBudget

**Choice:** No PDB.

**Alternatives:** `minAvailable: 1` or `maxUnavailable: 0`.

**Why:** With one replica, any PDB that protects availability also blocks
`kubectl drain` forever, which is worse than a short outage during planned node
maintenance. The pod is not highly available anyway, so a PDB would add an
operational trap and no real protection.

**When I'd choose differently:** With two or more replicas that can serve the
same keys, I would add `maxUnavailable: 1`.

## 4. RollingUpdate with maxSurge 1 / maxUnavailable 0

**Choice:** `RollingUpdate`, `maxSurge: 1`, `maxUnavailable: 0`,
`progressDeadlineSeconds: 120`.

**Alternatives:** `Recreate`; canary or blue-green (Argo Rollouts, Flagger).

**Why:** The old pod keeps serving until the new one passes readiness, so a bad
image that never becomes ready causes no downtime. The rollout stalls and
`KvCacheRolloutStuck` fires. The new pod always starts cold, and no traffic
strategy fixes that, so canary or blue-green would add a controller and
complexity for little gain. The 120s deadline makes a stuck rollout show up as
Degraded in Argo CD within minutes.

**When I'd choose differently:** With real traffic and an error budget worth
protecting, I would use a canary with automated analysis against the SLO
metrics. I would also look at cache warm-up before shifting traffic.

## 5. Pull-based GitOps with Argo CD

**Choice:** Argo CD runs in the cluster and pulls the Helm chart
`deploy/helm/kv-cache` with `values-local.yaml` from the public GitHub repo.
Automated sync has `prune` and `selfHeal` turned on. `targetRevision` is `devops` while the work
lives on that branch and moves to `main` after the merge.

**Alternatives:** `helm upgrade` from a CI job (push-based); Flux.

**Why:** The cluster is kind on my laptop. GitHub-hosted runners cannot reach
it, and I did not want to open a tunnel or store a kubeconfig in CI. With pull,
the cluster reaches out to GitHub and CI never holds cluster credentials. The repo
is public, so Argo CD needs no repository credentials either. `selfHeal` reverts
manual `kubectl` edits, so Git stays the source of truth and rollback is
`git revert`.

**When I'd choose differently:** Flux would serve equally well. I picked Argo CD
for its UI, which makes sync and health state easy to show. For a cluster CI
could reach and a team that did not want an in-cluster controller, push-based
Helm from CI is simpler.

## 6. Platform components outside the Argo CD boundary

**Choice:** `make platform-up` (`scripts/platform-up.sh`) installs Traefik,
kube-prometheus-stack, Loki, Alloy, Jaeger, Argo CD and metrics-server.
Chart versions are pinned in `deploy/platform/versions.env`. Argo CD manages
only the app.

**Alternatives:** App-of-apps, where Argo CD also manages the platform.

**Why:** Argo CD cannot install itself before it exists, and the app depends on
CRDs (ServiceMonitor, PrometheusRule) and an IngressClass that must exist first.
A plain ordered script is easier to reason about for a throwaway local cluster.
It also keeps the GitOps demo focused on the application lifecycle.

**When I'd choose differently:** On a long-lived cluster I would bootstrap Argo
CD once and let it manage the platform charts too, with sync waves for ordering.

## 7. No startup probe

**Choice:** Readiness (`/readyz`) and liveness (`/healthz`) probes only.

**Alternatives:** A startup probe guarding liveness during boot.

**Why:** The process has no warm-up. It is ready in milliseconds, and the cache
starts empty by design. A startup probe protects slow starters from being
killed by liveness, and this service is not one.

**When I'd choose differently:** If I added snapshot restore on boot, loading
could take seconds or minutes. I would then add a startup probe sized to the
largest snapshot.

## 8. No CPU limit

**Choice:** `requests.cpu: 100m`, no `limits.cpu`.

**Alternatives:** A CPU limit equal to or above the request.

**Why:** A CPU limit is enforced through CFS quota. A latency-sensitive server that
bursts gets throttled for the rest of the period, which shows up as p99 spikes
even when the node has idle CPU. The request already guarantees a share under
contention. This is a deliberate trade-off, and Trivy reports it (KSV-0011, see 19).

**When I'd choose differently:** On a shared cluster with a policy that requires
CPU limits, or in a namespace with a LimitRange, I would set a limit well above
observed peak usage.

## 9. Memory cap in Helm values, GOMEMLIMIT at 90% of the limit

**Choice:** The binary keeps its 1 GiB `MaxMemoryBytes` default. The chart sets
`MAX_MEMORY_BYTES` to 128 MiB with a 256Mi memory limit (192Mi request). The
app reads the container limit through the downward API (`MEMORY_LIMIT_BYTES`)
and sets the Go soft memory limit to 90% of it.

**Alternatives:** Change the default in code; set `GOMEMLIMIT` directly in the
manifest.

**Why:** Changing the code default would change behaviour for anyone running
the binary outside Kubernetes. The pod needs headroom above the cached bytes for
the Go runtime and map overhead, so the cache cap is half the limit. The
downward API can expose the limit but cannot scale it, so the app does the 90%
arithmetic. That keeps one source of truth (`resources.limits.memory`) and lets
the GC work harder before the kernel OOM-kills the pod. An explicit `GOMEMLIMIT`
still wins if set.

**When I'd choose differently:** With measured overhead ratios from production
I would tune the cap closer to the limit. `values-prod.yaml` uses 1 GiB of
cache with a 1536Mi limit.

## 10. `/get` is unauthenticated

**Choice:** Optional bearer token (`AUTH_TOKEN`) protects `/put` only. `/get`
is open.

**Alternatives:** Token on every route; auth at the ingress; mTLS via a mesh.

**Why:** Reads have no side effects. The Service is ClusterIP, and a NetworkPolicy
admits port 7171 only from the `traefik` namespace. `/get` is the hot path, and a
header check there costs latency on every request. Writes are what can corrupt
or evict data, so that is where I check the token, with a constant-time compare.

**When I'd choose differently:** If cached values were sensitive, I would require
auth on reads too. I would do it at the ingress (forward auth) or with mTLS
between clients and the service, not in the request handler.

## 11. Tracing off by default

**Choice:** `OTEL_ENABLED=false` in the binary and in `values.yaml`.
`values-local.yaml` turns it on with sampling 1.0. `values-prod.yaml` uses 0.1.

**Alternatives:** Always on with a low sample rate.

**Why:** Span creation allocates on the hot path. When tracing is off, every tracing
code path is behind a single boolean check, so existing users pay nothing.
Locally I want every request traced so the log-to-trace link always works. In
production, 10% head sampling keeps cost bounded.

**When I'd choose differently:** Behind an OpenTelemetry Collector with tail
sampling, I would keep head sampling at 1.0 and let the collector keep errors
and slow requests.

## 12. Metrics on a separate port

**Choice:** `/metrics` on 9091, API on 7171. The Ingress routes only exact
paths `/get`, `/put`, `/version` and `/health`.

**Alternatives:** `/metrics` on the API port, filtered at the ingress.

**Why:** A separate listener means metrics cannot leak through the ingress by a
path mistake. The NetworkPolicy can allow 9091 only from the `monitoring`
namespace. Scrapes also do not compete with API requests on the same server.

**When I'd choose differently:** I would not, for a service with a public API.

## 13. GitHub Actions for CI

**Choice:** GitHub Actions, one workflow (`.github/workflows/ci.yml`). Actions are
pinned by commit SHA and the top-level permission is `contents: read`.

**Alternatives:** GitLab CI, Jenkins, Tekton.

**Why:** The code is on GitHub. Actions needs no extra server, has hosted runners
with Docker, and its `GITHUB_TOKEN` authenticates to GHCR, so there are no long-lived
credentials. Jenkins means running and patching a server. Tekton runs in a
cluster, and my only cluster is a laptop. GitLab CI would mean mirroring the repo.

**When I'd choose differently:** If the code lived on GitLab, or the organisation
already ran Jenkins or Tekton with shared pipelines, I would use that.

## 14. GHCR for images

**Choice:** `ghcr.io/venki1402/kv-cache`, tagged with the full commit SHA and
`sha-<short>`. Multi-arch with provenance and SBOM attestations.

**Alternatives:** Docker Hub; a cloud registry (ECR, GAR).

**Why:** CI pushes with `GITHUB_TOKEN`, so I need no extra secret. The image
sits next to the code and links back to the repo through the
`org.opencontainers.image.source` label. Docker Hub would need a stored access
token and has pull rate limits for anonymous clients.

**When I'd choose differently:** On a cloud provider I would use its registry so
nodes pull with workload identity and stay in-region.

## 15. Traefik as the ingress controller

**Choice:** Traefik on NodePort 30080, mapped by kind to host `127.0.0.1:8080`.

**Alternatives:** ingress-nginx; Gateway API.

**Why:** ingress-nginx is retired upstream, and I did not want to build on
something with no future patches. Traefik supports the plain Ingress API, which
every platform chart here (Grafana, Prometheus, Alertmanager, Jaeger, Argo CD)
already templates. It also exposes Prometheus metrics with no extra setup.

**When I'd choose differently:** For a new production cluster I would use
Gateway API resources, which Traefik also implements, for the cleaner split
between platform and app ownership of routes.

## 16. Secret handling

**Choice:** `make secrets` (`scripts/secrets.sh`) creates the `kv-cache`
namespace with Pod Security `restricted`. It also creates Secret `kv-cache-auth`
with a random token, made in the cluster with `openssl rand`. The chart only
references it (`auth.existingSecret`) and never contains the value.

**Alternatives:** Token in values; Sealed Secrets; External Secrets Operator.

**Why:** Nothing secret ever enters Git, so gitleaks has nothing to find and
Argo CD never sees the token. For one local cluster, a one-time manual step is
simpler than running another controller.

**When I'd choose differently:** In production I would sync the token from a
secret manager with External Secrets Operator. `values-prod.yaml` already
points at `kv-cache-auth-external`. Sealed Secrets is the choice if there is no
external secret manager.

## 17. Multi-arch images by cross-compiling

**Choice:** The builder stage runs on `$BUILDPLATFORM` and cross-compiles with
`GOOS`/`GOARCH` (`CGO_ENABLED=0`) for `linux/amd64` and `linux/arm64`. The
runtime image is distroless static `nonroot` (UID 65532).

**Alternatives:** QEMU emulation in buildx; separate native runners per arch.

**Why:** Go cross-compiles natively, so there is no reason to emulate a whole
toolchain under QEMU, which is several times slower. One builder produces both
binaries. arm64 matters because I run kind on Apple silicon.

**When I'd choose differently:** If the build needed cgo or native
dependencies, I would use native arm64 runners and merge the manifests.

## 18. Release gate with GitHub environment approval

**Choice:** On push, after checks, build and e2e pass, a `release` job runs in
environment `production` with required manual approval. It uses `yq` to set
`image.tag` in `values-local.yaml` and pushes a bot commit
`chore(release): deploy <short-sha> [skip ci]`. Argo CD syncs that commit.

**Alternatives:** Auto-deploy every green build; Argo CD Image Updater; manual
PR to bump the tag.

**Why:** A person decides when a tested image reaches the cluster, and the
decision is recorded in Git and in the environment history. The deploy is still
a Git change, so rollback is `git revert`. Three guards stop the release commit
from triggering CI again: `[skip ci]`, `paths-ignore` on `values-local.yaml`, and
GitHub not starting workflows for pushes made with `GITHUB_TOKEN`.

**When I'd choose differently:** With good automated canary analysis I would
drop the manual gate for low-risk services and let green builds flow.

## 19. Accepted Trivy config findings

**Choice:** CI fails on HIGH and CRITICAL findings only. These MEDIUM and LOW
findings are accepted on purpose:

| ID | Finding | Why I accept it |
|---|---|---|
| KSV-0011 | No CPU limit | Intentional, see 8. |
| KSV-0125 | Image from an untrusted registry | GHCR is my registry. The rule's allow list does not know it. |
| KSV-01010 | Sensitive data in ConfigMap | False positive. It matches key names like `PORT` and `MAX_MEMORY_BYTES`. The token is in a Secret. |
| KSV-0110 | Workload in the default namespace | Appears only when rendering without `-n`. CI renders with `-n kv-cache`. |
| DS-0026 | No `HEALTHCHECK` in Dockerfile | Kubernetes probes do this job. Distroless has no shell or curl to run one. |

**Alternatives:** Suppress them in a `.trivyignore`; gate on MEDIUM too.

**Why:** Gating on MEDIUM would fail on deliberate choices. Recording them here
keeps the reasoning visible instead of hidden in an ignore file.

**When I'd choose differently:** If the list grew, I would move to a
`.trivyignore` with an expiry date and a link to this file for each entry.
