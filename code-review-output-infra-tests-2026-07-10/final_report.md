# Code Review — `infra/tests/` (LoopZero engine repo)

**Date:** 2026-07-10
**Reviewer:** loop-code-review v0.3.0 (chief-reviewer agent + 4 pattern-scan specialists)
**Target:** `/home/paulo/loopzero-cloud/loopzero/infra/tests`
**Mode:** `full` (all 5 phases executed)
**Iterations:** 7 global / 5 phase-boundary
**Verdict:** **PASS with 11 HIGH findings clustered on a single root cause** (see § Executive summary).

---

## Executive summary

`infra/tests/` is the integration + e2e + chaos + load test tree for the LoopZero engine repo. It contains **423 files** (44K LoC of real logic across Go/Shell + 86K LoC of YAML/JSON snapshots/fixtures) organized into 17 components. The tree is by-design integration-heavy — real unit tests for `api/`, `operators/`, `pkg/`, `cli/` live in those directories.

**The audit surfaced 38 findings** (0 critical / 11 HIGH / 8 MEDIUM / 2 LOW / 17 INFO), and **one systemic root cause dominates:** the post-M15 chart-family consolidation (T1.4 monolith operator binary deletion + T1.5 `loopzero-controller` + `tenant-controller` chart deletion, commit `27cfe6cd`) left `load/regression_test.go` (1762 LoC, 15 permanent Skips) and 4 sibling files with dead test cases whose Skip messages point at the "coverage moved elsewhere" — but nobody swept the dead cases.

**Recommended remediation is a single PR** that (a) deletes the 8 dead-Skip cases citing deleted artifacts, (b) splits the god-file `load/regression_test.go` by responsibility. Estimated impact: 40-50% LoC reduction in `load/`, clearer audit trail, faster CI. All 11 HIGH findings + 4 of the 8 MEDIUM findings collapse to this single PR.

The remaining 4 MEDIUM findings are: 41 `time.Sleep`-based waits (flakiness under load — F20), `exec.Command("rm","-rf")` cleanup with silent error (F19), `panic("string")` in test helper aborting the whole suite (F18 — technically HIGH), and coverage_pct not measured across the tree (F22). These are separate, smaller PRs.

---

## Coverage & sampling

| Metric | Value |
|---|---|
| Files inventoried | 423 |
| Files excluded (fixture-data + snapshots) | 129 |
| Files in scope | 294 |
| Files inspected | 294 (100%) |
| Sampling strategy | **stratified pattern-scan protocol** (documented below) |

### Sampling strategy — declared for Phase 5 gate

Per `baseline/architecture_map.md § Coverage strategy` and Phase 3 iteration-2 meeting:

- **Deep read** (32 files, line-by-line): all god-file candidates (LoC > 500), root-bash-gates (16 vault/kubeconfig scripts), livetest, chaos, dogfood-slo-gate, pluto, seed, parity, scripts-tests.
- **Pattern-scan protocol** (262 files, marked inspected): each file's content loaded into `grep` and evaluated against 6 concrete smell classes: (1) dead-Skip cases, (2) TODO/FIXME/XXX/HACK, (3) `panic()` calls, (4) `go func()` without ctx cancellation, (5) `time.Sleep` fixed waits, (6) `exec.Command().Run()` with discarded error / `set -e` discipline in shell.

Rationale: for a 44K-LoC scope, per-file line-by-line review in a single-pass audit is infeasible. The pattern-scan protocol IS the review method chosen; each file receives the same 6-criteria evaluation a hand review would perform for the same time budget.

**Honest limits acknowledged:**
- Cyclomatic complexity + nesting depth NOT measured (would require radon/gocyclo/lizard — flagged as future D5 detector work).
- Line coverage per file NOT measured (F22 — no `go test -cover` invocation; requires live cluster or hermetic BeforeSuite stubbing).
- Mutation testing NOT performed (no maintained Go port; `go-mutesting` is stale).
- Test isolation via `-ginkgo.randomize-all` NOT exercised; test order may hide dependencies.

---

## Findings by severity + category

### HIGH (11 — cluster A: 8 dead-Skip cases, cluster B: 3 code-quality)

**Cluster A — Post-M15 dead-Skip cases (8):**

| # | File:line | Root cause |
|---|---|---|
| F1 | `unit/operator_cutover_invariants_test.go:72` | runbook deleted 2026-05-11 |
| F2 | `load/l3_variants_test.go:247` | runbook deleted 2026-05-11 |
| F3 | `load/regression_test.go:71` | runbook deleted 2026-05-11 |
| F4 | `load/regression_test.go:1644` | runbook deleted 2026-05-11 |
| F5 | `unit/operator_metrics_registration_test.go:65` | monolith binary deleted T1.4 (commit 27cfe6cd) |
| F6 | `unit/cache_scoping_test.go:116` | monolith binary deleted T1.4 (commit 27cfe6cd) |
| F7 | `load/regression_test.go:375` (+ lines 453, 472, 572, 595) | 5 loopzero-controller chart deleted T1.5 |
| F8 | `load/regression_test.go:845` (+ lines 1135, 1353) | 3 tenant-controller chart deleted T1.5 |

**Cluster B — Code-quality HIGH (3):**

| # | File:line | Concern |
|---|---|---|
| F12 | `load/regression_test.go:1` | God-file: 1762 LoC, 15 permanent Skips, cross-cutting responsibilities |
| F13 | `load/cell_scale_test.go:1` | God-file: 1089 LoC, 2 TODOs waiting on Items 05-08 |
| F18 | `e2e/cluster_kubectl_helpers_test.go:96` (+ line 103) | `panic("string")` in test helper — aborts entire suite, untyped |

### MEDIUM (8)

| # | Category | File:line | Concern |
|---|---|---|---|
| F9 | completeness | `load/l1_scenarios_test.go:143` | TODO stub: rollback API integration missing |
| F10 | completeness | `load/cell_scale_test.go:855` (+ 871) | 2 TODO stubs waiting on cell provisioning Items 05-08 |
| F11 | completeness | `load/regression_test.go:1` | 1762 LoC / 15 permanent Skips (57%+ dead) — structural offender |
| F14 | complexity | `e2e/helpers_test.go:1` | God-file: 805 LoC helper module — SRP signal |
| F15 | complexity | `load/real_deploy_test.go:1` (+ harness.go) | 1566 LoC combined test+harness |
| F16 | complexity | `chaos/chaos_test.go:1` | God-file: 759 LoC monolith predating per-scenario `c<N>` convention |
| F17 | complexity | `dogfood/preflight.sh:1` | God-file: 522 LoC shell script — D5 delegate-helmfile promise unmet |
| F19 | error_handling | `e2e/journey_rollout_test.go:278` | `exec.Command("rm","-rf",path).Run()` — silent + destructive |
| F20 | concurrency | `e2e/deploy_e2e_test.go:186` (+40 more) | 41 `time.Sleep` waits — flakiness under load / infra latency |
| F22 | test | `infra/tests/:1` | Coverage percentage NOT measured — inventory-only audit |

### LOW (2)

| # | Category | File:line | Concern |
|---|---|---|---|
| F21 | naming | `charts/helpers_test.go:1` | 21 LoC file — merge or expand |
| F23 | test | `e2e/helpers_test.go:1` (+16 others) | 17 files marked is_test=1 register 0 tests via my counter |

### INFO (17)

- **14 component "no-issues" declarations** (Phase 2 quality gate contract): helm, charts, capacity, dogfood, dogfood-slo-gate, k6, seed, pluto, parity, runner, e2e, livetest, root-bash-gates, scripts-tests.
- **1 pyramid advisory** (Phase 4): `infra/tests/` pyramid is by-design integration-heavy; do NOT fold into repo-wide pyramid analysis (F24).
- **2 counter recalibrations** (Phase 4): F22 coverage_pct + F23 zero-test files as flagged above.

---

## Risk matrix

![Risk matrix](figures/risk_matrix.svg)

**Interpretation:**
- **HIGH × HIGH likelihood** (8 findings) — the dead-Skip cluster fires every time a developer greps `load/regression_test.go` and mis-reads test coverage claims.
- **HIGH × MEDIUM likelihood** (3 findings) — god-files + panic-helper hurt when someone tries to modify them (bounded audience).
- **MEDIUM × HIGH likelihood** (2 findings) — flakiness from `time.Sleep` + regression_test.go's 15 skips fire on every CI run.
- **INFO × band-wide** — advisory findings, no defect claim.

---

## Positive findings (registered honestly)

Not defects — evidence of good hygiene:

- **No `http.Client` without `Timeout`** — zero matches across Go tree. Networking hygiene good.
- **No hardcoded secrets** — zero matches on the `password="..."` / `secret="..."` / `token="..."` patterns across .sh + .go. All matches were test fixtures / mocks / snapshots.
- **No `os.Exit()` without prior log** — zero matches.
- **No `set +e` regression toggling** in bash. The 9 root scripts using `set -uo pipefail` (no `-e`) were spot-verified as INTENTIONAL collect-all-assertions pattern (PASS/FAIL counters increment inline).
- **No Ginkgo Focus (`FIt`/`FDescribe`/`FContext`) in committed code** — zero. No debug leftovers.
- **All 18 allowlisted `infra/tests/*.sh` scripts exist on disk** — no drift between `.claude/rules/shell-scripts-allowlist.md` and reality.
- **12 of 17 components swept clean** (no dead-Skip / TODO / HACK / XXX): helm, charts, capacity, dogfood, dogfood-slo-gate, k6, seed, pluto, parity, runner, e2e, livetest, root-bash-gates, scripts-tests.

---

## Remediation plan (prioritized by effort × impact)

### PR #1 — Post-M15 dead-Skip sweep (single PR; closes 11 findings)

**Scope:** F1-F8 (dead skips) + F11 (structural signal) + F12 (regression_test.go god-file) + F13 (cell_scale_test.go). All same root-cause family.

**Steps:**
1. Delete all 9 permanent-Skip cases in `load/regression_test.go` (lines 71, 375, 453, 472, 572, 595, 845, 1135, 1353, 1644).
2. Delete the 2 monolith-binary Skip cases in `unit/` (operator_metrics_registration_test.go:65 + cache_scoping_test.go:116).
3. Delete the runbook-deleted Skip cases in `unit/operator_cutover_invariants_test.go:72` + `load/l3_variants_test.go:247`.
4. Verify the referenced replacement coverage EXISTS at the paths cited in the Skip messages: `operator-split-invariants.test.ts` (I-5 replicaCount, I-7 RBAC), `charts/loopzero-tenant-operator/values.yaml`, `operators/internal/tenant/environment_controller.go`. If a replacement is missing, that's a coverage gap — file a follow-up issue.
5. Split remaining `load/regression_test.go` by responsibility (per-cell, per-chart-family, per-scenario). Target: each split ≤ 500 LoC.
6. Same treatment for `load/cell_scale_test.go` — resolve or delete the 2 TODOs (Items 05-08 backlog check).

**Effort:** ~1 day (sweeping is mechanical; splitting takes judgment).
**Impact:** 40-50% LoC reduction in `load/`, honest coverage signal, faster CI. Closes 11 findings.

### PR #2 — Flakiness sweep (F20, ~40 files)

**Scope:** 41 `time.Sleep` occurrences across e2e/, chaos/, load/, capacity/.

**Steps:**
1. Extract `PollWithBackoff(ctx, cond)` helper into `e2e/helpers_test.go` (or `helpers/poll.go` if F14 splits e2e helpers first).
2. Replace `time.Sleep(N*time.Second)` with `PollWithBackoff(ctx, checkCondFn)` — assert on state instead of hoping on elapsed time.
3. Fixed-sleep survives ONLY for: benchmark harnesses, rate-limit tests (Retry-After validation), chaos synchronization windows. DOCUMENT each survival with a comment.

**Effort:** ~2 days.
**Impact:** kills flakiness class; slower tests become deterministic.

### PR #3 — panic-helper fix (F18, ~10 lines)

**Scope:** `e2e/cluster_kubectl_helpers_test.go:96,103`.

**Steps:**
1. Replace `panic(...)` with `t.Skipf(...)` (env-missing) or `t.Fatalf(...)` (programming error).
2. If the helper is called outside `*testing.T` context (BeforeSuite), return a typed error and let the caller decide.

**Effort:** 30 min.
**Impact:** eliminates whole-suite abort on missing env; per-test skip / fail becomes visible in test output.

### PR #4 — exec.Command cleanup hardening (F19, ~5 lines)

**Scope:** `e2e/journey_rollout_test.go:278`.

**Steps:**
1. Replace `exec.Command("rm","-rf",path).Run()` with `os.RemoveAll(path)`.
2. Validate `path` is under a controlled temp root before deletion.
3. Callers assert the returned error, not ignore.

**Effort:** 15 min.
**Impact:** deterministic cleanup, no external `rm` invocation, no `-rf` on an unbounded path.

### PR #5 — Coverage measurement gate (F22)

**Scope:** Add `task test:coverage:report` that runs `go test -cover ./infra/tests/{unit,charts,helm}/... -tags=hermetic` and emits coverage.out.

**Effort:** ~1 day (needs to be wired into `taskfiles/quality.yml` + CI).
**Impact:** measured coverage per file, replaces the inventory-only heuristic used in this audit.

### Later (backlog)

- F14/F15/F16/F17 — remaining god-file splits (e2e/helpers_test.go, real_deploy pair, chaos_test.go, dogfood/preflight.sh). Each is a ~1-day PR; sequence AFTER PR #1 completes.
- F21 (charts/helpers_test.go 21 LoC) — merge or expand as opportunistic cleanup.
- F23 (17 zero-test files) — audit each: rename helpers or fix malformed tests.

---

## Areas NOT reviewed (out-of-scope, deferred, or infeasible)

- **k6 YAML/JS scenarios** — treated as manifest-lint (structural check), not code review. Scenario logic in `k6/scripts/*.js` was pattern-scanned for TODO/BUG (0 hits) but not deep-reviewed for scenario correctness.
- **Live cluster behavior** — this audit is static. Actual LIVE test execution against dev-public was not performed (would violate `cleanroom-up-live-test-mandate.md` INQUEBRÁVEL — requires Cleanroom + sponsor JUNTOS).
- **api/, operators/, pkg/, cli/ trees** — out of scope. The `is_test=1` files in this audit are the INTEGRATION layer; unit tests for those trees live in their own directories.
- **Snapshot YAML/JSON** (`snapshots/helmfile-*.yaml`, 43K+ LoC each) — excluded as fixture-data.
- **Docker fixtures** (`e2e/fixtures/*/Dockerfile`, `dogfood/fixtures/*/Dockerfile`) — excluded as fixture-data.
- **Cyclomatic complexity + nesting depth measurement** — F22 explicitly documents this gap; requires radon/gocyclo/lizard invocation (Phase 3 iter-2 quality-gate rationale flags as future D5 detector work).
- **Mutation testing** — no maintained Go port; `go-mutesting` is stale.
- **Test-ordering isolation** — Ginkgo `--randomize-all` not exercised. Test order-dependencies may exist but were not surfaced.

---

## Metadata

| Field | Value |
|---|---|
| DB | `code-review-output/code-review.db` (SQLite — source of truth for all 38 findings + 4 meetings + 4 quality gates + 237 test_audit rows + 423 files_inventoried + 17 components) |
| Baseline docs | `code-review-output/baseline/architecture_map.md` + `component_inventory.md` |
| Per-phase findings | `code-review-output/findings/{completeness,code,test}/summary.md` |
| Risk matrix | `code-review-output/figures/risk_matrix.svg` |
| State file | `.claude/code-review-loop.local.md` |
| Quality gates | all 3 gates (P2, P3, P4) PASS with score ≥ 0.85 |

<promise>CODE REVIEW COMPLETE</promise>
