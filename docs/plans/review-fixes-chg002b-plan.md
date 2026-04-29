# Plan: Review Fixes — CHG-002b Post-Review Hardening

> **Version 1.0** — Fixes the 5 issues identified in the pre-landing review of the `refactorhelms` branch (Sprint 8 v3.2 / CHG-002b). Addresses one race condition in Secret type migration, duplicate dead code in tests, a missing test for the maintenance middleware, and a missing observability signal for skipped pull secrets. All fixes are T0/T1 (local, no contract change).

## Context

The `/review` pass on `refactorhelms` found:
1. **P2 CRITICAL** — Race condition between `Delete` (outside `CreateOrUpdate`) and concurrent writers during one-time Secret type migration from `DockerConfigJson` → `Opaque`.
2. **INFO** — Triplicate unreachable `case` arms in `harbor/client_test.go` (dead test code).
3. **INFO** — `MaintenanceMode` middleware has zero tests (violates Rule 7).
4. **INFO** — `ensureHarborPullSecret` silently swallows `NotFound` without metrics — operators can't detect persistently missing pull secrets.

Evidence: diff review 2026-04-28, code read of `operators/internal/controller/tenant_build.go:458`, `tenant_harbor.go:262-271`, `api/internal/middleware/maintenance.go`, `operators/internal/harbor/client_test.go:401-411`.

## Objective

All 4 findings resolved, zero new test failures, zero lint warnings. Tests prove each fix.

## ADRs

**D1 — Inline Secret type healing into CreateOrUpdate mutator**
- **Decision:** Move the "delete if wrong type" logic inside the `CreateOrUpdate` mutator (checking `secret.Type` after the Get that `CreateOrUpdate` does internally) instead of doing a separate Get+Delete before `CreateOrUpdate`.
- **Rationale:** Eliminates the TOCTOU window between Delete and Create. `controllerutil.CreateOrUpdate` already does an internal Get — if the Secret exists with wrong type, we delete inside the mutator and return a conflict error so the next reconcile recreates cleanly. This keeps both writers (DOCR + Harbor) safe from each other's Delete.
- **Consequences:** The mutator may return an error on the first pass (type mismatch → Delete → return error → requeue). The Secret converges on the second pass. No behavioral change from the user's perspective.

**D2 — Add metric for skipped pull secrets**
- **Decision:** Increment existing `ReconcileErrors` counter with label `type=pull_secret_namespace_not_ready` instead of creating a new metric.
- **Rationale:** Reuses existing metric infrastructure. Operators already alert on `ReconcileErrors` — a dedicated metric would require new dashboards and alert rules. Using the existing counter with a distinguishing label is KISS.
- **Consequences:** Grafana dashboards that filter `ReconcileErrors` by `type` will surface this new label automatically.

## Dependency Graph

```
Phase 0 (all tasks independent, can run in parallel)
├── T0.1 — Fix Secret type migration race
├── T0.2 — Remove duplicate test case arms
├── T0.3 — Add maintenance middleware tests
└── T0.4 — Add metric for skipped pull secrets
```

All tasks are independent. No phase dependencies.

---

## Phase 0: Fix All Review Findings

**Objective:** Resolve all 4 findings identified in the pre-landing review.

### T0.1 — Fix Secret Type Migration Race (D1)

#### Objective
Eliminate the TOCTOU race between Delete + CreateOrUpdate in both `ensureBuildkitRegistrySecret` and `ensureHarborRegistrySecret`.

#### Evidence
Both functions do `Get → if wrong type → Delete → CreateOrUpdate`. Two concurrent reconcile passes (or DOCR + Harbor within the same pass) could race: Writer A deletes, Writer B's CreateOrUpdate sees empty state, data from Writer A is lost until next reconcile.

#### Files to edit
```
operators/internal/controller/tenant_build.go — remove pre-flight Delete, handle type mismatch inside mutator
operators/internal/controller/tenant_harbor.go — same pattern: remove pre-flight Delete, handle inside mutator
operators/internal/tenant/tenant_build.go — mirror the fix (multi-binary extraction copy)
operators/internal/tenant/tenant_harbor.go — mirror the fix
```

#### Deep file dependency analysis
- `tenant_build.go` — `ensureBuildkitRegistrySecret()` is called by `reconcileBuildRegistryCredentials()`. The change is internal to the function; no downstream callers are affected.
- `tenant_harbor.go` — `ensureHarborRegistrySecret()` is called by `ensureHarborRobotAccounts()`. Same containment.
- `operators/internal/tenant/` — these are copies for the multi-binary split. Must stay in sync.

#### Deep Dives
The fix strategy:
1. Remove the standalone `Get + Delete` block before `CreateOrUpdate`.
2. Inside the `CreateOrUpdate` mutator (which receives the live Secret), check `secret.Type != desiredType`.
3. If type mismatch: delete the Secret (from inside the mutator), return a sentinel error `errSecretTypeMismatch`.
4. The caller catches this error and returns it (triggering a reconcile requeue). On the next reconcile, the Secret won't exist → `CreateOrUpdate` creates it fresh with the correct type.

This is the pattern used by cert-manager and Crossplane for Secret type migration.

#### Tasks
1. Define `var errSecretTypeMismatch = fmt.Errorf("secret type mismatch, deleted for recreation on next reconcile")` in `tenant_build.go`.
2. In `ensureBuildkitRegistrySecret`: remove lines 457-463 (pre-flight Get+Delete). Inside the mutator, after `CreateOrUpdate` populates `secret`, add type check: if `secret.Type != "" && secret.Type != desiredType`, delete and return sentinel.
3. In `ensureHarborRegistrySecret`: same refactor — remove lines 262-274, add check inside mutator.
4. Mirror both changes in `operators/internal/tenant/` copies.
5. Run `cd operators && go test ./internal/controller/... ./internal/tenant/...`

#### TDD
```
RED:     TestEnsureBuildkitRegistrySecret_TypeMismatchRequeues — create a Secret with DockerConfigJson type, call ensureBuildkitRegistrySecret, assert it returns an error (triggering requeue) and the old Secret is deleted.
GREEN:   Implement the type-check-inside-mutator pattern.
REFACTOR: Remove dead pre-flight Get+Delete code.
VERIFY:  cd operators && go test ./internal/controller/... -run TestEnsure
```

#### Acceptance Criteria
- [ ] No standalone `Get + Delete` before `CreateOrUpdate` in either function
- [ ] Type mismatch returns error (requeue) instead of silent delete+recreate in same pass
- [ ] Existing tests still pass (convergence test, merge tests)
- [ ] `go vet ./...` clean

#### DoD
- [ ] All tasks completed
- [ ] `cd operators && go test ./...` passes
- [ ] Zero lint warnings

---

### T0.2 — Remove Duplicate Test Case Arms

#### Objective
Remove unreachable duplicate `case` arms in `harbor/client_test.go`.

#### Evidence
`TestDeleteRobotAccount_NotFoundIsIdempotent` (line 401-411) has 3 identical case clauses. `TestEnsureRobotAccount_PullOnlyPermissions` (line 512+) has 4 identical clauses. Go switches don't fall through — only the first match executes.

#### Files to edit
```
operators/internal/harbor/client_test.go — remove duplicate case arms
```

#### Deep file dependency analysis
- `client_test.go` — test-only file. No production code depends on it. Removing dead cases changes nothing about test behavior.

#### Deep Dives
Simple mechanical removal. Each test handler function needs exactly ONE `case` for the project-scoped robots endpoint (`/api/v2.0/projects/*/robots`).

#### Tasks
1. In `TestDeleteRobotAccount_NotFoundIsIdempotent`: remove 2 of the 3 duplicate project-robots case blocks (keep only one).
2. In `TestEnsureRobotAccount_PullOnlyPermissions`: remove 3 of the 4 duplicate project-robots case blocks (keep only one).
3. Run `cd operators && go test ./internal/harbor/... -v`

#### TDD
```
RED:     N/A — this is dead code removal, no behavior change.
GREEN:   N/A.
REFACTOR: Remove duplicates.
VERIFY:  cd operators && go test ./internal/harbor/... -count=1
```

#### Acceptance Criteria
- [ ] Each test handler has exactly ONE case for `/api/v2.0/projects/*/robots`
- [ ] All harbor tests pass unchanged
- [ ] `go vet ./...` clean

#### DoD
- [ ] Duplicate lines removed
- [ ] Tests pass

---

### T0.3 — Add Maintenance Middleware Unit Tests

#### Objective
Cover `MaintenanceMode` middleware with tests per Rule 7.

#### Evidence
`api/internal/middleware/maintenance.go` exists with zero test coverage. The middleware gates all mutating requests during PG resize — a bug here means either blocked reads (false positive) or allowed mutations during maintenance (data corruption).

#### Files to edit
```
api/internal/middleware/maintenance_test.go — (NEW) test file
```

#### Deep file dependency analysis
- `maintenance.go` — the middleware reads `LOOPZERO_MAINTENANCE_MODE` env var, passes through GET/HEAD/OPTIONS and `/api/v1/health`, blocks POST/PUT/PATCH/DELETE with 503 + JSON body.
- No other files depend on the test file.

#### Deep Dives
Test cases needed:
1. Env unset → all methods pass through
2. Env set to `"1"` → GET passes, POST returns 503 with default message
3. Env set to `"true"` → same as `"1"`
4. Env set to custom string → POST returns 503 with custom message as `message` field
5. Health endpoint POST passes through even in maintenance mode
6. Response includes `Retry-After` header = `300`
7. Response body matches JSON contract: `{code, message, retry_after_seconds}`

#### Tasks
1. Create `api/internal/middleware/maintenance_test.go` with table-driven tests.
2. Use `httptest.NewRecorder` + `http.NewRequest` + handler chain.
3. Use `t.Setenv` for env var manipulation (automatic cleanup).
4. Run `cd api && go test ./internal/middleware/... -run TestMaintenance -v`

#### TDD
```
RED:     TestMaintenanceMode_PassthroughWhenUnset — GET and POST both pass through when env is empty
RED:     TestMaintenanceMode_BlocksMutationsWhenSet — POST returns 503 with correct JSON
RED:     TestMaintenanceMode_AllowsReadsWhenSet — GET/HEAD/OPTIONS pass through
RED:     TestMaintenanceMode_AllowsHealthEndpoint — POST /api/v1/health passes through
RED:     TestMaintenanceMode_CustomMessage — env value "custom msg" appears in response body
RED:     TestMaintenanceMode_RetryAfterHeader — Retry-After header is "300"
GREEN:   Tests should pass immediately against existing implementation (no code change needed)
REFACTOR: None expected
VERIFY:  cd api && go test ./internal/middleware/... -count=1
```

#### Acceptance Criteria
- [ ] 6+ test cases covering all documented behavior
- [ ] Tests pass on existing implementation (green from start)
- [ ] `go vet ./...` clean
- [ ] Coverage of `maintenance.go` >= 90%

#### DoD
- [ ] Test file created and passing
- [ ] `cd api && go test ./internal/middleware/... -cover` shows >= 90%

---

### T0.4 — Add Metric for Skipped Pull Secrets (D2)

#### Objective
Make skipped pull secrets (namespace not ready) observable via existing `ReconcileErrors` counter.

#### Evidence
`tenant_harbor.go:153-158` logs `slog.Info` when `ensureHarborPullSecret` returns `NotFound` but increments no metric. If the namespace is permanently stuck, operators have no automated signal.

#### Files to edit
```
operators/internal/controller/tenant_harbor.go — add metric increment on NotFound continue
operators/internal/tenant/tenant_harbor.go — mirror
```

#### Deep file dependency analysis
- `tenant_harbor.go` — the skipped pull secret path is inside `ensureHarborRobotAccounts`. Adding a metric call here is safe — no control flow change.
- `operators/internal/metrics/metrics.go` — already exports `ReconcileErrors` CounterVec with labels `controller` and `type`. We add a new `type` value.

#### Deep Dives
At the `continue` branch (line 158), add:
```go
metrics.ReconcileErrors.WithLabelValues("tenant", "pull_secret_namespace_not_ready").Inc()
```

This uses the existing metric — no registration needed. The label value is descriptive enough to alert on.

#### Tasks
1. Import `metrics` package if not already imported.
2. Add `metrics.ReconcileErrors.WithLabelValues(...)` before the `continue` statement.
3. Mirror in `operators/internal/tenant/tenant_harbor.go`.
4. Run `cd operators && go test ./internal/controller/... ./internal/tenant/...`

#### TDD
```
RED:     TestEnsureHarborRobotAccounts_SkippedPullSecretIncrementsMetric — set up a tenant with environments where the namespace doesn't exist, call ensureHarborRobotAccounts, assert the counter was incremented.
GREEN:   Add the metric increment.
REFACTOR: None expected.
VERIFY:  cd operators && go test ./internal/controller/... -run TestEnsureHarbor
```

Note: Testing Prometheus metrics in Go typically uses `testutil.ToFloat64()` from `prometheus/client_golang/prometheus/testutil`.

#### Acceptance Criteria
- [ ] `ReconcileErrors` counter incremented with label `pull_secret_namespace_not_ready` on skip
- [ ] Existing tests unaffected
- [ ] `go vet ./...` clean

#### DoD
- [ ] Metric added in both locations
- [ ] Tests pass
- [ ] `cd operators && go test ./...` green

---

## Coverage Matrix

| # | Finding | Task(s) | Resolution |
|---|---|---|---|
| 1 | P2: Secret type migration race (Delete outside CreateOrUpdate) | T0.1 | Move type check inside mutator, return error for requeue |
| 2 | INFO: Duplicate unreachable case arms in harbor tests | T0.2 | Remove dead duplicate cases |
| 3 | INFO: No test for MaintenanceMode middleware | T0.3 | Add comprehensive unit tests |
| 4 | INFO: Skipped pull secrets have no observability signal | T0.4 | Increment existing ReconcileErrors metric |

**Coverage: 4/4 findings addressed (100%)**

Note: The CI DRY finding (4 near-identical build jobs) was intentionally excluded — YAML templating has limits, the current approach works correctly, and a reusable workflow refactor is a separate concern (T1 scope, can be done later without risk).

## Global Definition of Done

- [ ] All 4 tasks completed
- [ ] `cd operators && go test ./...` passes
- [ ] `cd api && go test ./internal/middleware/...` passes
- [ ] `task lint` passes
- [ ] No new files without tests
- [ ] Review findings checklist: all 4 items resolved
