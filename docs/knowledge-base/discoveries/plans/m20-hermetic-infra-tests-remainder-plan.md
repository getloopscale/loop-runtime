---
type: discovery-plan
title: "Discovery Plan: m20-hermetic-infra-tests-remainder"
---

# Discovery Plan: m20-hermetic-infra-tests-remainder

**Slug:** `m20-hermetic-infra-tests-remainder`
**Created:** 2026-07-10
**Plan version:** 1.0
**Owner:** Claude (Opus 4.7)
**Parent (canonical):** [`knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`](../blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md) (M16 SHIPPABLE 98.3 — same M-Migrate2 pattern, same shape families, same dependency envelope; shipped 2026-07-10 v0.60.0)

<!-- DEFER-CORNER: tests | Deferred to M16 blueprint canonical (identical Go gate hermetic test shape — testing.T + happy/parse-error/real-repo trio). -->
<!-- DEFER-CORNER: deps | Deferred to M16 blueprint canonical (identical envelope: stdlib + yaml.v3; NO new third-party deps). -->
<!-- M20 tools corner NOT deferred — 2 M20-specific tools questions on registry wiring + baseline oracle re-freeze. -->

## Context

M16 blueprint SHIPPABLE 98.3 canonically covered the hermetic-gate migration pattern (10 gates shipped 2026-07-10 v0.60.0). M20 is the **remainder application** of that exact pattern to the 18 remaining hermetic scripts in `infra/tests/`. Parsimony rung 1 (YAGNI): no new pattern research needed.

## Objective

Identify the shape-family mapping for the 18 M20 targets so `/to-plan` can group them into phases (Wave 1 syntax → Wave 2 helm-template → Wave 3 file-layout), mirror of M16 Wave 1+2 delivery order.

**Success:** every M20 target mapped to a shape family + a canonical M16 reference implementation.

## In-scope / Out-of-scope

**In-scope:** shape-family mapping for 18 M20 targets (17 `check-*_test.sh` + 1 `helm/*_test.sh`).

**Out-of-scope:**
- Test/deps/tools canonical shape → deferred to M16 blueprint via DEFER-CORNER markers.
- Wave 3 semantics (M16 Wave 3 was different scope — bash-target pairs).
- LIVE scripts (`infra/tests/e2e/*.sh`) → KEEP-BY-DESIGN per triplet contract.

## ADRs

### D1 — Reuse M16 blueprint as canonical shape source (Rule 9 no reinvent)

**Status:** Accepted. **Context:** M16 blueprint SHIPPABLE 98.3 exhaustively covered corners 1-4. Redoing = YAGNI. **Decision:** M20 discover reuses M16 findings as-is; this plan only maps the 18 M20 targets to M16 shape families. **Alternatives rejected:** Fresh 8-question research (violates Rule 9); shape-per-target investigation (would duplicate M16 Q1-Q6).

### D2 — Question budget = 3 questions, all in Corner 4 (Techniques)

**Status:** Accepted. **Context:** Only new research direction is M20-specific target mapping. **Decision:** 3 techniques questions Q1-Q3 covering: shape mapping table, similar-M16-gate reference per target, velocity split-decision anchor. Corners 1-3 deferred via markers. **Alternatives rejected:** 5+ questions across corners (duplicates M16, violates parsimony rung 5).

## Research Questions

Total: 3 questions, all in Corner 4 (Techniques). Corners 1-3 explicitly deferred to M16 blueprint via DEFER-CORNER markers at top of file.

| Q# | Question summary | Corner | Fase A (static method) | Fase B (LIVE probe) |
|---|---|---|---|---|
| Q1 | Map each of the 18 M20 targets to an M16 shape family | techniques | Read each bash source (~30-100 LoC each) + classify: (a) `bash -n` only (syntax family — M16 T2.1 sprintsyntax pattern), (b) `helm template \| grep/jq` (helm-template family — M16 T3.1-T3.7 pattern), (c) file-layout `filepath.WalkDir + regex` (M16 T2.2 archivescriptrepo pattern) | SKIP (source-static) |
| Q2 | For each target, identify the canonical M16 reference gate to inherit shape from | techniques | Cross-reference Q1 shape → M16 gate name (already shipped, code-inspectable in `cmd/loopzero-ops/internal/quality/<name>/check.go`) | SKIP (source-reuse) |
| Q3 | Where to place the velocity split-decision anchor (mid-milestone D4 protocol per grill Q4 R2) | techniques | Empirical analysis: which 8 of the 18 targets have highest velocity confidence (fewest complex asserts). Grouping proposal: Wave A = 4 syntax + 4 simple helm; Wave B = 6 vault-domain helm; Wave C = 4 residual | SKIP |
| Q4 | How to register 18 new gates in `quality_registry.go` without triggering `TestRegisteredRunFuncMatchesName` false-positive drift | tools | Read `cmd/loopzero-ops/quality_registry_test.go` line 195 assertion + the pre-existing failure list (8 legacy gates already failing). Establish naming convention: `run<CamelCase>` matches gate-name kebab-case. | SKIP |
| Q5 | Whether to re-freeze `TestOracle_MatchesFrozenBaseline` as part of M20 DoD (baseline was 73 lines, current 104, +18 = 122) | tools | Read `cmd/loopzero-ops/quality_registry_test.go` TestOracle_MatchesFrozenBaseline logic + decide: (a) re-freeze once at M20 close, or (b) leave stale as accepted debt with M21 cleanup ADR | SKIP |

**Coverage:** 100% — every question has an executable static method. Time budget: 20 + 15 + 15 + 15 + 10 = 75 min.

## Coverage Matrix

| Corner | Questions | Status |
|---|---|---|
| tests | (deferred) | DEFER — M16 blueprint canonical |
| deps | (deferred) | DEFER — M16 blueprint canonical |
| tools | Q4, Q5 | 2 — registry wiring + baseline oracle |
| techniques | Q1, Q2, Q3 | 3 — M20-specific target mapping |

## Halt-loop Checkpoints

- Checkpoint 1: 18 targets classified in Q1 table.
- Checkpoint 2: Q2 cross-reference cites real M16 gate for each shape family.
- Checkpoint 3: Q3 Wave A/B/C grouping proposed with 8-gate boundary at Wave A close.

## Acceptance Criteria

- Blueprint answers 3 questions with concrete M16 canonical references.
- Every M20 target mapped to M16 shape + reference gate.
- ≥ 1 ADR added to blueprint documenting the reuse strategy.
- SHIPPABLE per `rules/discover-blueprint-golden-rule.md`.

## Global Definition of Done

- [ ] `rules/discover-blueprint-golden-rule.md` hard caps satisfied.
- [ ] `/discover-confidence` verdict ≥ SHIPPABLE_WITH_CAVEATS.

## Cross-references

- M16 blueprint (canonical parent): [`knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`](../blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md)
- M16 shipped gates (reference source): `cmd/loopzero-ops/internal/quality/{sprintsyntax,archivescriptrepo,nocloud,lokivaultretention,lokiretentionschema,vaultpromalerts,day30day60cronjobs,esochartsanity,esocreationpolicy,openbaoinitauth}/check.go`
- Grill: [`knowledge-base/grills/m20-hermetic-infra-tests-remainder-feature-grill.md`](../../grills/m20-hermetic-infra-tests-remainder-feature-grill.md)
- Cycle rule: `.claude/rules/cycle-discover.md`
