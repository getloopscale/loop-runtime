---
type: operational-runbook
title: "LoopZero Bottleneck Simulator M5 (reconcile calculator ↔ engine)"
---

# Review — LoopZero Bottleneck Simulator M5 (reconcile calculator ↔ engine)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m5`.
> **VERDICT: READY_TO_MERGE** (unconditional — independent review returned 0 BLOCKER/HIGH/MEDIUM,
> 4 LOW; the 3 actionable LOWs closed with evidence, 1 informational accepted).

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| No regression | engine **45/45**, MCP **8/8**, exit 0; cost()/review() defaults unchanged ($72,566 / 32-CRITICAL) | ✅ |
| costBreakdown sum == cost().total | EXACT (diff 0) for single + sharded + active-active; independent `node -e` over 8 scenarios | ✅ |
| Calculator engine-backed | `<script type="module">` imports `loopzero-capsim.mjs`, renders `cost()`+`costBreakdown()`, NO `const ITEMS`/`const PRICES` (asserted structurally) | ✅ |
| Cells derived + defaults aligned | manual `cells` input replaced by `maxPerCell` (500 → cells derived); activePct 8 / podsPerTenant 1 / reservePct 10 / prefixDeleg off | ✅ |
| Single-source proof | calc default == `cost(DEFAULTS).total` by construction (calls cost()); reviewer diffed 23 `$('id')` vs 23 `id=` (1:1, zero stale refs); parse OK; serves 200 | ✅ |
| LIVE MCP in-session | `capsim_cost({tenants:50000})` → **$72,565.5** (100 cells, 279 nodes) = engine = calculator; `capsim_compare` returns the full posture table | ✅ |
| CODE-QUALITY | engine + calc-module + server syntax OK; 0 dead exports (13) | ✅ |

## The reconciliation (one source of truth, proven across 3 surfaces)

`loopzero-capsim.mjs cost()` == `MCP capsim_cost` == `aws-cost-calculator.html` — all **$72,566** for the
default 50k scenario. The M4-exposed divergence ($48k calc vs $72.6k engine) is **structurally
eliminated**: the calculator no longer holds an independent price table or capacity model; it derives
cells from `maxPerCell=500` (100 cells) and computes via the engine. A user can edit capacity
assumptions but cannot override a price to diverge.

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| LOW-1 | calc `prefixCap=110` mapped to engine `eniMaxPodsPrefix` (canonical 250) — label inconsistency (numerically nil, kubelet 110 caps) | **FIXED** — default 250 + label clarifies kubelet 110 still caps |
| LOW-2 | canonical-lock named only ~8 prices/3 qtys | **FIXED** — full `deepEqual` of PRICES + COST_QTY (every key guarded) |
| LOW-4 | node labels said `m7g.2xlarge` vs engine `m8g.2xlarge` | **FIXED** — labels → m8g.2xlarge |
| LOW-3 | breakdown group taxonomy differs from cost() buckets (numerically correct) | ACCEPTED (informational) — each line's `mult` matches cost(); no user-visible contradiction |

## Reviewer cross-check (quoted)

> costBreakdown sum-to-total identity verified exact (diff 0.00e+0) across default/r1/sharded/
> active-active/prefix+aa/25k/0-tenants/maxPerCell=1000; 23 `$('id')` refs match 23 `id=` 1:1 (zero
> stale); podsPerNode + cost return-fields all consumed; parse OK; serves 200; single-source honesty
> TRUE (prices read-only); retiring the M4 keyed guard removed no real coverage.

## Deliverables (M5)

- `loopzero-capsim.mjs` — `costBreakdown()` (sum-to-total identity with `cost()`).
- `loopzero-capsim.test.mjs` — **45 tests** (M4 keyed guard → M5 single-source + costBreakdown-sum + full canonical-lock).
- `aws-cost-calculator.html` — **engine-backed** (imports the engine; no independent cost model; maxPerCell-derived cells; aligned defaults).
- `loopzero-bottleneck-simulator-m5-{plan,review}.md`.
