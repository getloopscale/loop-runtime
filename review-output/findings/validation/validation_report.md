# Test Quality Audit — Validation Report (Phase 7)

**Date:** 2026-03-29  
**Auditor:** Test Auditor Agent  
**Scope:** Full codebase — `/home/paulo/loopzero`

---

## 1. Test Inventory by Layer

| Layer | Files | Test Cases | LOC (test) | LOC (prod) | Ratio |
|-------|-------|-----------|------------|------------|-------|
| CLI unit (Jest) | 27 | 498 | 8,511 | 4,961 | 1.71x |
| CLI integration | 1 | 0 (not runnable without live API) | — | — | — |
| API unit (Go) | 40 | ~406 (func Test + t.Run) | 13,308 | 15,484 | 0.86x |
| API integration (Go, real PG) | 2 | 7 | — | — | — |
| Operator (Ginkgo + envtest) | 18 | ~190 It blocks | 6,474 | 6,689 | 0.97x |
| Pkg shared libs (Go) | 15 | 161 | — | — | — |
| E2E (Ginkgo, real clusters) | 6 | 12 defined / 8 skipped / 4 active | — | — | — |
| Infra unit (Go) | 4 | 33 | — | — | — |
| Pulumi IaC (Vitest) | 5 | 32 | — | — | — |
| **Total** | **118** | **~1,339** | — | — | — |

---

## 2. Test Pyramid Assessment

```
          /   E2E   \          4 active, 8 skipped  ←  3% of tests (inverted: 0 non-skipped failure E2E)
         /------------\
        / Integration  \       7 API integration     ←  0.5% of tests
       /----------------\
      /  Unit + Component \    ~1,328 tests          ←  99% of tests
     /--------------------\
```

**Verdict: Correct pyramid shape (unit-heavy), but the E2E layer has a structural deficiency**: 8 of 12 E2E tests are permanently skipped, meaning all E2E coverage is happy-path only. The bottom layer is strong; the top layer is a facade.

---

## 3. Critical Path Coverage Assessment

### 3.1 Concurrency and Lock Correctness — STRONG

| Critical Test | Location | Status |
|--------------|----------|--------|
| Concurrent deploy → exactly one 202, one 409 | `api/tests/integration/concurrent_builds_test.go` | COVERED |
| Idempotency replay (same key+payload → same response) | `api/tests/integration/concurrent_builds_test.go` | COVERED |
| Idempotency mismatch (same key, different payload → 422) | `api/tests/integration/concurrent_builds_test.go` | COVERED |
| Lock released after build terminal | `api/tests/integration/concurrent_builds_test.go` | COVERED |
| Lock released on connection crash (pg_terminate_backend) | `api/tests/integration/lock_crash_recovery_test.go` | COVERED |
| Lock released on panic (deferred rollback) | `api/tests/integration/lock_crash_recovery_test.go` | COVERED |
| Lock released on error in fn | `api/internal/lock/` unit tests | COVERED |

These integration tests use a real PostgreSQL instance, real migrations, and real advisory locks. The concurrency and crash recovery coverage is genuinely excellent.

### 3.2 State Machine Transitions — STRONG

| Machine | File | Invalid transitions | Terminal state rejection | Timeout config |
|---------|------|--------------------|--------------------------| ---------------|
| Build (7 states) | `pkg/statemachine/build_test.go` | TESTED | TESTED (all events × terminal states) | TESTED |
| Release | `pkg/statemachine/release_test.go` | TESTED | TESTED | TESTED |
| Tenant | `pkg/statemachine/tenant_test.go` | TESTED | — | — |
| PlatformBootstrap | `pkg/statemachine/platformbootstrap_test.go` | TESTED | — | — |
| Preview | `pkg/statemachine/preview_test.go` | TESTED | — | — |
| Promotion | `pkg/statemachine/promotion_test.go` | TESTED | — | — |
| Rollback | `pkg/statemachine/rollback_test.go` | TESTED | — | — |

`TestBuildMachine_TerminalStatesRejectAllEvents` exhaustively checks all 3 terminal states × 11 events → panic. This is exemplary.

### 3.3 CLI Exit Codes — STRONG

The `deploy.test.ts` file has explicit test cases for every exit code:

| Exit Code | Scenarios Tested |
|-----------|-----------------|
| 0 | happy path, --wait=false, all apps deployed |
| 1 | no git repo, no loopzero.yaml, uppercase env, invalid env format, env too long |
| 2 | build failure, single app failure (DA-3 distinction) |
| 3 | deploy conflict (409), deploy failure |
| 4 | no token, empty token, undefined token |
| 5 | 2+ apps with one failure, partial SSE failure |

All 5 error exit codes are exercised. This is strong discipline.

### 3.4 CLI Internal Terms Compliance — STRONG

Tests in `deploy-renderer.test.ts`, `formatter.test.ts`, `client.test.ts`, `sse-client.test.ts`, `login.test.ts`, and `secret.test.ts` explicitly assert that forbidden terms (`ArgoCD`, `GitOps`, `Gitea`, `MinIO`, `Kaniko`, `pg_advisory`, etc.) do not appear in output. 50 assertions across 6 files enforce Rule 3.

Source code `deploy.ts` line 582-584 actively sanitizes error messages from backend. This is a defense-in-depth approach that is also tested.

### 3.5 Build Orchestrator — GOOD

`api/tests/unit/build_orchestrator_test.go` (213 test functions including subtests) covers:
- Concurrent build detection
- Workflow submission failure path
- Build completion callback (success and failure)
- State transition guards

### 3.6 Auth Middleware — GOOD

`api/tests/unit/auth_middleware_test.go` covers: no auth header → 401, invalid token → 401, valid token → context populated, token cache hit (same token not re-validated), cache expiry forces re-validation.

### 3.7 Operator Controllers — GOOD

18 test files covering all 7 wired controllers. Key quality markers:
- Idempotency tested: 6 `It("should not duplicate ... after re-reconcile")` specs
- envtest-based (real Kubernetes API server in-process)
- Ginkgo Ordered suites with proper `BeforeAll` setup
- Tenant namespacing, RBAC, ResourceQuota all verified

---

## 4. Failure Path Coverage Assessment

### 4.1 What Is Well-Tested

- Invalid auth → 401 UNAUTHORIZED
- Missing required fields → 400 BAD_REQUEST
- Concurrent deploy → 409 DEPLOY_IN_PROGRESS
- Lock timeout → 409 LOCK_TIMEOUT
- Build failure → FAILED state + SSE event
- Invalid state machine transitions → panic
- Build reaper reaps stale builds (QUEUED timeout, RUNNING timeout)
- Release convergence loop: timeout → SYNC_FAILED
- StartupReconciler: crash recovery (RUNNING builds requeued on restart)

### 4.2 What Is Missing

| Missing Failure Path | Severity | Finding ID |
|---------------------|----------|------------|
| BuildReconcileLoop: workflow succeeded → DeliveryActivator.Activate path | high | test_001 |
| BuildReconcileLoop: retryReleaseActivation on GIT_MIRROR_FAILED | high | test_001 |
| ContainerDeliveryActivator: gitops push failure → GIT_MIRROR_FAILED | high | test_002 |
| ContainerDeliveryActivator: ArgoCD sync timeout → SYNC_TIMEOUT | high | test_002 |
| StaticDeliveryActivator: KV write failure → FAILED + SSE event | high | test_003 |
| E2E: exit code 2 on build failure (Dockerfile syntax error) | high | test_004 |
| E2E: exit code 3 on ArgoCD sync timeout | high | test_004 |
| E2E: exit code 5 on partial monorepo failure | high | test_004 |
| RollbackOrchestrator: no prior release exists | medium | test_006 |
| RollbackOrchestrator: concurrent rollback+deploy blocked | medium | test_006 |
| SecretsRoute: PUT/DELETE operations | medium | test_007 |
| SecretsRoute: cross-tenant access → 403 | medium | test_007 |

---

## 5. Flaky Test Detection

### 5.1 Sleep-Based Tests (9 occurrences, 4 files)

| File | Sleep Duration | Context |
|------|---------------|---------|
| `api/tests/unit/release_convergence_loop_test.go` | 300ms, 200ms, 150ms (3 calls) | Wait for background loop tick |
| `api/tests/unit/pg_circuit_breaker_test.go` | 5ms, 5ms (2 calls) | Wait for circuit breaker state change |
| `api/tests/unit/workflow_circuit_breaker_test.go` | 20ms (1 call) | Wait for circuit breaker state change |
| `api/tests/unit/auth_middleware_test.go` | 5ms (1 call) | Wait for token cache TTL expiry |

The ReleaseConvergenceLoop sleeps (150-300ms) are the highest risk: the loop runs every 100ms in tests, so these margins are thin. Under a slow CI runner running parallel jobs this becomes the classic "works on my machine, fails in CI" pattern.

**Finding ID:** test_005

### 5.2 Skipped Tests

| File | Skipped Count | Skip Message |
|------|--------------|-------------|
| `infra/tests/e2e/failure_scenarios_test.go` | 8 | "E2E failure tests — deferred to hardening sprint" |
| `infra/tests/e2e/deploy_happy_path_test.go` | conditional | Skip unless `LOOPZERO_E2E=1` and clusters running |
| `infra/tests/e2e/concurrent_deploy_test.go` | conditional | Skip unless `LOOPZERO_E2E=1` and clusters running |

The `LOOPZERO_E2E=1` guards are appropriate — they protect against tests that require a real cluster. The 8 permanent skips in `failure_scenarios_test.go` are not appropriate — they represent acknowledged gaps disguised as test stubs.

---

## 6. Mock Usage Assessment

The API tests use a consistent "fake" pattern (not mock libraries): hand-written structs implementing the same interfaces as production code. Examples: `fakeBuildStoreOrch`, `fakeWorkflowSubmitter`, `fakeGitOpsPusher`, `fakeSyncClient`.

**This is the correct approach.** The fakes implement the real interface contract, test the orchestrator behavior, and are not testing mock call counts.

Mocks per test: typically 4-6 fakes per test suite, all at the infrastructure boundary (external services). This is within the acceptable 1-3 range per individual test case (fakes are shared at suite level).

**One anti-pattern found:** `TestClient_Put_Integration` in `api/internal/cfkv/client_test.go` sets up a real httptest server but never actually calls the client, then suppresses all captured values. This is a mock that tests nothing. **Finding ID:** test_008.

---

## 7. Edge Case Coverage Assessment

### Strong Edge Cases

- `TestBuildMachine_TerminalStatesRejectAllEvents`: exhaustive matrix of terminal states × all events
- Environment name validation: uppercase, numbers-first, special chars, 63-char limit (all in deploy.test.ts)
- CLI token validation: empty string, undefined, missing header
- Subdomain truncation at 63 chars (cfkv/client_test.go)
- Idempotency key mismatch (different payload, same key)

### Missing Edge Cases

- Empty string inputs to state machine (only tested with valid state names)
- Unicode/emoji in environment names or project names
- Extremely large bundle uploads (size limit enforcement)
- Concurrent rollback + concurrent deploy to same environment
- What happens when the build cluster is unreachable (Argo Workflows API down)

---

## 8. Test Independence Assessment

**CLI tests:** Fully independent. Each test creates its own mock deps via `createMockDeps()`. No shared mutable state.

**API unit tests:** Fully independent. Each test creates its own in-memory fakes with no shared state.

**API integration tests:** `cleanupTestData()` is called before each test, ensuring isolation. Uses `ON CONFLICT DO NOTHING` for seed data idempotency.

**Operator tests:** Ginkgo `Ordered` suites share a single envtest API server per `Describe` block but use unique CR names per test. `AfterAll` cleans up CRs. This is the standard envtest pattern.

**Potential issue:** `cleanupTestData` in integration tests deletes rows from `builds`, `releases`, etc. but leaves the `users`, `tenants`, and `projects` rows. If migration behavior changes these tables' schemas, tests could silently pass with stale seeded data. Minor concern.

---

## 9. Scoring

| Aspect | Weight | Score | Weighted |
|--------|--------|-------|---------|
| Test pyramid | 0.15 | 0.85 | 0.128 |
| Critical path coverage | 0.20 | 0.70 | 0.140 |
| Failure path testing | 0.20 | 0.60 | 0.120 |
| Flaky test presence | 0.10 | 0.70 | 0.070 |
| Mock quality | 0.10 | 0.85 | 0.085 |
| Edge cases | 0.10 | 0.65 | 0.065 |
| Test independence | 0.10 | 0.90 | 0.090 |
| Test naming/structure | 0.05 | 0.90 | 0.045 |
| **Total** | **1.00** | | **0.743** |

**Overall Test Quality Score: 7.4 / 10**

---

## 10. Findings Summary (Phase 7)

| ID | Title | Severity |
|----|-------|---------|
| test_001 | BuildReconcileLoop (510 LOC) has zero direct unit tests | high |
| test_002 | ContainerDeliveryActivator 5-phase activation has no unit tests | high |
| test_003 | StaticDeliveryActivator (344 LOC) has no unit tests — Sprint 10 shipped without tests | high |
| test_004 | All 8 E2E failure scenario tests are permanently skipped | high |
| test_005 | ReleaseConvergenceLoop tests use time.Sleep — timing-dependent flakiness | medium |
| test_006 | RollbackOrchestrator has only 2 test functions — missing 5 critical failure paths | medium |
| test_007 | SecretsRoute tests cover only GET — PUT, DELETE, and cross-tenant access untested | medium |
| test_008 | cfkv TestClient_Put_Integration is a hollow placeholder that makes no assertions | low |

---

## 11. What the Tests Do Well (Strengths to Preserve)

1. **Concurrency testing is genuinely excellent.** The integration tests use a real PostgreSQL advisory lock, real migrations, and real goroutines with a barrier primitive. This is not a mock of locking — it tests the actual serialization guarantee.

2. **State machine testing is exhaustive.** `TestBuildMachine_TerminalStatesRejectAllEvents` is a matrix test of 33 combinations. No other system in the codebase has this discipline.

3. **CLI exit codes are systematically tested.** Every exit code (0-5) has multiple test cases. The distinction between exit 2 (single app failure) and exit 5 (partial multi-app failure) is explicitly tested per design spec.

4. **Internal terms compliance is automatically enforced.** 50 assertions across 6 CLI test files verify that forbidden infrastructure terms never reach user-facing output. The source code also sanitizes backend error messages as defense-in-depth.

5. **Test independence is maintained throughout.** No global state, no test ordering dependencies, cleanup on every test.

6. **Fake pattern over mock frameworks.** The team uses hand-written fakes that implement the same interfaces as production code. This tests real behavior contracts, not mock call sequences.

---

## 12. Recommended Priority Order for Remediation

1. **Implement `BuildReconcileLoop` unit tests** (test_001) — this is the core async processing loop that runs continuously in production. 510 LOC with no tests.
2. **Implement `StaticDeliveryActivator` unit tests** (test_003) — Sprint 10 shipped to production without tests on the new delivery path.
3. **Implement `ContainerDeliveryActivator` unit tests** (test_002) — the gitops push failure and ArgoCD timeout failure paths are the most common production incidents.
4. **Remove the 8 E2E failure skips** (test_004) — at minimum implement 3 cases gated on LOOPZERO_E2E=1.
5. **Fix `TestClient_Put_Integration`** (test_008) — inject base URL and complete the HTTP assertion.
6. **Add RollbackOrchestrator failure cases** (test_006).
7. **Add SecretsRoute write operation tests** (test_007).
8. **Eliminate sleep-based synchronization** (test_005) — extract reconcileOnce() or use fake clocks.

