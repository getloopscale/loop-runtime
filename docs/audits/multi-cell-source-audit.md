# Multi-cell source audit (T0.1)

> **CHG-2026-04-30-INFRA-NORMALIZE Phase 0 deliverable.** Source-of-truth audit
> reconciling CLAUDE.md cell claims against the actual codebase. Catches
> documented features that don't exist + undocumented features that do.

## Executive summary

The LoopZero platform's CLAUDE.md describes multi-cell architecture as "placement once, route many" with cells stored in `tenants.cell_id` (canonical SoT) and `cells` SQL table for operational metadata. This audit cross-checks every CLAUDE.md claim against the codebase and flags gaps.

**Verdict:** the documented model is largely accurate. 3 gaps identified at audit time (Phase 0); 2 closed in Phase 1+2 (cells migration + admin pin), 1 deferred (cell-3 capacity reservation — see DEBT D10 in `docs/plans/infra-normalize-multicell-plan.md`).

## Reality vs Documented

| Claim (CLAUDE.md) | Reality (code) | Verdict |
|---|---|---|
| `tenants.cell_id` is canonical SoT | `api/internal/services/cell_pinner.go` writes via SERIALIZABLE TX | ✅ confirmed |
| `cells` SQL table holds operational metadata | Migration 000006 + 000007 ship table; 000007 activates cell-2 | ✅ confirmed (post-Phase-2) |
| `DynamicRouter.Resolve(cellID)` is pure hash lookup, zero decision | `api/internal/placement/dynamic_router.go:133` matches | ✅ confirmed |
| `K8sCellStore` looks up by `spec.cellID` not CR name | `K8sCellStore.LoadCells()` polls every 60s by spec.cellID (B9 fix) | ✅ confirmed |
| `RoundRobinEngine` uses bin-packing | `engine.go:179` picks fewest-tenants cell up to maxPerCell | ✅ confirmed |

## Gaps

### Gap 1: cells table existence

The `cells` table did NOT exist before migration 000006. CLAUDE.md described
it as if always present. Migration shipped 2026-04-30 (CHG-2026-04-30-INFRA-NORMALIZE).
Status: CLOSED in Phase 1.

### Gap 2: admin pin endpoint

`loopzero admin tenants pin` was not implemented before this audit. CLAUDE.md
referenced it as the only path to move tenants. Endpoint shipped in
`api/internal/routes/admin_pin_cell.go`. Status: CLOSED in Phase 2.

### Gap 3: cell capacity reservation (cell-3+)

CLAUDE.md mentions `cells` having capacity column but doesn't describe how
new cells are added. Currently manual: provision DOKS cluster + add row.
Operational tooling missing. Status: DEFERRED (DEBT D10, horizon 2026-Q3).

## References

- `api/internal/services/cell_pinner.go` — canonical cell pinning logic
- `api/internal/placement/dynamic_router.go` — routing
- `api/internal/db/migrations/000006_cells.up.sql` — cells table
- `api/internal/db/migrations/000007_activate_cell_2.up.sql` — cell-2 activation
- ADR-0021 D3
- `docs/runbooks/multi-cell-canary.md`
