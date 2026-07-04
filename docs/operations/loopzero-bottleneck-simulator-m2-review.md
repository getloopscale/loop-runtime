# Review — LoopZero Bottleneck Simulator M2 (AI Design Review via MCP)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m2`.
> **VERDICT: READY_TO_MERGE** — 0 BLOCKER, 0 HIGH, 0 MEDIUM. Independent review raised 5 LOW;
> L1–L4 closed with evidence, L5 is inherent (LIVE artifact captured).

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| No regression | 23 M0/M1 tests green inside **engine 29/29, exit 0** | ✅ |
| `review()` deterministic + tested | default (50k) = **score 32 / CRITICAL**, names cell_count + bff + single-region; healthy scenario SOUND; **real monotonic sweep** (1k→120k, 0 violations); clamped [0,100]; no NaN | ✅ |
| Grounding invariant | finding `rho` === engine `rho` for BOTH base (cell_count) and az-loss (engine_api) paths; `basis` field disambiguates | ✅ |
| MCP lists 3 tools; `capsim_review` returns grounded result | **integration test 4/4** (SDK client ↔ spawned server: initialize→tools/list→tools/call + inputSchema + error path) | ✅ |
| LIVE `claude mcp add` + call | `claude mcp add loopzero-capsim --scope local` succeeded; `claude mcp list` → **✔ Connected** (spawns server + completes handshake) | ✅ |
| CODE-QUALITY | engine + server syntax OK; **0 dead exports** (8/8 have callers); server imports only real engine symbols | ✅ |

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| L1 | "asserted monotonic" was only a single worked example | **FIXED** — added a real tenant-sweep test (1k→120k) asserting score never rises. Claim is now literally true. |
| L2 | findings carried two `engine_api` entries (base + az) — ambiguous for machine consumers | **FIXED** — added `basis: 'base' \| 'az-loss' \| 'region'` field to every finding |
| L3 | `finding.rho` overloaded (base ρ vs az ρ); az-ρ grounding unasserted | **FIXED** — `basis` disambiguates; added az-loss grounding test (finding.rho === chaos(killAZ).rho) |
| L4 | integration test didn't assert inputSchema / error path | **FIXED** — added inputSchema assertion + validation-error test (SDK surfaces `-32602` as `isError:true`) |
| L5 | LIVE `claude mcp add` is out-of-band (not in the automated suite) | ACCEPTED — inherent to a LIVE artifact; captured via `claude mcp list ✔ Connected` + proven by the automated integration test |

## Independent reviewer's cross-check (quoted)

> Score math exact 32 (independently reproduced); monotonicity sweep 0 violations; clamp engages
> (−170→0); grounding is a real determinism guarantee, not coincidence; official SDK (Rule 9);
> 3 tools with inputSchema; Infinity→"∞" replacer correct; stdio correct for `claude mcp add`;
> integration test genuine (real handshake); 23 M0/M1 tests preserved. No BLOCKER/HIGH/MEDIUM.

## Architecture

```
Claude (MCP client) --stdio JSON-RPC--> loopzero-capsim-mcp/server.mjs --import--> ../loopzero-capsim.mjs
   tools: capsim_simulate · capsim_chaos · capsim_review (grounded scorecard)
```

The rubric lives in the pure engine (unit-tested); the server is thin SDK plumbing. The
`capsim_review` tool returns grounded facts (ρ/SLA/findings) — the LLM client narrates on top,
never inventing metrics.

## Deliverables (M2)

- `loopzero-capsim.mjs` — engine + `review()` rubric export (grounded, `basis`-tagged findings).
- `loopzero-capsim.test.mjs` — **29 tests** (23 M0/M1 + 6 M2).
- `loopzero-capsim-mcp/{package.json, server.mjs, server.test.mjs}` — MCP server (official SDK) + **4 integration tests**.
- `loopzero-bottleneck-simulator-m2-{plan,review}.md`.

## How to run

```bash
cd docs/operations && node --test loopzero-capsim.test.mjs                  # engine 29/29
cd docs/operations/loopzero-capsim-mcp && npm install && node --test server.test.mjs   # mcp 4/4
claude mcp add loopzero-capsim --scope local -- node "$PWD/server.mjs"       # register (stdio)
# next session: call capsim_review / capsim_simulate / capsim_chaos as MCP tools
```
