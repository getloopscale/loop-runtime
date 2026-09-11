# LoopZero Test Runner

Orchestrates load + chaos experiments against a live LoopZero cluster. Wraps
k6 (load) and Chaos Mesh (chaos) behind a single command so CI and
humans can run the same suite.

## Commands

```bash
# Load testing
task test:load:l1              # 10 tenants smoke (5 min)
task test:load:l2              # 50 tenants sustained (30 min)
task test:load:l3              # 100 tenants sustained (60 min)
task test:load:stress          # Ramp to break point (15 min)

# Chaos testing
task test:chaos:controller-kill    # Kill loopzero-controller, verify recovery
task test:chaos:api-leader-kill    # Kill API leader, verify failover
task test:chaos:pg-latency         # 500ms PG latency, verify graceful
task test:chaos:pg-saturation      # Trip circuit breaker
task test:chaos:argocd-crash       # Kill ArgoCD, verify blast radius
task test:chaos:gitea-outage       # Drop Gitea traffic, verify fast-fail
task test:chaos:all                # Run all chaos experiments sequentially
```

## How a load test runs

1. `task test:load:l2` invokes `kubectl apply` with an envsubst'd
   `../performance/k6/job-template.yaml` + the L2 script.
2. Init container builds a fresh git bundle fixture at `/fixtures/bundle.bin`.
3. Init container seeds 50 tenants into PG via the shared `seed.sh`.
4. Main container runs `k6 run scripts/l2-50-tenants.js` with
   `--out=experimental-prometheus-rw` so metrics stream to Prometheus
   during the test.
5. k6 exits with code 0 (all thresholds passed) or 99 (any threshold
   breached). The Job fails in the 99 case, CI goes red.
6. Summary JSON is written to `/tmp/k6-l2-summary.json` inside the
   pod; a sidecar (optional) uploads it to Spaces for archival.

## How a chaos test runs

1. `task test:chaos:controller-kill` applies the PodChaos manifest.
2. Chaos Mesh starts killing the pod per the schedule (every 60s × 2).
3. The Go test runner (`infra/tests/chaos/chaos_test.go`) polls the
   Prometheus query from the `loopzero.io/slo-query` annotation every 10s.
4. If the SLO query ever returns `0` during the experiment window,
   the test fails.
5. After `spec.duration`, Chaos Mesh removes the fault automatically.
6. Test cleanup: `kubectl delete` the PodChaos resource.

## Required cluster state

- **For load tests:** loopzero-api, loopzero-controller, gitea, argocd all
  healthy. PG reachable. Build cluster ready. (Same as a normal
  dev/staging stack.)
- **For chaos tests:** all the above **plus** Chaos Mesh installed in
  the `chaos-mesh` namespace. See `../chaos/manifests/README.md`.

## Fail-safe

The runner Job has `backoffLimit: 0` and `ttlSecondsAfterFinished: 3600`
so an orphaned experiment cleans up after an hour. Every Chaos Mesh
manifest has a bounded `duration` so faults self-remove even if the
runner Job is killed mid-experiment.

**NEVER run these against production.** The runner validates the
current cluster context's name does NOT contain `prod` before applying
any experiment. See `safety.sh`.
