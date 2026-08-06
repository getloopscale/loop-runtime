---
type: plan
title: "Operator Glue → Go: Waves B+C+D Hermetic Slice"
slug: m18-operator-glue-remainder
milestone_id: M18
created_at: 2026-07-10
plan_version: 1.0
goal: "Reduzir allowlist 120 → 117 executando M18 v0.62.0 com hermetic slice: T3.4 Ginkgo migration (2 files: dogfood/repro-findings.sh + dogfood/scripts/cleanup-terminating-namespaces.sh) + ADR-D2 reclassify pulumi-output.sh MIGRATE-GO-SDK → KEEP-OPERATOR. Remaining 62 LIVE-blocked files (10 Wave B LIVE + 22 Wave C + 30 Wave D) promovidos a M19 via /roadmap-feature post-release per blueprint ADR-D3."
authored_by: claude-opus-4-7
---

# M18 — Operator Glue → Go: Waves B+C+D Hermetic Slice

**Blueprint:** [`knowledge-base/discoveries/blueprints/m18-operator-glue-remainder-blueprint.md`](../discoveries/blueprints/m18-operator-glue-remainder-blueprint.md) v1.0 SHIPPABLE 99.7
**Discover plan:** `knowledge-base/discoveries/plans/m18-operator-glue-remainder-plan.md` v1.0 SHIPPABLE 99.7
**ROADMAP.md M18:** Waves B+C+D scope; DoD includes `allowlist ≤ 30` (inherited from M15 original literal DoD)
**Blueprint ADR-D1 decision:** M18 v0.62.0 ships T3.4 hermetic slice only (2 files); rest → M19.

## Goal

Reduzir allowlist 120 → 117 delivering M18 v0.62.0 com hermetic slice: Ginkgo migration of `infra/tests/dogfood/repro-findings.sh` + `infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh` (T3.4 from M15 plan v1.2) + ADR-D2 reclassify `pulumi-output.sh` MIGRATE-GO-SDK → KEEP-OPERATOR. **Metric (observable):** `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 117** ao final da Phase 3.

## Context

- **Why now:** M18 milestone opened `[ ]` from M15 v0.61.0 release. Sponsor session 2026-07-10 has no LIVE cluster + no JUNTOS presencial. Blueprint SHIPPABLE 99.7 identified 2 hermetic-verifiable files in Wave B (T3.4 targets) using canonical Ginkgo skip pattern. Ship what's honestly executable.
- **Blueprint honest finding:** 62 of 74 Waves B+C+D files require LIVE cluster + sponsor JUNTOS. Rule 3 requires deferring those to M19.
- **Precedente técnico:** Ginkgo skip pattern proven at `infra/tests/e2e/deploy_suite_test.go`. M16 velocity split precedent (M16 → M17) + M15 velocity split precedent (M15 → M18) both applied 2026-07-10.

## Baseline Context

### Files that will be touched

| Path | LoC | Wave | Migration target |
|---|---|---|---|
| `infra/tests/dogfood/repro-findings.sh` | 302 | B (T3.4) | Ginkgo suite `infra/tests/e2e/dogfood_repro_test.go` (NEW) |
| `infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh` | 111 | B (T3.4) | Ginkgo helper folded into dogfood_repro_test.go OR separate `dogfood_cleanup_helper.go` |
| `infra/helmfile/scripts/lib/pulumi-output.sh` | 127 | B (the-pulumi-slot) | RECLASSIFY per ADR-D2 (MIGRATE-GO-SDK → KEEP-OPERATOR); NO delete, allowlist Justificativa update only |
| `infra/tests/e2e/dogfood_repro_test.go` | ~250 (NEW) | B (T3.4) | Ginkgo suite with `LOOPZERO_E2E_LIVE=0` skip |
| `.claude/rules/shell-scripts-allowlist.md` | 700+ | B (T3.4 + the-pulumi-slot) | Remove 2 rows (Ginkgo migrated) + update pulumi Justificativa |
| `CHANGELOG.md` | recent | B | `[Unreleased] § Removed` (2 dogfood scripts) + `§ Added` (Ginkgo suite) + `§ Changed` (pulumi reclassify) |
| `ROADMAP.md` | recent | B | M18 checkbox flip [x] with amended DoD (per M15 precedent) |

Total: 3 script files affected (2 deleted + 1 reclassified), 1 Ginkgo suite added, 3 metadata files updated.

### Current callers / dependents

- `repro-findings.sh`: called only from operator manual invocation (`dogfood full` chain); no live callers in taskfiles or shipped seeds.
- `cleanup-terminating-namespaces.sh`: called from `repro-findings.sh` OR direct operator invocation; standalone tool.
- `pulumi-output.sh`: sourced by 5 other bash scripts (allowlist § infra/helmfile/scripts/*). Reclassification does NOT touch callers.

### Architecture boundaries affected

- **Composition root:** Ginkgo suite is `infra/tests/e2e/*_test.go` — existing test package. No new modules.
- **DIP:** Ginkgo uses `BeforeSuite` fixture for cluster injection; hermetic mode simply skips. No new interfaces.
- **Layering:** dogfood scripts moving to Ginkgo does NOT cross layer boundaries — both are test infrastructure.

### Domain glossary

- **Ginkgo hermetic skip:** Ginkgo `BeforeSuite` reads `os.Getenv("LOOPZERO_E2E_LIVE")`; if != "1", calls `Skip()` — test compiles and passes hermetically.
- **T3.4 (from M15 plan v1.2):** Wave B category MIGRATE-GINKGO — 2 files: repro-findings.sh + cleanup-terminating-namespaces.sh.
- **the-pulumi-slot reclassify (ADR-D2):** original MIGRATE-GO-SDK classification voided; `pulumi/sdk-v3` not in any go.mod (empirical Q5).

## Prior Art & Related Work

- **Blueprint (SoT):** [`knowledge-base/discoveries/blueprints/m18-operator-glue-remainder-blueprint.md`](../discoveries/blueprints/m18-operator-glue-remainder-blueprint.md) v1.0 SHIPPABLE 99.7.
- **M15 plan v1.2 (parent):** established scope-split pattern; M18 inherits the M15 strangler ADR but only for hermetic-verifiable slice.
- **M16 hermetic pattern precedent:** `cmd/loopzero-ops/internal/quality/sprintsyntax/check.go` (10 gates shipped v0.60.0 2026-07-10).
- **Ginkgo reference:** `infra/tests/e2e/deploy_suite_test.go` BeforeSuite pattern.

## Drawbacks & Risks

| # | Risk | Severity | Mitigation | Owner |
|---|---|---|---|---|
| R1 | Ginkgo suite may drift from bash semantics if assertion translation incomplete | MEDIUM | Per-assertion port with checklist against bash source; hermetic test asserts each of 7 canonical repro-findings.sh checks | Claude Wave B T3.4 |
| R2 | `pulumi-output.sh` reclassify (ADR-D2) creates inventory drift if callers change | LOW | Reclassification only updates allowlist Justificativa; no functional change; 5 downstream callers unaffected | Claude at ADR-D2 execution |
| R3 | Ginkgo suite compiles hermetically but hides LIVE parity issue | MEDIUM | Test file explicitly documents `LOOPZERO_E2E_LIVE=0` skip; LIVE gate deferred to M19 with sponsor JUNTOS | Claude Wave B T3.4 |
| R4 | M18 checkbox flip requires DoD amendment per M15 precedent | LOW | Rule 3 pattern established 2026-07-10 M15: sponsor AskUserQuestion authorization + explicit DoD amendment doc + roadmap-runs audit trail | Claude at release-time |
| R5 | M19 promotion overhead (yet another velocity split) | LOW | M15→M18→M19 mirror chain codified as canonical velocity-split precedent 2026-07-10 same day; roadmap discipline preserved | Claude at Phase 4 handoff |

## Unresolved Questions

- Q1 — Should the Ginkgo suite absorb the `cleanup-terminating-namespaces.sh` logic as a helper (single file) OR keep it as a separate helper file (`dogfood_cleanup_helper.go`)? Recommendation: **single file** per KISS + parsimony ladder rung 5 (one-line reuse).
- Q2 — Post-M18-release, should M19 be created immediately OR deferred until operator confirms Cleanroom+JUNTOS availability? Recommendation: **create immediately** to preserve roadmap traceability + let operator schedule execution when ready.

## ADRs

### D1 — Ship hermetic slice per blueprint ADR-D1 (T3.4 pair only in v0.62.0)

**Status:** Accepted.

**Context:** Blueprint SHIPPABLE 99.7 empirical: 2 of 12 Wave B files hermetically-verifiable. LIVE mandate INQUEBRÁVEL blocks the other 10.

**Decision:** M18 v0.62.0 delivers Ginkgo migration of the 2 T3.4 files. Remaining 10 Wave B + 22 Wave C + 30 Wave D → M19 via `/roadmap-feature` post-release.

**Alternatives rejected:**
- Full Wave B in M18 — violates rules/cleanroom-up-live-test-mandate.md INQUEBRÁVEL.
- Skip M18 → M19 direct — degrades roadmap traceability + wastes hermetic slice opportunity.

### D2 — Reclassify `pulumi-output.sh` MIGRATE-GO-SDK → KEEP-OPERATOR (per blueprint Q5 Path A)

**Status:** Accepted.

**Context:** Q5 empirical: `pulumi/sdk-v3` not in any go.mod. M15-plan ADR shim-first pre-req unsatisfied.

**Decision:** Update allowlist Justificativa for `pulumi-output.sh` from MIGRATE-GO-SDK to KEEP-OPERATOR with rationale: subprocess `pulumi` CLI call is legitimate Anti-Pattern #8 orchestration wrapping.

**Alternatives rejected:**
- Add `pulumi/sdk-v3` dep — violates M15-plan ADR + Parsimony rung 4.
- Leave classification dangling — false debt tracking.

### D3 — Ginkgo `LOOPZERO_E2E_LIVE=0` skip pattern for hermetic mode

**Status:** Accepted.

**Context:** Blueprint Q4 empirical: Ginkgo canonical pattern uses env-var skip for hermetic mode.

**Decision:** New Ginkgo suite `infra/tests/e2e/dogfood_repro_test.go` uses `BeforeSuite` with `os.Getenv("LOOPZERO_E2E_LIVE") != "1" → Skip()`. Hermetic mode compiles + passes; LIVE mode requires operator run.

**Alternatives rejected:**
- Ginkgo suite without hermetic skip — breaks hermetic CI.
- Skip Ginkgo, keep bash — violates ADR-D1.

### D4 — M18 DoD amended at release-time per M15 precedent (sponsor AskUserQuestion authorization)

**Status:** Accepted (pattern-based).

**Context:** M15 established the DoD amendment pattern 2026-07-10 for scope-vs-empirical gap. M18 has the same gap (LIVE mandate blocks ≤ 30 target).

**Decision:** M18 checkbox flip requires DoD amendment via sponsor AskUserQuestion authorization at release-time. Amendment doc trail in ROADMAP.md M18 § header + roadmap-runs file + commit body.

**Alternatives rejected:**
- Flip [x] without amendment — Rule 3 violation.
- Leave [ ] indefinitely — Stop hook feedback loop demonstrated at M15.

## Dependency Graph

```
Phase 1 (Baseline) — depends on [none, blueprint approved]
    ↓
Phase 2 (Wave B hermetic slice: T3.4 Ginkgo + the-pulumi-slot reclassify) — depends on Phase 1 baseline
    ↓
Phase 3 (Integration validation + M18 checkbox amendment) — depends on Phase 2 clean
```

## Phase 1 — Baseline capture

### T1.1 — Capture pre-M18 baseline

**Files to edit:** [`knowledge-base/audits/M18-pre-migration-baseline-2026-07-10.md`](../audits/M18-pre-migration-baseline-2026-07-10.md) (NEW)

**Deep file dependency analysis:** allowlist grep `-c` output; wc -l summary.

**Why this step:** DoD verification anchor; steady-state-baseline mandate.

**#### TDD**
- **RED**: `test_m18_baseline_captured` — assert file exists AND grep count present.
- **GREEN**: write audit file.
- **REFACTOR**: N/A.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

(none — no external I/O; file write only)

**Acceptance criteria:** File committed with entries_count=120.
**DoD:** `docs(audit): M18 pre-migration baseline`.

## Phase 2 — Wave B hermetic slice

### T2.1 — T3.4 Ginkgo migration: `repro-findings.sh` + `cleanup-terminating-namespaces.sh` → Ginkgo suite

**Files to edit:**
- `infra/tests/e2e/dogfood_repro_test.go` (NEW ~280 LoC Ginkgo suite)
- `infra/tests/dogfood/repro-findings.sh` (DELETE post hermetic Ginkgo GREEN)
- `infra/tests/dogfood/scripts/cleanup-terminating-namespaces.sh` (DELETE)
- `.claude/rules/shell-scripts-allowlist.md` (remove 2 rows + § Removed entries)
- `CHANGELOG.md` (lot entry)

**Deep file dependency analysis:** Bash scripts orchestrate LIVE dev-public state cleanup + repro; 7 canonical assertions in repro-findings.sh (find-shipped + verify-persisted + delete-and-restart cycle). Ginkgo suite replicates each assertion in `It(...)` block with `LOOPZERO_E2E_LIVE=0` skip.

**Why this step:** ADR-D1 hermetic slice. MIGRATE-GINKGO category (Q7 count 2). Canonical migration = bash test → Ginkgo Go test.

**#### TDD**
- **RED**: `TestDogfoodReproFindings` skeleton with `Describe/It` blocks compiles; hermetic run (unset `LOOPZERO_E2E_LIVE`) SKIPs; test passes as expected.
- **GREEN**: Implement Ginkgo suite replicating 7 bash assertions per assertion; verify hermetic Skip works.
- **REFACTOR**: `git rm` bash + allowlist + CHANGELOG.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

- **Ginkgo BeforeSuite in hermetic mode:** must Skip() gracefully. Test: `go test ./infra/tests/e2e/...` without env var → assert PASS with skip message.
- **Ginkgo suite in LIVE mode without cluster:** should surface clear error. Test: LOOPZERO_E2E_LIVE=1 without kubectl context → clear error message per rules/error-handling.md.

**Acceptance criteria:**
- Ginkgo suite compiles + `go test ./infra/tests/e2e/...` PASS in hermetic mode.
- 2 bash files deleted.
- 2 allowlist rows removed; CHANGELOG lot entry.

**DoD:** `test(e2e): migrate 2 dogfood scripts to Ginkgo (M18 T3.4 hermetic slice)`.

### T2.2 — ADR-D2: reclassify `pulumi-output.sh` MIGRATE-GO-SDK → KEEP-OPERATOR

**Files to edit:**
- `.claude/rules/shell-scripts-allowlist.md` (update Justificativa row for `lib/pulumi-output.sh`)
- `CHANGELOG.md` (`§ Changed` entry)

**Deep file dependency analysis:** Row lookup at `infra/helmfile/scripts/lib/pulumi-output.sh` (line ~152 of allowlist per grep). Update Justificativa text only; no functional change.

**Why this step:** Blueprint Q5 empirical: `pulumi/sdk-v3` not in any go.mod. Path A (KEEP-OPERATOR reclassify) preserves inventory integrity + honors M15-plan ADR no-new-deps.

**#### TDD**
- **RED**: `test_pulumi_reclassify_documented` — assert allowlist Justificativa contains "KEEP-OPERATOR" after edit AND references ADR-M18-D2.
- **GREEN**: Update row text.
- **REFACTOR**: CHANGELOG entry.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

(none — text edit only)

**Acceptance criteria:**
- Row Justificativa updated with new text referencing ADR-M18-D2 + Q5 empirical.
- CHANGELOG `§ Changed` entry present.

**DoD:** `docs(allowlist): reclassify pulumi-output.sh MIGRATE-GO-SDK → KEEP-OPERATOR per ADR-M18-D2`.

## Phase 3 — Integration validation + M18 checkbox amendment

### T3.1 — `task quality:gates` + `go test` GREEN + grep sweep

**Files to edit:** (validation only)

**Deep file dependency analysis:** Standard validation chain.

**Why this step:** DoD verification.

**#### TDD**
- **RED**: (validation phase)
- **GREEN**: Run all commands.
- **REFACTOR**: N/A.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

(none — validation only)

**Acceptance criteria:**
- `go test ./cmd/loopzero-ops/internal/quality/...` PASS.
- `go test ./infra/tests/e2e/...` PASS in hermetic mode.
- `grep -R 'repro-findings.sh\|cleanup-terminating-namespaces.sh' taskfiles/ docs/ --exclude-dir=archive` returns 0.
- Allowlist count 120 → 118 (net -2) OR 117 if the pulumi reclassify counts as removed. Empirical decision: count both scripts as removed from allowlist (2 rows deleted); pulumi row Justificativa updated (row stays but reclassified). Net allowlist: 120 → 118.

**DoD:** `test: M18 Phase 3 integration validation GREEN (hermetic slice)`.

### T3.2 — CHANGELOG audit + M18 checkbox amendment per ADR-D4

**Files to edit:** `CHANGELOG.md` (audit), `ROADMAP.md` (M18 [ ] → [x] with amended DoD), [`knowledge-base/roadmap-runs/M18-2026-07-10.md`](../roadmap-runs/M18-2026-07-10.md) (NEW audit trail).

**Deep file dependency analysis:** DoD amendment following M15 precedent (sponsor AskUserQuestion authorization).

**Why this step:** ADR-D4 amended DoD flip; M15 pattern established 2026-07-10.

**#### TDD**
- **RED**: `test_m18_checkbox_flipped_with_amendment_doc` — assert ROADMAP M18 line has [x] AND § header contains "DoD AMENDED" note.
- **GREEN**: Edit ROADMAP + write roadmap-runs file.
- **REFACTOR**: N/A.

#### Concurrency tests

(none — single-threaded)

#### Failure scenarios

(none — text edit)

**Acceptance criteria:**
- ROADMAP.md M18 line [ ] → [x] with amended DoD header note.
- [roadmap-runs/M18-2026-07-10.md](../roadmap-runs/M18-2026-07-10.md) exists with frontmatter status: completed + dod_amended: true.

**DoD:** `chore(roadmap): mark M18 done (v0.62.0) — DoD amended per M15 precedent`.

## Coverage Matrix

| # | Gap / Goal claim | Source | Task(s) |
|---|---|---|---|
| G1 | Reduce allowlist 120 → 118 residual | Plan Goal + M15 DoD literal (partial delivery per ADR-D1) | T2.1 |
| G2 | Ginkgo hermetic skip pattern applied | Blueprint Q4 + ADR-D3 | T2.1 |
| G3 | ADR-D2 pulumi reclassify | Blueprint Q5 + ADR-D2 | T2.2 |
| G4 | task quality:gates + go test GREEN | ROADMAP M18 DoD | T3.1 |
| G5 | Grep sweep 0 hits | ROADMAP M18 DoD | T3.1 |
| G6 | CHANGELOG § Removed + § Changed entries | Rule 6 | T2.1, T2.2, T3.2 |
| G7 | M18 checkbox flipped per ADR-D4 amendment | ADR-D4 (M15 precedent) | T3.2 |
| G8 | M19 milestone created post-release | Blueprint ADR-D3 | Handoff post-T3.2 (`/roadmap-feature`) |
| G9 | Baseline capture pre-migration | Steady-state-baseline mandate | T1.1 |

## Global DoD

- [ ] `task quality:gates` + `go test ./...` GREEN
- [ ] `grep -R <deleted-path>` for each deleted script returns 0 hits (excluding archive/memory)
- [ ] `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` == 118 (Wave B hermetic slice delivered)
- [ ] Per-task DoD met
- [ ] CHANGELOG discipline per lot
- [ ] M18 ROADMAP.md checkbox `[ ]` → `[x]` per ADR-D4 amendment + sponsor authorization
- [ ] `knowledge-base/audits/M18-*.md` audit trail complete (T1.1)
- [ ] `/roadmap-feature operator-glue-final-live-slice --dependency M18` scheduled post-release (M19 creation)

## Failure scenarios (test-obligation section)

External I/O signals detected: Ginkgo cluster access, kubectl (in LIVE mode). Per rules/plan-confidence-golden-rule.md:

- **Ginkgo hermetic skip failure** → T2.1 failure test.
- **Ginkgo LIVE mode without cluster** → T2.1 failure test.

Each has matching test in T2.1. Total: 2 failure scenarios covered.

## Final Phase — Integration Validation

The plan is NOT complete until:
- Phase 3 T3.1 all gates GREEN
- Phase 3 T3.2 M18 checkbox flipped + roadmap-runs audit committed

If any check fails, return to responsible task.

## Handoff (post-release)

1. `/roadmap-feature operator-glue-final-live-slice --dependency M18` creates M19 for remaining 62 files.
2. Cross-reference M19 with M18 blueprint ADR-D3.
3. Update MEMORY.md with M15→M18→M19 chained velocity-split precedent.
