# 03 — Cell-Scoped Queries

**Phase:** A (Foundation)
**Complexity:** 3/5
**Dependencies:** None (parallel with 01, 02)
**Unblocks:** Multi-cell without cross-cell data contamination
**Cost delta:** $0
**Governance:** Pre-approved (MEET-046). No T2/T3 gate. Implement immediately via TDD. Validate on DigitalOcean.
**Status (2026-04-10):** Code + unit tests complete. Migration 000029 applied
locally. Pending: integration test with real PG (`TestFindPendingReleases_ExplainShowsIndexScan`)
and DigitalOcean migration run.

---

## Problem

Every reconciliation loop currently scans the **entire** builds/releases table with no cell_id filter. With 20 cells and 5000 tenants per cell, `FindPendingReleases` returns up to 100 rows from ALL cells. The controller cannot distinguish which releases belong to which cell's ArgoCD. This is the most dangerous global coupling — it means a single controller must understand all cells, defeating cell isolation.

**Global queries identified:**

| Query | File | Line |
|---|---|---|
| `FindActiveBuilds` | `api/internal/db/build_store.go` | ~405 |
| `FindPendingReleases` | `api/internal/db/release_store.go` | 339 |
| `FindActiveReleases` | `api/internal/db/release_store.go` | 378 |
| `FindStaleBuilds` | `api/internal/services/build_reaper.go` | ~240 |
| `FindStaleReleases` | `api/internal/services/release_reaper.go` | ~226 |
| `FindPendingForAutoVerify` | `api/internal/db/domain_store.go` | 262 |

## Scope

1. **Add `cellID` parameter to all `Find*` store methods** listed above
2. **SQL-level filtering** via `JOIN tenants t ON b.tenant_id = t.id WHERE t.cell_id = $N` — not fetch-all-then-filter in Go
3. **Denormalize `cell_id` onto `builds` and `releases` tables** — forward-only migration adding a `cell_id TEXT NOT NULL DEFAULT 'cell-default'` column. This avoids JOINs at scale and prepares for PG table partitioning (tracked as escalation risk in index.md). Populate from `tenants.cell_id` via `UPDATE builds SET cell_id = (SELECT cell_id FROM tenants WHERE tenants.id = builds.tenant_id)`.
4. **Controller config:** `--cell-id=cell-default` or `--all-cells` (Phase 0 backward compat)
5. **Backward compatibility:** When `cellID = "cell-default"`, existing L1 tests pass identically

## Acceptance Criteria

1. **Every global scan query includes cell_id filter.** All functions above accept and use `cellID` parameter.
2. **SQL-level filtering, not application-level.** Query includes `WHERE t.cell_id = $N`, not Go post-filter.
3. **Single-cell unchanged.** `cellID = "cell-default"` returns same results as before.
4. **Cross-cell isolation proven.** Test: Insert 5 builds for cell-a, 5 for cell-b. `FindActiveBuilds(ctx, "cell-a")` returns exactly 5.
5. **No unbounded queries remain.** `grep -rn "FROM builds\|FROM releases" api/internal/db/` — zero queries without tenant_id or cell_id filter.

## Required Tests

All tests below must be **implemented as part of this item**. None exist today.

| Test | Type | Exists? | What It Proves |
|---|---|---|---|
| `TestFindActiveBuilds_CellScoped_PassesCellIDToWhere` | Unit | ✅ Implemented (2026-04-10) | Only returns builds for target cell |
| `TestFindActiveBuilds_EmptyCellIDDefaultsToCellDefault` | Unit | ✅ Implemented (2026-04-10) | Empty cellID → cell-default |
| `TestFindPendingReleases_CellScoped_PassesCellIDToWhere` | Unit | ✅ Implemented (2026-04-10) | Only returns releases for target cell |
| `TestFindActiveReleases_CellScoped_PassesCellIDToWhere` | Unit | ✅ Implemented (2026-04-10) | Only returns active releases for target cell |
| `TestFindPendingForAutoVerify_CellScoped_JoinsTenants` | Unit | ✅ Implemented (2026-04-10) | Domain store JOINs tenants for cell filter |
| `TestPostgresStaleBuildChecker_FindStaleBuilds_ScopedByCellID` | Unit | ✅ Implemented (2026-04-10) | Reaper only reaps builds in its cell |
| `TestPostgresStaleReleaseChecker_FindStaleReleases_ScopedByCellID` | Unit | ✅ Implemented (2026-04-10) | Release reaper only reaps releases in its cell |
| `TestFindActiveBuilds_CrossCell_NoContamination` | Unit | ✅ Implemented (2026-04-10) | Cell-A query returns zero cell-B rows |
| `TestCellDefault_BackwardCompatible` | Unit | ✅ Implemented (2026-04-10) | cell-default returns all existing data |
| `TestFindPendingReleases_ExplainShowsIndexScan` | Integration | ⏸ Pending — requires real PG with LOOPZERO_TEST_DB_URL | Query plan uses idx_releases_cell_status_created |

## SLO Targets

| SLI | Target |
|---|---|
| Query latency (FindActiveBuilds with cell_id index) | < 5ms at 1000 builds |
| Cross-cell leakage incidents | 0 |

## Rollback Plan

All changes are Go query strings and function signatures. Revert the branch. `cell-default` backward compat means no data migration. MTTR: 3 minutes.

## DoD

```bash
# 1. Cross-cell isolation test (the definitive proof — not grep)
cd api && go test ./internal/db/ -run TestCrossCell_NoContamination -v -count=1
# PASS — inserts builds in cell-a and cell-b, queries cell-a, asserts zero cell-b results

# 2. All cell-scoped unit tests pass
cd api && go test ./internal/db/ -run TestFind.*CellScoped -v
# All PASS

# 3. SQL-level verification: no full-table scans without cell filter
# Run EXPLAIN ANALYZE on FindPendingReleases with cell_id parameter:
cd api && go test ./internal/db/ -run TestFindPendingReleases_ExplainShowsIndexScan -v
# PASS — verifies query plan uses idx_tenants_cell_id or partition pruning

# 4. Backward compatibility
LOOPZERO_LOAD_TEST=1 go test ./infra/tests/load/ -v -run TestL1_10Tenants -timeout 30m
# 10/10 success (cell-default path)

# 5. Interface compliance: every Find* in the store interface accepts cellID
cd api && go vet ./internal/db/...
# No compilation errors (proves all call sites updated)
```
