---
type: operational-runbook
title: LoopZero Bottleneck Simulator M8 (mgmt / control-plane cluster capacity)
---

# Plan — LoopZero Bottleneck Simulator M8 (mgmt / control-plane cluster capacity)

> Slug: `loopzero-bottleneck-simulator-m8`. Cycle: DISCOVER → PLAN → IMPLEMENT (TDD) → CODE-QUALITY → REVIEW.
> Origin: sponsor finding — "aumento pods de API/BFF mas não tem relação ao cluster mgmt?" The BFF
> (`loopzero-cloud`) + Engine-API (`loopzero-api`) run on the **mgmt / control-plane cluster**, but their
> replica sliders were modeled ONLY as rps queueing tiers (ρ) — **decoupled** from mgmt node capacity
> AND from cost (M7 even asserted `cost()` invariant to replicas). M8 closes that modeling gap.

## Goal

Make the BFF/Engine replica sliders **couple** to the mgmt cluster: (1) their `replicas × per-pod
requests` pack onto mgmt worker nodes; (2) a new `mgmt_nodes` bottleneck surfaces the mgmt-cluster
node pressure; (3) `cost()` derives mgmt nodes from that packing instead of the flat `fixedNodes: 10`.
Preserve the established **$72,566** default (the fiction removed, not the anchor moved).

## DISCOVER — grounding (real numbers, no invention)

| Fact | Source | Value |
|---|---|---|
| BFF (`loopzero-cloud`) pod requests | `charts/loopzero-cloud/values.yaml:64-66` | `50m` CPU / `64Mi` mem (128Mi is the LIMIT, not the request) |
| Engine-API (`loopzero-api`) pod requests | `charts/loopzero-api/values.yaml:181-184` | `100m` CPU / `64Mi` mem |
| mgmt node type | `loopzero-capsim.mjs` DEFAULTS (m8g.2xlarge) | 8 vCPU / 32 GiB, 10% reserve → 7200m / 29491Mi alloc |
| prior mgmt allowance | `loopzero-capsim.mjs:22` `fixedNodes: 10` | flat HA/headroom over-provision floor |
| mgmt-hosted platform set | `CLAUDE.md § Four Planes` | 8 operators + argocd + gitea + kratos + hydra + redis + minio + traefik + cert-manager + ESO + observability |

**Key insight:** BFF/Engine pods are **tiny** (100m/64-128Mi). 6+8 default pods pack into < 1 node by
requests. The `fixedNodes:10` is dominated by the fixed platform set (HA + headroom), NOT by the
sliders. So the honest model is **floor-dominated at default, coupled at scale** — small replica
bumps are absorbed by the HA floor; large ones force new nodes + cost. Overstating the per-replica
cost would be dishonest (Rule 3).

## Design

### Engine (`loopzero-capsim.mjs`)
1. DEFAULTS — add grounded per-pod requests + mgmt baseline:
   - `bff: { replicas: 6, muRps: 400, cpuM: 50, memMi: 64 }`
   - `engineApi: { replicas: 8, muRps: 350, cpuM: 100, memMi: 64 }`
   - `mgmt: { platformCpuM: 24000, platformMemMi: 73728, nodeSoftCeiling: 30 }` — platform baseline is an **ESTIMATE** (labelled), calibrated so default packs below the `fixedNodes:10` HA floor.
   - keep `fixedNodes: 10` as the **floor** (HA + headroom policy).
2. New export `mgmtNodes(s)` → `{ nodes, packed, cpuM, memMi, binding, floorBound }`:
   - `cpuM = mgmt.platformCpuM + bff.replicas*bff.cpuM + engineApi.replicas*engineApi.cpuM`
   - `memMi` analogous; `packed = max(ceil(cpuM/allocCpuM), ceil(memMi/allocMemMi))`
   - `nodes = max(fixedNodes, packed)` (HA floor).
3. `buildComponents` — add `mgmt_nodes` component: `used = mgmtNodes(s).nodes`, `capacity =
   round(mgmt.nodeSoftCeiling * zf)`, `saturatesAtTenants = Infinity` (replica/platform-driven, not
   tenant-driven), note names BFF+Engine+platform+floor.
4. `cost()` — `totalNodes = appNodes + cells*overheadPerCell + mgmtNodes(s).nodes` (replaces flat `fixedNodes`).

### MCP (`loopzero-capsim-mcp/server.mjs`)
- No new params (replicas already pass through). Add integration test: `mgmt_nodes` present in review; extreme `bffReplicas` raises mgmt nodes + cost (passthrough → cost coupling proven).

### Simulator UI (`loopzero-bottleneck-simulator.html`)
- `mgmt_nodes` flows into the ranked bottleneck list automatically. Add an explicit mgmt readout so
  scaling BFF/Engine visibly moves mgmt utilization (directly answers the sponsor's question).

### Calculator (`aws-cost-calculator.html`)
- `cost()`/`costBreakdown()` change flows through (imports the engine). Verify default still $72,566.

## Coverage Matrix

| Goal claim | Task | Test |
|---|---|---|
| Replicas pack onto mgmt nodes (grounded requests) | T1 engine `mgmtNodes()` | T-mgmt-packing, T-mgmt-default-10 |
| `mgmt_nodes` bottleneck surfaced | T2 buildComponents | T-mgmt-component, T-mgmt-bottleneck-fires |
| `cost()` derives mgmt nodes (coupled) | T3 cost() | T-cost-couples, T-cost-default-preserved ($72,565.5) |
| Default $72,566 preserved (anchor unmoved) | T3 | T-cost-default-preserved |
| MCP passthrough → cost/mgmt coupling | T4 server test | T-mcp-mgmt |
| UI shows mgmt relation | T5 html | LIVE browser test |
| Calculator still $72,566 | T6 | LIVE browser test |

## Drawbacks & Risks

1. **Platform baseline is an ESTIMATE** — calibrated to the prior 10-node anchor, not per-component measured. Mitigation: labelled ESTIMATE (like the existing `muRps`), documented, and the floor (`fixedNodes`) keeps it conservative. A future slice could ground each platform component.
2. **Floor-dominated at default may surprise** — scaling BFF 6→10 still shows mgmtNodes=10 (absorbed). Mitigation: this is the *honest* answer (tiny pods); the UI shows the mgmt utilization climbing toward the floor so the coupling is visible even when absorbed.
3. **Blast radius on cost()** — any error changes $72,566 everywhere. Mitigation: T-cost-default-preserved is a hard regression guard; calibration pre-verified (default packed=4 < floor 10).

## Unresolved Questions

(none — grounding is from the charts; the estimate is explicitly labelled and floor-guarded.)

## Concurrency / Failure scenarios

(none — pure deterministic engine, no I/O, no concurrency.)
