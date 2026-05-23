# Plan: Incremental bash→Go Migration (Strangler-Fig)

> **Version 1.0** — Migrate the remaining load-bearing shell scripts to Go (`cmd/loopzero-ops` + `pkg/`) **one script per PR**, with mandatory rewiring of all four caller surfaces and a LIVE gate before each bash deletion. This replaces the abandoned `develop-bkp` big-bang (111 commits, +72k LOC) whose Go code was healthy but left ~15 dangling caller references that broke the drill/cleanroom/chaos SKILLs. The strangler-fig cadence makes each step independently revertible and provable, so the system is never left in a broken intermediate state.

## Context

The `develop-bkp` branch attempted a single-shot bash→Go migration: 448 files changed, +72,582 / −27,886 lines, 201 new Go files replacing ~49 shell scripts across 111 commits. Investigation (2026-05-23) established:

- **The Go code is healthy.** On `develop-bkp`: `go build ./pkg/...` exit 0, `go test ./pkg/...` **0 FAIL** (~140 packages), `cmd/loopzero-ops` builds, `go vet` clean.
- **The migration failed at the integration boundary, not the logic.** It deleted `infra/scripts/dr/` entirely (11 scripts) but left dangling references in caller surfaces. Commit `2f760f5e` admits rewiring only "9 broken refs"; the precise live breakage (~15 executable references) is concentrated in the 4 drill SKILL.md files (`cleanroom-drill`, `chaos-test-drill`, `dogfood-drill`, `drill-test`) which still instruct operators to `bash infra/scripts/dr/<orchestrator>.sh` — files that no longer exist — plus 2 surviving scripts pointing at deleted `dr/jwt-diagnose.sh`.
- **No finding doc or log.md entry** documents the failure — it was abandoned, not diagnosed. `develop` was recreated from `refactor/shell-scripts-strategic` (the known-good state); `develop-bkp` is preserved locally + at `origin/develop-bkp` for salvage.

This is the LoopZero systemic pattern inverted: not "implemented but not wired" but **"deleted but callers not rewired"** — a partial cutover with no LIVE gate to catch it.

The current `develop` already has the foundation the big-bang built on: **31 `pkg/quality/*` gates migrated** + `cmd/loopzero-ops` CLI entrypoint. N(bash) = **209**. The strategic direction (shell is hard for humans to maintain; pure-logic belongs in tested Go) remains correct — this plan changes the *execution method*, not the goal.

Related: `docs/plans/2026-05-22-shell-script-strategic-plan.md` (D1-D5: no-new-bash gate, helmfile delegation, pure-logic Go gates, D4 keep-criteria), `docs/meetings/MEET-2026-05-21-bash-to-go-migration.md` (on `develop-bkp`).

## Objective

"Done" = every script selected for migration has been ported to Go, all four caller surfaces rewired, validated by the LIVE gate that exercises it, and the bash deleted — **with `develop` never broken between PRs**.

Measurable goals:
- Each migrated script lands in its own PR (or tightly-scoped commit group) that is independently green: `go test`, `task lint`, and the LIVE gate exercising that script.
- Zero dangling references after each deletion (enforced by a new `no-dangling-script-refs` gate).
- The 4 drill SKILLs always reference *existing* invocation paths (enforced by `infra/tests/skills/skill-paths_test.sh` extension).
- Net reduction in N(bash) tracked per PR; no big-bang.

## ADRs

### D1 — Strangler-fig cadence, one script per PR (NOT big-bang)
- **Decision:** Each script is migrated in isolation: port logic → rewire ALL callers → run LIVE gate → delete bash → next. No PR deletes a bash script whose callers are not 100% rewired in the same PR.
- **Rationale:** The `develop-bkp` big-bang failed precisely because deletions outran rewiring and no gate caught it. One-script-per-PR keeps every intermediate state shippable and revertible (`git revert` of one PR, not untangling 111 commits).
- **Consequences:** Slower wall-clock, but every step is provable. Enables partial completion — we can stop at any point with a working `develop`.

### D2 — Mandatory 4-surface rewiring checklist per migration
- **Decision:** Every migration task carries a checklist covering the four caller surfaces that the big-bang missed: (1) **Taskfile/taskfiles**, (2) **CI workflows** (`.github/workflows/`), (3) **SKILL.md** operator instructions, (4) **surviving shell scripts + Pulumi TS** (`infra/pulumi*/src/`). A task is not done until `grep -rn <deleted-basename>` returns zero live references outside the allowlist/audit/provenance-comment exclusions.
- **Rationale:** The root cause was an incomplete caller inventory ("9 refs" when there were many more). Making the four surfaces explicit and grep-verified prevents recurrence.
- **Consequences:** Each task ends with a grep-clean assertion. Adds a `no-dangling-script-refs` CI gate as the automated backstop.

### D3 — Salvage `develop-bkp` Go code, do not re-author
- **Decision:** Where `develop-bkp` already has a green Go port (e.g. `pkg/pulumi/archive`, `pkg/pulumi/orphanrm`, `pkg/quality/*` not yet on `develop`), cherry-pick the package + its tests rather than rewriting. Re-validate `go test` on `develop` before wiring.
- **Rationale:** The Go logic was proven correct (0 FAIL). Rewriting wastes effort and risks regressions. Don't reinvent what already passes tests.
- **Consequences:** Cherry-pick conflicts possible (the package may import helpers that also need picking) — resolved per-package, not bulk.
- **EC-4 (SHOULD TEST):** `develop-bkp` packages may import shared helpers (`pkg/shellrun`, `pkg/drillhelmfile`, etc.) that don't exist on `develop`. **Guard:** run `go build ./pkg/<pkg>/...` IMMEDIATELY after each cherry-pick; on a missing-import failure, pick the helper in the same PR before wiring the subcommand. Never wire a subcommand whose package doesn't build.

### D4 — LIVE gate selection is per-script and mandatory before deletion
- **Decision:** Each script maps to the LIVE gate that exercises it: pure-logic gates → `loopzero-ops quality <gate>` + unit tests (hermetic, no LIVE infra); drill/cleanroom/chaos orchestrators → the corresponding skill run on real infra. The bash is deleted ONLY after the Go path passes its gate. For operator-gated LIVE flows (drill on DOKS, chaos on k3d), deletion waits for explicit operator validation (HARD STOP).
- **Rationale:** Unit-test-green masked the big-bang breakage. Only the gate that actually runs the code proves the rewiring.
- **Consequences:** High-risk `dr/` orchestrators migrate LAST and require operator LIVE runs — they cannot be auto-completed by an autonomous loop.

### D5 — Risk-tiered ordering: lowest blast radius first
- **Decision:** Migrate in three tiers by caller blast radius: **Tier A** pure-logic (CI-only callers), **Tier B** Taskfile/CI orchestration scripts, **Tier C** SKILL-facing drill/cleanroom/chaos orchestrators (the ones that broke).
- **Rationale:** Build confidence and the rewiring muscle on low-risk scripts before touching the surfaces that caused the failure. Tier C inherits a hardened checklist + the `no-dangling-script-refs` gate from Tiers A/B.
- **Consequences:** The most valuable maintainability wins (drill SKILLs) come last, but land on a proven process.

### D6 — Stable binary, not `/tmp/loopzero-ops` rebuild-on-demand
- **Decision:** CI/Taskfile callers invoke `loopzero-ops` via a single canonical build step (cached artifact or `go run ./cmd/loopzero-ops`), not the `develop-bkp` pattern of `(cd cmd/loopzero-ops && go build -o /tmp/loopzero-ops .)` duplicated in every job.
- **Rationale:** The big-bang scattered the build-on-demand idiom across ~15 call sites — fragile and DRY-violating. One build path is simpler and testable.
- **Consequences:** A shared Taskfile target (`task ops:build`) or a documented `go run` convention; call sites reference it.

### Accepted Risks (edge-case review 2026-05-23)
- **EC-6 — partial completion may not reach the ≤30 target.** D1/D5 allow stopping anytime with a green `develop`. The ≤30 N(bash) goal from `2026-05-22-shell-script-strategic-plan` may not be hit — accepted: each PR reduces N(bash) and never breaks the system. Do NOT force a big-bang to hit a number.
- **EC-8 — tier classification is a heuristic, not a guarantee.** A "Tier-A pure-logic" script could also be SKILL-referenced. The `no-dangling-script-refs` gate (T0.1) catches this regardless of tier — the tier is only attack order; the gate is the real guard.

## Dependency Graph

```
Phase 0 (process scaffolding) ──▶ Phase 1 (Tier A: pure-logic salvage)
        │                                   │
        │                                   ▼
        │                          Phase 2 (Tier B: Taskfile/CI scripts)
        │                                   │
        ▼                                   ▼
  no-dangling gate              Phase 3 (Tier C: drill/cleanroom/chaos SKILLs)
  + skill-paths test                        │
  (BLOCKING backstop)                       ▼
                                  Phase 4 (LIVE validation — OPERATOR HARD STOP)
```

Phase 0 is a hard blocker for all tiers (the backstop gate must exist before any deletion). Phases 1→2→3 are sequential by design (D5). Phase 4 gates Tier C completion.

---

## Phase 0: Process Scaffolding

**Objective:** Build the automated backstop that makes "deleted but not rewired" impossible to merge, before migrating anything.

### T0.1 — `no-dangling-script-refs` CI gate

#### Objective
A pure-logic Go gate that fails when a `.sh` deleted in the diff is still referenced (live, executable context) anywhere in Taskfile/taskfiles/`.github/workflows/`/`infra/`/`.claude/skills/`, excluding allowlist, audit CSV, and provenance comments.

#### Evidence
The big-bang's only defense was a manual "9 refs" count that missed ~15 live references in SKILLs. Automation is the only reliable inventory.

#### Files to edit
```
pkg/quality/danglingrefs/check.go        — (NEW) gate logic: diff deleted .sh × grep live callers
pkg/quality/danglingrefs/check_test.go    — (NEW) hermetic tests with fixture repo trees
cmd/loopzero-ops/quality_subcmd.go            — register `dangling-script-refs` subcommand
.github/workflows/no-dangling-script-refs-gate.yml — (NEW) dedicated gate workflow (EC-2)
```

#### Deep file dependency analysis
- `cmd/loopzero-ops/quality_subcmd.go` today dispatches the 31 existing quality gates; adding one subcommand follows the established switch/registration pattern (OCP — new case, no edits to existing cases).
- **EC-2 (MUST FIX):** `ci.yml` does NOT invoke `loopzero-ops quality` directly — gates like `no-new-bash` live in **dedicated `*-gate.yml` workflows** with path filters + `fetch-depth: 0`. Wiring into `ci.yml` would run on every PR (waste) and `git diff <base>...<head>` fails on a shallow clone (base-ref unreachable). **Fix:** create `no-dangling-script-refs-gate.yml` mirroring `no-new-bash-gate.yml` exactly: path filter `**/*.sh`, `fetch-depth: 0` (mandatory), pre-flight `git rev-parse` confirming base-ref reachable, `BASE="origin/${{ github.event.pull_request.base.ref }}"`.

#### Deep Dives
- **Diff source:** `git diff --diff-filter=D --name-only <base>...<head> -- '*.sh'`.
- **Live-call detection:** a reference counts as "live" if the line matches `bash|source|^\s*\.|run:|exec|script=` AND contains the deleted path (full path or `<dir>/<base>` form used by SKILLs). Pure comments (`^\s*#`, "Migrated from", "replaced by", "per plan") are excluded.
- **Basename-collision guard:** match `<parentdir>/<basename>` not bare basename — `dr/preflight.sh` must NOT match `tests/parity/preflight.sh` (lesson from this investigation's inflated "53" false count).
- **Edge cases:** rename (R) treated as delete-of-old-path; script referencing itself in its own removal commit; allowlist `.md` legitimately naming the script.

#### Tasks
1. Write `check_test.go` with fixtures: (a) deleted script with live SKILL ref → FAIL, (b) deleted script with only comment ref → PASS, (c) basename collision (different dir survives) → PASS, (d) rename → treated as delete.
2. Implement `Check` consuming `git diff` output (injectable for tests) + repo-root grep.
3. Register subcommand + wire `ci.yml` step.
4. Verify the gate would have caught `develop-bkp` (run against `develop...develop-bkp` diff → expect FAIL listing the 4 SKILLs).

#### TDD
```
RED:     TestDangling_LiveSkillRef_Fails — deleted dr/x.sh referenced via `bash dr/x.sh` in SKILL.md → non-zero
RED:     TestDangling_CommentOnly_Passes — deleted dr/x.sh only in "Migrated from" comment → zero
RED:     TestDangling_BasenameCollision_Passes — dr/preflight.sh deleted, tests/parity/preflight.sh survives + referenced → zero
RED:     TestDangling_Rename_TreatedAsDelete — R old→new, old path referenced → non-zero
GREEN:   Implement danglingrefs.Check
REFACTOR: Extract live-call matcher shared with future gates if a 2nd consumer appears (else inline — YAGNI)
VERIFY:  cd pkg && go test ./quality/danglingrefs/...
```

#### Acceptance Criteria
- [ ] Gate FAILs on the `develop...develop-bkp` diff, naming the 4 broken SKILLs
- [ ] Gate PASSes on a clean diff
- [ ] Basename collision does not produce false positive
- [ ] Pass: /code-audit complexity (CCN ≤ 10), coverage ≥ 90%, lint zero warnings, file ≤ 500 lines

#### DoD
- [ ] `cd pkg && go test ./quality/danglingrefs/...` green
- [ ] `loopzero-ops quality dangling-script-refs --base-ref origin/develop --head-ref HEAD` runs
- [ ] `no-dangling-script-refs-gate.yml` added (path filter + fetch-depth: 0, EC-2); zero golangci-lint warnings

### T0.2 — Extend `skill-paths_test.sh` to validate drill-skill invocation paths

#### Objective
The existing `infra/tests/skills/skill-paths_test.sh` checks SKILL.md paths resolve. Extend it to assert that every `bash <path>.sh` invocation in the 4 drill SKILLs points at an existing file (catches the exact failure class at test time, independent of git diff).

#### Files to edit
```
infra/tests/skills/skill-paths_test.sh   — add assertion: extract `.sh` refs from drill SKILLs, assert file exists
```

#### Deep Dives (EC-1 — MUST FIX)
- The existing grep is `infra/scripts/[a-zA-Z0-9_./-]+\.sh` — it MISSES the **abbreviated** form the SKILLs actually use (verified 2026-05-23: ``Pular `dr/teardown-validator.sh` `` escapes it). The SKILLs reference scripts in TWO forms: full-path (`bash infra/scripts/dr/preflight.sh`) and bare (`dr/teardown-validator.sh`). A SKILL referencing a deleted script in the bare form would pass the test falsely — the exact failure class this gate exists to catch.
- **Fix:** widen the extraction to `(infra/scripts/)?(dr/)?[a-zA-Z0-9_./-]+\.sh` and resolve bare/relative paths against `infra/scripts/` before the existence check.

#### TDD
```
RED:     temp SKILL line `bash infra/scripts/dr/ghost.sh` (full form) → test FAILs
RED:     temp SKILL line `Pular `dr/ghost.sh`` (abbreviated form, EC-1) → test FAILs
GREEN:   widen extraction regex + resolve relative paths against infra/scripts/; remove temp lines → PASS
VERIFY:  bash infra/tests/skills/skill-paths_test.sh
```

#### Acceptance Criteria
- [ ] Test FAILs if any drill SKILL references a non-existent `.sh` in EITHER full-path OR abbreviated `dr/x.sh` form (EC-1)
- [ ] Test PASSes on current `develop`

#### DoD
- [ ] `bash infra/tests/skills/skill-paths_test.sh` exit 0 on develop
- [ ] Wired in the same CI job that already runs skill-paths

### T0.3 — `task ops:build` canonical binary target (D6)

#### Objective
One Taskfile target that builds `loopzero-ops` to a stable path; document `go run ./cmd/loopzero-ops` as the alternative for CI. Replace any `/tmp/loopzero-ops` rebuild-on-demand idiom as scripts get migrated.

#### Files to edit
```
taskfiles/test.yml (or a new ops.yml include) — add `ops:build` target
docs/operations/loopzero-ops.md (NEW)             — invocation convention for migrated gates
```

#### TDD
```
RED:     bats/shell test asserting `task ops:build` produces an executable + `loopzero-ops --help` exits 0
GREEN:   implement target
VERIFY:  task ops:build && <bin> --help
```

#### Acceptance Criteria
- [ ] `task ops:build` produces a runnable binary
- [ ] Convention doc exists; no new `/tmp/loopzero-ops` idioms introduced in later phases

#### DoD
- [ ] Target runs; doc committed

---

## Phase 1: Tier A — Pure-Logic Salvage (CI-only callers)

**Objective:** Migrate scripts whose only callers are CI/Taskfile gate steps, salvaging `develop-bkp`'s green Go packages. Lowest blast radius — build the rewiring muscle here.

> **Per-script task template (applies to every T1.x):** ① cherry-pick or port the Go package + tests from `develop-bkp`; ② re-run `go test` on `develop`; ③ register `loopzero-ops` subcommand; ④ **4-surface rewire** (Taskfile/CI/SKILL/surviving-scripts) — grep-clean; ⑤ run the hermetic gate; ⑥ delete the bash + its `_test.sh`; ⑦ assert `no-dangling-script-refs` clean; ⑧ remove allowlist entry.

> **Execution status (2026-05-23):**
> - ✅ **T1.4** migration_test_coverage → `pkg/quality/migrationtestcoverage` (pure-logic, hermetic). Done; verdict parity preserved.
> - ✅ **T1.5** pkg-usage-matrix → `pkg/quality/pkgusagematrix` (pure-logic, hermetic). Done; verdict parity preserved.
> - ⚠️ **T1.3** crd-semantic-diff + ⚠️ **T1.6** helm-diff-structural are **NOT pure-logic** (EC-4 finding): they shell out to `helm template`/`helm diff` via `pkg/shellrun` — a ~1100-LOC shared abstraction (9 files incl. tailpods/mock/OS-specific) absent on `develop`. Adopting `pkg/shellrun` is an **automatic T2** (new shared `pkg/`) requiring a governance decision, not an autonomous step. **Blocked pending T2** — see finding `finding-2026-05-23-shellrun-dependency-blocks-tier-a.md`.
> - ⚠️ **T1.1** pulumi-archive: operator-gated (real archive needs Pulumi) + has a non-obvious caller surface (`adr-verification-matrices.test.ts` asserts the bash file EXISTS — that assertion must be updated when the bash is deleted).
>
> **Refined Tier-A reality:** only 2 of the 5 listed candidates were truly pure-logic/hermetic (both done). The rest carry a shared-helper or runtime dependency — effectively Tier-A.5/B.

### T1.1 — `pulumi-archive.sh` → `pkg/pulumi/archive`
- **Salvage source:** `develop-bkp` commit `b8659429` (green).
- **Callers:** Taskfile drill targets + (per allowlist) MIGRATE-GO-SDK candidate.
- **TDD:** cherry-pick brings `archive_test.go`; add a wiring test asserting `loopzero-ops pulumi archive --dry-run` exits 0.
- **LIVE gate (D4):** dry-run only (no real Pulumi state mutation in CI); real archive is operator-gated.
- **Acceptance:** bash deleted; `grep -rn pulumi-archive.sh` zero live refs; allowlist row removed.

### T1.2 — `pulumi-orphan-rm.sh` → `pkg/pulumi/orphanrm`
- **Salvage source:** `develop-bkp` commit `d75daf7d`.
- **Note:** This script was already deleted on `develop` (per `adr-verification-matrices.test.ts` line 90 — "deleted 2026-05-22, recreate ad-hoc"). **Decision:** if not needed, SKIP migration (YAGNI) — only migrate if a caller resurfaces. Document as N/A in coverage matrix.

### T1.3 — `crd-semantic-diff.sh` → `pkg/quality/crdsemanticdiff`
- **Salvage source:** `develop-bkp` commit `c5810e7c`.
- **Callers:** `.github/workflows/helm-charts.yml` + `infra/scripts/crd-semantic-diff.test.sh`.
- **4-surface note:** the `.test.sh` companion must be ported to `check_test.go` and deleted together.
- **LIVE gate:** hermetic (`loopzero-ops quality crd-semantic-diff` on fixture CRDs).

### T1.4 — `migration_test_coverage.sh` → `pkg/quality/migrationtestcoverage`
- **Salvage source:** `develop-bkp` commit `9e526a95`.
- **Callers:** CI coverage job.
- **LIVE gate:** hermetic.

### T1.5 — `pkg-usage-matrix.sh` → `pkg/quality/pkgusagematrix`
- **Salvage source:** `develop-bkp` commit `7b4c88b2`.
- **Callers:** `taskfiles/test.yml` (`--check` / `--write`).
- **LIVE gate:** hermetic; assert `--write` is idempotent.

### T1.6 — `helm-diff-structural.sh` → `pkg/quality/helmdiffstructural`
- **Salvage source:** `develop-bkp` commit `3fbac925` (includes `_test.sh` port).
- **Callers:** helmfile pre-sync + `infra/scripts/helm-diff-structural.sh` direct.
- **Caution:** this shells out to the `helm diff` plugin — the Go port wraps the external CLI, not reimplements it (KISS: don't reinvent helm-diff). Verify it's still a thin wrapper, not a shell-wrap anti-pattern flagged by `no-shell-wrap`.

**Phase 1 exit:** ≥4 Tier-A scripts migrated, each its own PR, `develop` green throughout, N(bash) reduced by the count migrated.

---

## Phase 2: Tier B — Taskfile/CI Orchestration Scripts

**Objective:** Migrate scripts with Taskfile + CI callers (no SKILL operator-facing surface yet). Each carries the full D2 checklist; the `no-dangling-script-refs` gate is now BLOCKING.

Candidate set (from allowlist MIGRATE-* dispositions + `develop-bkp` salvage):
- `check-drill-credentials-isolation.sh` → `pkg/drill/credentialsisolation` (salvage `82c87f66`)
- `check-drill-outputs-consumers.sh` → `pkg/quality/drilloutputs` (salvage `0f46c895`)
- `lib/pulumi-output.sh` → `pkg/pulumi/output` (salvage exists)
- `audit-helm-drift.sh` → `pkg/quality/helmdrift` (salvage `d0b407b5`)

> Each follows the per-script template. The distinguishing risk vs Tier A: callers include `taskfiles/drill.yml` and multiple workflows — the grep-clean assertion spans more files. Use `task ops:build` (D6), never `/tmp/loopzero-ops` rebuild-on-demand.

### T2.1 (representative) — `check-drill-credentials-isolation.sh` → `pkg/drill/credentialsisolation`

#### Files to edit
```
pkg/drill/credentialsisolation/{check.go,check_test.go}  — cherry-pick from develop-bkp 82c87f66
cmd/loopzero-ops/drill_subcmd.go (or quality)                — register subcommand
.github/workflows/drill-hermetic-tests.yml               — repoint to loopzero-ops invocation
taskfiles/drill.yml                                       — repoint if referenced
.claude/rules/shell-scripts-allowlist.md                 — remove row
infra/scripts/check-drill-credentials-isolation.sh + _test.sh — DELETE (after gate green)
```

#### TDD
```
RED:     cherry-picked check_test.go must pass on develop (re-validate, not re-author)
RED:     wiring test: loopzero-ops drill credentials-isolation on fixture → expected exit codes
GREEN:   register subcommand
REFACTOR: none expected (salvaged code)
VERIFY:  cd pkg && go test ./drill/credentialsisolation/... && bash no-dangling check
```

#### Acceptance Criteria
- [ ] `go test` green (salvaged tests pass on develop)
- [ ] `grep -rn check-drill-credentials-isolation.sh` → zero live refs
- [ ] `no-dangling-script-refs` gate green for this PR's diff
- [ ] allowlist row removed; N(bash) −2 (script + its _test.sh)

#### DoD
- [ ] Hermetic CI gate green; lint clean; bash deleted

---

## Phase 3: Tier C — Drill/Cleanroom/Chaos SKILL Orchestrators (the ones that broke)

**Objective:** Migrate the `infra/scripts/dr/*.sh` orchestrators (11 scripts) that the big-bang deleted while leaving the 4 SKILLs broken. This tier lands on the hardened process and the `no-dangling-script-refs` + `skill-paths` backstops.

**Critical difference from Tiers A/B:** the 4th caller surface is **SKILL.md operator instructions**. Each `dr/` migration MUST rewrite the corresponding SKILL section to invoke the Go path (`loopzero-ops drill <subcmd>` via `task ops:build`), and the `skill-paths_test.sh` extension (T0.2) blocks any SKILL referencing a deleted `.sh`.

Mapping (salvage Go from `develop-bkp` `pkg/drill`):
| bash (deleted on bkp) | Go target | SKILL surface to rewrite |
|---|---|---|
| `dr/preflight.sh` | `pkg/drill` preflight | cleanroom-drill, drill-test |
| `dr/teardown-validator.sh` | `pkg/drill` teardown-validator | drill-test, cleanroom-drill |
| `dr/state-reset.sh` | `pkg/drill` state-reset | drill-test, cleanroom-drill |
| `dr/cancel-refresh.sh` | `pkg/drill` cancel-refresh | drill-test |
| `dr/watchdog.sh` | `pkg/drill` watchdog | taskfiles/drill.yml, auto-destroy-watchdog.yml |
| `dr/cost-report.sh` | `pkg/drill` cost-report | drill-test, pulumi-drill index.ts |
| `dr/chaos-orchestrator.sh` | `pkg/chaos` orchestrator | chaos-test-drill |
| `dr/cleanroom-orchestrator.sh` | `pkg/cleanroom` orchestrator | cleanroom-drill |
| `dr/dogfood-orchestrator.sh` | `pkg/dogfood` orchestrator | dogfood-drill |
| `dr/jwt-diagnose.sh` | `pkg/drill` jwt-diagnose | drill-helmfile-bootstrap.sh, lib/drill-jwt-gate.sh diagnostic pointers |
| `dr/evidence-capture.sh` | `pkg/drill` evidence-capture | (CI + skills) |

### T3.1 (representative) — `dr/preflight.sh` → `pkg/drill` preflight

#### Objective
Port the operator pre-flight consolidator (credentials + isolation + leftover-stack checks, 5 structured exit codes, `--json`) to Go; rewire Taskfile + CI + the 2 SKILLs + surviving callers; delete the bash.

#### Evidence
`dr/preflight.sh` had the widest dangling-ref footprint in the failure (referenced by taskfiles/drill.yml, multi-cell.yml, parity-validation.yml, cleanroom-drill SKILL). It is the canonical example of the rewiring discipline.

#### Files to edit
```
pkg/drill/{preflight.go,preflight_test.go}     — salvage from develop-bkp pkg/drill
cmd/loopzero-ops/drill_subcmd.go                    — register `drill preflight`
taskfiles/drill.yml                             — repoint to `task ops:build` + `loopzero-ops drill preflight`
.github/workflows/multi-cell.yml                — repoint (note: this calls tests/parity/preflight.sh — verify which preflight!)
.github/workflows/parity-validation.yml         — repoint if it calls dr/preflight
.claude/skills/cleanroom-drill/SKILL.md          — rewrite invocation + troubleshooting rows
.claude/skills/drill-test/SKILL.md               — rewrite invocation rows
infra/scripts/dr/preflight.sh                    — DELETE (after LIVE gate)
.claude/rules/shell-scripts-allowlist.md         — remove row
```

#### Deep Dives
- **Basename-collision trap (from this investigation):** `multi-cell.yml:165` calls `infra/tests/parity/preflight.sh`, NOT `dr/preflight.sh`. Do NOT repoint the parity preflight. The grep-clean assertion must use the full `dr/preflight.sh` path.
- **Exit-code contract:** preserve the 5 structured codes (0 PASS / 1 creds / 2 scripts / 3 isolation / 4 stack / 5 arg) — these are an operator contract; the Go port must match exactly.

#### TDD
```
RED:     salvaged preflight_test.go passes on develop (re-validate)
RED:     exit-code parity test: each failure mode → documented code
RED:     skill-paths test: cleanroom-drill no longer references dr/preflight.sh
GREEN:   register subcommand + rewrite SKILLs
VERIFY:  cd pkg && go test ./drill/... ; bash infra/tests/skills/skill-paths_test.sh ; no-dangling gate
```

#### Acceptance Criteria
- [ ] `go test ./pkg/drill/...` green
- [ ] 5 exit codes match the bash contract
- [ ] Both SKILLs invoke `loopzero-ops drill preflight`; zero `dr/preflight.sh` live refs
- [ ] `skill-paths_test.sh` + `no-dangling-script-refs` green
- [ ] **LIVE (D4, operator HARD STOP):** `loopzero-ops drill preflight` runs in a real drill pre-flight before the bash is deleted

#### DoD
- [ ] Hermetic tests green; SKILLs rewritten; LIVE preflight validated by operator; bash deleted

> **T3.2–T3.11:** one task per remaining `dr/` script, same template. The orchestrators (`chaos/cleanroom/dogfood-orchestrator`) require their full skill run on LIVE infra (Phase 4) before deletion.

---

## Phase 4: LIVE Validation — OPERATOR HARD STOP

**Objective:** Prove the Tier-C Go orchestrators actually drive the LIVE drill/cleanroom/chaos flows before their bash is deleted. Autonomous loops CANNOT complete this phase.

### T4.1 — Cleanroom + dogfood + chaos LIVE on migrated Go orchestrators
- Run `/cleanroom`, `/dogfood full`, `/chaos-test --full` where the skills now invoke the Go orchestrators.
- Each must pass at the same bar as the current bash (cleanroom doctor 38+/0, dogfood 0 S0/S1, chaos 10/10).
- ONLY after a LIVE pass is the corresponding `dr/*orchestrator.sh` deleted.

### T4.2 — Drill LIVE (DOKS, cost-gated)
- `task drill:l2-scale` (or skill equivalent) on the migrated `pkg/drill` preflight/teardown-validator/watchdog/cost-report path.
- Operator-authorized, cost-tracked, mandatory teardown.

---

## Coverage Matrix

| # | Gap / Requirement | Task(s) | Resolution |
|---|---|---|---|
| 1 | Big-bang left dangling refs undetected | T0.1, T0.2 | Automated gate + skill-paths test as backstop |
| 2 | `/tmp/loopzero-ops` rebuild-on-demand fragility | T0.3, D6 | Canonical `task ops:build` |
| 3 | Salvage healthy `develop-bkp` Go code | D3, all T1/T2/T3 | Cherry-pick + re-validate, not re-author |
| 4 | Tier-A pure-logic scripts | T1.1–T1.6 | Migrate, CI-only callers |
| 5 | Tier-B orchestration scripts | T2.1+ | Migrate with full 4-surface rewire |
| 6 | Tier-C `dr/` SKILL orchestrators (root failure) | T3.1–T3.11 | Migrate + SKILL rewrite + LIVE gate |
| 7 | LIVE proof before deletion | T4.1, T4.2 | Operator-gated skill runs |
| 8 | Never break `develop` mid-migration | D1 | One script per PR, each independently green |

**Coverage: 8/8 requirements mapped (100%)**

## Global Definition of Done

- [ ] Every migrated script: own PR, `go test` + `task lint` + LIVE gate green
- [ ] Zero golangci-lint / ESLint warnings
- [ ] `no-dangling-script-refs` gate BLOCKING and green on every PR
- [ ] `skill-paths_test.sh` extension green (no drill SKILL references a deleted `.sh`)
- [ ] N(bash) reduced; each reduction tracked per PR (no big-bang)
- [ ] No `/tmp/loopzero-ops` rebuild-on-demand idioms (D6)
- [ ] `/cross-validation 2026-05-23-incremental-bash-to-go-migration` PASSED — zero CRITICO, zero GRAVE
- [ ] **`/dogfood full` PASSED** on clean infra (see Dogfood Gate)

> **Note:** This plan is execution-method infrastructure, not a maturity-score claim — Rule 11 3-source evidence applies only if a tier completion is claimed as a score uplift.

---

## Dogfood Gate (MANDATORY — NON-NEGOTIABLE)

> No tier is DONE without the LIVE gate that exercises its scripts. For Tier C specifically, `/dogfood full` + `/cleanroom` + `/chaos-test --full` on fresh infra is the only proof the Go orchestrators work — unit tests are necessary but not sufficient (this is the exact lesson from the `develop-bkp` failure).

### Execution
```bash
task clean:all && task setup && tilt up && task doctor
/dogfood full
/chaos-test --full
```

### Pass Criteria
- [ ] Executed on clean infrastructure
- [ ] Zero S0; zero S1 in scope
- [ ] For each migrated Tier-C orchestrator: the skill that invokes it passes LIVE
- [ ] No regressions vs the bash baseline (doctor 38+/0, dogfood clean, chaos 10/10)
- [ ] Rule 3 clean: zero internal terms in any CLI output

### On Failure
1. Fix all S0/S1 (root cause — no masking)
2. Re-run on fresh infra
3. Do NOT delete the corresponding bash until the Go path passes LIVE
