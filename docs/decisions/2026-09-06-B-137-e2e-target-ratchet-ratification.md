# ADR: Test-pyramid e2e band — ratify the ratchet, amend ADR D7

**Status:** ACCEPTED (ratified by sponsor 2026-09-06 via chat "TUDO APROVADOS", Option A)
**Date:** 2026-09-06
**Item:** B-137
**Change Tier:** T2 (quality gate modification + ADR amendment)
**Amends:** `docs/plans/completed/2026-05-21-comprehensive-quality-gates-plan.md § D7`
**Evidence:** `.claude/records/alignment/b137-e2e-test-strategy-alignment.md`

## Context

ADR D7 (2026-05-21) declared a three-band test-pyramid target: `unit ≥ 70% / integração ≤ 25% / e2e ≤ 5%`. Measured 2026-08-17 when the pyramid was DERIVED from the tree for the first time (`B-106`), the actual composition was **989 files — unit 73.5% / integração 15.7% / e2e 10.8%**. Re-measured 2026-09-06: **1110 files — unit 76.1% / integração 14.2% / e2e 9.6%**. Trajectory across four measurements: **10.8 → 10.4 → 10.2 → 9.6** — a monotonic 1.2-point decline in 20 days with **no active intervention**; unit-band growth naturally dilutes e2e.

Two facts about the original target:

1. **Calibrated against fiction.** D7 was written when `infra/quality/test-inventory.json` DECLARED 0.1% e2e over 375 of 988 files (62% blind — the entire `infra/tests/e2e/` suite was excluded from the sample). The number the target was designed against was not the tree.
2. **Compares incomparable quantities.** The "e2e" band aggregates four subtrees on two semantic axes: journey (`e2e/` 51, `livetest/` 39 = 90 files) and stimulus (`chaos/` 7, `performance/` 23 = 30 files). Journey-only fraction: 8.1%.

**Product context:** LoopZero's primary object IS deploying user code. The user-visible journey (`loopzero deploy` → build → release → healthy pod → URL) traverses four planes; every step is I/O across a plane boundary. Under-weighting the tests that exercise that promise (target ≤ 5%) treats deploy as peripheral; the product treats it as the concern.

## Decision

**Ratify the current ratchet trajectory for the e2e band and retire the ≤5% target for e2e.** The ratchet — baseline frozen at 10.8%, only-improve semantics with 0.5-point tolerance — is the canonical enforcement mechanism. The unit ≥ 70% and integração ≤ 25% bounds of ADR D7 remain in force and remain reported as aspiration gaps.

Concretely:

- `cmd/loopzero-ops/internal/quality/testpyramid/derived.go` no longer emits an `AspirationGap` for e2e; the code carries a comment citing this ADR. The unit and integração aspiration gaps are unchanged.
- `infra/quality/test-pyramid-baseline.json` `note` is updated to reference this ADR.
- ADR D7 is amended by this file: `e2e ≤ 5%` is retired as a hard aspiration; the ratchet is the enforcement.

## Options considered

### Option A — Ratify + amend (CHOSEN)

Amend D7 for the e2e axis; silence the e2e aspiration gap. **Cost:** 1 ADR + ~6 LoC gate change + baseline note edit. **Risk:** future contributors reading generic "e2e ≤ 5%" references may think LoopZero is out of compliance; mitigated by the ratchet-plus-ADR citation in code.

### Option B — Refactor to a moved target (~7.4%)

Amend D7 target to `e2e-journey ≤ 7.5%`, refactor gate to a 4-band decomposition removing chaos+performance from the e2e numerator. **Cost:** ADR + ~50-80 LoC gate refactor + baseline re-freeze. **Rejected because:** the ratchet already achieves what a 7.5% target would achieve, in the same timeframe (~14 months to 5% by unit dilution alone), without touching code.

### Option C — Partition the metric (scope vs stimulus)

Amend D7 to partition into two verdicts (scope pyramid vs stimulus counts). **Cost:** ADR + ~120-180 LoC refactor + baseline schema migration. **Rejected because:** the semantic clarity gain does not justify the largest fatia of the three when the ratchet is already delivering the outcome; the aggregation question is registered for future consideration if the sponsor's read changes.

## Consequences

- **(+)** The DoD bullet 3 of B-137 closes: the distance stops being reported-and-not-blocking (it stops being reported at all for e2e).
- **(+)** B-127 (`HARD` refusal) and B-119 (enforcement total) evidence blocks — both shipped independently but citing the same tension — now have a canonical decision to point at for the classe.
- **(+)** The ~5-7 points per trimester ratchet trajectory is codified as the enforcement, not as a temporary posture. Reversal cost: one ADR + one gate change.
- **(−)** The test-pyramid heuristic becomes LoopZero-specific for the e2e axis; a future contributor inheriting the codebase without the amendment context may misread the target. Mitigated by the citation in `derived.go`.
- **(−)** The aggregation defect (chaos+performance under e2e band) is not fixed — deferred to a future item if the sponsor's read changes.

## Verification

- `go test ./cmd/loopzero-ops/internal/quality/testpyramid/...` — 7 tests PASS including new `TestE2EAspirationGapIsSilencedAfterADRAmendment`.
- `loopzero-ops quality test-pyramid` output no longer contains `e2e … > alvo … (ADR D7)`; the ratchet verdict is unchanged.
- Baseline note references this ADR path.

## Related

- Alignment brief: `.claude/records/alignment/b137-e2e-test-strategy-alignment.md`
- Original D7: `docs/plans/completed/2026-05-21-comprehensive-quality-gates-plan.md § D7`
- Backlog block: `BACKLOG.md § B-137`
- Sibling shipped items citing the same tension: `B-127`, `B-119`
