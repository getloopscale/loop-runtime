---
slug: m9-steps-3-4-pkg-quality-relocation
milestone_id: M9
created_at: 2026-07-02
goal: Relocate pkg/quality (14.6k LOC / 59% of the pkg module) out of the shared-contracts base into cmd/loopzero-ops/internal/quality, behavior-preserving, with the CI gate-set proven byte-identical.
base_sha: aacb4e45
branch: melhorias
governance: T2 (MEET-2026-07-02-001 APPROVED_WITH_CONDITIONS)
---

# M9 Steps 3+4 — Relocate `pkg/quality` out of the shared-contracts base

## Goal

Move the `pkg/quality` CI-gate suite (77 subpackages, 14 639 non-test LOC ≈ **59 %** of the `pkg` module) from the shared `github.com/getloopscale/loopzero/pkg` module into `cmd/loopzero-ops/internal/quality` (the module of its sole consumer, the `loopzero-ops` binary). The move is **behavior-preserving**: every one of the 70 registered `loopzero-ops quality` gates runs identically, `loopzero-ops quality --list` is byte-identical to the frozen baseline, and `task quality:gates` stays green. After the move the `pkg` module drops ~59 % of its LOC and the future `loopzero-contracts` extraction (jwt/serviceauth/plan) can never drag CI tooling into the contracts base (audit finding #3).

## Baseline Context

### Files & modules (current state, sha `aacb4e45`)

| Path | Module | Role | LoC (non-test) |
|---|---|---|---|
| `pkg/quality/` (77 subpkgs) | `github.com/getloopscale/loopzero/pkg` | CI gate suite (each subpkg = one gate: `check.go` + `check_test.go`) | 14 639 |
| `cmd/loopzero-ops/quality_subcmd.go` | `github.com/getloopscale/loopzero/cmd/loopzero-ops` | dispatch switch (70 `case "<gate>":`) + `--list` case | 2 909 |
| `cmd/loopzero-ops/quality_registry.go` | idem | declarative `gateRegistry` (70) + `taskWiredSet` + `runQualityListTo` | 259 |
| `cmd/loopzero-ops/quality_registry_test.go` | idem | 16 parity/negative tests (MA-1 oracle proof) | ~400 |
| `cmd/loopzero-ops/{chaos,drill}_subcmd.go` | idem | import 2 quality subpkgs each | — |
| `docs/operations/baselines/2026-07-02-m9-gateset.before` | — | frozen `--list` baseline (native order, 70 gates) — MA-3 anchor | — |
| `taskfiles/quality.yml` | — | `quality:gates` (full-scan, 27 gate cmds) + `quality:diff` (changed-files gates) | — |

### Import graph (measured)

- **Sole external consumer of `pkg/quality`:** `cmd/loopzero-ops` (`quality_subcmd.go`, `chaos_subcmd.go`, `drill_subcmd.go`). No `api/`, `operators/`, `cloud/`, or other `pkg/` package imports it (grep-verified).
- **`pkg/quality` imports the `pkg` runtime base:** the real compiled edges (`go list -deps`) are `pkg/errors` + `pkg/shellrun` only. (`enforcement` / `statemachine` appear in the suite ONLY as string-literal DATA — the frozen pkg-inventory list in `baseline/baseline.go` and an `acyclicgraph` test fixture — NOT as Go imports; corrected post-review from the initial grep that conflated string matches with imports.) None of these is in the contracts base (jwt/serviceauth/plan) — the contracts base imports NOTHING (umbrella ADR).
- **Destination `cmd/loopzero-ops/internal/quality`** is DOWNSTREAM of everything: `loopzero-ops` already depends on `pkg`; the relocated gates keep importing `pkg/errors` + `pkg/shellrun` as a **cross-module downstream edge** (loopzero-ops → pkg), which is legal and acyclic. `pkg` never imports `cmd/loopzero-ops`.

### Glossary

- **gate** — one `loopzero-ops quality <name>` check; one `pkg/quality/<name>/` subpackage exposing `Check(Config) (Outcome, error)`.
- **wired** — a gate invoked by a task in `taskfiles/quality.yml`. Today the oracle reports this as a boolean UNION over the whole file (the verification-architect gap — see Phase 0).
- **the oracle** — `loopzero-ops quality --list` + the frozen baseline; MA-1's silent-un-wire detector for steps 4 & 7.
- **RepoRoot** — the scan root injected into most gate `Config`s (`filepath.Join(cfg.RepoRoot, "charts")` …); path resolution is relative to the INJECTED root, not the package's own location.

### Architecture boundaries

`cmd/loopzero-ops` is a leaf binary (interface layer). `pkg` is the shared base (domain/infra libraries). `pkg/quality` is CI tooling misfiled into the base — the audit's finding #3. Moving it to the binary that runs it restores the layering: tooling lives with the tool, the base stays minimal.

## Coverage Matrix

Every Goal claim maps to ≥ 1 task.

| Goal claim | Task(s) |
|---|---|
| Oracle honestly detects silent un-wire (close verification-architect BLOCK) | T0.1, T0.2, T0.3, T0.4 |
| `pkg/quality` physically relocated to `cmd/loopzero-ops/internal/quality` | T1.1, T1.2 |
| All import paths rewired (gates + dispatch + chaos/drill + intra-quality) | T1.2, T1.3 |
| `pkg` module drops ~59 % LOC | T1.4 (verification) |
| Scanned paths externalized (no package-location-relative resolution) | T2.1 |
| Per-gate positive-control dirty fixtures confirmed/added (vacuous-pass owned) | T2.2 |
| `loopzero-ops quality --list` byte-identical (MA-3) | T3.1 |
| No new import edge into contracts base; DAG acyclic (MA-5) | T3.2 |
| `task quality:gates` + `task lint` + `go build ./...` + `go test ./...` green | T3.3 |
| Behavior-preserving (each gate runs identically) | T3.3, T3.4 |

## Prior Art

- Go convention: `internal/` packages are importable only within their module subtree — exactly matches "only `loopzero-ops` may import the gates".
- Kubernetes `staging/` + `cmd/*/internal/` pattern: CLI-only tooling lives under the command, not in the shared `pkg/`.
- This repo's own decomposition ADR (`decisions/2026-06-16-monorepo-decomposition.md`) + finding #3 in `architect-output/architecture-report.md`.
- MEET-2026-07-02-001 (T2 approval, MA-1..MA-12) + the MA-1 oracle already built (`quality_registry.go`).

## Phase 0 — Oracle hardening (unblock the verification-architect BLOCK)

Pre-step-3 gate. The verification-architect BLOCKED sign-off: the `wired` bit is a whole-file UNION, so a gate dropped from `quality:gates` but still referenced elsewhere (a `quality:diff` invocation or a stray `echo`) keeps `wired=true` → baseline byte-identical → silent un-wire MISSED. Fix by making the oracle per-task-attributed and relocation-proof.

### T0.1 — Per-task attribution replaces the boolean union

Replace the boolean `wired` with the **sorted set of task names** that invoke each gate. `taskWiredSet` → `taskGateTasks(taskfilePath) (map[string][]string, error)` attributing each gate to the top-level task(s) whose `cmds:` invoke it. Default `--list` line becomes `NAME\tTASKS\tSUMMARY` where TASKS is comma-joined sorted task names or `-`.

#### Why this step
A boolean union cannot distinguish "enforced in `quality:gates`" from "referenced anywhere"; per-task attribution makes any drop/move/removal of a gate change the baseline bytes. It is strictly more sensitive AND more honest (shows gates: vs diff: enforcement).

#### TDD
- RED: `TestTaskGateTasks_AttributesPerTask` — a fixture taskfile with gate `g` under task `gates` and gate `h` under task `diff` asserts `taskGateTasks` returns `{g:[gates], h:[diff]}`. Fails (function absent).
- RED: `TestQualityList_ShowsTasksColumn` — `--list` default output for a gate shows its task list, not `wired`/`unwired`.
- GREEN: implement `taskGateTasks` (walk `tasks:` mapping; for each top-level task collect its `cmds:` gate names via the existing structural parser; attribute).
- REFACTOR: keep `collectCmdStrings` as the per-task cmd extractor.

#### Acceptance criteria
- `taskGateTasks` returns, per gate, the exact sorted list of top-level tasks invoking it (measured against the live `taskfiles/quality.yml`).
- `--json` row gains `"tasks": [...]` (replaces `"wired": bool`); `--names` unchanged.

### T0.2 — Positive-control test: the oracle fires on un-wire

#### Why this step
MA-1's entire purpose is to fire when a gate stops being enforced; nothing currently proves it fires. This is the single most important missing test (verification-architect condition 2).

#### TDD
- RED: `TestOracle_FiresOnUnwireFromGates` — take a fixture taskfile where gate `g` is in `gates` + `diff`; render `--list`; remove `g` from the `gates` task only; re-render; assert the two outputs DIFFER on `g`'s TASKS column (union boolean would NOT differ — proves the gap is closed). Fails against the current boolean.
- GREEN: satisfied by T0.1's attribution.

#### Acceptance criteria
- Dropping a gate from `quality:gates` (while it survives in `quality:diff`) changes the `--list` bytes.
- Dropping a gate from ALL tasks changes the bytes (TASKS → `-`).

### T0.3 — Relocation-proof the parity read

#### Why this step
`switchCaseGates` (test helper) hard-reads `quality_subcmd.go`; if the dispatch switch is split/moved the parity guarantee silently evaporates (verification-architect condition 3).

#### TDD
- RED: `TestParityRead_GlobsAllPackageFiles` — a fixture with a `case "x":` in a second `*.go` file is discovered by the parity scanner. Fails (helper reads one file).
- GREEN: change `switchCaseGates` to glob `*.go` in the package dir (skip `_test.go`), parse `case "<name>":` across all. Re-run `TestGateRegistry_CoversAllSwitchCases` → count 70.

#### Acceptance criteria
- The parity scanner reads every non-test `*.go` in the package; registry↔dispatch parity still holds at 70 after any future file split.

### T0.4 — Re-freeze baseline + confirm vacuous-pass ownership + re-get sign-off

#### Why this step
The format change (T0.1) re-freezes the MA-3 anchor with the corrected semantic BEFORE step 3 (MA-1 mandates freeze before step 3). Confirm the vacuous-pass class (condition 4) is owned by existing per-gate `check_test.go` dirty fixtures (69 present).

#### TDD
- Regenerate `docs/operations/baselines/2026-07-02-m9-gateset.before` from `loopzero-ops quality --list` (native order); update the header comment to document the TASKS-column semantic + the MA-3 check.
- Add `TestFrozenBaseline_ByteIdentical` (already exists as a self-check) — re-point to the new baseline; assert byte-identity.
- Confirm ≥ 1 representative gate (`no-env-vault-secrets`) has a dirty-fixture test that FAILS on a gutted Check (grep the existing `check_test.go`).

#### Acceptance criteria
- `loopzero-ops quality --list | diff - <(grep -v '^#' baseline)` empty.
- verification-architect re-review returns APPROVE (conditions 1-4 satisfied).

## Phase 1 — Step 3: relocate the package (behavior-preserving)

### T1.1 — `git mv pkg/quality → cmd/loopzero-ops/internal/quality`

#### Why this step
The physical move is what removes 59 % LOC from the `pkg` module and restores layering.

#### TDD
- No new test — this is a move; the RED/GREEN is the compiler + the existing 69 gate test suites + the oracle. Pre-move: capture `go test ./pkg/quality/...` PASS count as the behavior oracle (MA-4 set-diff of test function names == ∅ after move).
- `git mv pkg/quality cmd/loopzero-ops/internal/quality` (preserves history).
- Remove the `./pkg` sub-path from nothing (pkg module keeps its go.mod; quality just leaves the tree). Remove `pkg/quality` from any `pkg` module-internal references (none — it was leaf).

#### Acceptance criteria
- `cmd/loopzero-ops/internal/quality/<77 subpkgs>` exists; `pkg/quality` gone.
- `git log --follow` resolves moved files (history preserved).

### T1.2 — Rewrite import paths (mechanical, compiler-verified)

#### Why this step
Every `github.com/getloopscale/loopzero/pkg/quality/...` import must become `github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/...`.

#### TDD
- RED: `go build ./...` fails with unresolved `pkg/quality` imports (proves the edges).
- GREEN: rewrite imports across (a) the 77 relocated subpkgs' intra-imports, (b) `cmd/loopzero-ops/{quality_subcmd,chaos_subcmd,drill_subcmd}.go`, (c) `quality_registry.go` if it imports any subpkg. Use a scripted `gofmt -r`/`sed` over the exact old→new path, then `goimports`.
- Update `go.work` if `./pkg/quality` was ever a separate use entry (it is not — it was inside `./pkg`).

#### Acceptance criteria
- `go build ./...` green; zero references to `loopzero/pkg/quality` remain (grep == 0).

### T1.3 — Wire the moved tests + verify no test-drop (MA-4)

#### Why this step
MA-4: a silent test-drop during the move is a BLOCKER. Prove the moved test-function-name set is unchanged.

#### TDD
- Capture `go test -list '.*' ./cmd/loopzero-ops/internal/quality/...` function-name set == the pre-move `pkg/quality` set (set-diff == ∅).
- `go test ./cmd/loopzero-ops/internal/quality/...` green with the same PASS count.

#### Acceptance criteria
- Test-function-name set-diff == ∅ (not just count).

### T1.4 — Verify the `pkg` module shrinks materially

#### TDD
- Measure `pkg/` non-test LOC after the move; assert it dropped by ≈ 14.6k (to ≈ 10k) — the "pkg LOC drops materially" DoD.

#### Acceptance criteria
- `pkg/` non-test LOC < 45 % of its pre-move value.

## Phase 2 — Step 4: externalize scanned paths + positive controls

### T2.1 — Externalize package-location-relative path resolution

#### Why this step
Any gate resolving a scan path relative to its own source location (not an injected `RepoRoot`/CWD) would break after the move. The 4 `os.Getwd()` sites + any `runtime.Caller`/relative-fixture path are the risk surface.

#### TDD
- RED: audit — grep the relocated tree for `runtime.Caller`, `os.Getwd`, and any path literal not joined to `cfg.RepoRoot`/CWD; for each risky site write a test that runs the gate from a DIFFERENT CWD and asserts it still resolves the repo root (or fails loud). Any gate that silently scans nothing (vacuous pass) from the new location is RED.
- GREEN: convert each risky site to take the scan root from `Config.RepoRoot` (injected by the caller) or an explicit CWD-independent resolver. No hardcoded package-relative paths.

#### Acceptance criteria
- Every gate resolves its scan root independent of the package's on-disk location (proven by a from-`/tmp` invocation test for each externalized gate).

### T2.2 — Confirm/complete per-gate positive-control dirty fixtures

#### Why this step
MA-3: "positive-control dirty fixture per externalized gate" — a gate must FAIL on a dirty input, else a vacuous pass hides a broken gate. 69/70 gates already have `check_test.go`; confirm each externalized gate has a dirty-fixture assertion; add for any missing.

#### TDD
- For each gate touched by T2.1: assert its `check_test.go` has a case that FAILS the gate on a deliberately dirty fixture (not only a clean-pass case).

#### Acceptance criteria
- Every externalized gate has ≥ 1 dirty-fixture test that is RED when the gate's Check is gutted.

## Phase 3 — Verification & MA proofs

### T3.1 — MA-3 byte-identity

#### TDD
- `loopzero-ops quality --list | diff - <(grep -v '^#' docs/operations/baselines/2026-07-02-m9-gateset.before)` empty; every gate resolves individually (`loopzero-ops quality <name>` no `unknown gate`).

### T3.2 — MA-5 DAG proof

#### TDD
- `go list -deps ./cmd/loopzero-ops/internal/quality/... | grep 'loopzero/pkg/\(jwt\|serviceauth\|plan\)'` == ∅ (no new edge into the contracts base).
- `loopzero-ops quality acyclic-graph` green (decomposition DAG intact).
- `go build ./...` authoritative — no import cycle.

### T3.3 — Hermetic gate suite

#### TDD
- `go build ./... && go test ./... && task quality:gates && task lint` all green (MA-10 per-step hermetic validation; envtest not required — no controller runtime touched).

### T3.4 — CHANGELOG + baseline cross-link

#### Why this step
Unbreakable Rule 6; MA-2 (each step a CHANGELOG `[Unreleased] § Changed` entry, no behavior-change claim).

#### Acceptance criteria
- `CHANGELOG.md [Unreleased] § Changed` documents the relocation (behavior-preserving) + the oracle-semantic change (TASKS column).

## Failure scenarios

External I/O touched: **filesystem only** (git mv, file reads by gates). No network/DB/queue.

- **A gate scans nothing from the new location (vacuous pass):** caught by T2.1 from-`/tmp` invocation tests + T2.2 dirty fixtures. FAIL-loud, not silent.
- **`git mv` loses history:** verified by `git log --follow` in T1.1 acceptance.
- **Import rewrite misses a build-tagged/reflection edge:** `go build ./...` is authoritative (compiler catches all static edges); grep-for-`loopzero/pkg/quality` == 0 catches string references.
- **Taskfile read fails in the oracle:** `taskGateTasks` returns a typed error naming the path (never a silent empty set) — preserved from `taskWiredSet`.

## Drawbacks & Risks

1. **Large mechanical diff (77 packages, ~15k LOC moved).** Mitigation: `git mv` (rename detection keeps the diff reviewable as renames); `behavior_change: none`; `go build`+`go test`+set-diff gate the move.
2. **Oracle format change re-freezes the baseline mid-milestone.** Mitigation: re-freeze happens in Phase 0 BEFORE step 3 (exactly when MA-1 mandates freezing); the new semantic is strictly more correct; the change is documented in the baseline header + CHANGELOG.
3. **`internal/` visibility could block a future non-loopzero-ops consumer.** Accepted: today there is exactly one consumer; if a second appears, promote to a `pkg`-level tooling module then — YAGNI now.
4. **Step-4 externalization could surface a gate that was already vacuously passing pre-move.** This is a latent-bug discovery, not a regression; document + fix per gate (does not block the relocation if the gate was already green-by-vacuity — flag it).

## Unresolved Questions

1. Do the 2 `chaos_subcmd.go`/`drill_subcmd.go` quality imports reference gates that must ALSO move, or shared helpers? (Resolve in T1.2 by following the exact imported subpkgs — they move with the tree regardless.)
2. Is there any `go:generate` or build-tag in `pkg/quality` that bakes the old import path? (Resolve in T1.2 grep for `go:generate` + `//go:build`.)
3. Does `taskfiles/quality.yml` reference `pkg/quality` paths (e.g., `go test ./pkg/quality/...`) that must be repointed? (Resolve in T1.2 grep of all taskfiles.)

## Test Plan

- **Unit:** oracle tests (T0.1-T0.4), each gate's existing `check_test.go` (69) run from the new location.
- **Integration:** `task quality:gates` (full-scan enforcement) + `task quality:diff` green post-move.
- **Regression/oracle:** MA-3 byte-identity; MA-4 test-name set-diff == ∅; MA-5 `go list` DAG proof.
- **Edge/negative:** oracle positive-control (fires on un-wire); per-gate dirty fixture (fires on gutted Check); from-`/tmp` CWD-independence.

## Dependencies

No new third-party dependency. Uses stdlib (`go/parser`, `path/filepath`, `os`) + existing `gopkg.in/yaml.v3` (already a `cmd/loopzero-ops` dep). `git mv` (git). No CVE surface added.

## ADRs

### ADR-1 — Destination is `cmd/loopzero-ops/internal/quality`, not a new module

**Alternatives considered:** (a) new sibling module `tools/quality/` with its own go.mod + go.work entry; (b) `cmd/loopzero-ops/internal/quality` (chosen); (c) leave in `pkg/quality` and only rewire (rejected — does not fix finding #3).
**Decision:** (b). The sole consumer is `cmd/loopzero-ops`; `internal/` enforces exactly that visibility; no new go.mod/go.work ceremony (KISS/YAGNI). Downstream of the contracts base (MA-5 satisfied).
**Trade-off:** a future second consumer would force a promotion to a shared tooling module — accepted (YAGNI; one consumer today).

### ADR-2 — Oracle `wired` boolean → per-task TASKS attribution

**Alternatives considered:** (a) guard union==membership with a ">1 task fails" test (verification-architect option b — keeps the misleading boolean); (b) scope wired to `quality:gates` only (marks diff-enforced gates as unwired — misleading the other way); (c) per-task attribution (chosen).
**Decision:** (c). Honest (shows gates: vs diff:), strictly more sensitive (any un-wire/move/removal changes bytes), and gives the positive-control test a concrete assertion.
**Trade-off:** re-freezes the baseline — acceptable, done in Phase 0 before step 3 per MA-1.
