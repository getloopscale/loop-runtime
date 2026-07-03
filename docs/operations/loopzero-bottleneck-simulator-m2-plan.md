# Plan — LoopZero Bottleneck Simulator M2 (AI Design Review via MCP)

> Slug: `loopzero-bottleneck-simulator-m2`. Builds on M0/M1 engine. FAANG-level, no fabrication.
> Expose the simulator as an MCP server so Claude (or any MCP client) reviews LoopZero's architecture
> choices grounded on the deterministic engine outputs — never on invented metrics.

## Goal

Turn the deterministic bottleneck data (M0/M1) into an **actionable design review**: a rubric-based
score (0-100) + verdict + findings + recommendations, delivered as an **MCP tool** (`capsim_review`)
so an LLM client narrates the critique *on top of real numbers* (anti-hallucination). Also expose
`capsim_simulate` and `capsim_chaos` as tools.

## Baseline Context (grounded)

| Fact | Value | Source |
|---|---|---|
| MCP transport | stdio via `claude mcp add <name> -- node server.mjs` | `claude mcp add --help` (DISCOVER) |
| MCP SDK | `@modelcontextprotocol/sdk` v1.29.0 (official) | npm (DISCOVER) — Rule 9: don't reinvent the protocol |
| Engine | `loopzero-capsim.mjs` pure, 23/23 tests | M0/M1 |
| Grounding rule | LLM narrates ON the engine's ρ/SPOF/SLA — never invents metrics | `plan-confidence-golden-rule` (post-rationalization risk) |

## Architecture

```
Claude (MCP client)  --stdio JSON-RPC-->  loopzero-capsim-mcp/server.mjs  --import-->  ../loopzero-capsim.mjs
                                              tools: capsim_simulate / capsim_chaos / capsim_review
```

- The **rubric lives in the engine** (`review(scenario)` export) → unit-tested (real evidence).
- The **server is thin** (SDK `McpServer` + 3 tool registrations calling engine functions).
- The LLM client produces prose; the tool returns structured, grounded facts.

## Rubric (the moat — deterministic, testable)

`review(scenario)` runs `simulate` + `chaos(killAZ)` + `chaos(killRegion)` and scores from 100:

| Signal | Deduction | Rationale |
|---|---|---|
| Component saturated at base (ρ ≥ 1) | −20 each | a live bottleneck now |
| Component warm at base (0.8 ≤ ρ < 1) | −8 each | headroom gone soon |
| AZ-fragile (crosses to ρ ≥ 1 under killAZ, was < 1 at base) | −10 each | loses an AZ → saturates |
| Single-region (region-outage SLA < 99.9%) | −10 | no regional failover |

Verdict bands: `SOUND` (≥85) · `NEEDS_REVISION` (60-84) · `AT_RISK` (40-59) · `CRITICAL` (<40).
Each finding carries `{severity, component, rho, message, recommendation}`. Output also includes the
base bottleneck, AZ bottleneck, region SLA, and capLoss — the numbers the LLM narrates.

## Tasks

- **T1 — `review()` in engine (TDD).** Pure rubric. RED test first. Deterministic score.
- **T2 — MCP server (SDK).** `loopzero-capsim-mcp/{package.json,server.mjs}`; tools `capsim_simulate` /
  `capsim_chaos` / `capsim_review`; input schemas; imports the engine.
- **T3 — MCP integration test.** Spawn the server via SDK `Client` + `StdioClientTransport`;
  `initialize` → `tools/list` (assert 3 tools) → `tools/call capsim_review` (assert score/verdict).
- **T4 — Register + LIVE.** `claude mcp add loopzero-capsim -- node .../server.mjs`; call the tool for real.
- **T5 — REVIEW.** Independent adversarial review vs DoD.

## Acceptance Criteria / DoD (all evidenced 100%)

1. **No regression** — engine tests still green (M0/M1). Full suite exit 0.
2. `review()` deterministic + tested: default (50k) verdict ∈ {AT_RISK, CRITICAL} with findings
   naming cell_count + bff saturated + single-region; a healthy scenario scores strictly higher
   (asserted monotonic). Score clamped [0,100]. No NaN.
3. Grounding invariant: every finding's `rho`/`sla` comes from the engine (a test asserts the
   review numbers equal the simulate/chaos numbers — no independent/invented figure).
4. MCP server lists exactly 3 tools; `tools/call capsim_review` returns a valid structured result —
   proven by an **automated integration test** (SDK client ↔ spawned server, exit 0).
5. LIVE: `claude mcp add` succeeds and a real `capsim_review` call returns the grounded scorecard
   (evidence: command output).
6. CODE-QUALITY: engine + server syntax OK; 0 dead exports; server imports only real engine symbols.

## Test Plan
- Unit (node:test): rubric score/verdict/monotonicity/clamp/grounding + no-regression.
- Integration (node:test): spawn server via SDK, tools/list + tools/call, assert shape.
- LIVE: `claude mcp add` + real tool call (operator-visible evidence).

## Risks / Drawbacks
- Rubric weights are a **design judgment** (labeled) — tunable, not physics. Documented in the table.
- MCP server adds a node dependency (`@modelcontextprotocol/sdk`) — justified (Rule 9; standard).
- The prose critique quality depends on the client LLM — but it is grounded, so it cannot invent metrics.

## Unresolved Questions
- (none blocking) — the HTML playground calling the MCP (browser needs an HTTP-MCP proxy) is deferred;
  M2 targets the Claude Code client (stdio), which is the immediate use.
