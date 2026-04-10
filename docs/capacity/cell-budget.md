# Cell Capacity Budget

**Status:** v1 — based on measured values from the dev stack (2026-04-10)
**Roadmap:** Item 04 Single-Cell Hardening
**ADR:** 0008 (cell-based architecture)

> One cell must have a **known, tested limit**. Multi-cell is pointless
> without that number — you do not know when to add a cell and you do not
> know if a cell is saturated until it breaks.
>
> This document is the authoritative capacity budget for one LoopZero cell. It
> is the contract Item 07 (Backpressure & Admission) enforces. Every
> threshold here becomes a yellow/red line on the Grafana `cell-saturation`
> dashboard and a limit in the admission controller.

## Executive summary

| Budget | L2 (current dev) | L3 (target) | L4 (stretch) |
|---|---|---|---|
| Tenants per cell | 50 | 100 | 500 |
| Concurrent builds | 20 | 30 | 50 |
| ArgoCD applications | 50 | 100 | 500 |
| PG active connections | < 80% of 50 (= < 40) | < 80% of 100 (= < 80) | < 80% of 200 (= < 160) |
| Runtime pods per cell | < 40 | < 100 | < 500 |
| Gitea repos (source + gitops) | 100 | 200 | 1,000 |

**Operating rule.** When *any* of these metrics crosses its yellow line for
more than 5 minutes, page the oncall. When any crosses red, admission
control MUST start rejecting new work (roadmap Item 07).

## How this document was built (honesty)

The numbers below are **not aspirational**. They come from three sources,
labeled accordingly:

- `MEASURED` — observed on the live `loopzero-infra-dev-sfo3-*` stack on
  2026-04-10 during roadmap iteration 7, with `kubectl top`, `doctl`, and
  `psql pg_stat_activity`. Reproducible any time by re-running the
  commands in the Appendix.
- `CAPPED` — values set in code / Helm / Pulumi. These are the current
  config and may or may not match the underlying headroom. Item 07 reads
  these to enforce rejection.
- `DERIVED` — arithmetic from MEASURED + CAPPED. Always shown with the
  formula so it can be re-checked.
- `PROJECTED` — extrapolated to L2/L3/L4 from DERIVED. These are the
  values Item 08 will validate under real load; until then treat them as
  planning input, not guarantees.

## Physical cell shape (MEASURED, 2026-04-10)

The dev stack used to derive everything below:

| Cluster | Node pool | Nodes | Per-node | Total vCPU / RAM |
|---|---|---|---|---|
| mgmt | `s-2vcpu-4gb` | 3 | 2 vCPU / 4 GiB | 6 vCPU / 12 GiB |
| build | `s-4vcpu-8gb-amd` | 2 | 4 vCPU / 8 GiB | 8 vCPU / 16 GiB |
| runtime | `s-2vcpu-4gb` | 2 | 2 vCPU / 4 GiB | 4 vCPU / 8 GiB |

Managed Postgres: `db-s-1vcpu-2gb` single node, `max_connections = 50`
(MEASURED via `SHOW max_connections`).

**Idle resource usage MEASURED** (no load, only platform pods + one
loopzero-controller + two loopzero-api replicas):

| Cluster | Node CPU | Node memory | Headroom to 80% |
|---|---|---|---|
| mgmt | 5–9 % | 56–74 % | 6 % – 24 % (tight) |
| build | 2 % | 18–30 % | 50 % + |
| runtime | 3–4 % | 31–40 % | 40 % + |

**Observation.** Even with zero tenants, the mgmt cluster is already at
74 % memory on one of three nodes. ArgoCD + Prometheus + Gitea + Kratos
+ Hydra dominate. Scaling to 100 apps without giving the mgmt node pool
more memory will push the node into eviction. This matches the L3 cost
budget in `docs/roadmap/04-single-cell-hardening/roadmap.md` which
calls for `+$450/mo for mgmt+runtime+ArgoCD memory`.

## Budget by subsystem

### PG connections (hardest constraint today)

| Layer | CAPPED | MEASURED idle | Notes |
|---|---|---|---|
| `max_connections` (DO managed PG) | 50 | 36 in use | 72 % at idle |
| `DB_MAX_CONNS` per Go process | 25 | n/a | default in `config.go:173` |
| `DB_MIN_CONNS` per Go process | 5 | n/a | default in `config.go:174` |
| Processes using the pool | 3 (2 api + 1 controller) | | |
| LoopZeroretical pool ceiling | **75** | | 2×25 + 1×25 |
| Observed ceiling today | **~50** | 36 | bounded by PG `max_connections` |

**Finding.** The ceiling (75) exceeds the server limit (50). Under burst
load the API will see `too many connections` errors before its own pool
thinks it is full. The admission controller in Item 07 MUST:

1. Trip at `pg_stat_activity.active >= 40` (80 % of 50).
2. Prefer to reject `POST /api/v1/builds` since it is the bulk work.
3. Surface `LOOPZERO_DB_POOL_NEAR_EXHAUSTION` as a Prometheus alert.

**Tuning for L2 (50 tenants).** Either:

- Bump DO PG tier to `db-s-1vcpu-4gb` (`max_connections = 100`) and keep
  `DB_MAX_CONNS = 25`, OR
- Drop `DB_MAX_CONNS` to 12 per process so `3 × 12 = 36 < 40` safe line.

The first option is cleaner and already planned by the L3 cost budget.

### Build concurrency

| Layer | CAPPED | Where |
|---|---|---|
| `MaxConcurrent` in BuildReconcileLoop | 10 | `services/build_reconcile_loop.go` default |
| `MaxBuildsPerTenant` | 3 | env `MAX_BUILDS_PER_TENANT` |
| Argo Workflows `workflowWorkers` | 32 (chart default) | |
| Argo Workflows `podWorkers` | 32 (chart default) | |
| BuildKit concurrent solves per pod | ~4 | rootless buildkit default |
| Build nodes | 2 × `s-4vcpu-8gb-amd` = 8 vCPU | |

**DERIVED build parallelism ceiling.** 2 nodes × ~4 solves/node ≈ 8
concurrent image builds before BuildKit serializes at the node level.
With `MaxConcurrent=10` the reconcile loop can dispatch 10 workflows but
only 8 will actually make forward progress on the image step. The
extras queue in `build-apps` → `build-image-api` via the existing PVC
multi-attach self-resolution (Correction I).

**L2 target.** 20 concurrent builds without `Pending > 60s` requires:

- Bump build pool to 3 × `s-4vcpu-8gb-amd` (12 vCPU, ≈ 12 concurrent
  solves). +$72/mo.
- Keep `MaxConcurrent=10` (per-cell loop dispatch). Admission in Item 07
  returns 429 when queued builds exceed this × 2.
- Fix Correction I upstream in `loopzero-forge` (nodeAffinity on workflow
  steps) so the PVC multi-attach self-resolution stops being on the
  critical path.

### ArgoCD

| Layer | CAPPED | Where |
|---|---|---|
| `application-controller` memory | 256 Mi request / 512 Mi limit | `kubectl describe` |
| `status-processors` | 20 (chart default) | |
| `operation-processors` | 10 (chart default) | |
| `repo-server` replicas | 1 | |
| MEASURED idle memory | 127 Mi / 512 Mi limit (25 %) | |

**DERIVED.** ArgoCD's application-controller memory scales roughly
linearly with Application + managed-resources count. At ~500 kB of
allocated memory per Application (community observation), the 512 Mi
limit supports ~1,000 Applications but GC will thrash well before that.
A conservative budget: **128 apps per cell before bumping memory to 2 Gi**.

**Tuning for L3 (100 apps).**

- Bump limit to 1 Gi — comfortable headroom at 100 apps.
- Increase `statusProcessors` to 40 and `operationProcessors` to 20.
- Increase `repoServerReplicas` to 2 for HA during rolling restarts.

### Runtime pods

| Layer | CAPPED | MEASURED | Notes |
|---|---|---|---|
| Runtime nodes | 2 × `s-2vcpu-4gb` | | |
| Max pods per node (DOKS default) | 110 | | kubelet `max-pods` |
| Platform pods on runtime | ~10 | 10 | traefik, cert-manager, cloudflared × 2, ESO × 3 |
| MEASURED node memory | 31 % – 40 % | | |
| Tenant pod average cost | ~50 Mi, ~10m CPU | | loadtest fixture |

**DERIVED tenant capacity (runtime).** At 50 Mi/pod, 4 GiB node has
~8 GiB − 1.5 GiB system = 6.5 GiB usable ≈ **130 tenant pods per node**.
Two nodes ≈ **260 tenant pods per cell** before memory pressure.
Practical L2 budget: **80 pods** (40 % headroom for spikes) which
corresponds to ~50 tenants × 2 pods.

**Tuning for L3 (100 tenants, ~200 pods).**

- Runtime pool: bump to 4 × `s-2vcpu-4gb` or 2 × `s-4vcpu-8gb`. +$120/mo.
- NetworkPolicy + PodDisruptionBudget as Helm chart (already present).

### Gitea

| Layer | CAPPED | MEASURED idle | Notes |
|---|---|---|---|
| Gitea memory limit | 512 Mi | 105 Mi (20 %) | |
| Repos per tenant | 2 (source + gitops) | | roadmap § index.md |
| MEASURED repo count today | ~8 | | 4 tenants × 2 |

**Projected ceiling (from roadmap index.md escalation risks):**
Gitea disk I/O becomes the bottleneck around ~500 tenants = 1,000 repos.
This is an **escalation-risk item**, not part of this budget — tracked
in `docs/roadmap/index.md` "Gitea Bottleneck" section. Pre-L4
deliverable: add `loopzero_gitea_push_duration_seconds` histogram before
L4 runs.

### loopzero-api + loopzero-controller process memory

| Process | CAPPED request | CAPPED limit | MEASURED idle | Headroom |
|---|---|---|---|---|
| loopzero-api (×2 replicas) | 512 Mi | 1 Gi | 15–16 Mi each | 98 % |
| loopzero-controller (×1) | 128 Mi | 512 Mi | 11 Mi | 98 % |

**Finding.** The Go binaries are overwhelmingly memory-efficient at idle.
The limits are correct — under L2 we expect loopzero-api to climb to
~200–300 Mi per replica during a burst of concurrent builds (Go runtime
GC pressure). No tuning needed until L3.

## Yellow / Red thresholds (cell-saturation dashboard)

These feed the Grafana `cell-saturation` dashboard and the PrometheusRule.

| SLI | PromQL | Yellow | Red | Source |
|---|---|---|---|---|
| PG active connections | `sum(loopzero_db_pool_acquired_conns)` | 30 (60 %) | 40 (80 %) | `metrics.PoolStatsCollector` |
| Build queue depth | `loopzero_build_queue_depth` | 10 | 20 | `metrics.BuildQueueDepth` |
| Active builds | `loopzero_builds_active` | 8 | 10 | `metrics.BuildsActive` |
| ArgoCD sync p99 | `histogram_quantile(0.99, sum by (le) (rate(argocd_app_sync_total_bucket[5m])))` | 120 s | 300 s | ArgoCD metrics |
| Runtime node memory | `max(node_memory_MemUsed_bytes / node_memory_MemTotal_bytes)` | 0.70 | 0.85 | node-exporter |
| Gitea push p99 | `histogram_quantile(0.99, sum by (le) (rate(loopzero_gitops_operation_duration_seconds_bucket{op="push"}[5m])))` | 1 s | 2 s | `metrics.GitOpsOperationDuration` |
| API p99 in-cell | `histogram_quantile(0.99, sum by (le) (rate(loopzero_http_request_duration_seconds_bucket[5m])))` | 500 ms | 1 s | `metrics.HTTPRequestDuration` |

## Assumptions that MUST be re-validated under L2

Every row below is either PROJECTED or MEASURED in a near-idle state.
None has been validated with 50 concurrent tenants sustained for ≥ 30
minutes. Item 08 (progressive load validation) is the validation gate.

1. **Go runtime memory under load.** LoopZero-api at idle is 15 Mi. The
   projection of 200–300 Mi under burst is an educated guess based on
   typical pgx pool + buffered JSON encoding working set. Must measure.
2. **BuildKit concurrent-solve ceiling.** The ~4 solves/node figure is a
   community observation, not a LoopZero-measured value. Must pin by
   launching N parallel builds and graphing actual CPU / disk I/O.
3. **ArgoCD memory vs Application count.** The ~500 kB/app figure is
   from community discussions, not LoopZero profiling. Must measure by
   creating 100 synthetic Applications and watching `go_memstats_*`.
4. **Gitea disk I/O at 100 repos.** The roadmap escalation says this is
   fine; LoopZero has not measured push latency under sustained load.
5. **Runtime pod memory.** The 50 Mi/pod average is from one fixture
   (`loadtest-app` in `infra/tests/load/fixtures`). Real tenant apps
   (Django, Rails, Next.js) will be bigger. Must measure against the
   top-3 framework templates.

## Rollout sequence

1. **Ship this doc** (iteration 7).
2. **Ship the `cell-saturation` Grafana dashboard JSON** (same iteration).
3. **Run L2 10-tenant smoke** with the new dashboard live — take
   screenshots, measure actual yellow/red positions.
4. **Tune per section above** based on the measurements.
5. **Re-run L2 at 50 tenants** — Item 04 DoD #1.
6. **Item 07 admission** reads these thresholds and starts rejecting.

## Appendix — how to reproduce the measurements

```bash
# mgmt cluster
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt top nodes
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-mgmt -n platform-system top pods

# build + runtime clusters
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-build top nodes
kubectl --context=do-sfo3-loopzero-infra-dev-sfo3-runtime top nodes

# Node pool sizes
doctl kubernetes cluster node-pool list <cluster-id> --format Name,Size,Count

# PG max_connections and live activity (run from inside mgmt cluster)
kubectl -n platform-system run pgstat --rm -i --restart=Never \
  --image=postgres:16-alpine -- sh -c \
  'psql "$DATABASE_URL" -c "SHOW max_connections" \
                        -c "SELECT count(*), state FROM pg_stat_activity GROUP BY state"'
# (DATABASE_URL must be injected via envFrom secretRef: loopzero-api-secrets)
```

## References

- [ADR-0008: Cell-Based Architecture](../meetings/adr/0008-cell-based-architecture.md)
- [Roadmap Item 04](../roadmap/04-single-cell-hardening/roadmap.md)
- [Roadmap Item 07](../roadmap/07-backpressure-and-admission/roadmap.md)
- [api/internal/config/config.go](../../api/internal/config/config.go) (defaults)
- [infra/pulumi/src/components/mgmt-bootstrap.ts](../../infra/pulumi/src/components/mgmt-bootstrap.ts) (Pulumi resources)
