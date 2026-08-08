---
type: operational-runbook
title: LoopZero Bottleneck Simulator M1 (calibrate + deepen)
---

# Review — LoopZero Bottleneck Simulator M1 (calibrate + deepen)

> Cycle verdict record. Slug: `loopzero-bottleneck-simulator-m1`.
> **VERDICT: READY_TO_MERGE** — 0 BLOCKER, 0 HIGH. Independent review raised 1 MEDIUM + 4 LOW;
> the MEDIUM + 3 LOW closed with evidence, 1 LOW is informational.

## DoD → evidence (all satisfied)

| DoD | Evidence | Status |
|---|---|---|
| No regression | 14 M0 tests still green inside **23/23 pass, exit 0** | ✅ |
| C1 — computed RDS max_connections | `rdsMaxConns(32)=3604, (16)=1802, (8)=901, (1e5)=5000`; **no `maxConns:` literal remains** (grep) | ✅ |
| C2 — RDS Proxy multiplex linear | `proxyMaxConnPct 100→10812 / 50→5406` (asserted) | ✅ |
| C3 — promptly saturates before memory | promptly ρ=0.0355 > memory ρ=0.0111 (3.2×, driven by the 901-vs-3604 conn gap, not holdSec) | ✅ |
| C4 — rhoOf correctness | `rhoOf(1,0)=∞, rhoOf(0,0)=0` (asserted) | ✅ |
| C5 — region chaos total blast radius | killRegion → **every** component ρ=∞ (incl. cell_count), capLoss=1, sla<base (asserted) | ✅ |
| C6 — regionalOutageAvail SLA floor | `0.90→sla 90`; labeled POLICY; clamped to [0,1] | ✅ |
| C7 — calibration honesty | muRps/holdSec/regionalOutageAvail labeled ESTIMATE/POLICY; procedure = operator load test (not fabricated); sensitivity test (2× muRps ⇒ ½ ρ) | ✅ |
| UI contract + purity + serves | UI-contract test covers simulate + killAZ + killRegion; engine pure; both files HTTP 200 | ✅ |

## Findings raised by independent review — disposition

| Sev | Finding | Disposition |
|---|---|---|
| MEDIUM-1 | killRegion left `cell_count` finite (EKS clusters in the dead region should be down) | **FIXED** — `cellFactor` param; region loss zeroes cell capacity → ρ=∞; killAZ still keeps it finite (correct). Asserted: `every()` component down under region loss. |
| LOW-1 | sort comparator emits NaN on ∞-vs-∞ ties | **FIXED** — `rank(∞)=MAX_VALUE` → NaN-free, stable ordering |
| LOW-2 | plan-promised `killRegion + 0 tenants` boundary test absent | **FIXED** — test added (no NaN, sla=95) |
| LOW-3 | `regionalOutageAvail` unbounded (sla could exceed 100) | **FIXED** — clamped to [0,1]; asserted (1.5 → sla 100) |
| LOW-4 | "HTML HTTP 200" is a manual claim (no automated suite check) | ACCEPTED — the UI-contract tests validate the meaningful field contract; http fetch confirmed by curl (HTTP 200) |

## Independent reviewer's own math cross-check (quoted)

> C1 correct (3604/1802/901/5000 match AWS `LEAST(DBInstanceClassMemory/9531392, 5000)`); C2 linear
> (Little's Law `L=λ·W` legitimate); C3 genuine (capacity gap drives it, robust to holdSec); C4/C6/C7
> correct + honestly labeled; regression clean; ρ=∞ render path crash-free.

## Deliverables (M1)

- `loopzero-capsim.mjs` — engine (+`rdsMaxConns`, `rhoOf` exports, promptly domain, RDS Proxy, region chaos).
- `loopzero-capsim.test.mjs` — **23 tests** (14 M0 + 9 M1).
- `loopzero-bottleneck-simulator.html` — UI (region-chaos button, promptly node, ρ=∞, calibration note).
- `loopzero-bottleneck-simulator-m1-{plan,review}.md`.
