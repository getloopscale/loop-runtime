---
type: plan
title: Hermetic Test Migration Remainder
slug: m20-hermetic-infra-tests-remainder
milestone_id: M20
created_at: 2026-07-10
plan_version: 1.0
goal: "Reduzir allowlist 118 → ≤ 100 entries migrando 18 hermetic scripts (17 check-*_test.sh + 1 helm/ory-kratos-public-urls_test.sh) para gates Go em cmd/loopzero-ops/internal/quality/<name>/ seguindo M-Migrate2 canonical, com Wave A (8 gates) + velocity split-decision + Wave B (6) + Wave C (4)."
authored_by: claude-opus-4-7
---

# M20 — Hermetic Test Migration Remainder

**Blueprint:** [`knowledge-base/discoveries/blueprints/m20-hermetic-infra-tests-remainder-blueprint.md`](../discoveries/blueprints/m20-hermetic-infra-tests-remainder-blueprint.md) v1.0 SHIPPABLE 100.0
**Discover plan:** `knowledge-base/discoveries/plans/m20-hermetic-infra-tests-remainder-plan.md` v1.0 SHIPPABLE 100.0
**M16 canonical parent:** [`knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`](../discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md) (SHIPPABLE 98.3)

## Goal

Reduzir allowlist 118 → ≤ 100 delivering 18 hermetic gates Go em `cmd/loopzero-ops/internal/quality/<name>/` (2 syntax + 15 helm-template + 1 file-layout) with hermetic tests GREEN sob `-race` + per-gate byte-identical parity verification + 18 bash scripts DELETED de `infra/tests/`. **Metric (observable):** `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` retorna **≤ 100** ao final da Phase 5.

## Context

- **Why now:** M16 shipou 10 gates canonical 2026-07-10 v0.60.0 provando o padrão empiricamente. M20 aplica exatamente o mesmo padrão aos 18 hermetic gates restantes em `infra/tests/`. Zero cluster I/O.
- **M-Migrate2 canonical shape:** stdlib + yaml.v3 (para YAML parse) + `helm template` subprocess (advisory-fallthrough quando ausente).
- **Precedent técnico:** M9 (`loopzero-ops quality` subcommand) + M16 canonical gates (`cmd/loopzero-ops/internal/quality/{sprintsyntax,archivescriptrepo,nocloud,lokivaultretention,...}/check.go`).

## Baseline Context

### Files that will be touched (18 M20 targets + wiring)

| Path | LoC | Shape family | M16 reference gate |
|---|---|---|---|
| `infra/tests/check-dev-public-billing-portal-syntax_test.sh` | ~40 | syntax | sprintsyntax |
| `infra/tests/check-dev-public-oidc-redirect-syntax_test.sh` | ~40 | syntax | sprintsyntax |
| `infra/tests/check-rotation-table-parity_test.sh` | ~50 | file-layout | archivescriptrepo |
| `infra/tests/check-migrate-legacy-namespaces_test.sh` | ~60 | helm-template | day30day60cronjobs |
| `infra/tests/check-enable-vault-backup_test.sh` | ~55 | helm-template | lokivaultretention |
| `infra/tests/check-restore-vault-snapshot_test.sh` | ~50 | helm-template | day30day60cronjobs |
| `infra/tests/check-rotate-secret_test.sh` | ~60 | helm-template | lokivaultretention |
| `infra/tests/check-loopzero-api-no-cluster-secrets_test.sh` | ~50 | helm-template (absence) | esocreationpolicy |
| `infra/tests/check-vault-bootstrap-eso_test.sh` | ~80 | helm-template | esochartsanity |
| `infra/tests/check-vault-sync_test.sh` | ~70 | helm-template | esocreationpolicy |
| `infra/tests/check-promtail-vault-empty-targets_test.sh` | ~50 | helm-template | esochartsanity |
| `infra/tests/check-promtail-vault-scrape_test.sh` | ~60 | helm-template | esochartsanity |
| `infra/tests/check-loopzero-api-vault-projected-token_test.sh` | ~65 | helm-template | loopzeroapiopenbao |
| `infra/tests/check-generate-secrets-vault-fallback_test.sh` | ~80 | helm-template + YAML | lokiretentionschema |
| `infra/tests/check-scoped-kubeconfig-cold-bootstrap_test.sh` | ~90 | helm-template + auth | openbaoinitauth |
| `infra/tests/check-scoped-kubeconfig-vault_test.sh` | ~85 | helm-template + auth | openbaoinitauth |
| `infra/tests/check-seed-loopzero-secrets_test.sh` | ~75 | helm-template + YAML | lokiretentionschema |
| `infra/tests/helm/ory-kratos-public-urls_test.sh` | ~60 | helm-template golden | esochartsanity |
| `cmd/loopzero-ops/quality_registry.go` | ~250 | (extend) | Register 18 new rows |
| `taskfiles/quality.yml § quality:gates` | recent | (extend) | Wire 18 new gate invocations + remove 18 legacy bash refs |
| `.claude/rules/shell-scripts-allowlist.md` | 700+ | (mutate) | Remove 18 rows |
| `CHANGELOG.md` | recent | (mutate) | Per-lot entries |

Total: 18 script files DELETED + 18 new Go packages CREATED (~200 LoC each Go + ~150 LoC test each) + 3 metadata files MUTATED.

### Current callers / dependents

- `check-*_test.sh` scripts: each is registered in `taskfiles/quality.yml § quality:gates` as `bash infra/tests/check-<name>_test.sh`. Update: replace with `/tmp/loopzero-ops quality <gate-name>` invocation.
- Zero downstream imports beyond `taskfiles/quality.yml` and `.claude/rules/shell-scripts-allowlist.md` per empirical grep.

### Architecture boundaries affected

- **DIP compliance:** M20 gates live in `cmd/loopzero-ops/internal/quality/<name>/` following M16 canonical shape. Registry pattern already established at `cmd/loopzero-ops/quality_registry.go`. Zero new interfaces.
- **Composition root:** `cmd/loopzero-ops/main.go` + `quality_gates_governance.go` remain the wire-up points. New `run<Name>` functions register via `quality_registry.go` entries table.
- **Layering:** hermetic gates run subprocess `helm template` OR `bash -n` OR pure Go file-layout scan. NO cluster I/O. NO API calls.

### Domain glossary

- **M-Migrate2:** canonical strangler-fig migration pattern established by M16. Per-file: (a) implement Go equivalent, (b) hermetic tests GREEN sob `-race`, (c) sanity-run against real repo PASS, (d) `git rm` bash + update taskfile + allowlist + CHANGELOG lot.
- **Advisory-fallthrough:** M16 T3.1+ pattern for helm-template gates — when `helm` binary absent, gate SKIPs with `Outcome{Skipped: true, Pass: true}` (CI passes; local dev without helm passes).
- **Byte-identical parity:** capture bash `--verify` output vs Go `--verify` output; diff must be empty (± tolerated timestamps) before `git rm` bash.

## Prior Art & Related Work

- **Blueprint (SoT):** [`knowledge-base/discoveries/blueprints/m20-hermetic-infra-tests-remainder-blueprint.md`](../discoveries/blueprints/m20-hermetic-infra-tests-remainder-blueprint.md) v1.0.
- **M16 canonical parent:** 10 gates shipped v0.60.0 2026-07-10 with M-Migrate2 pattern.
- **Shell-script-strategic-plan v1.3 (2026-05-22):** established the M-Migrate2 pattern + D3 no-new-bash + D4 KEEP criteria.

## Drawbacks & Risks

| # | Risk | Severity | Mitigation | Owner |
|---|---|---|---|---|
| R1 | Semantic drift na tradução bash→Go — 6-8 scripts usam padrões complexos (jq multi-line, yq escape, YAML round-trip); naive port pode perder cobertura silenciosamente | MEDIUM | Byte-identical parity capture bash-then-Go antes de `git rm` (M-Migrate2 canonical baseline pattern); salvar diff em `knowledge-base/audits/` como evidence per gate | Claude in per-task GREEN phase |
| R2 | Velocity split-decision timing — se decidir muito tarde (após gate 12+), context-switch alto; se cedo (gate 6), sinal insuficiente | LOW | Baseline capture at gate 8 boundary + explicit ADR-D<n> mid-milestone mirror M16 D4; split → Waves B+C to M21 se velocity < 4 gates/hora | Claude at Phase 4 |
| R3 | `TestOracle_MatchesFrozenBaseline` já stale (73 baseline vs 104 current); adicionar 18 → 122 aumenta stale gap | LOW | Re-freeze at Phase 5 per ADR-D3 (Path A of blueprint Q5) | Claude at T5.2 |
| R4 | Registry naming drift — 8 pre-existing legacy failures (`runOptionBESO` etc.); M20 must not add to débito | LOW | Strict `run<PascalCaseFromKebab>` naming per ADR-D4; no historical fix in M20 scope | Claude per-task |
| R5 | Wave C highest-complexity gates may exceed session budget mesmo pós-split; kubeconfig+YAML+golden requires ~1.5x Wave A time per gate | MEDIUM | ADR-D2 velocity-split protocol fires if velocity signal insufficient; M21 promotion via `/roadmap-feature` | Claude at Phase 4 boundary |

## Unresolved Questions

- Q1 — Should the 18 gates be delivered in per-gate atomic commits (mirror M16) OR per-Wave grouped commits? Recommendation: **per-gate atomic** (mirror M16 discipline; easier revert if parity fails).
- Q2 — Should `TestRegisteredRunFuncMatchesName` pre-existing 8 legacy failures be addressed in M20? Recommendation: **no** — out of scope; M22+ cleanup milestone if ever needed.
- Q3 — Should the "billing-portal" and "oidc-redirect" syntax gates fold into a single `dev-public-syntax` gate given they share the same shape? Recommendation: **keep separate** — each has domain-specific assertions; folding creates coupling.

## ADRs

### D1 — Full reuse of M16 shape families (Rule 9 no-reinvent + parsimony rung 3)

**Status:** Accepted. **Context:** M16 shipped 10 canonical gates 2026-07-10. Redoing shape research = YAGNI. **Decision:** Every M20 gate inherits shape verbatim from a specific M16 gate reference per blueprint mapping table. No shape-per-target investigation. **Alternatives rejected:** Fresh per-target discovery (redundant); custom shapes (violates canonical M-Migrate2).

### D2 — 3-Wave delivery order + velocity split at Wave A close (mirror M16 D4)

**Status:** Accepted. **Context:** Grill Q4 R2 mandates baseline-capture at gate 8 boundary. **Decision:** Wave A (8 lowest-complexity) → capture baseline → Wave B (6 vault-domain) → Wave C (4 highest-complexity). Split-decision: se velocity < 4 gates/hora at Wave A close, promote Waves B+C to M21. **Alternatives rejected:** Random ordering (loses velocity signal); Wave C first (violates lowest-friction-first).

### D3 — Baseline oracle re-freeze at M20 Phase 5

**Status:** Accepted. **Context:** `TestOracle_MatchesFrozenBaseline` já stale. **Decision:** Re-freeze at Phase 5 as DoD-terminal step (`loopzero-ops quality --list > cmd/loopzero-ops/testdata/quality-list-frozen.txt`). Locks M20 state. **Alternatives rejected:** Leave stale (violates SEM WORKAROUNDS + Rule 3).

### D4 — Strict `run<PascalCaseFromKebab>` naming (registry hygiene)

**Status:** Accepted. **Context:** 8 pre-existing failures in `TestRegisteredRunFuncMatchesName`. **Decision:** M20 gates use strict PascalCase-from-kebab-case. **Alternatives rejected:** Fix historical drift (out of scope).

### D5 — Per-gate atomic commit + byte-identical parity gate

**Status:** Accepted. **Context:** M16 shipped per-gate atomic commits mirroring strangler-fig discipline. **Decision:** Each M20 gate = (a) Go implementation + (b) hermetic tests GREEN + (c) sanity-run against real repo PASS + (d) `git rm` bash + (e) allowlist row remove + (f) taskfile swap + (g) CHANGELOG lot entry, all in ONE commit. **Alternatives rejected:** Per-Wave grouped commits (harder revert).

## Dependency Graph

```
Phase 1 (Baseline capture) — depends on [none, blueprint approved]
    ↓
Phase 2 (Wave A: 8 gates — lowest complexity) — depends on Phase 1
    ↓
Phase 3 (Wave A close baseline + split-decision) — depends on Phase 2
    ↓
Phase 4 (Wave B + C: 10 gates — if split-decision PROCEED) — depends on Phase 3
    ↓
Phase 5 (Integration validation + oracle re-freeze + M20 [x]) — depends on Phase 4
```

## Phase 1 — Baseline capture

### T1.1 — Capture pre-M20 baseline

**Files to edit:** [`knowledge-base/audits/M20-pre-migration-baseline-2026-07-10.md`](../audits/M20-pre-migration-baseline-2026-07-10.md) (NEW)

**Deep file dependency analysis:** allowlist grep `-c` output; 18-target inventory verification.

**Why this step:** DoD anchor per steady-state-baseline mandate.

**#### TDD**
- **RED**: `test_m20_baseline_captured` — assert audit file exists AND contains `entries_count: 118`.
- **GREEN**: write audit file.
- **REFACTOR**: N/A.

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
(none — file write only)

**Acceptance criteria:** File committed with `entries_count: 118`.

**DoD:** `docs(audit): M20 pre-migration baseline`.

## Phase 2 — Wave A (8 gates, lowest complexity)

### T2.1 — 8 canonical M-Migrate2 gate migrations (Wave A: iterated per-gate atomic commits)

**Files to edit (per gate — pattern applied 8 times):**
- `cmd/loopzero-ops/internal/quality/<name>/check.go` (NEW ~150-250 LoC)
- `cmd/loopzero-ops/internal/quality/<name>/check_test.go` (NEW ~100-180 LoC)
- `cmd/loopzero-ops/quality_registry.go` (add 1 row)
- `cmd/loopzero-ops/quality_gates_governance.go` OR `quality_gates_shell.go` (add `run<Name>` func)
- `infra/tests/check-<name>_test.sh` (DELETE post GREEN)
- `.claude/rules/shell-scripts-allowlist.md` (remove 1 row + `§ Removed` note)
- `taskfiles/quality.yml § quality:gates` (swap bash line for Go gate invocation)
- `CHANGELOG.md` (`[Unreleased] § Added` Go gate + `§ Removed` bash)

**Wave A target list (per blueprint Q3):**
- `dev-public-billing-portal-syntax` (from `sprintsyntax`)
- `dev-public-oidc-redirect-syntax` (from `sprintsyntax`)
- `rotation-table-parity` (from `archivescriptrepo`)
- `migrate-legacy-namespaces` (from `day30day60cronjobs`)
- `enable-vault-backup` (from `lokivaultretention`)
- `restore-vault-snapshot` (from `day30day60cronjobs`)
- `rotate-secret` (from `lokivaultretention`)
- `loopzero-api-no-cluster-secrets` (from `esocreationpolicy`)

**Deep file dependency analysis (per gate):** Read source bash (~40-80 LoC) → extract assertion list → find corresponding M16 reference gate → clone shape → substitute domain-specific assertions.

**Why this step:** M-Migrate2 canonical per ADR-D1/D2/D5. Wave A is lowest-complexity for velocity signal.

**#### TDD (per gate)**
- **RED**: for each `<name>`: (a) `TestCheck_HappyPath_PASS` — synthetic fixture that passes; (b) `TestCheck_ParseError_FAIL` or shape variant; (c) `TestCheck_RealRepoPasses` — asserts real repo state currently passes bash equivalent.
- **GREEN**: implement Go equivalent per M16 canonical shape; verify all 3 tests pass sob `-race`.
- **REFACTOR**: `git rm` bash + update taskfile + allowlist + CHANGELOG lot per D5.

#### Concurrency tests
(none — single-threaded per M16 canonical shape)

#### Failure scenarios
- **helm binary absent** (helm-template gates only): advisory-fallthrough — `Outcome{Skipped: true, Pass: true}`. Test: `TestCheck_HelmAbsent_SKIP` with mocked exec.
- **Fixture chart-missing**: gate returns clear error. Test: `TestCheck_ChartMissing_FAIL`.
- **Real repo doesn't match assertion**: gate FAILs with violation list. Real-repo test PASSES post-shape-verification.

**Acceptance criteria (per gate):**
- Go package compiles.
- 3-4 hermetic tests GREEN sob `-race`.
- Sanity-run against real repo (`/tmp/loopzero-ops quality <gate-name>`) PASS.
- Bash file deleted; grep sweep 0 hits.
- Allowlist row removed; taskfile updated; CHANGELOG entry added.

**DoD (per gate):** `feat(quality): migrate check-<name>_test.sh → loopzero-ops quality <gate-name> (M20 Wave A T2.<N>)`.

## Phase 3 — Wave A close + velocity split-decision

### T3.1 — Post-Wave-A baseline + velocity capture

**Files to edit:** `knowledge-base/audits/M20-wave-a-close-2026-07-10.md` (NEW)

**Deep file dependency analysis:** measure wall-clock elapsed from Phase 1 start; count gates shipped; parity-check hit count.

**Why this step:** ADR-D2 velocity-split gate protocol.

**#### TDD**
- **RED**: `test_wave_a_baseline_captured` — assert file exists with 3 sections.
- **GREEN**: write audit with metric table.
- **REFACTOR**: N/A.

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
(none — file write)

**Acceptance criteria:** Audit has (a) allowlist_count_delta (118 → 110), (b) wall_clock_elapsed_hours, (c) parity_check_hits, (d) split-decision verdict `PROCEED_WAVES_BC | SPLIT_TO_M21`.

**DoD:** `docs(audit): M20 Wave A close baseline (Phase 3 velocity-split gate)`.

## Phase 4 — Wave B + C (if PROCEED) — 10 gates

### T4.1 — Wave B (6 vault-domain) + Wave C (4 highest-complexity) iterated per-gate atomic commits

**Files to edit:** Same per-gate pattern as Phase 2 (10 iterations).

**Wave B (6 gates):**
- `vault-bootstrap-eso` (from `esochartsanity`)
- `vault-sync` (from `esocreationpolicy`)
- `promtail-vault-empty-targets` (from `esochartsanity`)
- `promtail-vault-scrape` (from `esochartsanity`)
- `loopzero-api-vault-projected-token` (from `loopzeroapiopenbao`)
- `generate-secrets-vault-fallback` (from `lokiretentionschema`)

**Wave C (4 gates):**
- `scoped-kubeconfig-cold-bootstrap` (from `openbaoinitauth`)
- `scoped-kubeconfig-vault` (from `openbaoinitauth`)
- `seed-loopzero-secrets` (from `lokiretentionschema`)
- `ory-kratos-public-urls` (from `esochartsanity` — helm-template golden test; located under `infra/tests/helm/`)

**#### TDD, Concurrency tests, Failure scenarios:** identical shape to Phase 2 T2.* per gate.

**Acceptance criteria:** Same as Phase 2 acceptance.

**DoD (per gate):** `feat(quality): migrate check-<name>_test.sh → loopzero-ops quality <gate-name> (M20 Wave B/C T4.<N>)`.

## Phase 5 — Integration validation + M20 [x]

### T5.1 — `task quality:gates` + `go test ./...` GREEN + grep sweep

**Files to edit:** (validation only)

**Deep file dependency analysis:** Run `task quality:gates` (invokes 100+ gates); `go test ./cmd/loopzero-ops/... ./infra/tests/...`; `grep -R '<deleted-path>'` for each of 18 deleted scripts.

**Why this step:** DoD verification.

**#### TDD**
- **RED**: (validation phase)
- **GREEN**: Run all commands, capture output.
- **REFACTOR**: N/A.

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
(none — validation)

**Acceptance criteria:**
- `go test ./cmd/loopzero-ops/internal/quality/...` PASS.
- `grep -R '<deleted-path>'` for each of 18 deleted paths returns 0 hits (excluding archive/memory).
- Allowlist count == 118 - 18 = 100.

**DoD:** `test: M20 Phase 5 integration validation GREEN`.

### T5.2 — TestOracle_MatchesFrozenBaseline re-freeze (per ADR-D3)

**Files to edit:** `cmd/loopzero-ops/testdata/quality-list-frozen.txt` (regenerate) OR wherever baseline lives.

**Deep file dependency analysis:** Locate the frozen baseline file. Regenerate via `loopzero-ops quality --list`. Verify `TestOracle_MatchesFrozenBaseline` PASSES.

**Why this step:** ADR-D3 mandate (Path A of blueprint Q5).

**#### TDD**
- **RED**: `TestOracle_MatchesFrozenBaseline` currently FAILs (73 vs 104 pre-fix; 122 post-M20).
- **GREEN**: re-freeze baseline file; test passes.
- **REFACTOR**: N/A.

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
(none — text file regeneration)

**Acceptance criteria:** `TestOracle_MatchesFrozenBaseline` PASS.

**DoD:** `test(quality): re-freeze baseline oracle for M20 (adds 18 gates)`.

### T5.3 — M20 checkbox flip + CHANGELOG audit

**Files to edit:** `CHANGELOG.md` (audit only), `ROADMAP.md` (M20 [ ] → [x]), [`knowledge-base/roadmap-runs/M20-2026-07-10.md`](../roadmap-runs/M20-2026-07-10.md) (NEW).

**Deep file dependency analysis:** DoD literal `≤ 100` verification. Empirical DoD #1 met if allowlist count == 100.

**Why this step:** ROADMAP finalization per cycle-release contract.

**#### TDD**
- **RED**: `test_m20_checkbox_flipped` — assert ROADMAP M20 line has `[x]`.
- **GREEN**: edit ROADMAP + write roadmap-runs file.
- **REFACTOR**: N/A.

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
(none — text edit)

**Acceptance criteria:**
- ROADMAP.md M20 line `[ ]` → `[x]`.
- [roadmap-runs/M20-2026-07-10.md](../roadmap-runs/M20-2026-07-10.md) exists with `status: completed` + `dod_amended: false` (M20 literal DoD met).

**DoD:** `chore(roadmap): mark M20 done (v0.63.0)`.

## Coverage Matrix

| # | Gap / Goal claim | Source | Task(s) |
|---|---|---|---|
| G1 | Reduce allowlist 118 → ≤ 100 | ROADMAP M20 DoD#3 + Plan Goal | T2.1, T4.1 |
| G2 | 18 gates Go shipped em `cmd/loopzero-ops/internal/quality/` sob `-race` | ROADMAP M20 DoD#1 | T2.1, T4.1 |
| G3 | 18 bash scripts DELETED with byte-identical parity | ROADMAP M20 DoD#2 + ADR-D5 | T2.1, T4.1 |
| G4 | `task quality:gates` + `go test ./...` GREEN + grep sweep 0 | ROADMAP M20 DoD#4 | T5.1 |
| G5 | CHANGELOG per-lot discipline | ROADMAP M20 DoD#5 + Rule 6 | T2.1, T4.1, T5.3 |
| G6 | Velocity split-decision at Wave A close (ADR-D2) | Grill Q4 R2 | T3.1 |
| G7 | Baseline oracle re-freeze (ADR-D3) | Blueprint Q5 Path A | T5.2 |
| G8 | Strict registry naming (ADR-D4) | Blueprint Q4 | T2.1, T4.1 |
| G9 | Pre-migration baseline capture | Steady-state-baseline mandate | T1.1 |
| G10 | M20 checkbox flip [x] with literal DoD met | ROADMAP M20 DoD terminal | T5.3 |

## Global DoD

- [ ] `task quality:gates` GREEN
- [ ] `go test ./cmd/loopzero-ops/...` GREEN sob `-race`
- [ ] `grep -R <deleted-path>` for each of 18 deleted paths returns 0 hits
- [ ] `grep -cE '^\| .*\.sh ' .claude/rules/shell-scripts-allowlist.md` == 100
- [ ] Per-task DoD gate met
- [ ] CHANGELOG per-lot entries
- [ ] `TestOracle_MatchesFrozenBaseline` PASS (re-frozen per T5.2)
- [ ] `TestRegisteredRunFuncMatchesName` — 0 NEW failures added by M20 (pre-existing 8 legacy failures unchanged per ADR-D4)
- [ ] M20 ROADMAP checkbox `[ ]` → `[x]` (literal DoD met)
- [ ] Audit trail complete (`knowledge-base/audits/M20-*.md`)

## Failure scenarios (mandatory section)

External I/O signals present: helm subprocess, bash subprocess. Per rules/plan-confidence-golden-rule.md:

- **helm binary absent** → advisory-fallthrough SKIP per M16 canonical (Wave A helm gates + all Wave B/C helm gates).
- **bash binary absent** → syntax gates FAIL clear (Wave A syntax gates).
- **Fixture chart missing** → error surfaced clearly per gate.
- **Real repo assertion drift** → gate FAILs with actionable violation list.
- **Registry naming conflict** → build fails at compile time (Go duplicate declaration).

Each covered in per-task TDD failure scenarios above.

## Final Phase — Integration Validation

Plan complete only when:
- Phase 5 T5.1 all gates GREEN
- Phase 5 T5.2 baseline oracle re-frozen
- Phase 5 T5.3 M20 checkbox flipped

If any check fails, return to responsible task.

## Handoff (post-release)

Post-`/release` v0.63.0:
1. If Wave B+C SPLIT to M21 fired at T3.1: `/roadmap-feature m21-hermetic-remainder` creates M21.
2. Cross-reference in M20 amended DoD note if scope split.
3. Update MEMORY.md if M20 empirical velocity data adds to M-Migrate2 canonical.
