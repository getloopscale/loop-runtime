---
type: operational-runbook
title: LoopZero Bottleneck Simulator (M0)
---

# Review — LoopZero Bottleneck Simulator (M0)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator`.
> **VERDICT: READY_TO_MERGE** — 0 BLOCKER, 0 HIGH; 2 MEDIUM + 1 LOW raised by independent
> review, all closed with evidence below.

## DoD → evidence (all 6 satisfied)

| # | DoD | Evidence | Status |
|---|---|---|---|
| 1 | `node --test` all pass | **14/14 pass, exit=0** (`node --test loopzero-capsim.test.mjs`) | ✅ |
| 2 | Engine pure (no DOM) | ran headless in Node; only match is "docu**ment**ed" (comment). Independent review confirmed no DOM/window/globalThis/navigator usage | ✅ |
| 3 | Default scenario → named bottleneck, real number | `cell_count` ρ=1.56 → saturates @ **32 000** tenants; asserted in test + printed | ✅ |
| 4 | Chaos reduces capacity + re-rank possible | `capLossPct=1/3` asserted; **re-rank proven**: base top=`cell_count` → chaos top=`bff` (asserted) | ✅ |
| 5 | HTML loads the engine | both files **HTTP 200** over http (browser import path); UI-contract test enforces every field render() reads | ✅ |
| 6 | No fabricated constants | grounded (`renderer.go:161`, `engine.go:91`) + `ESTIMATE`/`POLICY` labels on tunables (muRps, holdSec, cellSoftCeiling); AWS defaults (eni=58, maxConns≈3600) confirmed plausible by independent review | ✅ |

## Cycle gates

| Phase | Result |
|---|---|
| DISCOVER | real constants extracted from code (file:line), not memory |
| PLAN | grounded plan w/ Goal, Baseline, model math, DoD, Test Plan, Risks |
| IMPLEMENT (TDD) | RED (ERR_MODULE_NOT_FOUND) → GREEN (14/14) — real evidence both ways |
| CODE-QUALITY | syntax OK · **0 dead exports** (5/5 have callers) · **0 fabrication** (4/4 HTML imports resolve) |
| REVIEW | independent adversarial agent: math recomputed & correct, purity confirmed, contract test-enforced |

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| MEDIUM | DoD #6: muRps/holdSec not source-labeled | **FIXED** — `ESTIMATE` labels added (`loopzero-capsim.mjs`) |
| MEDIUM | DoD #4: "re-ranks" not proven at defaults | **FIXED** — reworded to accurate claim + added re-rank test (bff overtakes cell_count under load) |
| LOW | div-by-zero in saturatesAtTenants (agent slider min=0) | **FIXED** — `satAt()` guard → Infinity (never NaN); test added; UI renders `∞` |
| LOW | agent/rag RPS never loads compute tiers | intentional model (agent traffic hits domain services, not BFF) — documented in review; accepted |
| LOW | dead field in CTRL tuples | descriptive-only; harmless; left as documentation |
| INFO | AWS defaults plausible | confirmed |

## Deliverables

- `loopzero-capsim.mjs` — pure engine (5 exports, unit-tested).
- `loopzero-capsim.test.mjs` — 14 tests (node:test).
- `loopzero-bottleneck-simulator.html` — interactive UI (imports the engine).
- [`loopzero-bottleneck-simulator-plan.md`](./loopzero-bottleneck-simulator-plan.md) — plan + DoD.
- `loopzero-bottleneck-simulator-review.md` — this file.

## How to run

```bash
cd docs/operations
node --test loopzero-capsim.test.mjs            # engine tests (evidence)
python3 -m http.server 8080                 # serve (module import needs http)
# open http://localhost:8080/loopzero-bottleneck-simulator.html
```
