---
type: operational-runbook
title: "LoopZero Bottleneck Simulator M5 (reconcile calculator ↔ engine)"
---

# Plan — LoopZero Bottleneck Simulator M5 (reconcile calculator ↔ engine)

> Slug: `loopzero-bottleneck-simulator-m5`. Builds on M0-M4. FAANG-level, no fabrication.
> Collapse the two cost implementations into ONE: make `aws-cost-calculator.html` engine-backed so
> it can no longer diverge from `loopzero-capsim.mjs`. Closes the M4-exposed divergence permanently.

## Goal

M4 exposed that the calculator (manual `cells=25`, own capacity model) and the engine (derived
`cells=100` via `maxPerCell=500`) disagree ($48k vs $72.6k). The M4 keyed guard only *detects* it.
M5 *eliminates* it: the calculator imports the engine and computes its total via `cost()` +
`costBreakdown()` — one source of truth. The calculator becomes a rich what-if UI over the engine.

## Baseline Context (grounded — DISCOVER)

| Fact | Value | Source |
|---|---|---|
| Calculator has its OWN cost logic | inline `ITEMS` + `podsPerNode()` + `totalNodes()` + `recompute()` | `aws-cost-calculator.html:131-253` |
| Calculator manual cells | `cells=25` (violates maxPerCell=500) | `:70` |
| Calculator diverging defaults | activePct 10 / podsPerTenant 1.5 / reservePct 15 / prefixDeleg ON | `:91,92,97,101` |
| Engine authoritative | cells=⌈tenants/maxPerCell=500⌉; 0.08/1/0.10/false | `loopzero-capsim.mjs` DEFAULTS + `engine.go:91` |
| M4 keyed guard parses calculator ITEMS | becomes obsolete when ITEMS is removed | `loopzero-capsim.test.mjs` M4 guard |

## Changes

- **C1 — `costBreakdown(s)` in the engine.** Line-item view (group/id/label/qty/unitPrice/subtotal),
  region-aware, whose subtotals **sum exactly to `cost(s).total`**. The shared breakdown the
  calculator renders. Export.
- **C2 — calculator becomes engine-backed.** `<script type="module">` imports `{ DEFAULTS, scn, cost,
  costBreakdown, podsPerNode }`. `recompute()` builds a scenario override from the inputs →
  `cost(scn(o)).total` for the total + `costBreakdown(scn(o))` for the line items + capacity readout
  from `podsPerNode`. Removes the inline `ITEMS`/`podsPerNode`/`totalNodes`/`rdsInstances`/`derivedQty`.
- **C3 — derive cells + align defaults.** Replace the manual `cells` input with a `maxPerCell` input
  (default 500); cells = ⌈tenants/maxPerCell⌉ shown in the readout. Align input defaults to the
  engine (activePct 8, podsPerTenant 1, reservePct 10, prefixDeleg OFF).
- **C4 — retire the M4 keyed guard; add the M5 single-source test.** With one price source, the
  two-source guard is moot. Replace it with a structural test: the calculator imports
  `loopzero-capsim.mjs` and defines NO independent `ITEMS`/`PRICES` table.

## Tasks
- **T1 — `costBreakdown()` (TDD).** Sum-to-total identity. RED first.
- **T2 — calculator refactor.** Engine-backed recompute + costBreakdown render + maxPerCell + aligned defaults.
- **T3 — test evolution.** Replace M4 keyed guard with M5 single-source test; keep cost/posture/delta tests.
- **T4 — LIVE MCP evidence.** Call `capsim_cost`/`capsim_compare` in-session (tools now loaded) to
  prove the same engine numbers flow through the MCP.
- **T5 — REVIEW.** Independent adversarial review.

## Acceptance Criteria / DoD (all evidenced 100%)
1. **No regression** — all engine + MCP tests green (exit 0).
2. `costBreakdown(s)` sum == `cost(s).total` — EXACT, for single-region AND multi-region (asserted).
3. Calculator is **engine-backed**: imports `loopzero-capsim.mjs`, calls `cost()`+`costBreakdown()`, and
   contains NO independent `const ITEMS`/`const PRICES` cost table (asserted structurally).
4. Calculator derives cells from `maxPerCell` (no manual `cells` input); capacity defaults aligned to
   the engine (asserted: the calc's default input values map to engine DEFAULTS).
5. Single-source proof: the number the calculator shows for the default scenario == `cost(DEFAULTS).total`
   ($72,566) — by construction (it calls `cost()`), verified by the structural test + a Node
   assertion that costBreakdown(default) sums to 72,566.
6. LIVE: `capsim_cost({tenants:50000})` via MCP returns the same total (in-session tool call).
7. CODE-QUALITY: engine + server + calculator syntax/parse OK; 0 dead exports; HTML serves 200.

## Test Plan
- Unit (node:test): costBreakdown sum-to-total (single + multi-region); M5 single-source structural
  test (calc imports engine, no independent ITEMS); no-regression.
- LIVE: MCP capsim_cost in-session == cost() total.
- Integration (manual + curl): calculator served over http renders total + breakdown from the engine.

## Risks / Drawbacks
- The calculator loses per-line price/qty editing (prices are now canonical in the engine) — this is
  the point of single-source; the capacity what-if inputs remain editable. Labeled.
- ES-module import needs http (browsers block module imports over file://) — same as the simulator;
  documented (`python3 -m http.server`).
- Removing the M4 keyed guard reduces one test, but the divergence it guarded is now structurally
  impossible (one source) — a net integrity gain, not a loss.

## Unresolved Questions
- (none blocking) — making the global/traffic quantities (S3/CF GB) editable scenario inputs is a
  future enhancement; M5 keeps them canonical in COST_QTY.
