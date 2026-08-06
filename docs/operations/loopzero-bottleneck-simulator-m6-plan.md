---
type: operational-runbook
title: "LoopZero Bottleneck Simulator M6 (operationalize: AWS region-posture decision memo)"
---

# Plan — LoopZero Bottleneck Simulator M6 (operationalize: AWS region-posture decision memo)

> Slug: `loopzero-bottleneck-simulator-m6`. Uses M0-M5. FAANG-level, NO fabricated numbers.
> Stop building tooling; USE it. Produce the AWS re-platform region-posture decision memo (MADR),
> every number sourced from a LIVE `capsim_*` MCP call.

## Goal

Turn the decision-support suite (M0-M5) into an actual decision: a MADR-shaped memo that recommends
single vs sharded vs active-active for LoopZero's AWS re-platform, plus the capacity levers to pull first,
with the cost / blast-radius / SLA / review-score trade-offs quantified from the LIVE simulator.

## Baseline Context — LIVE MCP evidence (gathered in DISCOVER, reproducible)

All numbers below are verbatim `capsim_*` tool outputs (in-session, 2026-07-02).

| Call | Result |
|---|---|
| `capsim_review {tenants:50000}` | score **32 / CRITICAL**; bottlenecks cell_count ρ=1.56 + bff ρ=1.04; single-region SLA 95% |
| `capsim_compare {tenants:50000, compareRegions:3}` | single $72,566/100%/95%/32 · sharded $78,344/33%/98.3%/37 (+$5,778) · active-active $189,478/0%/99.98%/37 (+$116,912) |
| `capsim_compare {tenants:100000, compareRegions:3}` | single $126,403/30 · sharded +$5,778 · active-active +$224,586 (compute replication scales with tenants) |
| `capsim_cost {tenants:50000, prefixDelegation:true}` | $70,317 (−$2,249; 266 nodes) |
| `capsim_cost {tenants:50000, maxPerCell:2000}` | **$41,141 (−$31,425; cells 100→25, nodes 279→129)** |
| `capsim_review {tenants:50000, maxPerCell:2000, prefixDelegation:true}` | score **52 / AT_RISK** (cell_count bottleneck RESOLVED; BFF now top) |
| `capsim_compare {tenants:50000, maxPerCell:2000, prefixDelegation:true, compareRegions:3}` | single $38,892/52 · sharded $44,670/33%/57 (+$5,778) · active-active $88,456/0%/57 (**+$49,564** — levers cut failover cost 2.4×) |

## Decision the data supports (to be argued in the memo)

1. **Pull capacity levers FIRST** (before any multi-region spend): raise `maxPerCell` (500→~2000)
   → −$31,425/mo AND resolves the #1 bottleneck (cell_count ρ 1.56→gone); enable prefix delegation
   → small extra saving + unblocks the ENI pod ceiling. Net $72.6k→$38.9k (−46%), review 32→52.
2. **Then adopt sharded multi-region** (regions≥2, no replication): +$5,778/mo cuts region blast
   radius 100%→33%, SLA 95%→98.3%. Cheap insurance. RECOMMENDED.
3. **Defer active-active** unless an enterprise SLA contract (≥99.9%) demands it: +$49,564/mo
   (post-levers) for 0% blast radius. Revisit trigger documented.
4. **Address the BFF/Engine-API request tier** (review's next bottleneck) — add replicas.

## Tasks
- **T1 — memo (MADR 3.0).** [`docs/architecture/aws-region-posture-decision.md`](../architecture/aws-region-posture-decision.md): context, decision
  drivers, options (single/sharded/active-active) with the LIVE comparison, the lever-ordering
  argument, decision + consequences + risks + revisit triggers + a Reproduction section listing every
  MCP call.
- **T2 — index wiring.** Link the memo from `docs/architecture/index.md`.
- **T3 — REVIEW.** Independent adversarial review: every memo number matches a tool output (no
  fabrication), the recommendation follows from the data, MADR completeness.

## Acceptance Criteria / DoD (all evidenced 100%)
1. Memo exists at [`docs/architecture/aws-region-posture-decision.md`](../architecture/aws-region-posture-decision.md), MADR-shaped (context, drivers,
   considered options, decision outcome, consequences, risks, revisit triggers).
2. **No fabricated number** — every quantitative claim traces to a LIVE `capsim_*` call listed in the
   Reproduction section; an independent reviewer re-runs ≥3 of them and confirms they match.
3. Options compared on objective criteria (cost/mo, region blast radius, SLA, review score,
   cost-of-resilience) in one table sourced from `capsim_compare`.
4. The recommendation is decision-actionable: ordered steps (levers first → sharded → defer aa) with
   the $ deltas that justify each.
5. Revisit triggers are explicit (scale, SLA contract) with the numeric threshold from the data.
6. Memo linked from `docs/architecture/index.md`.
7. No regression: the suite still green (engine 45/45 + MCP 8/8) — the memo cites a stable tool.

## Test Plan
- Evidence: LIVE MCP calls (DISCOVER) are the source; the Reproduction section makes them re-runnable.
- Review: independent agent re-runs a sample of the calls, diffs against the memo, checks MADR shape
  + logical validity of the recommendation.
- Regression: `node --test` on engine + MCP suites (the memo must cite a passing tool).

## Risks / Drawbacks
- The model's estimate-labeled inputs (muRps/holdSec/regionalOutageAvail, traffic quantities) carry
  through to the memo — the memo must flag which conclusions are robust vs estimate-sensitive.
- BFF/Engine replica tuning is not MCP-parametrized — the memo notes it as a finding + tooling gap,
  not a quantified lever.
- A memo is a point-in-time decision; the revisit triggers guard against it going stale.

## Unresolved Questions
- (none blocking) — exposing BFF/Engine replicas + traffic quantities as MCP inputs is a future
  tooling enhancement (would let the memo quantify the request-tier fix); out of M6 scope.
