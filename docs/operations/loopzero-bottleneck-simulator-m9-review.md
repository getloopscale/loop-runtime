---
type: operational-runbook
title: LoopZero Bottleneck Simulator M9 (intra-cell blast radius)
---

# Review — LoopZero Bottleneck Simulator M9 (intra-cell blast radius)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m9`.
> **VERDICT: READY_TO_MERGE** — independent adversarial review returned 0 BLOCKER / 0 HIGH / 0 MEDIUM;
> 2 LOW (test/UX hygiene) closed with evidence.

## Origin

Complements M8 (mgmt capacity) + the existing AZ/region chaos. Models the scenario the whole cell
architecture exists to contain: **one cell (EKS cluster) fails within a region.**

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| killCell blast = cellTenants/total | `chaos(scn(),{killCell:true}).capLossPct === 0.01` (500/50000 at 50k); reviewer reproduced by hand | ✅ |
| Survivors untouched (isolation) | every component ρ under killCell == no-chaos base ρ (reviewer verified all 9); killAZ changes 8/9 — isolation genuinely distinct | ✅ |
| SLA blended per-tenant | `sla=(1-0.01)*99.985 + 0.01*95 = 99.9352` reproduced independently | ✅ |
| Cell isolation << region blast (value prop) | killCell 0.01 ≪ killRegion(single) 1.0 | ✅ |
| cellTenants exposed + capped | 500 at 50k; capped at maxPerCell (100k→500); tenants=300→300 (1-cell=100% blast) | ✅ |
| MCP mode:'cell' passthrough | real stdio boundary: `mode='cell'`→0.01/500, `mode='az'`→0.333 — distinct routing, no NaN | ✅ |
| UI button + callout | `btnCell` "Derrubar 1 célula"; LIVE callout "CÉLULA down (500 tenants · survivors intactos) · 1% · SLA 99.9352%"; console clean | ✅ |
| Boundary tenants=0 | capLoss 0, cellTenants 0, no NaN | ✅ |
| No regression | engine **62/62** (+7 M9), MCP **11/11** (+1 M9), all pre-M9 green | ✅ |

## What M9 delivers

`chaos(s,{killCell:true})` quantifies the cell-isolation value proposition. Killing one cell loses
`cellTenants/total` (~1% at 50k) with **survivors provably untouched** (survivorFactor=1 — sticky
placement means no failover load shift), vs a single-region failure at 100%. The model is
**conservative** (the reviewer confirmed it does NOT overstate the benefit): survivor ρ is computed
against the full tenant count, so it reflects the dead cell's tenants *still* polling — reality
(dead tenants poll less) would leave survivors even lighter. Exposed via MCP (`mode:'cell'`) + a UI
chaos button. This closes the chaos-mode trilogy: **AZ** (capacity drop in survivors) · **cell**
(contained blast, survivors intact) · **region** (single/sharded/active-active).

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| LOW | test M9-6 had a tautological assertion `cost(scn()).total === cost(scn()).total` | **FIXED** → asserts `cost(scn()).total === 72565.5` (real anchor check); the cells/appNodes assertions already carried the intent |
| LOW | cell callout could be over-read as "1% lost, all else perfect" | **FIXED** → callout now reads "CÉLULA down (N tenants · survivors intactos)" making the isolation explicit |

## Reviewer cross-check (quoted)

> `chaos(scn(),{killCell:true})` returned `cells=100, cellTenants=500, capLossPct=0.01, sla=99.9352`
> — recomputed as `(1-0.01)*99.985 + 0.01*95`. The isolation invariant held across all 9 components
> (every ρ under killCell equalled the no-chaos base; killAZ changed 8 of 9) — isolation genuinely
> distinct, not a no-op. Value-prop confirmed killCell 0.01 ≪ killRegion 1.0. Edges: 300→300/1.0,
> 100000→500(capped), 0→0/0/no-NaN. MCP `mode='cell'`→0.01/500 vs `mode='az'`→0.333, distinct routing,
> no NaN. 62/62 + 11/11 exit 0. VERDICT: READY_TO_MERGE.

## Deliverables (M9)

- `loopzero-capsim.mjs` — `chaos()` `killCell` branch (isolation: survivorFactor=1; blended SLA) + `cellTenants` return field.
- `loopzero-capsim.test.mjs` — **62 tests** (+7 M9: blast, isolation, sla, value-prop, cellTenants cap, snapshot-invariance, boundary) + killCell in the UI-contract test.
- `loopzero-capsim-mcp/{server.mjs, server.test.mjs}` — **11 tests**; `mode` enum gains `'cell'`.
- `loopzero-bottleneck-simulator.html` — `btnCell` "Derrubar 1 célula" + wiring + callout.
- `docs/architecture/index.md` + `CHANGELOG.md` — M9 note.
- `loopzero-bottleneck-simulator-m9-{plan,review}.md`.
