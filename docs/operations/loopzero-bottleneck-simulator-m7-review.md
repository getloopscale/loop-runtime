# Review — LoopZero Bottleneck Simulator M7 (quantify the request tier)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m7`.
> **VERDICT: READY_TO_MERGE** (unconditional — independent review returned 0 BLOCKER/HIGH, 4 LOW;
> 3 actionable LOWs closed with evidence, 1 cosmetic accepted).

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| No regression | engine **48/48**, MCP **9/9**, exit 0; default review 32/CRITICAL; cost() invariant to replicas ($72,565.5 with/without) | ✅ |
| scn flat aliases | `scn({bffReplicas:7}).bff.replicas===7`, no leftover key; flat wins over nested; muRps sibling preserved; DEFAULTS immutable (structuredClone) | ✅ |
| Request tier quantified | BFF 6→7 resolves saturation (ρ 1.04→0.89 warm), 6→10 fully clears (base+AZ); Engine 8→11 clears base+AZ-fragility (AZ ρ 1.34→0.97) — asserted | ✅ |
| MCP exposes + passthrough | schema advertises `bffReplicas`+`engineReplicas` (int≥1); passthrough proven for BOTH (tuned clears bff + engine_api findings) | ✅ |
| UI sliders | BFF (pre-existing) + Engine API replicas sliders wired (id/CTRL/seed) | ✅ |
| Memo Phase 4 quantified | M6 memo Phase 4 updated with the replica counts; "not MCP-parametrized" caveat removed | ✅ |
| CODE-QUALITY | engine + server + calc syntax OK; 0 dead exports; suites green | ✅ |

## What M7 delivers

The M6 memo's Phase 4 ("add BFF/Engine-API replicas") is now **quantified + tunable**: `bffReplicas`
and `engineReplicas` are flat scenario inputs (engine alias → nested), exposed via MCP + the simulator
UI. The exact fixes: **BFF 6→7** resolves saturation, **6→10** fully clears (an AZ loss re-saturates
BFF at 8-9); **Engine 8→11** clears base + AZ-fragility. These are review (ρ) fixes, not cost levers
(small platform pods, not separately priced — cost() verified invariant).

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| LOW-1 | memo "45/45 + 8/8" stale (now 48/9) in a file M7 edited | **FIXED** → "M7 snapshot 48/48 + 9/9; M6 decision numbers locked at 45/8" |
| LOW-2 | MCP passthrough asserted bff but not engineReplicas independently | **FIXED** → test now asserts engine_api finding clears too |
| LOW-4 | alias `!= null` guard let `bffReplicas:0` set 0 (direct-call footgun) | **FIXED** → `>= 1` guard (0/negative ignored; verified `bffReplicas:0`→keeps 6) |
| LOW-3 | UI CTRL alias-name column is dead metadata | ACCEPTED (cosmetic) — pre-existing CTRL pattern; the UI wires to nested `mk`, alias name is documentation-only |

## Reviewer cross-check (quoted)

> scn alias correct (post-merge, flat wins, keys stripped, structuredClone → DEFAULTS immutable);
> all resolving counts reproduce exactly (BFF 1.042→0.893→(8:base clears,AZ 1.172)→(10:clears);
> Engine 8:AZ1.339→11:AZ0.974); cost() invariant to replicas; MCP passthrough genuine; memo edits
> accurate; 48/48 + 9/9 green.

## Deliverables (M7)

- `loopzero-capsim.mjs` — `scn()` flat aliases `bffReplicas`/`engineReplicas` (≥1 guard).
- `loopzero-capsim.test.mjs` — **48 tests** (+3 M7: alias, request-tier resolution, review clearing).
- `loopzero-capsim-mcp/{server.mjs, server.test.mjs}` — schema + **9 integration tests** (both params passthrough-proven).
- `loopzero-bottleneck-simulator.html` — Engine API replicas slider.
- `docs/architecture/aws-region-posture-decision.md` — Phase 4 quantified.
- `loopzero-bottleneck-simulator-m7-{plan,review}.md`.
