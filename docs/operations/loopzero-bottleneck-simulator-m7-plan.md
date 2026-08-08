---
type: operational-runbook
title: LoopZero Bottleneck Simulator M7 (quantify the request tier)
---

# Plan — LoopZero Bottleneck Simulator M7 (quantify the request tier)

> Slug: `loopzero-bottleneck-simulator-m7`. Uses M0-M6. FAANG-level, no workarounds.
> Close the one tooling gap the M6 memo flagged: BFF/Engine-API replicas are not MCP/UI-tunable, so
> the memo's Phase-4 (request tier) recommendation is un-quantified. Expose them → quantify it.

## Goal

Expose `bffReplicas` + `engineReplicas` as flat scenario inputs across MCP + UI (the engine already
supports them via nested `bff.replicas`/`engineApi.replicas`), so `capsim_review`/`capsim_simulate`
can prove exactly how many replicas resolve the request-tier bottlenecks — and quantify M6's Phase 4.

## Baseline Context (grounded — DISCOVER, LIVE engine)

| Fact | Value | Source |
|---|---|---|
| BFF tier | `bff:{replicas:6, muRps:400}` | `loopzero-capsim.mjs:32` |
| Engine API tier | `engineApi:{replicas:8, muRps:350}` | `loopzero-capsim.mjs:33` |
| `scn` nested merge of `bff.replicas` | works | verified `node -e` |
| **BFF resolves at 6→7 replicas** | ρ 1.042 → 0.893 | `simulate(scn({bff:{replicas:7}}))` |
| **Engine AZ-fragility resolves at 8→11 replicas** | AZ ρ 1.339 → 0.974 | `chaos(scn({engineApi:{replicas:11}}),{killAZ})` |
| MCP/UI cannot set replicas today | scenarioShape omits them | `loopzero-capsim-mcp/server.mjs` |

## Changes

- **C1 — flat aliases in `scn()`.** `bffReplicas` → `bff.replicas`, `engineReplicas` →
  `engineApi.replicas` (applied after the merge, flat wins; alias keys stripped from the scenario).
  Lets MCP/UI pass replicas without nested objects; the engine reads the same nested fields.
- **C2 — MCP schema.** Add `bffReplicas` + `engineReplicas` (int ≥1) to `scenarioShape`; the tools
  already `scn(args)`, so the aliases flow through.
- **C3 — UI.** Add BFF replicas + Engine API replicas sliders to the simulator.
- **C4 — quantify M6 Phase 4.** Update the memo's Phase 4 with the now-derivable numbers (BFF 6→7,
  Engine 8→11) — the tooling gap it flagged is closed.

## Tasks
- **T1 — `scn` aliases (TDD).** RED first. Flat maps to nested; flat wins; keys stripped.
- **T2 — request-tier resolution tests.** `bffReplicas:7` → BFF ρ<1; `engineReplicas:11` → AZ ρ<1.
- **T3 — MCP schema + passthrough test.**
- **T4 — UI sliders.**
- **T5 — memo Phase 4 quantified.**
- **T6 — REVIEW.**

## Acceptance Criteria / DoD (all evidenced 100%)
1. **No regression** — engine + MCP suites green (exit 0); default review still 32/CRITICAL.
2. `scn({bffReplicas:7}).bff.replicas === 7` and no leftover `bffReplicas` key; same for
   `engineReplicas`; flat wins over nested when both given (asserted).
3. Request-tier quantified: `simulate(scn({bffReplicas:7}))` BFF ρ<1 (resolved); `chaos(...,killAZ)`
   with `engineReplicas:11` → engine AZ ρ<1 (asserted with the exact resolving counts).
4. MCP exposes `bffReplicas` + `engineReplicas`; a `capsim_review` call with `bffReplicas:7` drops the
   BFF finding vs default — passthrough proven (result differs).
5. UI has BFF + Engine replica sliders wired (structural test / grep).
6. M6 memo Phase 4 updated with the quantified numbers; the "not MCP-parametrized" caveat removed.
7. CODE-QUALITY: engine + server + calc syntax OK; 0 dead exports; suites green.

## Test Plan
- Unit (node:test): scn alias mapping + precedence + key-strip; BFF/Engine resolution counts;
  no-regression.
- Integration (MCP): schema exposes bffReplicas/engineReplicas; capsim_review with bffReplicas:7
  removes the BFF critical finding (passthrough proof).
- LIVE: call capsim_review {bffReplicas:7, engineReplicas:11} in-session → request-tier findings gone.

## Risks / Drawbacks
- Replica count changes the review (ρ) but NOT the cost model (BFF/Engine are small platform pods, not
  separately priced) — documented; the value is bottleneck quantification, not a cost lever.
- `muRps` stays an ESTIMATE (calibrate via load test) — not exposed as a lever in M7 (out of scope;
  it's the calibration track).

## Unresolved Questions
- (none blocking) — pricing BFF/Engine replicas as their own cost line is a future enhancement if the
  platform-services footprint grows enough to matter.
