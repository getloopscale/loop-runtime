# FINDING — tenant operator envtest suite is RED on branch `melhorias` (pre-existing)

- **Date:** 2026-07-02
- **Discovered during:** M9 Step 2 (extract environment) baseline capture — `docs/meetings/records/MEET-2026-07-02-001.md` MA-9.
- **Severity:** HIGH (blocks any operators refactor that needs envtest as a behavior oracle — e.g. M9 Step 2).
- **Build under test:** branch `melhorias`, engine sub-tree; `go build ./operators/...` OK.
- **Filed to GitHub:** NOT filed — `gh` is unauthenticated for `getloopscale/loopzero` in this environment. This doc is the durable record until an operator files it.

## Summary (expected vs actual)

The `operators/internal/tenant` envtest suite does NOT pass green on the current branch, **before any M9 change**. This means it cannot serve as the behavior-preservation oracle that MEET-2026-07-02-001 MA-9 mandates for extracting the Environment reconciler.

- **Expected:** `go test ./operators/internal/tenant/...` GREEN.
- **Actual:** `FAIL` — 1 deterministic unit failure + ~11 Ginkgo envtest spec failures (quiet + isolated: `77 Passed | 11 Failed | 1 Pending | 72 Skipped`, `Ran 88 of 161 Specs`).

## Repro

```bash
cd operators
ASSETS=$(setup-envtest use -p path)

# (1) Deterministic unit failure — no envtest, no timing:
go test ./internal/tenant/ -run TestBuildAppProjectDestNamespaces -count=1
# --- FAIL: TestBuildAppProjectDestNamespaces/empty_ProjectID_on_preview_—_fail-loud...
#   tenant_gitops_test.go:265: error "preview env (project=\"\", pr=9): invalid EnvRef field\nproject empty"
#   must contain "project slug must not be empty"

# (2) Full suite (quiet, isolated) — 11 envtest specs fail:
KUBEBUILDER_ASSETS="$ASSETS" go test ./internal/tenant/ -count=1 -p 1
# FAIL! -- 77 Passed | 11 Failed | 1 Pending | 72 Skipped
```

## Evidence

- **(1) Deterministic** — `tenant_gitops_test.go:265` asserts the error contains `"project slug must not be empty"`, but the production code (`pkg/naming` EnvRef validation) emits `"project empty"`. A test/code wording mismatch — fails every run in 0.00s, independent of envtest/timing. Fix is one-line (align the test expectation OR the error string) but it touches EnvRef contract wording — classify before editing.
- **(2) Envtest specs** — 11 Ginkgo `Eventually(...).Should(Succeed())` specs time out (30s) across `tenant_controller_test.go`, `tenant_isolation_test.go`, `tenant_integration_test.go`, `environment_controller_t31_test.go`, `environment_crd_validation_test.go`. Failure count varied (2 isolated earlier, 6 under full-suite contention, 11 quiet-isolated), so at least some are flaky/timing-sensitive; whether all 11 are timing or some are genuinely broken by prior WIP on `melhorias` needs investigation.

## Impact

- **Blocks M9 Step 2** (extract `environment` from `tenant`): MA-9's acceptance gate is "full operators envtest green + Tenant→Environment cascade proof + no coverage drop". With a red/flaky baseline there is no green to preserve, so behavior-preservation cannot be certified. Shipping Step 2 anyway would be a false PASS.
- M9 Step 1 (rename `shared` → `reconcilehelpers`) is UNAFFECTED and shipped (`ab3ed163`) — verified via build + the non-tenant consumer suites (app/platform/preview) which are green.

## Recommended corrective action (prerequisite for M9 Step 2)

1. Fix the deterministic `TestBuildAppProjectDestNamespaces` wording mismatch (T1 tier — align test/error string; classify per pre-release-governance since it touches an EnvRef error contract).
2. Triage the 11 flaky envtest specs: raise `Eventually` timeouts / add `--flake-attempts`, OR fix the underlying reconcile latency; establish a reliably-green tenant suite.
3. Re-run this repro to confirm GREEN, THEN resume M9 Step 2 with a trustworthy envtest oracle.

This corrective work is its own `/to-plan` scope (operators test stabilization), NOT part of M9 Step 1/2.

## Update (2026-07-02) — stabilized (deterministic) + one residual pre-existing flake

- **Deterministic failures FIXED** (commit `986b60b4`): the ~50 stale M2 per-project
  namespace assertions + 2 stale CRD-minItems specs + 1 EnvRef wording. The tenant
  suite now passes green on clean isolated runs.
- **Residual: `environment_controller_t31_test.go` "EC-3 63-char overflow"** is an
  INTERMITTENT flake (~50% observed). Root cause (confirmed via reconcile
  logs): under repeated reconciles the environment reconcile hits a state-machine
  `invalid transition` error + optimistic-concurrency conflicts (`object has been
  modified`) and errors out BEFORE reaching the `NameTooLong` status write, so the
  condition the spec asserts is never set. This is NOT caused by the M9
  Step 2 extraction (the environment reconcile code is byte-identical post-move —
  only the package clause + a shared RBAC helper relocated). It predates all M9 work
  (it was among the original 11 failures). Its 10s Eventually was aligned to the suite
  standard 30s, which did not resolve the underlying race. Tracked for a separate
  root-cause fix (make the reconciler robust to a not-yet-persisted spec, or gate the
  spec on a "CR fully persisted" precondition). NOT in M9 Step 1/2 scope.
