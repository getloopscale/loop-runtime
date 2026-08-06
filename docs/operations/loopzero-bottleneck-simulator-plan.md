---
type: operational-runbook
title: LoopZero Bottleneck Simulator (M0)
---

# Plan — LoopZero Bottleneck Simulator (M0)

> Slug: `loopzero-bottleneck-simulator` · Cycle: DISCOVER→PLAN→IMPLEMENT→REVIEW · FAANG-level, no workarounds.
> Deliverable: interactive HTML simulator seeded with LoopZero's REAL AWS-first topology that finds
> capacity/latency/chaos bottlenecks at 50k tenants.

## Goal

Given LoopZero's chosen AWS-first architecture, **surface where it bottlenecks** — which ceiling binds
first, at how many tenants it saturates, and how a zone/cell failure changes that. Decision-support
for the re-platform, not a generic product.

## Baseline Context (grounded in code — file:line)

| Fact | Value | Source |
|---|---|---|
| Tenant pod request | 100m CPU / 128Mi mem | `api/internal/manifests/renderer.go:161-164` |
| Tenant pod limit | 500m / 512Mi (overcommit 5:1 CPU) | same |
| Cell capacity | 500 tenants/cell | `api/internal/placement/engine.go:91` |
| KEDA scale-to-zero | plan ≥ Pro | `pkg/enforcement/scale.go:42,78` |
| Replica caps | Starter=2 / Pro=5 / Team=10 | CLAUDE.md · `scale.go` |
| Node (default) | m8g.2xlarge 8 vCPU / 32 GiB · $173/mo RI | Vantage (cost calc) |
| Cache | ElastiCache r7g.large (Valkey) $128/mo | Vantage |
| RDS per-domain | memory/rag m8g.2xlarge · lens m8g.xlarge · promptly/trust m8g.large | Vantage (cost calc) |

## Simulation model (the moat — must be correct + tested)

Two coupled layers. Bottleneck = component with **max utilization** (used/capacity) across both.

### Layer A — Steady-state packing (does 50k FIT?)
- `cells = ceil(tenants / maxPerCell)` — maxPerCell=500.
- `activePods = tenants * activeFraction * podsPerTenant`.
- **Pod density per node** = min of 3 ceilings:
  - CPU: `floor(allocatableMilli / cpuRequestMilli)` (allocatable = vCPU*1000*(1-reserve)).
  - Mem: `floor(allocatableMem / memRequestMi)`.
  - ENI/IP: `eniMaxPods` (prefix delegation raises it) — the AWS VPC-CNI ceiling.
- `appNodes = ceil(activePods / podsPerNode)`.
- **RDS connections** per domain: `activePods * connsPerPod` vs `dbMaxConns * proxyMultiplex`.
- **Cache memory**: `tenants * cacheKbPerTenant` vs `cacheNodeGiB`.

### Layer B — Request queueing (does it HANDLE the load?)
- `totalRps = tenants * activeFraction * rpsPerActiveTenant`.
- Per tier (BFF, Engine API, App pod) with `c` servers × service rate `mu`:
  - `rho = totalRps / (c * mu)`; `rho >= 1` → saturated (bottleneck).
  - p50 ≈ sum of tier service times; p99 tail grows with rho (M/M/c waiting approximation).

### Chaos
- Kill AZ (removes 1/zones fraction of capacity) or a cell.
- Recompute Layer A/B on survivors → **capacity loss %**, new bottleneck, and **SLA** via
  series/parallel availability composition along the request path.

### Bottleneck report
Sort components by utilization desc. Top one = the bottleneck. For each: `{used, capacity, rho,
saturatesAtTenants}` (solve rho=1 for tenants). This is the deliverable's core output.

## Tasks

- **T1 — Engine (pure JS, testable).** `loopzero-capsim.mjs`: topology + Layer A + Layer B + chaos +
  bottleneck ranking. Zero DOM. Deterministic.
- **T2 — Tests (RED→GREEN).** `loopzero-capsim.test.mjs` (node:test): assert each ceiling formula, the
  bottleneck ranking, saturatesAt solving, and a chaos scenario. **Evidence = `node --test` green.**
- **T3 — UI.** `loopzero-bottleneck-simulator.html`: import the engine, seed LoopZero topology, sliders
  (tenants / active% / RPS / prefix-delegation toggle), chaos buttons, bottleneck panel.
- **T4 — Review.** Self-review vs DoD + math correctness + LoopZero fidelity.

## Acceptance Criteria / DoD (all must be evidenced 100%)

1. `node --test loopzero-capsim.test.mjs` → **all pass** (paste output). ≥ 12 assertions across the 3
   ceilings, queueing rho, saturatesAt, chaos.
2. Engine is **pure** (no DOM refs) — `node -e "import(...)"` loads clean; no undefined symbols.
3. Default scenario (50k tenants, 8% active) returns a **named bottleneck** with a real number
   (not NaN/undefined), and it matches a hand-computed value in the test.
4. Chaos (kill 1 AZ of 3) reduces zone-distributed capacity (`capLossPct=1/3`) and worsens every
   zone-distributed component's ρ (asserted: bff ρ increases). A **re-rank** of the top bottleneck
   is proven possible under a load-bound scenario (separate asserted test: bff overtakes cell_count).
5. HTML loads the engine and renders the bottleneck panel (manual evidence: open + screenshot-desc).
6. No fabricated constants — every capacity number traces to Baseline Context or an AWS-documented
   default, OR is explicitly labeled `ESTIMATE`/`POLICY input` in the engine (muRps, holdSec,
   cellSoftCeiling, nodeSoftCeiling). Nothing is passed off as a hard fact when it is a tunable.

## Test Plan

- Unit (node:test): ceilings, ranking, saturatesAt, chaos delta. Deterministic, no I/O.
- Boundary/negative: 0 tenants; tenants=maxPerCell exactly; prefix-delegation on/off flips the
  ENI ceiling; active%=100.
- Integration (manual): open HTML, move sliders, toggle prefix delegation → ENI ceiling flips CPU;
  kill AZ → bottleneck changes.

## Risks / Drawbacks

- **Model accuracy** — analytical, not discrete-event; labeled as estimate. Mitigation: ground every
  constant in file:line/AWS docs; unit-test the formulas.
- **AWS defaults drift** (ENI max-pods, RDS max_connections) — cite source in comments; make them
  inputs, not hardcoded magic.
- **Scope creep** — M0 is the 6 ceilings above + chaos + ranking. Throughput backpressure fine-grain
  is M2 (v2). Not in M0.

## Unresolved Questions
- (none blocking) — RDS Proxy multiplex factor is an estimate (labeled); refine with real
  `max_connections` per instance class in a follow-up.
