# 04 — Single-Cell Hardening

**Phase:** B (Cell-Ready)
**Complexity:** 4/5
**Dependencies:** 01 (defects fixed), 02 (controller extracted), 03 (queries scoped), **07 (co-dependent — admission enforces the budget this item defines)**
**Unblocks:** L2, L3
**Cost delta:** +$450/mo for L2-L3 infra
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately via TDD. Validate on DigitalOcean. Infra cost authorized.

---

## Problem

Today nobody can answer: "What is the capacity of one cell?" Without a known, tested limit per cell, multi-cell is meaningless — you don't know when to add a cell, and you don't know if a cell is saturated until it breaks.

Current limits are accidental, not designed:
- PG connections: 25/pod, 50 total, DO max 97
- Build MaxConcurrent=10
- ArgoCD: ~200 apps before memory issues (8GB RAM)
- Runtime: 2 small nodes, ~40 pods
- Mgmt cluster: already 80% memory

## Scope

1. **Publish cell capacity budget** (`docs/capacity/cell-budget.md`):
   - Max tenants per cell
   - Max concurrent builds per cell
   - Max ArgoCD applications per cell
   - Max PG connections per cell
   - Max pods on runtime cluster per cell

2. **ArgoCD tuning:** Increase status-processors, operation-processors, repo-server parallelism. Measure sync time p99 at 100 apps.

3. **PG pool tuning:** Size `DB_MAX_CONNS` to cell capacity. Monitor `pg_stat_activity` under L2 load.

4. **Argo Workflows tuning:** Configure `workflowWorkers`, `podWorkers`, parallelism per namespace.

5. **Saturation dashboards:** Grafana "Cell Saturation" with: PG pool %, build queue depth, ArgoCD sync queue, runtime pod count. Yellow (70%) and Red (90%) thresholds.

6. **Graceful degradation proof:** At 110% of budget, system slows but does not crash or corrupt.

## Acceptance Criteria

1. **Cell budget published** with concrete numbers and evidence from load test.
2. **ArgoCD stable at 100 apps.** Controller memory < 80% limit. Sync p99 < 120s.
3. **PG pool < 80% at L2.** 50 tenants sustained, `pg_stat_activity` active connections < 80% of max.
4. **Build queue bounded.** 20 concurrent builds, no workflow Pending > 60s due to controller capacity.
5. **Saturation dashboard operational** in Grafana with yellow/red thresholds.

## Required Tests

| Test | Type | Exists? | What It Proves |
|---|---|---|---|
| `TestL2_50Tenants` | Load | **Partial — exists in cell_scale_test.go, needs enhancement** | Cell handles 50 tenants sustained |
| `TestArgoCD_100Apps_SyncTime` | Load | **No — implement** | Sync p99 < 120s at 100 apps |
| `TestPG_PoolSaturation_50Concurrent` | Load | **No — implement** | PG pool < 80% at L2 |
| `TestBuildQueue_20Concurrent` | Load | **No — implement** | No workflow pending > 60s |
| `TestCellBudget_GracefulDegradation` | Load | **No — implement** (requires item 07 admission control) | At 110% budget: slower, not crashed |

## SLO Targets

| SLI | Target | Yellow | Red |
|---|---|---|---|
| PG pool saturation | < 70% steady | 70% | 90% |
| Build queue wait time | < 60s p99 | 60s | 120s |
| ArgoCD sync time | < 120s p99 | 120s | 300s |
| API p99 (within cell) | < 500ms | 500ms | 1s |

## Cost

| Level | Delta from current ($350/mo) | Total |
|---|---|---|
| L2 (50 tenants) | +$150 (build nodes) | ~$500 |
| L3 (100 tenants) | +$450 (mgmt+runtime+ArgoCD memory) | ~$800 |

## Rollback Plan

All tuning is in Helm values.yaml or Pulumi config. Revert: `helm rollback` or `pulumi config set`. MTTR: 10 minutes per component.

## DoD

```bash
# 1. Cell budget document exists
test -f docs/capacity/cell-budget.md && echo "EXISTS"

# 2. Grafana dashboard exists
curl -s "http://grafana.dev.loopzero.example/api/dashboards/uid/cell-saturation" | jq '.dashboard.title'

# 3. L2 passes
LOOPZERO_LOAD_TEST=1 go test ./infra/tests/load/ -v -run TestL2_50Tenants -timeout 60m
# Success rate 100%, p99 < 5min

# 4. PG pool under control during load
psql $DB_URL -c "SELECT count(*) as active FROM pg_stat_activity WHERE state='active';"
# < 80% of max_connections
```
