# ultra fast sharded key value lru cache

![Go Version](https://img.shields.io/badge/Go-1.26-blue.svg)
![Coverage](https://img.shields.io/badge/Coverage-92%25-brightgreen.svg)
![Build](https://img.shields.io/badge/Build-Passing-brightgreen.svg)

a high throughput, memory-bounded, sharded LRU key-value cache written in Go. uses bitwise request routing across 32 shards to minimize lock contention and deterministic O(1) eviction to maintain a strict memory limit, sustaining **172K+ requests/sec** at **~2.15 ms median (p50)** latency.

## performance benchmarks

try it yourself - https://gist.github.com/Venki1402/4c9c0e99106ede89a558c04768a56e3d

## instructions to benchmark venki1402/kv-cache yourself

### run it
```bash
docker run -p 7171:7171 venki1402/kv-cache
```

#### seed the cache with 1,000 keys

```bash
for i in {1..1000}; do
  curl -X POST http://localhost:7171/put -H "Content-Type: application/json" -d "{\\"key\\":\\"key$i\\", \\"value\\":\\"This is the cached value for item $i\\"}"
done
```

#### create the `random_keys.lua` script

```lua
math.randomseed(os.time())
request = function()
   local random_id = math.random(1, 1000)
   local path = "/get?key=key" .. random_id
   return wrk.format("GET", path)
end
```

#### run `wrk`

```bash
wrk -t8 -c400 -d30s --latency -s random_keys.lua http://localhost:7171
```

#### results

```
Running 30s test @ http://localhost:7171
  8 threads and 400 connections
  Thread Stats   Avg      Stdev     Max   +/- Stdev
    Latency     2.15ms  817.35us  38.41ms   90.43%
    Req/Sec    21.77k     1.35k   33.01k    79.08%
  Latency Distribution
     50%    2.15ms
     75%    2.28ms
     90%    2.49ms
     99%    4.41ms
  5205733 requests in 30.10s, 0.99GB read
Requests/sec: 172965.97
Transfer/sec:     33.62MB
```

benchmarked using `wrk` and a `Lua` randomization script to simulate 400 concurrent connections across 8 threads for 30 seconds. the workload is read-heavy: the script issues only `GET` requests against 1,000 pre-seeded keys (near-100% cache hits).

* **Throughput**: 172,965 Requests / Second
* **Median Latency (p50)**: 2.15 ms
* **p99 Latency**: 4.41 ms
* **Total Volume**: 5,205,733 requests served in 30 seconds
* **Data Transferred**: 0.99 GB

## architecture highlights

* Minimized Lock Contention: Utilizes a 32-way sharded architecture. Requests are routed to specific shards using a djb2 bitwise hashing algorithm, so parallel requests rarely fight for the same sync.Mutex.

* Deterministic O(1) Eviction: Implements a strict doubly-linked list (LRU) combined with a map pointer system. When a shard hits its math-allocated memory threshold, it evicts stale data in true O(1) time.

* Strict Memory Bounding: Tracks exact byte allocations per shard instead of relying on standard runtime.MemStats, keeping the cached data strictly under a configurable 1GB ceiling.

* High-Speed HTTP: Powered by fasthttp, the fastest HTTP package available for Go, bypassing the standard library's overhead for extreme raw throughput.

## local development

> prerequisites

Go 1.26 or higher

> clone the repository

```bash
git clone https://github.com/venki1402/kv-cache
cd kv-cache
```

> install dependencies

```bash
go mod tidy
```

> run the server

```bash
go run .
```

**The server will start on localhost:7171**

## run with docker

> build the docker image

```bash
docker build -t kv-cache .
```

> run the container

```bash
docker run -p 7171:7171 kv-cache
```

**The server will start on localhost:7171**

## api usage

### 1. Insert / Update a Key (PUT)

```Bash
curl -X POST <http://localhost:7171/put> \\
  -H "Content-Type: application/json" \\
  -d '{"key":"king", "value":"venki"}'
```

Response:

```JSON
{"status":"OK","message":"Key inserted/updated"}
```

### 2. Retrieve a Key (GET)

```Bash
curl -X GET "<http://localhost:7171/get?key=king>"
```

Response:

```JSON
{"status":"OK","key":"king","value":"venki"}
```

## testing

The project maintains 92% test coverage (77.5% at baseline), specifically targeting core sharding logic, hash collisions, O(1) memory eviction constraints, and HTTP edge cases.

> To run the test suite:

```Bash
go test -v -cover
```

## run on kubernetes

The cache runs on a local [kind](https://kind.sigs.k8s.io/) cluster, deployed by Argo CD from this repo, with Prometheus, Grafana, Alertmanager, Loki and Jaeger around it. GitHub Actions tests every commit, builds a multi-arch image to GHCR and, after a manual approval, bumps the image tag that Argo CD deploys.

### prerequisites

Tested on macOS arm64 with Docker Desktop (8 GB memory for the VM).

| Tool | Version |
|---|---|
| Docker Desktop | 8 GB VM |
| kind | v0.33.0 (node image kindest/node v1.37.0) |
| kubectl | any recent client |
| helm | v4.3.1 |
| argocd CLI | v3.x (only for `make evidence`) |
| jq, yq, hey, curl | any recent |

Chart versions for the platform are pinned in [`deploy/platform/versions.env`](deploy/platform/versions.env).

### bring it up

```bash
make up        # kind cluster, platform, namespace + secret, Argo CD Application
make urls      # print UI addresses and how to read their credentials
```

`make up` runs `cluster-up`, `platform-up`, `secrets` and `argo-app`. Argo CD then pulls `deploy/helm/kv-cache` from the `devops` branch and deploys the image tag in `values-local.yaml`.

### get the token

`make secrets` puts a random token into the `kv-cache-auth` Secret. It is never written to the repo.

```bash
TOKEN=$(kubectl -n kv-cache get secret kv-cache-auth -o jsonpath='{.data.token}' | base64 -d)
```

### access

Everything goes through Traefik on `127.0.0.1:8080`; `*.localhost` resolves to loopback, so no `/etc/hosts` edits are needed.

| What | URL |
|---|---|
| App | http://kv.localhost:8080 (`/put`, `/get`, `/version`, `/health`) |
| Grafana | http://grafana.localhost:8080 (dashboard "kv-cache") |
| Prometheus | http://prometheus.localhost:8080 |
| Alertmanager | http://alertmanager.localhost:8080 |
| Jaeger | http://jaeger.localhost:8080 |
| Argo CD | http://argocd.localhost:8080 |

If you prefer port-forwards: `kubectl -n monitoring port-forward svc/kps-prometheus 9090` and `kubectl -n kv-cache port-forward svc/kv-cache 7171`.

### smoke test

```bash
make smoke
# or by hand
curl -X POST http://kv.localhost:8080/put -H "Authorization: Bearer $TOKEN" -d '{"key":"king","value":"venki"}'
curl "http://kv.localhost:8080/get?key=king"
curl -i http://kv.localhost:8080/version
```

Other targets: `make load` (hey through the ingress), `make fault-demo`, `make drift-demo`, `make evidence`, `make dev-deploy` (build locally and `kind load` for fast iteration).

### teardown

```bash
make down
```

### known limitations

- One replica by design: a restart, eviction or node drain empties the cache and causes a short outage. See [DECISIONS](docs/DECISIONS.md).
- Prometheus, Loki and Jaeger keep data for hours, in memory or on the kind node; nothing survives `make down`.
- Everything is plain HTTP on loopback. `values-prod.yaml` shows the TLS setup but is never deployed.
- The Secret is created by hand (`make secrets`); production would use External Secrets or Sealed Secrets.

## what changed from the baseline

Baseline is commit [`8c563c0`](https://github.com/venki1402/kv-cache/tree/8c563c08056dc1e9a69196ef5bab6fd3bdacde23) (tag `baseline`).

| Area | Baseline | Now |
|---|---|---|
| Packaging | `scratch` image, amd64 only, Docker Hub | Distroless nonroot, amd64 + arm64 cross-compiled, GHCR tagged by git SHA, SBOM and provenance |
| Runtime | `docker run` | kind cluster, Helm chart, Traefik ingress |
| Config | Hard-coded constants | Env vars with the same defaults; 128 MiB cap set only in Helm values |
| CI | None | GitHub Actions: vet, golangci-lint, race tests + coverage, govulncheck, gitleaks, helm lint, kubeconform, trivy config and image |
| CD and release | Manual | Argo CD GitOps; release job behind a manual approval bumps the image tag |
| Health | `/health` | `/health` kept, plus `/healthz` liveness, `/readyz` readiness and `/version` |
| Metrics | None | Prometheus metrics on port 9091, ServiceMonitor, SLO burn-rate alerts, Grafana dashboard |
| Logs | `log.Printf` | JSON (`log/slog`) with version, commit and trace_id, collected by Alloy into Loki |
| Tracing | None | OpenTelemetry to Jaeger, off by default, on locally |
| Security | Non-root user | Restricted Pod Security, read-only root fs, dropped capabilities, NetworkPolicy, optional bearer auth on writes, pinned actions, no long-lived credentials |

## release flow

1. Push to `devops` (or `main`). CI runs checks, builds and pushes `ghcr.io/venki1402/kv-cache:<sha>`, then installs that exact image on a throwaway kind cluster and runs the smoke test.
2. The `release` job waits for approval in the `production` environment.
3. On approval it commits `chore(release): deploy <short-sha> [skip ci]`, which sets `image.tag` in `values-local.yaml`.
4. Argo CD syncs the change. `/version` and `kvcache_build_info` report the new commit.

The release commit is a bot commit; the source it deploys is its parent.

## rollback

`git revert` the release (or values) commit and push. Argo CD syncs the previous tag. Don't `kubectl edit` the Deployment: selfHeal reverts manual changes. [`docs/FAULT-DEMO.md`](docs/FAULT-DEMO.md) shows a full bad-release-and-revert run.

## docs

- [Architecture](docs/ARCHITECTURE.md)
- [Decisions](docs/DECISIONS.md)
- [Runbook](docs/RUNBOOK.md)
- [Fault demo](docs/FAULT-DEMO.md)
- [Evidence](docs/EVIDENCE.md)
- [Submission](docs/SUBMISSION.md)

## License

This project is licensed under the MIT License.
