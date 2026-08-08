---
type: plan
title: "Plan: M16 Hermetic Test Migration — `infra/tests/check-*_test.sh` → Go Quality Gates"
generated: { by: process:to-plan, at: 2026-07-10 }
version: 1.1
slug: infra-tests-hermetic-to-quality-gates
milestone_id: M16
owner: claude-code (autonomous per sponsor goal-directive 2026-07-10)
created_at: 2026-07-10
generated_by: /to-plan
goal: Migrate 27 hermetic bash tests (infra/tests/check-*_test.sh) to Go quality gates under cmd/loopzero-ops/internal/quality/*/ following the M-Migrate2 pattern codified in the blueprint (SHIPPABLE 98.3), reducing shell-scripts-allowlist by 27 entries with zero regression in task quality:gates GREEN.
based_on: knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md v1.0 (SHIPPABLE 98.3)
---

# Plan: M16 Hermetic Test Migration — `infra/tests/check-*_test.sh` → Go Quality Gates

## Context

**What this plan implements:** M16 milestone per `ROADMAP.md § M16` — migrate 27 hermetic bash tests to Go quality gates in 3 waves (3 syntax + 6 helm-template + 11 bash-target pairs) plus 5 pre-hygiene backfills.

**Prior art consumed:**

- Discovery blueprint at [`knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`](../discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md) v1.0 (SHIPPABLE 98.3) — codifies M-Migrate2 pattern, 5 blueprint-ADRs, 14-step Migration recipe, 8 recommendations
- 12 M-Migrate2 precedent gates in `cmd/loopzero-ops/internal/quality/*/`
- M-Migrate2 pattern established by M15 (20 seeds shipped in v0.59.0)
- Registration precedent: `cmd/loopzero-ops/quality_registry.go`
- Wiring precedent: `taskfiles/quality.yml § quality:gates`
- Allowlist SoT: `.claude/rules/shell-scripts-allowlist.md`

**Rules read at Step 0:** `cycle-plan.md`, `cycle-implement.md`, `testing.md`, `tdd-workflow.md`, `parsimony-ladder.md`, `architecture.md`, `error-handling.md`, `git-safety.md`, `shell-scripts-allowlist.md`. Unbreakable Rules 1, 3, 4, 6, 7, 9, 10, 11 apply.

## Objective

Complete the M-Migrate2 strangler-fig migration of 27 hermetic bash tests to pure-logic Go quality gates in 3 waves, with byte-for-byte parity verified per gate before deletion, reducing `.claude/rules/shell-scripts-allowlist.md` from 121 entries to ≤ 94 entries, with `task quality:gates` GREEN + `go test ./cmd/loopzero-ops/...` GREEN + zero regressions at every commit boundary.

**Measurable success criteria (10):**

1. 27 new Go packages exist under `cmd/loopzero-ops/internal/quality/*/` following the canonical shape (Q1-Q8 of blueprint)
2. All 27 registered in `quality_registry.go` with `run<CamelCasedPkgName>` function-naming convention
3. All 27 wired into `taskfiles/quality.yml § quality:gates` with docs comments
4. All 27 bash tests deleted (`git rm`)
5. All 27 rows removed from `shell-scripts-allowlist.md`
6. Wave 2 pre-flight guards added for `helm`, `jq`, `yq` (per plan D3 below)
7. 5 pre-Wave-1 unwired gates backfilled into `quality:gates` (per plan D1 below)
8. `task quality:gates` GREEN at final commit
9. `go test ./cmd/loopzero-ops/... -race` GREEN
10. CHANGELOG `[Unreleased] § Added` gains 27 gate bullets; `§ Removed` gains 27 bash-script bullets

## Baseline Context

### Files that will be touched

| Path | Current LoC | Last commit | Purpose |
|---|---|---|---|
| `cmd/loopzero-ops/internal/quality/allowlistsunsets/check.go` | 251 | 042014a | M-Migrate2 precedent (read-only reference) |
| `cmd/loopzero-ops/internal/quality/allowlistsunsets/check_test.go` | 51 | 042014a | M-Migrate2 test precedent |
| `cmd/loopzero-ops/internal/quality/esooverlay/check.go` | ~180 | 042014a | Helm-template precedent |
| `cmd/loopzero-ops/internal/quality/esooverlay/check_test.go` | 134 | 042014a | Test precedent |
| `cmd/loopzero-ops/internal/quality/installgithooks/check.go` | ~85 | 042014a | Syntax precedent |
| `cmd/loopzero-ops/internal/quality/installgithooks/check_test.go` | 46 | 042014a | Test precedent |
| `cmd/loopzero-ops/internal/quality/webhookcanonicalpath/check.go` | ~120 | 042014a | File-layout precedent |
| `cmd/loopzero-ops/internal/quality/webhookcanonicalpath/check_test.go` | 69 | 042014a | Test precedent |
| `cmd/loopzero-ops/quality_registry.go` | ~180 | 042014a | Central registry — new rows added |
| `taskfiles/quality.yml` | ~200 | 042014a | Central task wiring — new invocations added |
| `.claude/rules/shell-scripts-allowlist.md` | ~800 | 042014a | Allowlist SoT — row deleted per script |
| `CHANGELOG.md` | (varies) | 042014a | Added/Removed bullets per script |
| `ROADMAP.md § M16` | — | 331ce892 | Checkbox flipped `[ ]`→`[x]` at release |

### Current callers / dependents

- **Symbol:** `loopzero-ops quality <name>` (CLI subcommand)
  - **Callers (production):** `taskfiles/quality.yml § quality:gates` — invokes each gate line-by-line
  - **Callers (tests):** `go test ./cmd/loopzero-ops/internal/quality/<pkg>/... -race`
- **Symbol:** `runXxx` functions in `cmd/loopzero-ops/quality_registry.go`
  - **Callers (production):** `cmd/loopzero-ops/main.go` routes `quality <name>` CLI to registry lookup
  - **Callers (tests):** direct calls via `Check(cfg Config)` API in each `check_test.go`
- **Callers to be created:** NONE — gates are self-contained; no additional integration outside registry + taskfile

### Domain glossary

- **Gate** — Go package under `cmd/loopzero-ops/internal/quality/<pkgname>/` implementing `Check(cfg Config) (Outcome, error)` + `Config` struct + `Outcome` struct + `ErrViolations` sentinel
- **Wave** — a batch of gates migrated together (Wave 1 = 3 syntax gates, Wave 2 = 6 helm-template gates, Wave 3 = 11 bash-target pairs)
- **M-Migrate2** — the pattern-of-record for bash-to-Go migrations established by v0.59.0's 20 seed migrations + the 12 gates already registered
- **Registry row** — entry in `cmd/loopzero-ops/quality_registry.go` mapping `{"kebab-name", "description", runFunc}`
- **Wiring line** — one `- /tmp/loopzero-ops quality <name>` line in `taskfiles/quality.yml § quality:gates`
- **Pre-flight guard** — `command -v <bin> >/dev/null 2>&1 || { echo FAIL... >&2; exit 1; }` line at task top (precedent line 160 for `helmfile`)
- **Bash-target pair** — for Wave 3, the bash test AND the target script it tests are BOTH migrated together

### Architecture boundaries affected

- Each new gate lives under `cmd/loopzero-ops/internal/quality/<pkgname>/` — Go `internal/` package boundary respects DIP (per `architecture.md`)
- Gates depend on stdlib ONLY per plan D2 (see § ADRs) — no third-party or internal `github.com/getloopscale/loopzero/*` imports
- No cross-gate imports — each gate is standalone (SRP at package level)
- No CLI-output changes to gate handlers — `loopzero-ops quality <name>` output format is stable (Rule 3 forbids internal terms; gates output structured failures only)
- Composition root unchanged — `main.go` router untouched; new gates plug into existing registry

## Prior Art & Related Work

- **Internal:** M15 (12 M-Migrate2 gates shipped in v0.59.0 covering `infra/tests/check-*_test.sh` variants), M13 (`no-new-bash` gate inverted), M9 (`loopzero-ops quality` subcommand introduced), M8 1.5 (`install-git-hooks.sh` D4 hook).
- **Discovery blueprint:** [`knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`](../discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md) — 8 research questions answered with file:line evidence, 5 blueprint-ADRs codifying pattern, 14-step Migration recipe.
- **Rule inheritances:** `parsimony-ladder.md` GREEN-phase deliberation, `testing.md § 4.1` edge+negative case mandate.
- **External SOTA:** none — this is an internal codebase migration. Blueprint's 12-gate precedent set is the reference.

## ADRs

### D1 — Wire the 5 pre-hygiene unwired gates before Wave 1 starts

**Context:** Discovery blueprint identified 5 M-Migrate2 gates that are registered in `quality_registry.go` but NOT wired into `taskfiles/quality.yml § quality:gates` (`webhook-canonical-path`, `oidc-cred-admin-loopzero`, `canonical-fixture-repo`, `install-git-hooks`, `render-schema-gen-sql`). This is a Rule 3 dead-code gap.

**Decision:** Wire all 5 into `quality:gates` as Phase 1 (before Wave 1). Each wiring is one line + docs comment referencing M-Migrate2 origin.

**Alternatives rejected:**
- Leave unwired → Rule 3 violation; hidden coverage gaps in CI
- Delete registrations → destroys 5 shipped gates + tests

### D2 — Every new gate uses stdlib-only imports; NO third-party packages

**Context:** Blueprint Q3 confirmed ZERO third-party imports across all 12 M-Migrate2 precedents. Waves 1+2+3 must preserve this.

**Decision:** New gates in Waves 1+2+3 import ONLY Go stdlib. NO third-party. NO internal `github.com/getloopscale/loopzero/*` (gates are pure-logic).

**Alternatives rejected:**
- Add `gopkg.in/yaml.v3` for Wave 2 helm-template parsing → NO — `regexp.MatchString` proves sufficient in 12 precedents (esooverlay is proof)
- Add JSON assertion lib → NO — `encoding/json` (stdlib) suffices

### D3 — Wave 2 pre-flight binary guards MUST be added before wiring first Wave 2 gate

**Context:** Blueprint Q4 + EC-3 confirmed only `helmfile` has a `command -v` guard at `taskfiles/quality.yml:160`. Wave 2's 6 helm-template gates require `helm`, `jq`, `yq` — all currently unguarded.

**Decision:** First task of Phase 3 (Wave 2) adds `command -v helm/jq/yq` guards following line 160 precedent BEFORE any Wave 2 gate wiring.

**Alternatives rejected:**
- Assume Docker CI has all binaries → fragile; CI runners drift
- Guards inside each check.go → duplication; task-boundary guard is earlier/cleaner

### D4 — Wave 3 split decision made mid-milestone based on Waves 1+2 velocity

**Context:** Wave 3 = 11 bash-target pairs (~4,000 LoC bash → ~6,000 LoC Go, 2-3 weeks estimated). Risk of milestone non-close.

**Decision:** After Wave 2 closes, measure elapsed days + LoC per gate. If extrapolated velocity for 11 pairs > 2× Waves 1+2 combined, SPLIT Wave 3 into Wave 3a (6 simpler pairs) + Wave 3b (5 complex pairs) — Wave 3b promoted to a new M17 milestone. Decision documented in `knowledge-base/audits/M16-wave3-decision-{date}.md`.

**Alternatives rejected:**
- Pre-commit to 11-pair scope → risks milestone non-close (documented in Drawbacks)
- Pre-split all → premature; Wave 1+2 velocity is the empirical input

## Drawbacks & Risks

| # | Risk | Severity | Mitigation | Owner |
|---|---|---|---|---|
| 1 | Semantic drift bash → Go: Go passes but silently accepts what bash rejected → false-positive `task quality:gates` GREEN | HIGH | Byte-for-byte parity per gate (see Phase 2 T2.0 baseline capture + parity check per gate) | Implementing agent per gate |
| 2 | Wave 3 scope non-close: 11 pairs × ~1000 LoC each may exceed one release cycle | HIGH | ADR D4 split-decision mid-milestone | Operator/sponsor at Wave 2 close |
| 3 | Wave 2 gates silently NOOP when binary absent → hidden coverage | MEDIUM | ADR D3 pre-flight guards as first Wave 2 task | Wave 2 first commit |
| 4 | 5 unwired backfill gates + new Wave 1 gate wiring collision on `taskfiles/quality.yml` | MEDIUM | Backfill happens FIRST (Phase 1) — separate commits | First 5 commits of the milestone |
| 5 | `Outcome` bimodal (7/12 `Failures` vs 4/12 `Violations` in precedents) confuses new-gate authors | LOW | Blueprint ADR D1 + plan D2 mandate `Failures []string` in review | Review-time |
| 6 | Bash test semantics that use `set -e` + short-circuit `&&` may not map cleanly to Go collect-all pattern | MEDIUM | Parity artifact per gate documents any deviation; blueprint ADR D1 allows opt-out per gate with recorded rationale | Implementing agent per gate |

## Unresolved Questions

- Q1: Do the 3 Wave 1 bash tests have `--help`/`--verbose` flags Go must support? — Resolved: NO — grep verified absence in `check-sprint-syntax_test.sh`, `check-archive-script-repo-root_test.sh`, `check-no-cloud-dev-default_test.sh`
- Q2: Does `task quality:gates` currently include ANY Wave 3 target scripts as guards? — DEFERRED to Wave 3 pre-flight audit at Phase 4 start (blocks T4.0)
- Q3: Should parity diff artifacts live under `knowledge-base/audits/` or a new `knowledge-base/migrations/` folder? — Resolved: reuse `knowledge-base/audits/` per KISS (Rule 10)
- Q4: Is the `render-schema-gen-sql` unwired gate the same as `render-schema-gen-sql_test.sh` in `infra/tests/`? — DEFERRED to Phase 1 T1.5 implementation-time verification
- Q5: Should Wave 2 pre-flight guards fail-fast at task boundary OR emit WARN + skip gate? — Resolved: fail-fast (line 160 precedent + Rule 3 no-silent-failure)

## Coverage Matrix

| # | Success criterion | Phase(s) | Tasks | Status |
|---|---|---|---|---|
| G1 | 27 Go packages exist under `cmd/loopzero-ops/internal/quality/*/` | Phases 2-4 | T2.1 T2.2 T2.3 T3.1 T3.2 T3.3 T3.4 T3.5 T3.6 T3.7 T4.1 T4.2 T4.3 T4.4 T4.5 T4.6 T4.7 T4.8 T4.9 T4.10 T4.11 | Mapped |
| G2 | 27 registered in `quality_registry.go` with `run<Pkg>` naming | Phases 2-4 | T2.1 T2.2 T2.3 T3.1 T3.2 T3.3 T3.4 T3.5 T3.6 T3.7 T4.1 T4.2 T4.3 T4.4 T4.5 T4.6 T4.7 T4.8 T4.9 T4.10 T4.11 | Mapped |
| G3 | 27 wired into `taskfiles/quality.yml § quality:gates` | Phases 2-4 | T2.1 T2.2 T2.3 T3.1 T3.2 T3.3 T3.4 T3.5 T3.6 T3.7 T4.1 T4.2 T4.3 T4.4 T4.5 T4.6 T4.7 T4.8 T4.9 T4.10 T4.11 | Mapped |
| G4 | 27 bash tests deleted (`git rm`) verified at T5.3 | Phases 2-4 | T2.1 T2.2 T2.3 T3.1 T3.2 T3.3 T3.4 T3.5 T3.6 T3.7 T4.1 T4.2 T4.3 T4.4 T4.5 T4.6 T4.7 T4.8 T4.9 T4.10 T4.11 T5.3 | Mapped |
| G5 | 27 allowlist rows removed, delta verified | Phases 2-5 | T2.1 T2.2 T2.3 T3.1 T3.2 T3.3 T3.4 T3.5 T3.6 T3.7 T4.1 T4.2 T4.3 T4.4 T4.5 T4.6 T4.7 T4.8 T4.9 T4.10 T4.11 T5.3 | Mapped |
| G6 | Wave 2 pre-flight guards for helm/jq/yq | Phase 3 | T3.0 T5.2 | Mapped |
| G7 | 5 pre-Wave-1 backfill gates wired | Phase 1 | T1.1 T1.2 T1.3 T1.4 T1.5 | Mapped |
| G8 | `task quality:gates` GREEN at final commit | Phase 5 | T5.1 | Mapped |
| G9 | `go test ./cmd/loopzero-ops/... -race` GREEN | Phase 5 | T5.1 | Mapped |
| G10 | CHANGELOG § Added + § Removed populated, count verified | Phases 2-5 | T2.1 T2.2 T2.3 T3.1 T3.2 T3.3 T3.4 T3.5 T3.6 T3.7 T4.1 T4.2 T4.3 T4.4 T4.5 T4.6 T4.7 T4.8 T4.9 T4.10 T4.11 T5.5 | Mapped |

**Coverage: 10/10 (100%)** — every success criterion mapped to ≥ 1 concrete task ID.

## Phase 1 — Pre-Wave-1 hygiene (backfill 5 unwired gates)

### T1.1 — Wire `webhook-canonical-path` into `taskfiles/quality.yml`

#### Why this step
Blueprint ADR D4 confirmed 5 M-Migrate2 gates registered but unwired. `webhook-canonical-path` is the first; backfilling closes Rule 3 dead-code gap BEFORE Wave 1 adds new gates.

#### Files to edit
- `taskfiles/quality.yml` — add one line + docs comment
- `CHANGELOG.md` — add `[Unreleased] § Changed` bullet

#### TDD
- Given: `grep -c 'quality webhook-canonical-path' taskfiles/quality.yml == 0`
- When: I add `- /tmp/loopzero-ops quality webhook-canonical-path # M-Migrate2 backfill 2026-07-10` to `quality:gates`
- Then: `grep -c 'quality webhook-canonical-path' taskfiles/quality.yml == 1`
- test_wire_webhook_canonical_path_adds_one_line — verifies grep count transitions 0→1

#### Concurrency tests
(none — single-threaded)

#### Deep file dependency analysis
- Only `taskfiles/quality.yml § quality:gates` gains a new line
- `cmd/loopzero-ops/internal/quality/webhookcanonicalpath/check.go` unchanged (already exists)
- No downstream ripple

#### Acceptance criteria
- `grep -c 'quality webhook-canonical-path' taskfiles/quality.yml` returns 1
- `task quality:gates` GREEN (no new failures introduced)
- CHANGELOG `[Unreleased] § Changed`: `- Wire 5 previously-registered M-Migrate2 gates into task quality:gates (pre-M16 Rule 3 hygiene).`

#### DoD
- Commit `chore(quality): T1.1 wire webhook-canonical-path into quality:gates (M-Migrate2 backfill)` on develop
- `task quality:gates` GREEN local run
- CHANGELOG bullet present

### T1.2 — Wire `oidc-cred-admin-loopzero`

#### Why this step
Second backfill; same rationale as T1.1.

#### Files to edit
- `taskfiles/quality.yml`

#### TDD
- test_wire_oidc_cred_admin_loopzero — asserts `grep -c 'quality oidc-cred-admin-loopzero' taskfiles/quality.yml == 1`

#### Concurrency tests
(none — single-threaded)

#### Acceptance criteria
- Grep count = 1
- `task quality:gates` GREEN

#### DoD
- Commit `chore(quality): T1.2 wire oidc-cred-admin-loopzero` on develop

### T1.3 — Wire `canonical-fixture-repo`

#### Why this step
Third backfill; same rationale.

#### Files to edit
- `taskfiles/quality.yml`

#### TDD
- test_wire_canonical_fixture_repo — asserts `grep -c 'quality canonical-fixture-repo' taskfiles/quality.yml == 1`

#### Concurrency tests
(none — single-threaded)

#### Acceptance criteria
- Grep count = 1
- `task quality:gates` GREEN

#### DoD
- Commit `chore(quality): T1.3 wire canonical-fixture-repo` on develop

### T1.4 — Wire `install-git-hooks`

#### Why this step
Fourth backfill; same rationale.

#### Files to edit
- `taskfiles/quality.yml`

#### TDD
- test_wire_install_git_hooks — asserts `grep -c 'quality install-git-hooks' taskfiles/quality.yml == 1`

#### Concurrency tests
(none — single-threaded)

#### Acceptance criteria
- Grep count = 1
- `task quality:gates` GREEN

#### DoD
- Commit `chore(quality): T1.4 wire install-git-hooks` on develop

### T1.5 — Wire `render-schema-gen-sql` (or verify already-wired)

#### Why this step
Fifth backfill; verify at implementation-time whether target is registered under this exact name (Unresolved Question #4).

#### Files to edit
- `taskfiles/quality.yml` (if not already wired)

#### TDD
- test_wire_render_schema_gen_sql — asserts `grep -c 'quality render-schema-gen-sql' taskfiles/quality.yml >= 1` after edit

#### Concurrency tests
(none — single-threaded)

#### Acceptance criteria
- Grep count ≥ 1
- `task quality:gates` GREEN
- Phase 1 complete: all 5 backfill gates wired

#### DoD
- Commit `chore(quality): T1.5 wire render-schema-gen-sql (final Phase 1 backfill)` on develop
- CHANGELOG closing bullet: `- 5 M-Migrate2 gates now wired in task quality:gates (M16 Phase 1 hygiene complete).`

## Phase 2 — Wave 1 (3 syntax/layout gates)

### T2.0 — Read + parity-baseline the 3 target bash tests

#### Why this step
Plan D-risk 1 mitigation: capture the bash test's exact assertion output on the current repo before writing Go, to compare against Go output later (parity artifact).

#### Files to edit
- None — read-only + create 3 baseline files

#### TDD
- For each of `{check-sprint-syntax_test.sh, check-archive-script-repo-root_test.sh, check-no-cloud-dev-default_test.sh}`:
  - Given: repo at HEAD
  - When: I run `bash infra/tests/{script}`
  - Then: exit code + stdout + stderr captured to `knowledge-base/audits/M16-wave1-{script}-baseline-2026-07-10.txt`
- test_baseline_files_created — asserts `ls knowledge-base/audits/M16-wave1-*-baseline-*.txt | wc -l == 3`

#### Concurrency tests
(none — single-threaded)

#### Acceptance criteria
- 3 baseline files exist
- Each contains exit code + stdout + stderr

#### DoD
- Baselines committed: `chore(quality): T2.0 Wave 1 parity baselines captured`

### T2.1 — Migrate `check-sprint-syntax_test.sh` → `cmd/loopzero-ops/internal/quality/sprintsyntax/`

#### Why this step
Blueprint recommendation #2 — Wave 1 = syntax subtype. Smallest bash surface. Prove the M-Migrate2 pattern end-to-end (plan D2 stdlib-only). Blueprint Q7 Shape 1 mapping applies.

#### Files to edit
- `cmd/loopzero-ops/internal/quality/sprintsyntax/check.go` (NEW)
- `cmd/loopzero-ops/internal/quality/sprintsyntax/check_test.go` (NEW)
- `cmd/loopzero-ops/quality_registry.go` — add row
- `taskfiles/quality.yml` — add wiring line
- `infra/tests/check-sprint-syntax_test.sh` — DELETE
- `.claude/rules/shell-scripts-allowlist.md` — REMOVE row
- `CHANGELOG.md` — Added + Removed bullets
- `knowledge-base/audits/M16-wave1-sprintsyntax-parity-2026-07-10.md` (NEW parity artifact)

#### TDD
- RED (write `check_test.go` FIRST):
  - test_check_HappyPath_PASS — canonical bash file in `t.TempDir()`, expect `Outcome{Failures: nil}`, `err == nil`
  - test_check_BashSyntaxError_FAIL — bash file with syntax error, expect `Outcome{Failures: [msg]}`, `errors.Is(err, ErrViolations)`
  - test_check_MissingScript_FAIL — empty tempdir, expect Outcome with "script not found" failure
  - test_check_ContentMismatch_FAIL — script missing required grep patterns, expect Outcome with content-mismatch failure
- Run tests, confirm all 4 FAIL (RED phase)
- GREEN: implement `check.go` with stdlib-only. Use `exec.Command("bash", "-n", path).Run()` per Q7 Shape 1.

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Scenario 1: `bash` binary missing on PATH
  - How test reproduces: mock `exec.LookPath` to return error, OR run with sanitized PATH
  - Expected behavior: gate returns `Outcome{Failures: ["bash binary not on PATH"]}, ErrViolations` (per Q8 exit-2 translation from blueprint)

#### Deep file dependency analysis
- New pkg `sprintsyntax` imports: `os`, `os/exec`, `path/filepath`, `errors`, `fmt`, `strings`, `bufio` (stdlib only per plan D2)
- `quality_registry.go` gains: `{"sprint-syntax", "Bash script syntax + canonical pattern check (M-Migrate2 2026-07-10 replaces infra/tests/check-sprint-syntax_test.sh).", runSprintSyntax},`
- `taskfiles/quality.yml § quality:gates` gains: `- /tmp/loopzero-ops quality sprint-syntax # M-Migrate2 (2026-07-10): replaced infra/tests/check-sprint-syntax_test.sh with pure-logic Go gate.`

#### Acceptance criteria
- `go test ./cmd/loopzero-ops/internal/quality/sprintsyntax/... -race` GREEN
- `loopzero-ops quality sprint-syntax` PASS on real repo
- Parity file shows Go outcome matches bash outcome (or documented deviation approved)
- `infra/tests/check-sprint-syntax_test.sh` no longer exists
- `grep -c 'check-sprint-syntax_test.sh' .claude/rules/shell-scripts-allowlist.md == 0`
- CHANGELOG `[Unreleased] § Added` and `§ Removed` each gain one bullet

#### DoD
- Single atomic commit: `refactor(quality-gates): T2.1 migrate check-sprint-syntax to Go (M-Migrate2 Wave 1)`
- `task quality:gates` GREEN
- CHANGELOG updated

### T2.2 — Migrate `check-archive-script-repo-root_test.sh` → `cmd/loopzero-ops/internal/quality/archivescriptrepo/`

#### Why this step
Second Wave 1 gate. Shape 3 (file-layout via `grep -rEn` → `filepath.Walk`) per blueprint Q7 Shape 3.

#### Files to edit
- `cmd/loopzero-ops/internal/quality/archivescriptrepo/check.go` (NEW)
- `cmd/loopzero-ops/internal/quality/archivescriptrepo/check_test.go` (NEW)
- `quality_registry.go` + `taskfiles/quality.yml` + `shell-scripts-allowlist.md` + `CHANGELOG.md`
- DELETE `infra/tests/check-archive-script-repo-root_test.sh`

#### TDD
- test_check_CleanRepo_PASS — no forbidden `../../../..` patterns; empty Outcome
- test_check_ForbiddenTraversal_FAIL — file with `../../../..` in path; Outcome contains file:line
- test_check_MissingDir_PASS — target dir missing; treated as no-content (empty Outcome)

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Scenario: symlink loop in target dir → `filepath.Walk` returns error; gate returns `Outcome{}, error`

#### Deep file dependency analysis
- Imports: `os`, `path/filepath`, `regexp`, `strings`, `errors`
- Registry + taskfile lines follow same pattern as T2.1

#### Acceptance criteria
- Tests GREEN
- Parity artifact matches
- `task quality:gates` GREEN

#### DoD
- Commit `refactor(quality-gates): T2.2 migrate check-archive-script-repo-root (M-Migrate2 Wave 1)`

### T2.3 — Migrate `check-no-cloud-dev-default_test.sh` → `cmd/loopzero-ops/internal/quality/nocloud/`

#### Why this step
Third Wave 1 gate. Shape TBD at implementation-time (likely Shape 1 or 3).

#### Files to edit
- `cmd/loopzero-ops/internal/quality/nocloud/check.go` (NEW)
- `cmd/loopzero-ops/internal/quality/nocloud/check_test.go` (NEW)
- `quality_registry.go` + `taskfiles/quality.yml` + `shell-scripts-allowlist.md` + `CHANGELOG.md`
- DELETE `infra/tests/check-no-cloud-dev-default_test.sh`

#### TDD
- test_check_HappyPath — canonical config; empty Outcome
- test_check_CloudDevDefault_FAIL — config with `cloud-dev` default; Outcome contains file:line
- test_check_MissingConfig — missing target; documented failure mode

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Scenario: config file unreadable → gate returns `Outcome{}, error`

#### Deep file dependency analysis
- Imports TBD at read-time; likely stdlib-only per plan D2

#### Acceptance criteria
- Tests GREEN
- Parity matches
- Wave 1 complete: `task quality:gates` GREEN with all 3 new gates invoked

#### DoD
- Commit `refactor(quality-gates): T2.3 migrate check-no-cloud-dev-default (M-Migrate2 Wave 1) — Wave 1 complete`

## Phase 3 — Wave 2 (6 helm-template gates + pre-flight guards)

### T3.0 — Add pre-flight binary guards to `taskfiles/quality.yml`

#### Why this step
Plan D3 mandate: Wave 2 pre-flight guards for `helm`, `jq`, `yq` added BEFORE any Wave 2 gate wiring, per blueprint EC-3. Without this, Wave 2 gates silently NOOP when binaries absent.

#### Files to edit
- `taskfiles/quality.yml` — add 3 guards near line 160 precedent

#### TDD
- test_helm_guard_present — asserts `grep -c 'command -v helm ' taskfiles/quality.yml >= 1`
- test_jq_guard_present — asserts `grep -c 'command -v jq ' taskfiles/quality.yml >= 1`
- test_yq_guard_present — asserts `grep -c 'command -v yq ' taskfiles/quality.yml >= 1`

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Scenario 1: `helm` missing on runner PATH
  - How test reproduces: run `PATH=/nonexistent task quality:gates`
  - Expected behavior: task fails fast with `"FAIL quality:gates: helm NOT on PATH"` message (NOT with helm-not-found stack trace)
- Scenario 2: `jq` missing → fail-fast with jq guard message
- Scenario 3: `yq` missing → fail-fast with yq guard message

#### Deep file dependency analysis
- Only `taskfiles/quality.yml` touched; guards added at top of `quality:gates` task cmds sequence

#### Acceptance criteria
- 3 new `command -v` guards present
- `PATH=/nonexistent task quality:gates` fails with guard message (test-run verification)
- CHANGELOG `[Unreleased] § Changed`: `- Add pre-flight guards for helm/jq/yq in task quality:gates (per M16 D3).`

#### DoD
- Commit `chore(quality): T3.0 add pre-flight guards for helm/jq/yq (Wave 2 prerequisite)`

### T3.1 — Migrate `check-loki-vault-retention_test.sh` → `lokivaultretention/`

#### Why this step
First Wave 2 gate. Shape 2 (helm-template + regex assertion) per blueprint Q7 Shape 2.

#### Files to edit
- `cmd/loopzero-ops/internal/quality/lokivaultretention/check.go` (NEW)
- `cmd/loopzero-ops/internal/quality/lokivaultretention/check_test.go` (NEW)
- `quality_registry.go` + `taskfiles/quality.yml` + `shell-scripts-allowlist.md` + `CHANGELOG.md`
- DELETE `infra/tests/check-loki-vault-retention_test.sh`

#### TDD
- test_check_CanonicalRetention_PASS — helm-templated Loki values with correct retention; empty Outcome
- test_check_MissingRetention_FAIL — values missing retention; Outcome contains failure
- test_check_HelmMissing_FAIL — helm binary shim returns error; Outcome contains parse error

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Scenario 1: `helm template` returns non-zero exit → gate returns `Outcome{Failures: [msg]}, ErrViolations`
- Scenario 2: helm output has unexpected YAML shape → gate returns `Outcome{}, error` (unrecoverable parse)

#### Deep file dependency analysis
- Imports: `os`, `os/exec`, `path/filepath`, `regexp`, `strings`, `errors`
- Uses `exec.Command("helm", "template", ...)` — guarded by T3.0 pre-flight

#### Acceptance criteria
- Tests GREEN
- Parity matches
- `task quality:gates` GREEN

#### DoD
- Commit `refactor(quality-gates): T3.1 migrate check-loki-vault-retention (M-Migrate2 Wave 2)`

### T3.2 — Migrate `check-loki-retention-schema_test.sh` → `lokiretentionschema/`

#### Why this step
Second Wave 2 gate. Same shape as T3.1 (helm-template + regex).

#### Files to edit
- New pkg + registry row + taskfile line + allowlist row removal + CHANGELOG

#### TDD
- test_check_HappyPath_PASS + test_check_SchemaMismatch_FAIL + test_check_HelmMissing

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Same as T3.1: helm binary absence, YAML shape mismatch

#### Acceptance criteria
- Tests GREEN, parity, `task quality:gates` GREEN

#### DoD
- Commit `refactor(quality-gates): T3.2 migrate check-loki-retention-schema`

### T3.3 — Migrate `check-loopzero-external-secrets-chart-sanity_test.sh` → `esochartsanity/`

#### Why this step
Third Wave 2 gate.

#### Files to edit
- Standard M-Migrate2 files

#### TDD
- test_check_SaneChart_PASS + test_check_MissingResources_FAIL

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Helm template failure → `Outcome{}, error`

#### Acceptance criteria
- Tests GREEN, parity, task GREEN

#### DoD
- Commit `refactor(quality-gates): T3.3 migrate check-loopzero-external-secrets-chart-sanity`

### T3.4 — Migrate `check-loopzero-external-secrets-creation-policy_test.sh` → `esocreationpolicy/`

#### Why this step
Fourth Wave 2 gate.

#### Files to edit
- Standard M-Migrate2 files

#### TDD
- test_check_CreationPolicyOwner_PASS + test_check_WrongPolicy_FAIL

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Helm failure fallback

#### Acceptance criteria
- Tests GREEN, parity, task GREEN

#### DoD
- Commit `refactor(quality-gates): T3.4 migrate check-loopzero-external-secrets-creation-policy`

### T3.5 — Migrate `check-vault-prom-alerts_test.sh` → `vaultpromalerts/`

#### Why this step
Fifth Wave 2 gate.

#### Files to edit
- Standard M-Migrate2 files

#### TDD
- test_check_AlertsPresent_PASS + test_check_MissingAlerts_FAIL

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Prometheus rule YAML unreadable → error

#### Acceptance criteria
- Tests GREEN, parity, task GREEN

#### DoD
- Commit `refactor(quality-gates): T3.5 migrate check-vault-prom-alerts`

### T3.6 — Migrate `check-day30-day60-cronjobs_test.sh` → `day30day60cronjobs/`

#### Why this step
Sixth Wave 2 gate.

#### Files to edit
- Standard M-Migrate2 files

#### TDD
- test_check_CronsPresent_PASS + test_check_MissingCrons_FAIL

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- CronJob YAML shape mismatch

#### Acceptance criteria
- Tests GREEN, parity, Wave 2 close: task GREEN

#### DoD
- Commit `refactor(quality-gates): T3.6 migrate check-day30-day60-cronjobs — Wave 2 complete`
- CHANGELOG closing bullet for Wave 2

### T3.7 — Migrate `check-openbao-init-auth-mounts_test.sh` → `openbaoinitauth/`

#### Why this step
Seventh Wave 2 gate — verify at implementation-time whether this is in scope (M16 spec says "6" but 7 candidates listed in ROADMAP; reconcile).

#### Files to edit
- Standard M-Migrate2 files (if in scope)

#### TDD
- test_check_HappyPath_PASS + test_check_MissingMounts_FAIL

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- OpenBao init YAML mismatch

#### Acceptance criteria
- Tests GREEN, parity, task GREEN (if in scope) OR mark as Wave 3 candidate (if out of scope)

#### DoD
- Commit `refactor(quality-gates): T3.7 migrate check-openbao-init-auth-mounts` OR document deferral

## Phase 4 — Wave 3 (11 bash-target pairs)

### T4.0 — Wave 3 split decision (plan D4 protocol)

#### Why this step
Plan D4 mandate: measure Waves 1+2 velocity + decide split before starting Wave 3.

#### Files to edit
- `knowledge-base/audits/M16-wave3-decision-{date}.md` (NEW)

#### TDD
- test_wave3_decision_document_exists — asserts `test -f knowledge-base/audits/M16-wave3-decision-*.md`
- test_wave3_decision_declares_scope — asserts document contains either "Wave 3 remains 11 pairs" or "Wave 3 split into 3a+3b with 3b promoted to M17"

#### Concurrency tests
(none — single-threaded)

#### Acceptance criteria
- Decision document exists with quantitative velocity data + split verdict

#### DoD
- Commit `docs(audit): T4.0 Wave 3 split decision`

### T4.1 through T4.11 — Migrate 11 bash-target pairs

Each pair migrates BOTH the bash test AND its target script. Ordered by increasing complexity:

- T4.1 — `migrate-legacy-namespaces` pair
- T4.2 — `rotate-secret` pair
- T4.3 — `enable-vault-backup` pair
- T4.4 — `restore-vault-snapshot` pair
- T4.5 — `seed-loopzero-secrets` pair
- T4.6 — `vault-sync` pair
- T4.7 — `generate-secrets-vault-fallback` pair
- T4.8 — `promtail-vault-scrape` pair
- T4.9 — `promtail-vault-empty-targets` pair
- T4.10 — `scoped-kubeconfig-cold-bootstrap` pair
- T4.11 — `scoped-kubeconfig-vault` pair

Each follows the 14-step Migration recipe from the blueprint.

#### Why this step (representative)
Complete the M-Migrate2 strangler by migrating both test AND target so no orphan tests remain.

#### Files to edit (per T4.N)
- New Go pkg + test + registry row + taskfile line + allowlist row removal + CHANGELOG bullets
- DELETE both `infra/tests/check-<script>_test.sh` AND `infra/scripts/**/<target>.sh`

#### TDD (per T4.N)
- test_check_HappyPath_PASS
- test_check_FailureScenario_FAIL (per Q8 exit-1 translation)
- test_check_StructuralError (per Q8 exit-2 translation)

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios (per T4.N)
- Documented per pair at implementation-time based on target script behavior

#### Acceptance criteria (per T4.N)
- Tests GREEN, parity matches, task GREEN, bash + target both deleted

#### DoD (per T4.N)
- Commit `refactor(quality-gates): T4.N migrate <pair-name> pair (M-Migrate2 Wave 3)`

## Phase 5 — Integration Validation

### T5.1 — Full test suite validation

#### Why this step
Terminal gate: prove the whole M16 chain works end-to-end before /release.

#### Files to edit
- None (validation only)

#### TDD
- test_go_test_all_races_green — `go test ./cmd/loopzero-ops/... -race` exit 0
- test_go_vet_clean — `go vet ./...` exit 0
- test_golangci_lint_clean — `golangci-lint run ./cmd/loopzero-ops/...` exit 0
- test_quality_gates_green — `task quality:gates` exit 0

#### Concurrency tests
Yes — `go test -race` IS the race-detector run. This IS the concurrency test for the entire M16 delivery (per plan Baseline: gates use no shared mutable state, but the race detector proves it).

#### Failure scenarios
- Scenario: any test flakes → HALT; fix source of flakiness before advancing to T5.2

#### Acceptance criteria
- 4 test-run gates GREEN

#### DoD
- Commit `test(quality): T5.1 M16 integration validation GREEN`

### T5.2 — Failure-scenarios chaos pass

#### Why this step
Plan D3 mandate: prove Wave 2 pre-flight guards fail-fast, not silent-NOOP.

#### Files to edit
- None (chaos test only)

#### TDD
- test_helm_absent_fails_fast — `PATH=/nonexistent:/usr/bin task quality:gates` exits 1 with `"FAIL quality:gates: helm NOT on PATH"` message
- test_jq_absent_fails_fast — same for jq
- test_yq_absent_fails_fast — same for yq

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Guard missing → test FAILS (this is the whole point)

#### Acceptance criteria
- 3 chaos probes emit correct guard messages

#### DoD
- Commit `test(quality): T5.2 M16 chaos pass — pre-flight guards fail-fast verified`

### T5.3 — Allowlist delta assertion

#### Why this step
Success criterion 5 + 6: 27 rows removed + delta measurable.

#### Files to edit
- None (measurement only)

#### TDD
- test_allowlist_bash_count_delta — `grep -c '| infra/tests/check-' .claude/rules/shell-scripts-allowlist.md` decreased by ≥ 27 from M16-start baseline

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Delta < 27 → HALT; some scripts not migrated

#### Acceptance criteria
- Delta ≥ 27

#### DoD
- Commit `test(quality): T5.3 M16 allowlist delta verified — 27 rows removed`

### T5.4 — Grep sweep for pendured references

#### Why this step
Success criterion 4 hygiene: no stale references to deleted scripts.

#### Files to edit
- None (sweep only)

#### TDD
- test_no_pending_references — for each of the 27 deleted script names, grep across `taskfiles/`, `docs/`, `.claude/skills/` returns 0 hits

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Any hit → identify caller + either update reference to new gate name OR delete stale doc

#### Acceptance criteria
- 27 × 0 hits = zero pendured refs

#### DoD
- Commit `test(quality): T5.4 M16 grep sweep clean — zero pendured refs`

### T5.5 — CHANGELOG discipline check

#### Why this step
Unbreakable Rule 6: CHANGELOG bullets present per script migrated.

#### Files to edit
- None (measurement only)

#### TDD
- test_changelog_added_count — `[Unreleased] § Added` has ≥ 27 new-gate bullets
- test_changelog_removed_count — `[Unreleased] § Removed` has ≥ 27 bash-script bullets
- test_changelog_changed_count — `[Unreleased] § Changed` has ≥ 3 bullets (Phase 1 hygiene + Phase 3 guards + Phase 4 decision doc)

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Any count < required → HALT; backfill missing bullets

#### Acceptance criteria
- All 3 counts correct

#### DoD
- Commit `test(quality): T5.5 M16 CHANGELOG discipline verified`

### T5.6 — ROADMAP.md checkbox flip contract pre-verification

#### Why this step
Contract with cycle-release: plan frontmatter `milestone_id: M16` MUST match a ROADMAP.md milestone header exactly, so post-merge auto-flip works.

#### Files to edit
- None (measurement only)

#### TDD
- test_plan_frontmatter_carries_milestone_id — `head -20 knowledge-base/plans/infra-tests-hermetic-to-quality-gates-plan.md | grep -c 'milestone_id: M16' == 1`
- test_roadmap_has_m16_unchecked — `grep -c '^### M16 — \[ \]' ROADMAP.md == 1` (pre-release state)
- test_roadmap_has_m16_target_header — `grep -c '^### M16 —' ROADMAP.md == 1`

#### Concurrency tests
(none — single-threaded)

#### Failure scenarios
- Frontmatter missing → HALT; edit plan
- Header mismatch → HALT; verify slug/frontmatter matches ROADMAP.md M16 line verbatim

#### Acceptance criteria
- All 3 assertions GREEN

#### DoD
- Commit `test(quality): T5.6 M16 checkbox-flip contract verified pre-release`
- **Phase 5 close: `<promise>IMPLEMENTATION_COMPLETE</promise>` from cycle-implement halt-loop**

## Global Definition of Done

- [ ] Every phase task committed atomically on `develop` (never `main` — Unbreakable Rule 4)
- [ ] Every commit references its task ID (`T{N.M}` convention)
- [ ] `task quality:gates` GREEN at final commit
- [ ] `go test ./cmd/loopzero-ops/... -race` GREEN (T5.1)
- [ ] `go vet ./...` clean (T5.1)
- [ ] `golangci-lint run ./cmd/loopzero-ops/...` clean (T5.1)
- [ ] CHANGELOG `[Unreleased]` populated per phase (T5.5)
- [ ] All 27 bash tests physically deleted (T5.3)
- [ ] All 27 gates registered in `quality_registry.go` (T5.6)
- [ ] All 27 gates wired in `taskfiles/quality.yml § quality:gates` with docs comments (T5.6)
- [ ] All 27 allowlist rows removed from `.claude/rules/shell-scripts-allowlist.md` (T5.3)
- [ ] 5 pre-Wave-1 backfilled gates wired (Phase 1 close)
- [ ] 3 pre-flight guards for helm/jq/yq present (T3.0 + T5.2)
- [ ] Wave 3 split decision documented (T4.0)
- [ ] 27 parity artifacts stored under `knowledge-base/audits/M16-*-parity-*.md`
- [ ] Plan frontmatter `milestone_id: M16` present (T5.6)
- [ ] Zero `Co-Authored-By` trailers on any commit (Unbreakable Rule + user policy)
- [ ] File-size budget respected: no file > 500 LoC per architecture.md defaults
- [ ] Cross-repo consumers not broken (grep sweep at T5.4)
- [ ] Race detector clean on gates that touch shared state (T5.1 -race flag)

## Cross-references

- Discovery blueprint: [`knowledge-base/discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md`](../discoveries/blueprints/infra-tests-hermetic-to-quality-gates-blueprint.md)
- Discovery plan (v1.1): `knowledge-base/discoveries/plans/infra-tests-hermetic-to-quality-gates-plan.md`
- Confidence reports: `knowledge-base/reviews/infra-tests-hermetic-to-quality-gates-{discover-plan-confidence,discover-confidence,edge-cases}-2026-07-10.{json,md}`
- Roadmap: `ROADMAP.md § M16`
- 12 M-Migrate2 precedents: `cmd/loopzero-ops/internal/quality/{allowlistsunsets,caddyfilenodockerip,optionbeso,vaultbootstrapinmgmt,canonicalfixturerepo,esooverlay,installgithooks,k6loadrigor,oidccredadminloopzero,routeauthcoverage,webhookcanonicalpath,vaultbootstrapunsealloop}/`
- Registry: `cmd/loopzero-ops/quality_registry.go`
- Taskfile: `taskfiles/quality.yml`
- Allowlist: `.claude/rules/shell-scripts-allowlist.md`
- Rules consumed: `.claude/rules/cycle-plan.md`, `.claude/rules/cycle-implement.md`, `.claude/rules/testing.md`, `.claude/rules/tdd-workflow.md`, `.claude/rules/parsimony-ladder.md`, `.claude/rules/architecture.md`, `.claude/rules/error-handling.md`, `.claude/rules/git-safety.md`, `.claude/rules/shell-scripts-allowlist.md`
- Unbreakable Rules: 1, 3, 4, 6, 7, 9, 10, 11
