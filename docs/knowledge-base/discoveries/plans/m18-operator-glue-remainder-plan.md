# Discovery Plan: m18-operator-glue-remainder

**Slug:** `m18-operator-glue-remainder`
**Created:** 2026-07-10
**Plan version:** 1.0
**Owner:** Claude (Opus 4.7)
**Ancestor:** `knowledge-base/discoveries/blueprints/operator-glue-go-migration-final-blueprint.md` v1.0 SHIPPABLE 98.8 (already covered 121-entry classification)

<!-- DEFER-CORNER: tests | Deferred to M15 blueprint Q1-Q2 canonical (identical Go seed shape + hermetic test pattern; no new investigation needed). Ancestor cross-reference at bottom of plan. -->
<!-- DEFER-CORNER: deps | Deferred to M15 blueprint Q3-Q4 canonical (identical dep envelope: stdlib + pgx/v5 + yaml.v3 + client-go; ADR-M15-D5 no new third-party deps). -->
<!-- M18 tools corner NOT deferred — 2 new questions on Ginkgo/Pulumi migration tooling specific to Wave B remainder. -->

## Context

M15 delivered Wave A in v0.61.0 (allowlist 121 → 120). M18 was created 2026-07-10 to track Waves B (12 mechanical migrations) + C (~30 REVIEW-NEEDED) + D (~30 aggressive KEEP-OPERATOR re-audit) per plan v1.2 ADR-D6 empirical LIVE-cluster boundary + sponsor Path A choice.

**Empirical constraint (from M15 blueprint):**
- Wave B strangler requires per-file `--verify` GREEN em Go LIVE BEFORE `git rm` bash (ADR-D2).
- LIVE validation requires Cleanroom TOTAL + UP + sponsor JUNTOS presencial per `rules/cleanroom-up-live-test-mandate.md` INQUEBRÁVEL.
- Wave D requires sponsor re-authorization of Phase-5 ledger 2026-05-23 (which ratified KEEP-OPERATOR entries as legitimate).

## Objective

Identify **which subset of Waves B+C+D is hermetically-verifiable** (does NOT require LIVE cluster) so M18 can deliver honest slice in v0.62.0 without violating INQUEBRÁVEL mandates. The remainder that requires LIVE+JUNTOS becomes M18 residual (handled via sponsor AskUserQuestion pattern if session ends without JUNTOS).

**Success criterion:** every research question has a method + expected answer shape + reachable citation.

## In-scope / Out-of-scope

**In scope:**
- Re-examine M15 blueprint Q7 classification (121 entries) for hermetic-vs-LIVE boundary per entry.
- Reference implementation review: M16 hermetic gates (v0.60.0 shipped 10 gates same-day, all hermetic) — proven hermetic-only migration pattern.
- Ginkgo hermetic scaffolding pattern (Ginkgo compiles + hermetic assertions work without cluster).

**Out of scope:**
- LIVE-cluster validation (blocked by INQUEBRÁVEL mandate).
- Sponsor Phase-5 ledger re-authorization (blocked by absent JUNTOS).
- New reference project introduction (blueprint already exhausted M15 references).

## ADRs

### D1 — Reuse M15 blueprint as canonical classification; discover-plan for M18 focuses on hermeticity boundary

**Status:** Accepted.

**Context:** M15 blueprint SHIPPABLE 98.8 already answered 8 questions across 4 corners with 121-entry classification. Redoing that is scope creep (YAGNI, parsimony rung 1).

**Decision:** M18 discover reuses M15 blueprint findings as canonical. This discover-plan focuses on ONE new research direction: identify hermetically-verifiable subset within Waves B+C+D.

**Alternatives rejected:**
- Fresh discovery from scratch — YAGNI violation.
- Skip discovery entirely — violates cycle-discover contract (blueprint needed for M18 plan).

### D2 — Question budget: 5 questions, all in Corner 4 (Techniques); Corners 1-3 deferred via DEFER-CORNER markers

**Status:** Accepted.

**Context:** M15 blueprint covered Corners 1 (Tests) + 2 (Deps) + 3 (Tools) canonically. M18 needs only new technique classification per hermeticity.

**Decision:** Budget = 3 questions, all Corner 4. Corners 1-3 explicitly deferred to M15 blueprint via cross-reference. ADR justifies the "empty corner" per `rules/discover-plan-golden-rule.md § 4`.

**Alternatives rejected:**
- 5-8 questions with cross-corner spread — duplicates M15 blueprint (Rule 3 violation).

## Research Questions

Total: 3 questions, all in Corner 4 (Techniques). Corners 1-3 explicitly deferred to M15 blueprint via DEFER-CORNER markers at top of file.

| Q# | Question summary | Corner | Fase A (static method) | Fase B (LIVE probe) |
|---|---|---|---|---|
| Q1 | Which of the 12 Wave B files can migrate hermetically (no cluster) | techniques | Per-file classification against 2 criteria: (a) does migration touch live cluster? (b) can Go test hermetically prove parity? | SKIP (source-static) |
| Q2 | What is the canonical hermetic Wave B pattern (referenceable from M16 shipped 2026-07-10) | techniques | Read `cmd/loopzero-ops/internal/quality/sprintsyntax/check.go` + `nocloud/check.go` — extract subprocess+assertion pattern | SKIP (M16 reference is hermetic-only by design) |
| Q3 | Which Wave C REVIEW-NEEDED files have clear hermetic-vs-LIVE boundary (sample of 5-10) | techniques | Sample 5-10 REVIEW-NEEDED allowlist entries. Read each source. Classify hermetic vs LIVE. | SKIP |
| Q4 | What Ginkgo hermetic skip pattern lets `dogfood/*.sh` migrate without LIVE cluster requirement | tools | Read `infra/tests/e2e/deploy_suite_test.go` BeforeSuite fixture; identify `LOOPZERO_E2E_LIVE=0` skip mechanism | SKIP (hermetic reference) |
| Q5 | Is `pulumi/sdk-v3` already in `loopzero/go.mod` (shim-first ADR-M15-D5 pre-req) | tools | `grep pulumi/sdk-v3 go.mod` from repo root | SKIP |
**Coverage:** 100% — every question has an executable static method. Time budget: 30 + 20 + 40 = 90 min. Max 3 per corner respected. Corners 1-3 deferred via DEFER-CORNER markers (see top of file + ADR-D2).

Additional research topics folded into Q1/Q2/Q3 rather than separate questions per corner-max-3 rule:
- Ginkgo migration pattern (M15 Wave B T3.4 target) → folded into Q1 hermetic classification + Q2 pattern extraction.
- Pulumi SDK caller pattern (M15 Wave B T3.5 target, ADR-M15-D5 shim-first) → folded into Q1 hermetic classification.

## Coverage Matrix

| Corner | Questions | Status |
|---|---|---|
| tests | (deferred via marker) | DEFER — M15 blueprint Q1-Q2 canonical |
| deps | (deferred via marker) | DEFER — M15 blueprint Q3-Q4 canonical |
| tools | Q4, Q5 | 2 — Ginkgo skip + Pulumi SDK dep verification |
| techniques | Q1, Q2, Q3 | 3 — hermetic classification + M16 pattern + Wave C boundary |

## Halt-loop checkpoints

- Checkpoint 1: All 3 questions have answers with citations. Method: post-blueprint audit.
- Checkpoint 2: Q1 answer identifies ≥ 2 hermetic Wave B targets. Method: table row-count.
- Checkpoint 3: Q3 sample classification cites file:line for each of ≥ 5 entries. Method: verify grep.

## Acceptance criteria

- Blueprint answers 3 questions with hermetic-vs-LIVE classification for Wave B + sampled Wave C.
- Every citation reaches a real file/section.
- ≥ 1 ADR added to blueprint documenting the hermetic slice scope.
- SHIPPABLE per `rules/discover-blueprint-golden-rule.md`.

## Global Definition of Done

- [ ] `rules/discover-blueprint-golden-rule.md` hard caps satisfied (no fabricated citation; all 4 corners populated OR ADR-justified deferral).
- [ ] `/discover-confidence` verdict ≥ SHIPPABLE_WITH_CAVEATS.

## Cross-references

- M15 blueprint (canonical Corners 1-3): `knowledge-base/discoveries/blueprints/operator-glue-go-migration-final-blueprint.md`
- M15 plan v1.2 (ADR-D6 scope boundary): `knowledge-base/plans/operator-glue-go-migration-final-plan.md`
- M16 hermetic gate reference: `cmd/loopzero-ops/internal/quality/sprintsyntax/check.go`
- Cycle rule: `.claude/rules/cycle-discover.md`
- LIVE mandate: `.claude/rules/cleanroom-up-live-test-mandate.md`
