# Submission

| Field | Value |
|---|---|
| Name | `Sai Venkatesh Alampally` |
| Student ID | `23bcs10095` |
| Project | kv-cache: DevOps on local Kubernetes |
| Repository | https://github.com/venki1402/kv-cache |
| Pull request | https://github.com/venki1402/kv-cache/pull/2 |
| Baseline commit | `8c563c08056dc1e9a69196ef5bab6fd3bdacde23` (tag `baseline`) |
| Baseline permalink | https://github.com/venki1402/kv-cache/tree/8c563c08056dc1e9a69196ef5bab6fd3bdacde23 |
| FINAL_COMMIT | Recorded in the PR description and the submission form (a commit can't contain its own hash). |

## Description

kv-cache is a high-throughput, memory-bounded, sharded LRU key-value cache written in Go on top of fasthttp.
This submission adds a complete DevOps setup around it: CI with security gates, multi-arch images on GHCR,
a Helm chart deployed to a local kind cluster through Argo CD (GitOps), metrics, logs, traces, SLO-based alerts,
and scripted fault and reliability tests with captured evidence.

## Existing DevOps features at baseline

Verified against commit `8c563c0`:

| Feature | Present at baseline |
|---|---|
| Dockerfile (multi-stage, `scratch` runtime, amd64 only) | Yes |
| `.dockerignore` | Yes |
| Public Docker Hub image (`venki1402/kv-cache`) | Yes |
| Unit tests (`main_test.go`, ~77.5% coverage) | Yes |
| `/health` endpoint (plain-text logs only, no metrics or tracing) | Yes |
| Non-root container user | Yes |
| CI pipeline | **No.** `git log --all -- .github .gitlab-ci.yml .circleci Jenkinsfile` returns nothing |
| Kubernetes manifests, CD, metrics, tracing | No |

## Change summary

All changes are in PR #2 (`devops` → `main`). Details are in the [README](../README.md#what-changed-from-the-baseline), and the evidence is in [EVIDENCE.md](EVIDENCE.md).

- **App:** env-var config with unchanged defaults; `/healthz`, `/readyz` and `/version`; Prometheus metrics on a separate port; JSON logs; optional bearer auth on writes; OpenTelemetry tracing, off by default; graceful shutdown. Coverage went from 77.5% to 92%.
- **Container:** multi-arch distroless image, cross-compiled, with OCI labels. Published to GHCR by git SHA, with SBOM and provenance.
- **Kubernetes:** a Helm chart with restricted pod security, NetworkPolicy, ServiceMonitor, SLO alerts and a Grafana dashboard. Environment differences live only in values files.
- **CI/CD:** GitHub Actions runs checks, then a build with trivy, then kind e2e, then a release gated by a required reviewer. Argo CD GitOps deploys to a local kind cluster.
- **Observability:** Prometheus, Alertmanager, Grafana, Loki/Alloy and Jaeger, with a log-to-trace link.
- **Reliability:** a scripted bad release with recovery by `git revert`, a drift demo, pod delete, node drain, NetworkPolicy, read-only filesystem and load tests.
- **Docs:** architecture, decision records, runbook, fault demo and evidence.
