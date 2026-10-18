# Submission

| Field | Value |
|---|---|
| Name | `<NAME>` |
| Student ID | `<STUDENT_ID>` |
| Project | kv-cache: DevOps on local Kubernetes |
| Repository | https://github.com/venki1402/kv-cache |
| Pull request | _filled in after the PR is opened_ |
| Baseline commit | `8c563c08056dc1e9a69196ef5bab6fd3bdacde23` (tag `baseline`) |
| Baseline permalink | https://github.com/venki1402/kv-cache/tree/8c563c08056dc1e9a69196ef5bab6fd3bdacde23 |
| FINAL_COMMIT | `<fill after last push>` |

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
| CI pipeline | **No.** `git log --all -- .github .gitlab-ci.yml .circleci Jenkinsfile` returns nothing |
| Kubernetes manifests, CD, observability | No |

## Change summary

_Filled in at the end of the work._
