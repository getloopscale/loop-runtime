# LoopRuntime — k6 Load Testing Suite

Industry-standard load testing for the LoopRuntime platform using
[k6](https://k6.io). Covers deploy pipeline validation, API
benchmarking, soak testing, SSE streaming, and chaos resilience.

## Architecture

Two complementary tools:

| Tool | Purpose | Entry Point |
|------|---------|-------------|
| **k6** (this dir) | HTTP-level SLO testing: latency distributions, throughput, error rate | `task test:load:l1` |
| **Go harness** (`../load/`) | Pipeline E2E tests: provision → build → deploy → verify | `task loadtest:run LEVEL=l1` |

Why both? k6 measures coordinated-omission-safe latency distributions
with open workload models. The Go harness exercises the full deploy
pipeline end-to-end (real builds, real images, real ArgoCD sync). Each
answers different questions.

## Quick Start

```bash
# Install k6 (>= 0.52)
brew install k6   # or: go install go.k6.io/k6@latest

# Run L1 smoke against dev cluster (in-cluster Job)
task test:load:l1

# Run API benchmark locally
export K6_API_URL=https://api.dev.getloopscale.com
export K6_SVC_AUTH=dev-service-auth-secret-default
k6 run scripts/api-endpoints.js

# Run soak test locally (6h — use screen/tmux)
k6 run scripts/soak-6h.js
```

## Test Suite

### Deploy Pipeline Tests (L1-L5)

Progressive load levels validating the full deploy pipeline under
increasing load.

| Script | Level | Tenants | Duration | Executor | Purpose |
|--------|:-----:|:-------:|:--------:|----------|---------|
| `l1-10-tenants.js` | L1 | 10 | 15 min | per-vu-iterations | Smoke — every component works |
| `l2-50-tenants.js` | L2 | 50 | 35 min | ramping-arrival-rate | Moderate — autoscaler responds |
| `l3-100-tenants.js` | L3 | 100 | 65 min | ramping-arrival-rate | Scale — 99.5% success required |
| `l4-500-tenants.js` | L4 | 500 | 2h | ramping-arrival-rate | Capacity — find bottlenecks |
| `l5-multicell.js` | L5 | 5000 | 4h | ramping-arrival-rate | Multi-cell stress |

### Specialized Tests

| Script | Duration | Executor | Purpose |
|--------|:--------:|----------|---------|
| `stress-break-point.js` | 15 min | ramping-arrival-rate | Find breaking point — ramp 0→10 RPS until SLO breaks |
| `soak-6h.js` | 6h | constant-arrival-rate | Detect memory leaks, FD exhaustion, pool starvation |
| `sse-deploy-stream.js` | 15 min | constant-arrival-rate | Validate SSE streaming during deploys |
| `chaos-load.js` | 25 min | ramping-arrival-rate | Measure resilience under active chaos experiments |
| `api-endpoints.js` | 5 min | constant-arrival-rate | Pure HTTP latency benchmark at 100 RPS |

### Shared Libraries

| File | Purpose |
|------|---------|
| `scripts/lib/loop-runtime-api.js` | Shared API client (auth, request helpers, custom metrics) |
| `scripts/lib/thresholds.js` | SLO thresholds — the `cell-budget.md` they were derived from exists in no commit |

### Fixtures

| File | Purpose |
|------|---------|
| `fixtures/bundle-builder.js` | Loads git bundle for upload (from `/fixtures/bundle.bin` or env) |

## Thresholds (SLOs)

Every script enforces thresholds hardcoded in `scripts/lib/thresholds.js`. They were described as derived from `cell-budget` until 2026-09-11; that file exists in no commit.
A failed threshold fails the k6 exit code → fails the Job → fails CI.

```js
// Base thresholds (all levels)
http_req_failed:            rate < 1%
http_req_duration:          p50 < 2s, p95 < 10s, p99 < 30s
loop_runtime_build_succeeded:       rate > 99%
loop_runtime_app_verified:          rate > 99%
loop_runtime_deploy_duration_ms:    p95 < 600s

// API-specific (api-endpoints.js)
loop_runtime_api_health_duration:   p99 < 100ms
loop_runtime_api_projects_list:     p99 < 500ms
loop_runtime_api_builds_get:        p99 < 200ms

// SSE-specific (sse-deploy-stream.js)
loop_runtime_sse_connect_duration:  p95 < 5000ms
loop_runtime_sse_terminal_received: rate > 95%
```

## Coordinated Omission

All sustained-load scripts (L2+) use **open workload models**
(`constant-arrival-rate` or `ramping-arrival-rate`). This ensures:

- Requests fire at a fixed rate regardless of response time
- Slow responses don't "hide" latency of requests that should have been sent
- Reported p99 reflects real response time, not service time

L1 uses `per-vu-iterations` (acceptable for 10 VUs smoke test).

Reference: Gil Tene, "How NOT to Measure Latency" — open workload is
the only correct way to measure latency under sustained load.

## Which runner per level (canonical)

Two execution paths exist. Pick by level — the rule is **"is the platform or
the k6 generator the bottleneck?"**:

| Level | Canonical runner | Task | Why |
|---|---|---|---|
| L1, L2, L3, stress, soak | **Single-pod Job** (`run-k6.sh`) | `task test:load:l1..l3`, `:stress`, `:soak` | Low arrival rate (1–3/min). A single k6 pod is never the bottleneck — the **platform** is what's measured. |
| L4 (500 tenants) | **Distributed (k6 Operator)** | `task k6:distributed:l4` (8 pods) | At 500 tenants a single pod can become the **generator** bottleneck; the operator fans out so the platform stays the bottleneck. |
| L5 (5,000 tenants, multi-cell) | **Distributed (k6 Operator)** | `task k6:distributed:l5` (16 pods) | Largest test (`maxVUs:1000`). MUST run distributed — a single pod would generator-throttle and silently under-load (the `dropped_iterations` threshold guards this). |

> L3 is the cross-over: the single-pod path is fine for routine L3, the
> distributed path (`k6:distributed:l3`) exists for higher-throughput L3 runs.
> Anything ≥ L4 should use the operator path.

## Running In-Cluster

### Job Template (L1-L3, stress)

```bash
# The run-k6.sh script handles ConfigMap creation, Job lifecycle, and log streaming
task test:load:l1   # or l2, l3, stress, soak, sse, chaos, api
```

### k6 Operator (L3+ distributed)

For L4/L5 level testing requiring multiple pods:

```bash
# Install k6 Operator (one-time)
task k6:operator:install

# Run distributed L3 (4 pods)
task k6:distributed:l3

# Run distributed L4 (8 pods)
task k6:distributed:l4

# Run distributed L5 multi-cell (16 pods, 5,000 tenants, ~4h)
task k6:distributed:l5

# Monitor
kubectl -n loop-runtime-system get testrun -w
```

### Soak Test (Weekly CronJob)

```bash
# Apply CronJob + alerts (one-time)
task k6:soak:apply

# Manual trigger
kubectl -n loop-runtime-system create job k6-soak-manual --from=cronjob/k6-soak-weekly

# Check next schedule
kubectl -n loop-runtime-system get cronjob k6-soak-weekly
```

## Observability

### Prometheus Remote Write

All scripts output metrics to Prometheus via `--out experimental-prometheus-rw`.
Configuration:

```
K6_PROMETHEUS_RW_SERVER_URL=http://prometheus-server.loop-runtime-system.svc.cluster.local/api/v1/write
K6_PROMETHEUS_RW_TREND_STATS=p(50),p(95),p(99)
K6_PROMETHEUS_RW_PUSH_INTERVAL=5s  (30s for soak)
```

### Grafana Dashboard

`load-test.json` (both `infra/helm/monitoring/dashboards/` and `charts/loop-runtime-monitoring/dashboards/`)
shows:
- Real-time: RPS, latency p50/p95/p99, error rate, VU count
- Soak: memory RSS slope, FD count, goroutine count
- Historical: trend per release (Grafana annotations)

### Alerts (Soak)

PrometheusRule `k6-soak-alerts` fires:
- `LoopRuntimeSoakTestFailed` — Job exited non-zero (warning)
- `LoopRuntimeAPIMemoryLeak` — RSS growing 2+ hours during soak (critical)
- `LoopRuntimeAPIFDExhaustion` — FDs growing 2+ hours during soak (critical)

Investigate: check k6 Job logs and API memory/FD metrics in Grafana.

## Chaos + Load

The `chaos-load.js` script is designed to run IN PARALLEL with Chaos Mesh
experiments. Workflow:

```bash
# Terminal 1: Apply chaos experiment
kubectl apply -f infra/tests/chaos/manifests/pg-network-latency.yaml

# Terminal 2: Run chaos load test (measures impact)
task test:load:chaos

# After test: Remove chaos
kubectl delete -f infra/tests/chaos/manifests/pg-network-latency.yaml
```

The script measures 3 phases:
1. **Baseline** (5 min) — normal operation, establishes reference
2. **Chaos** (15 min) — chaos active, measures degradation
3. **Recovery** (5 min) — chaos removed, measures recovery speed

## CI Integration

| Trigger | What Runs | Gate |
|---------|-----------|------|
| PR (api/operators/pkg) | Go harness L1 smoke (8 min max) | Block merge on failure |
| Weekly (Monday 06:00 UTC) | Go harness L0 baseline | Alert on regression |
| Weekly (Sunday 02:00 UTC) | k6 soak 6h | Alert on leak/failure |
| Manual (workflow_dispatch) | Any level L0-L4 | Informational |

## All Task Commands

```bash
task test:load:l1        # L1 smoke (10 tenants, 15 min)
task test:load:l2        # L2 sustained (50 tenants, 35 min)
task test:load:l3        # L3 scale (100 tenants, 65 min)
task test:load:l4        # L4 capacity (500 tenants, 2h)
task test:load:l5        # L5 multi-cell (5000 tenants, 4h)
task test:load:stress    # Break point (ramp to failure, 15 min)
task test:load:soak      # 6h soak (memory/FD leak detection)
task test:load:sse       # SSE streaming (deploy events, 15 min)
task test:load:chaos     # Chaos + load combined (25 min)
task test:load:api       # API latency benchmark (100 RPS, 5 min)
task test:load:all       # Sequential: L1 → L2 → L3 → stress → API

task k6:operator:install    # Install k6 Operator (one-time)
task k6:distributed:l3      # Distributed L3 (4 pods)
task k6:distributed:l4      # Distributed L4 (8 pods)
task k6:soak:apply          # Apply soak CronJob + alerts
```

## File Layout

```
k6/
├── README.md                          ← this file
├── scripts/
│   ├── lib/
│   │   ├── loop-runtime-api.js                ← shared API client
│   │   └── thresholds.js             ← SLO thresholds
│   ├── l1-10-tenants.js              ← L1 smoke
│   ├── l2-50-tenants.js              ← L2 sustained
│   ├── l3-100-tenants.js             ← L3 scale
│   ├── l4-500-tenants.js             ← L4 capacity
│   ├── l5-multicell.js               ← L5 multi-cell
│   ├── stress-break-point.js         ← Break point finder
│   ├── soak-6h.js                    ← 6h soak (leak detection)
│   ├── sse-deploy-stream.js          ← SSE streaming validation
│   ├── chaos-load.js                 ← Chaos + load combined
│   └── api-endpoints.js              ← Pure HTTP benchmark
├── fixtures/
│   └── bundle-builder.js             ← Git bundle fixture loader
├── manifests/
│   ├── rbac.yaml                     ← ServiceAccount + Role
│   ├── soak-cronjob.yaml            ← Weekly soak CronJob
│   ├── soak-alerts.yaml             ← PrometheusRule (leak detection)
│   ├── testrun-l3-distributed.yaml  ← k6 Operator TestRun (4 pods)
│   └── testrun-l4-distributed.yaml  ← k6 Operator TestRun (8 pods)
└── job-template.yaml                 ← Job template for run-k6.sh
```

## References

- [k6 Official](https://k6.io/docs/) — Load testing for engineering teams
- [k6 Operator](https://github.com/grafana/k6-operator) — Distributed testing via CRD
- [Coordinated Omission](https://redhatperf.github.io/post/coordinated-omission/) — Why open model matters
- `SOTA Research` *(removido do versionamento)* — Full competitive analysis
- `Plan` *(removido do versionamento)* — Implementation plan
