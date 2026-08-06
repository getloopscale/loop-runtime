---
type: operational-runbook
title: LoopZero Bottleneck Simulator M4 (cost-coupled review)
---

# Review — LoopZero Bottleneck Simulator M4 (cost-coupled review)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m4`.
> **VERDICT: READY_TO_MERGE** (after remediation). Independent review first returned READY_TO_MERGE
> *conditional* (1 HIGH + 2 MEDIUM + 4 LOW); HIGH-1, M-1, M-2, L-1..L-4 closed with evidence; re-confirmed.

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| No regression | 36 prior tests green inside **engine 42/42**; **MCP 8/8**; exit 0 | ✅ |
| Keyed consistency guard | parses each calculator ITEMS row by id; asserts PRICES==unitDef + COST_QTY==qtyDef **positionally**; negative control proves it reads the column (173) not the on-dem label (262) | ✅ |
| `cost()` grounded + deterministic | default single **$72,566** (compute $55,567 + control $2,889 + global $14,110), 100 cells, 279 nodes; total==sum; no NaN | ✅ |
| Posture ordering + exact Δ | single < sharded < active-active; Δ(sharded−single)==controlPlane·(R−1), Δ(aa−sharded)==compute·(R−1) — exact (unrounded) | ✅ |
| `comparePostures` table + grounding | 3 rows {cost, blastRadius, sla, score, costOfResilience}; `review().metrics.costPerMonth == round(cost().total)` | ✅ |
| MCP `capsim_cost` + `capsim_compare` | 5 tools; passthrough proven; integration test | ✅ |
| CODE-QUALITY | engine + server syntax OK; 0 dead exports (11); no fabrication; HTML wired + serves 200 | ✅ |

## The decision-support output (default 50k)

| Posture | Cost/mo | Blast radius | SLA | Cost of resilience |
|---|---|---|---|---|
| single | $72,566 | 100% (total outage) | 95% | — |
| sharded ×3 | $78,344 | 33% | 98.3% | **+$5,778** (control-plane ×2) |
| active-active ×3 | $189,478 | 0% | 99.98% | **+$116,912** (replicate compute ×2) |

"Is full failover worth +$117k/mo?" — now a number, not a guess.

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| **HIGH-1** | Price guard was membership-only → an on-dem label (262/490/160) could false-pass a wrong unit price | **FIXED** — keyed/positional guard parses ITEMS by id, compares unitDef column; negative control asserts ec2 price==173≠262. Proven: calc contains "262" (membership would pass) but keyed guard rejects. |
| M-1 | Quantities (COST_QTY) had no guard | **FIXED** — COST_QTY exported; keyed guard asserts all 19 qtys == calculator qtyDef |
| M-2 | Divergence disclosure incomplete (only cells) | **FIXED** — plan documents full $72.6k vs $48.4k decomposition: +$31,425 cell overhead offset −$7,266 by 4 differing capacity defaults (279 vs 171 nodes) |
| L-1 | UI-contract test didn't cover M4 UI | **FIXED** — M4 UI-contract test asserts cost() + comparePostures() fields |
| L-2 | costOfResilience rounded both sides (±1 drift) | **FIXED** — computed from unrounded totals then rounded; `_raw` deleted from output |
| L-3 | over/under-count directions unspecified | **FIXED** — plan Risks spells out control-plane over-counts, compute under-counts |
| L-4 | DoD counts stale | **FIXED** — 42 engine + 8 MCP |

## Deliverables (M4)

- `loopzero-capsim.mjs` — `PRICES`, `COST_QTY`, `cost()`, `comparePostures()`, `review().metrics.costPerMonth`.
- `loopzero-capsim.test.mjs` — **42 tests** (+ keyed guard + UI contract + cost posture/deltas).
- `loopzero-capsim-mcp/{server.mjs, server.test.mjs}` — `capsim_cost` + `capsim_compare`; **8 integration tests**.
- `loopzero-bottleneck-simulator.html` — cost KPI + posture trade-off table.
- `loopzero-bottleneck-simulator-m4-{plan,review}.md`.
