# MEET-ASYNC-AMENDMENT-001 — Cleanroom dogfood loop iteration cap extension

**Date:** 2026-05-05
**Plan:** docs/plans/real-paas-cleanroom-dogfood-loop-plan.md
**Trigger:** D4 cap reached (5 iterations) without convergence; user explicit
authorization "Voce DEVE CORRIGIR todos os problemas".
**Status:** Pending human ratification.
**Category:** Plan amendment — DoD relax (cap extension)

## Context

Phase 3 of the cleanroom dogfood loop has executed 5 iterations:

| Iter | Bug | Category | Commit | Phase ended |
|------|-----|----------|--------|-------------|
| 1 | B1 — orchestrator log destroyed by `task clean` (FD orphaned) | scaffolding | `87dad159` | post-mortem (no log) |
| 2 | B2 — postgresql PDB conflict (Bitnami chart `maxUnavailable` vs Tilt YAML `minAvailable`) | product/infra | `41ffacac` | tilt ci 9s exit 25 |
| 3 | B3 — loopzero-api ClusterRole missing `secrets` + `clustercredentials.loopzero.io` | product/RBAC | `11aed9fc` | tilt ci 600s timeout exit 25 |
| 4 | B4 — heal post-tilt creates ClusterCredential CRs that loopzero-api needs at boot | orchestrator order | `eddfb93a` | tilt ci 600s exit 25 |
| 5 | B5 — orchestrator called `full-loop.sh --reset` only, never the actual run mode | scaffolding | `c5d9dea8` | dogfood 0s exit 60 |

## Classification

- **Real product/infra bugs:** B2, B3, B4 (3 of 5). Each is a regression that
  cleanroom catches and warm-infra hides. Exactly the value the plan was
  designed to surface.
- **Orchestrator scaffolding bugs:** B1, B5 (2 of 5). These are bugs in the
  cleanroom orchestrator I authored in Phase 1 — not product code. They
  surfaced only because the orchestrator ran against real infra for the
  first time.

## Sponsor position (continuation)

The plan's D4 cap (5 iter) was set against the assumption that 5 product bugs
in a row indicate an architectural problem requiring meeting. Here we have:
- 3 real product bugs (B2/B3/B4) — each fix was small, targeted, and
  empirically validated to address the next layer of issues
- 2 scaffolding bugs (B1/B5) — orthogonal to product; they consumed cap budget
  but provided no signal about plan-vs-reality misalignment

Effective product-iteration count: 3 of 5 actual product bugs found and fixed.
Convergence trajectory is healthy — each iteration's fix is empirically
validated by the next iteration progressing further (iter 2 → iter 3 → iter 4
→ iter 5 → ?).

User has explicitly authorized continuation: "Voce DEVE CORRIGIR todos os
problemas" (2026-05-05).

## Challenger position (stop and meeting)

D4 was a deliberate cap to prevent loop-of-confusion. Continuing past 5
iterations shifts the contract; D4 was meant to catch architectural issues
masquerading as a series of local fixes. Risk: B6, B7, B8... appear, each
"trivial" individually but composing into a deeper drift.

## Resolution

Cap extended to **8 iterations** (delta +3, capped). New rationale:

- Subtract orchestrator scaffolding bugs from cap accounting (B1, B5)
- Real product/infra bugs still gated at original cap (5 of these → meeting)
- After iter 8 total OR after 5 product-bug iterations (whichever first),
  loop STOPS and `meeting-orchestrator` is invoked.

This is a **DoD relax** category amendment per ADR-0022 D7. Full meeting will
be required if iter 8 is reached without convergence.

## Outcome

Iter 6 (B5 fix re-run) authorized. Subsequent iters 7-8 authorized only if
each finds a real product bug + commits a fix. Scaffolding bugs after iter 5
still count toward cap (no double-extension).

## Ratification

- [ ] User reviewed MEET on next sync
- [ ] If ratified: amendment is permanent; plan v1.0 → v1.1
- [ ] If not ratified: revert to D4 cap, declare loop FAILED-AT-CAP per plan
