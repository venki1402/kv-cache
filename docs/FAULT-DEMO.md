# Fault demo: bad release and GitOps recovery

Run with `make fault-demo`. Raw output: [`evidence/fault-demo.txt`](evidence/fault-demo.txt).

The bad release is a values change, `config.failReadiness: true`, committed to `devops` like any other change. It makes `/readyz` return 503 while the process stays alive. That is the failure mode of a release which starts but cannot serve, such as a missing dependency or a bad config. Recovery is `git revert` of that commit; nobody touches the cluster by hand.

Times are UTC on 2026-10-18. The running release before the demo was image `326a537`.

## Timeline

| Time | Event |
|---|---|
| 16:34:44 | Bad commit [`67faf39`](https://github.com/venki1402/kv-cache/commit/67faf39ea19d31d66030a05146e618d279904a98) pushed: `chore(helm): enable failing readiness drill` |
| 16:34:48 | Argo CD synced it (4 s; the script asks Argo for a refresh instead of waiting for the 3-minute poll). A new pod started, `0/1` Ready |
| 16:35:01 | `KvCachePodNotReady` and `KvCacheRolloutStuck` became pending |
| 16:35:08 | `kubectl rollout status` stalled: "1 old replicas are pending termination". The old pod was still serving `/version` = `326a537` |
| 16:36:49 | Argo CD health: **Degraded** (`progressDeadlineSeconds: 120` exceeded) |
| 16:37:04 | `KvCachePodNotReady` (critical) **firing**, 140 s after the bad push |
| 16:37:10 | Revert [`fa0d8bf`](https://github.com/venki1402/kv-cache/commit/fa0d8bfe88386a520f7a44654dadb070e10e5f65) pushed: `fix(helm): revert failing readiness drill` |
| 16:37:13 | Argo CD synced the revert; rollout complete and app Healthy 3 s after the push |
| 16:37:32 | `KvCachePodNotReady` resolved, 22 s after the revert push. Smoke test passed |

## Symptom

- Argo CD showed `kv-cache` Synced but **Degraded**.
- `KvCachePodNotReady` (critical) fired and `KvCacheRolloutStuck` was pending.
- `kubectl rollout status` hung with one new pod at `0/1` and the old ReplicaSet still holding its pod.
- Users saw nothing. A probe of `http://kv.localhost:8080/version` once per second for the whole demo got 169 of 169 responses with 200.

## Hypothesis

The new pod was running but failed readiness. Because of `maxUnavailable: 0`, the Deployment would not remove the old pod until a new one became Ready, so traffic stayed on the old version. The last change to the desired state was the most recent commit touching the chart values.

## Evidence

- `kubectl get pods`: new pod `kv-cache-744f4656c6-pvl87` `0/1 Running` with 0 restarts. Liveness was passing and readiness was failing, so the process was alive but not serving.
- Argo CD's synced revision was `67faf39`, and `git show 67faf39` shows the only change: `config.failReadiness: true`.
- Prometheus counted 30 `503` responses on `route="readyz"`, all from the new pod: kubelet probes every 5 s for about 2.5 min. The SLO error ratio, which excludes probe routes, stayed at 0. The script's raw "5xx rate 0.1" line counts these probe responses; the breakdown by route is at the end of the raw output.

## Change

`git revert` of `67faf39`, committed as `fa0d8bf` and pushed. Nobody touched the cluster: no `kubectl edit`, no `kubectl rollout undo`. Argo CD's selfHeal would have reverted manual changes, and Git stays the source of truth.

## Verification

- Argo CD: Synced to `fa0d8bf`, **Healthy**.
- Only one pod was left, the original one, still on image `326a537`. The bad ReplicaSet was scaled to zero.
- `KvCachePodNotReady` resolved.
- `scripts/smoke.sh` passed: 401 without a token, 200 with one, GET returns the value, 404 for a missing key, and `/version` reports `326a537`.
- User-facing errors were 0 for the whole demo.

## What this shows

- `maxUnavailable: 0` plus a readiness probe turns a bad release into a stalled rollout rather than an outage.
- `progressDeadlineSeconds: 120` makes Argo CD report Degraded within about 2 minutes.
- The alert fired 140 s after the push: 2 minutes of `for:` plus scrape and evaluation intervals.
- Recovery is a normal Git operation with an audit trail, and took 3 s to apply.

## Drift demo

Raw output: [`evidence/drift-demo.txt`](evidence/drift-demo.txt).

I ran `kubectl -n kv-cache scale deploy kv-cache --replicas=3`, and Argo CD's selfHeal set it back to 1 within **2 s**.

One side effect: when the ReplicaSet scaled back down, it kept one of the brand-new pods and terminated the original. A drift like this therefore empties the cache, just as a restart does. This is one more reason the one-replica decision treats the cache as disposable.
