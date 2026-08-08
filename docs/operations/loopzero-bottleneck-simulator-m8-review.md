---
type: operational-runbook
title: LoopZero Bottleneck Simulator M8 (mgmt / control-plane cluster capacity)
---

# Review — LoopZero Bottleneck Simulator M8 (mgmt / control-plane cluster capacity)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m8`.
> **VERDICT: READY_TO_MERGE** — independent adversarial review returned 0 BLOCKER / 0 HIGH after the
> HIGH grounding fix was re-verified; 1 LOW closed; 1 cosmetic LOW closed. Anchor $72,565.5 preserved.

## Origin

Sponsor finding: *"aumento o número de pods do API e BFF mas ele não tem relação ao cluster mgmt?"*
Correct — the BFF (`loopzero-cloud`) and Engine-API (`loopzero-api`) run on the mgmt cluster, but their replica
sliders were modeled ONLY as rps queueing tiers, decoupled from mgmt node capacity AND cost (M7 even
asserted `cost()` invariant to replicas). M8 closes the gap.

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| Replicas pack onto mgmt nodes (grounded requests) | `mgmtNodes()` packs `platformBaseline + replicas×{cpuM,memMi}`; grounded BFF 50m/64Mi (`charts/loopzero-cloud/values.yaml:64-66`), Engine 100m/64Mi (`charts/loopzero-api/values.yaml:181-184`) | ✅ |
| `mgmt_nodes` bottleneck surfaced | component in `simulate()`/`review()`; MCP exposes it; UI KPI + ranked panel row | ✅ |
| `cost()` derives mgmt nodes (coupled) | `totalNodes = appNodes + cells×overhead + mgmtNodes(s).nodes` in both `cost()` + `costBreakdown()`; extreme replicas raise cost by EXACTLY nodesDelta×$173 (verified $1,384 for +8 nodes @ bff 2000) | ✅ |
| Default **$72,566 preserved** (anchor unmoved) | `cost(scn()).total === 72565.5` (default packs to 4 < HA floor 10) — reviewer re-verified exactly | ✅ |
| Honest: floor-dominated / coupled-at-scale, no overstatement | platform baseline labelled ESTIMATE; floor documented; small bumps absorbed (verified), extremes cost | ✅ |
| No regression | engine **55/55** (+7 M8), MCP **10/10** (+1 M8), exit 0; pre-M8 48+9 unaffected | ✅ |
| LIVE browser test | both pages render in real Chrome, **console-error-free**, $72,566, mgmt KPI shows `packs 4→5` on scale, mgmt row "dirigido por replicas/plataforma" | ✅ |

## What M8 delivers

The BFF/Engine replica sliders now **couple to the mgmt cluster**. `mgmtNodes(s)` packs
`replicas × per-pod requests` (grounded) + a labelled platform baseline onto mgmt worker nodes; a
`mgmt_nodes` bottleneck surfaces; `cost()` derives mgmt nodes instead of the flat `fixedNodes:10`.
The model is **floor-dominated at default, coupled at scale**: BFF/Engine pods are tiny (50m/100m),
so realistic replica counts are absorbed by the HA floor of 10 nodes — the honest answer — while
extreme counts break the floor (cost rises by exactly the extra EC2 nodes + the bottleneck fires).
The UI KPI (`packs 4→5`) makes the relation visible even while node count is floored.

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| HIGH | BFF was mis-grounded at 100m/128Mi (the `.example` values); canonical `charts/loopzero-cloud/values.yaml` requests are 50m/64Mi (128Mi is the LIMIT) — overstated coupling ~46% at scale | **FIXED** → `cpuM:50, memMi:64`; test thresholds re-tuned (2000/5000 replicas to still cross the floor); plan grounding updated; reviewer re-verified magnitude now sane ($1,384 for +8 nodes) |
| LOW | `mgmt_nodes` rendered "satura @ ∞ tenants" (misreadable as infinite capacity) | **FIXED** → row + callout branch on `mgmt_nodes` → "dirigido por replicas/plataforma (não por tenants)" |
| LOW | stale comment on test line 460 ("bff 100m/128Mi") | **FIXED** → comment updated to 50m/64Mi (assertion was already dynamic/correct) |

## Reviewer cross-check (quoted, re-verification)

> `node --test` returned 55/55 and 10/10 both exit 0; `cost(scn()).total` printed literally `72565.5`;
> `mgmtNodes(scn())` printed `{nodes:10,packed:4,cpuM:25100,memMi:74624,binding:"cpu",floorBound:true}`
> matching my by-hand arithmetic exactly; the coupling one-liner printed
> `costDelta 1384 nodeDelta 8 expected 1384 MATCH true`; and the chart file read shows requests
> `50m/64Mi` with `128Mi` under `limits:`. VERDICT: READY_TO_MERGE — 0 BLOCKER, 0 HIGH.

## Deliverables (M8)

- `loopzero-capsim.mjs` — `mgmtNodes()` export; DEFAULTS `bff/engineApi` per-pod requests + `mgmt` baseline; `mgmt_nodes` component; `cost()`+`costBreakdown()` derive mgmt nodes.
- `loopzero-capsim.test.mjs` — **55 tests** (+7 M8: floor-bound default, packing math, coupling, anchor-preserved, cost-couples, component, bottleneck-fires).
- `loopzero-capsim-mcp/{server.test.mjs}` — **10 tests** (+1 M8: mgmt_nodes exposed + cost coupling passthrough).
- `loopzero-bottleneck-simulator.html` — mgmt KPI (`packs N`), emoji, replica-driven labels.
- `aws-cost-calculator.html` — engine-backed, $72,566 preserved (no code change needed — flows through `cost()`).
- `docs/architecture/index.md` — M0–M8 / 55/55 / mgmt capacity note.
- `loopzero-bottleneck-simulator-m8-{plan,review}.md`.
