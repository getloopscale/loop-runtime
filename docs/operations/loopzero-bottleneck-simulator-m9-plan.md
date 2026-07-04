# Plan — LoopZero Bottleneck Simulator M9 (intra-cell blast radius)

> Slug: `loopzero-bottleneck-simulator-m9`. Cycle: DISCOVER → PLAN → IMPLEMENT (TDD) → CODE-QUALITY → REVIEW.
> Complements M8 (mgmt capacity) + the existing AZ/region chaos: models a **single cell failure**
> within a region — the scenario the whole cell architecture exists to contain.

## Goal

Add `chaos(s, {killCell:true})` that quantifies the blast radius of losing ONE cell (one EKS cluster
hosting up to `maxPerCell` tenants). Prove the cell-isolation value proposition numerically: a cell
failure caps per-tenant blast at `cellTenants / total` (~1% at 50k) with **survivors untouched**,
vs a single-region failure at 100%. Expose via MCP (`mode:'cell'`) + a UI chaos button.

## DISCOVER — grounding (real LoopZero cell semantics)

| Fact | Source | Consequence |
|---|---|---|
| Placement is sticky, "once route many" | `CLAUDE.md § Multi-Cell Routing`; `placement/engine.go:91` maxPerCell=500 | a cell holds ≤ maxPerCell tenants; balanced by round-robin bin-packing |
| No automatic rebalancing | ADR-0008 / ADR-0021 | a dead cell's tenants stay down until operator re-pin OR cell recovery — survivors get **no failover load** |
| Cells are independent EKS clusters | `CLAUDE.md § Four Planes` (workload plane = multiple clusters) | killing one does NOT reduce surviving cells' capacity (unlike an AZ loss, which drops zone-distributed capacity within a cell) |

**Key insight — this is the OPPOSITE of active-active region failover.** killAZ drops capacity in
survivors (survivorFactor<1); active-active killRegion shifts load onto survivors (ρ×=r/(r-1)); but
**killCell leaves survivors at base ρ** (survivorFactor=1) — sticky isolation means the blast is
*contained*, not *redistributed*. That containment is precisely why LoopZero shards into cells.

## Design

### Engine (`loopzero-capsim.mjs`)
`chaos(s, {killCell:true})` branch:
- `cells = ceilDiv(tenants, maxPerCell)`; `cellTenants = tenants>0 ? min(maxPerCell, ceil(tenants/cells)) : 0` (realistic balanced occupancy, capped at maxPerCell).
- `capLossPct = tenants>0 ? cellTenants / tenants : 0` (per-tenant blast radius of one cell down).
- `survivorFactor = 1, cellFactor = 1` — **isolation**: surviving cells untouched (no ρ increase).
- `sla` blended per-tenant (same pattern as sharded region): `(1 - capLossPct)*full + capLossPct*drFloor`.
- Return an extra `cellTenants` field (0 for non-cell modes) so the UI/MCP can show how many tenants the dead cell held.

### MCP (`loopzero-capsim-mcp/server.mjs`)
- `mode` enum gains `'cell'`; handler maps `'cell' → {killCell:true}`; description updated. Integration test: cell blast << region blast + survivors' bff ρ unchanged.

### Simulator UI (`loopzero-bottleneck-simulator.html`)
- `btnCell` "Derrubar 1 célula"; `chaosMode='cell'` branch; callout "CÉLULA down (X% · N tenants)"; wire setChaos/reset. Add `chaos(killCell)` to the UI-contract test.

### Calculator
- No change (chaos is not modeled in the calculator).

## Coverage Matrix

| Goal claim | Task | Test |
|---|---|---|
| killCell blast = cellTenants/total | T1 chaos branch | M9-blast (1% at 50k) |
| Survivors untouched (isolation) | T1 survivorFactor=1 | M9-isolation (bff ρ == base, unlike killAZ) |
| SLA blended per-tenant | T1 sla | M9-sla |
| Cell isolation << region blast (the value prop) | T1 | M9-vs-region (1% ≪ 100%) |
| cellTenants exposed + correct | T1 return field | M9-celltenants (500, capped at maxPerCell) |
| MCP mode:'cell' passthrough | T2 server test | M9-mcp |
| UI button + callout | T3 html | LIVE browser test |
| Boundary tenants=0 | T1 | M9-boundary (no NaN) |

## Drawbacks & Risks

1. **cellTenants uses balanced occupancy** (tenants/cells), not the true fullest cell. Round-robin keeps cells balanced (≤ maxPerCell), so the worst-case (a cell at exactly maxPerCell) equals the average when tenants is a multiple of maxPerCell — the honest common case. Mitigation: capped at maxPerCell; documented.
2. **drFloor reused as the cell-outage floor.** A cell outage's recovery (operator re-pin) differs from a region DR, but both are "affected fraction sees the outage floor until recovery." Mitigation: same labelled POLICY (`regionalOutageAvail`); documented as a shared floor.
3. **Chaos snapshot, not duration.** capLossPct is the instantaneous blast, not an annualized availability. Mitigation: consistent with the existing killAZ/killRegion semantics.

## Unresolved Questions

(none — cell semantics are grounded in the ADRs + engine.go; the floor is an explicitly labelled POLICY reused from the region model.)

## Concurrency / Failure scenarios

(none — pure deterministic engine, no I/O, no concurrency. This milestone *models* a failure scenario; it does not introduce one.)
