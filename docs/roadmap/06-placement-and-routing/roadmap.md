# 06 — Placement and Routing

**Phase:** B (Cell-Ready)
**Complexity:** 3/5
**Dependencies:** 03 (cell-scoped queries), 05 (cell provisioning — second cell must exist)
**Unblocks:** L4-L5 (multi-cell operation)
**Cost delta:** $0 (code changes only)
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately via TDD. Validate on DigitalOcean.

---

## Problem

The `RoundRobinEngine` (actually a least-loaded bin-packing engine) and `StaticRouter` are **implemented but not wired**. The API still uses `PassthroughRouter` (returns cell-default for everything) and `DefaultEngine` (assigns cell-default to every tenant). Even if a second cell exists, no tenant will be placed there and no operation will route there.

**Note on naming:** The code calls it `RoundRobinEngine` but the algorithm is least-loaded with capacity cap (bin-packing). See `api/internal/placement/engine.go:77-143`.

Evidence:
- `api/internal/placement/engine.go:77-143` — RoundRobinEngine (least-loaded) exists
- `api/internal/placement/router.go:60-106` — StaticRouter exists
- Both are unused in production

## Scope

1. **Wire `RoundRobinEngine`** in tenant creation path (API + TenantController)
2. **Wire `StaticRouter`** in API startup (populated from ClusterCredential CRDs or Pulumi outputs)
3. **Build orchestrator** routes to correct cell's ArgoCD (via CellClientManager)
4. **GitOps pusher** tags repos with cell context
5. **Fix TOCTOU race** in `AssignCell` — use `SELECT ... FOR UPDATE` or serialized transaction
6. **Integration test:** Create tenant → verify cell_id ≠ "cell-default" when 2nd cell exists

## Acceptance Criteria

1. **New tenants placed in least-loaded cell.** With 2 cells, tenants distribute evenly.
2. **Deploy routes to correct cell.** Tenant in cell-sfo3-02 → build submits to cell-sfo3-02's ArgoCD.
3. **TOCTOU eliminated.** 10 concurrent tenant creations → even distribution, no hot-spotting.
4. **cell-default backward compat.** Existing tenants remain in cell-default. No migration.
5. **PlacementEngine fully exercised.** `RoundRobinEngine` in use, `DefaultEngine` only when single cell.

## Required Tests

All tests below must be **implemented as part of this item**.

| Test | Type | Exists? | What It Proves |
|---|---|---|---|
| `TestPlacement_RoundRobin_EvenDistribution` | Unit | **Yes — exists** in `placement_test.go` | 10 tenants across 2 cells → ~5 each |
| `TestPlacement_ConcurrentCreation_NoTOCTOU` | Integration | **No — implement** | 10 simultaneous creates → no hot-spotting |
| `TestRouting_DeployToCorrectCell` | Integration | **No — implement** | Tenant in cell-B deploys to cell-B ArgoCD |
| `TestRouting_CellDefault_BackwardCompat` | Unit | **No — implement** | Existing tenants still work |
| `TestStaticRouter_RejectsUnknownCell` | Unit | **Yes — exists** in `placement_test.go` (as `TestStaticRouter_UnknownCell`) | Unknown cell_id → error |

## SLO Targets

| SLI | Target |
|---|---|
| Placement fairness (2 cells, 100 tenants) | 40-60% split |
| Deploy to correct cell | 100% |
| Placement latency | < 10ms |

## Rollback Plan

Feature flag: `LOOPZERO_PLACEMENT_ENGINE=default` reverts to DefaultEngine. All tenants go to cell-default. MTTR: 5 minutes.

## DoD

```bash
# 1. RoundRobinEngine wired in startup (search for the actual symbol name)
grep -n "NewRoundRobinEngine" api/cmd/main.go api/cmd/controller/main.go
# Must show wiring in startup code

# 2. StaticRouter wired in startup
grep -n "NewStaticRouter" api/cmd/main.go
# Must show wiring in startup code

# 3. All placement tests pass (unit + integration)
cd api && go test ./internal/placement/ -v -count=1
# All PASS

# 4. TOCTOU test passes
cd api && go test ./internal/placement/ -v -run TestPlacement_ConcurrentCreation -count=1
# PASS

# 5. Full deploy to non-default cell
LOOPZERO_LOAD_TEST=1 LOOPZERO_CELL=cell-sfo3-02 go test ./infra/tests/load/ -v -run TestL0_Baseline -timeout 10m
# SUCCEEDED
```
