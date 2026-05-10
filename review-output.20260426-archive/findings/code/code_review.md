# Phase 4 Code Review Report — operators/

**Reviewer:** code-reviewer + test-auditor (synthesized by chief-reviewer)
**Phase:** 4 (Deep Code Review)
**Iteration:** 1
**Date:** 2026-04-26
**Scope:** Top-10 longest non-test files by line count + all provisioner clients

---

## File Hotspot Analysis

```
11320 total lines (non-test .go)
 1453  api/v1/zz_generated.deepcopy.go        [generated — not reviewed]
  490  internal/controller/tenant_build.go
  464  internal/harbor/client.go
  438  cmd/main.go
  379  internal/controller/clustercredential_controller.go
  369  internal/gitea/api.go
  342  internal/controller/tenant_controller.go
  337  internal/controller/gitrepositorybinding_controller.go
  332  operatortest/setup.go
  316  internal/argocd/real_provisioner.go
  301  internal/controller/platformbootstrap_controller.go
  296  internal/controller/application_controller.go
  290  internal/controller/tenant_harbor.go
  274  internal/controller/exposure_controller.go
  255  internal/minio/provisioner.go
  251  internal/controller/tenant_deletion.go
```

All critical paths were reviewed: tenant_controller.go, tenant_build.go, tenant_deletion.go, tenant_harbor.go, argocd/real_provisioner.go, gitea/api.go, harbor/client.go, minio/provisioner.go, gitrepositorybinding_controller.go, clustercredential_controller.go, httpclient/client.go, cmd/main.go.

---

## Test Execution Results

```
cd /home/paulo/loopzero/operators && go test -count=1 ./...

Ran 186 of 188 Specs in 143.355 seconds
FAIL! — 185 Passed | 1 Failed | 0 Pending | 2 Skipped
--- FAIL: TestControllers (143.36s)
FAIL  github.com/loopzero/loopzero/operators/internal/controller
ok    github.com/loopzero/loopzero/operators/internal/harbor
ok    github.com/loopzero/loopzero/operators/internal/minio
ok    github.com/loopzero/loopzero/operators/operatortest
```

**1 failing test.** File: `operators/internal/controller/tenant_integration_test.go:266`
**Assertion:** `policy count should be identical — Expected <int>: 3 to equal <int>: 0`

This contradicts the CLAUDE.md status claim that "41 Go packages passing." The failure is in the tenant integration test suite and blocks the confidence claim for operators.

---

## 4a. Error Handling

### Finding CODE-001 (HIGH) — Failing integration test: NetworkPolicy idempotency broken

**File:** `operators/internal/controller/tenant_integration_test.go:224-267`

The idempotency test `countResources()` (line 224-237) lists NetworkPolicies in the runtime namespace `loopzero-{tenant}-production`:

```go
var policies networkingv1.NetworkPolicyList
Expect(k8sClient.List(ctx, &policies, client.InNamespace("loopzero-"+tenantName+"-production"))).To(Succeed())
```

But `reconcileBuildNetworkPolicy()` creates its NetworkPolicy in the **build** namespace via `r.BuildClient`. After 3 spec-triggered reconciles, the EnvironmentController creates 3 NetworkPolicy objects in the runtime namespace (one per reconcile) instead of 1 stable idempotent one. Confirmed by the test failure: count goes from 0 to 3.

**Impact:** Test regression that masks EnvironmentController idempotency gap. CHANGELOG incorrectly states tests pass.

### Finding CODE-006 (MEDIUM) — Deletion Phase 1: environment delete failure does not return error

**File:** `operators/internal/controller/tenant_deletion.go:76-91`

```go
if err := r.Delete(ctx, env); err != nil {
    logger.Error(err, "failed to delete child Environment CR", "environment", env.Name)
    conditions.SetCondition(&tenant.Status.Conditions, "EnvironmentCleanup", ...)
    // MISSING: return ctrl.Result{}, fmt.Errorf(...) or at minimum track the error
} else {
    logger.Info("deleted child Environment CR", "environment", env.Name)
}
```

On transient API error, `childEnvCount` is still incremented, causing a `RequeueAfter: 5s` that waits for an environment that was never successfully deleted. No max-retry tracking for Phase 1 unlike Phase 2. Soft hang risk.

### Finding CODE-002 (MEDIUM) — panic() in reconcile hot path at tenant_harbor.go:271

**File:** `operators/internal/controller/tenant_harbor.go:271`

```go
data, err := json.Marshal(cfg)
if err != nil {
    // This should never happen with string fields, but fail loud if it does.
    panic(fmt.Sprintf("harbor: marshal docker config: %v", err))
}
```

`buildDockerConfigJSON` is called from `ensureHarborRobotAccounts()` which is in the TenantReconciler step list. A panic in a reconcile goroutine is caught by controller-runtime's recovery middleware, but the reconcile queue item is silently dropped until the next watch event. CLAUDE.md Rule 8 prohibits panic in non-init paths.

---

## 4b. Concurrency and Transactions

### MaxConcurrentReconciles Audit

Only `TenantController` has `MaxConcurrentReconciles` configured (default 5, configurable via `LOOPZERO_TENANT_MAX_CONCURRENT`). All other controllers use the controller-runtime default (1). This is correct given their workloads:

| Controller | MCC | Justification |
|---|---|---|
| TenantController | 5 | Bulk deletion support (MEET-052) |
| EnvironmentController | 1 (default) | Correct — single-item operations |
| ApplicationController | 1 (default) | Correct |
| ExposureController | 1 (default) | Correct |
| GitRepositoryBindingController | 1 (default) | Correct |
| ClusterCredentialController | 1 (default) | Correct |
| PlatformBootstrapController | 1 (default) | Correct (singleton resource) |

### Finding CODE-005 (LOW) — ArgoCD EnsureApplication/EnsureAppProject use full Update, not Patch

**File:** `operators/internal/argocd/real_provisioner.go:120-132, 220-235`

```go
// Get + SetResourceVersion + full Update pattern:
existing, err := p.client.Resource(applicationGVR).Namespace(p.namespace).Get(ctx, spec.Name, ...)
// ...
app.SetResourceVersion(existing.GetResourceVersion())
// ...
_, err = p.client.Resource(applicationGVR).Namespace(p.namespace).Update(ctx, app, metav1.UpdateOptions{})
```

With MCC=5, two concurrent reconciles for the same tenant can race on the same Application CR. The second Update receives a 409 Conflict (resourceVersion changed). Not a data integrity issue (controller-runtime requeues), but causes spurious error logs and Prometheus error counter increments.

### Multi-cluster Client Races

`TenantReconciler.BuildClient` and `RuntimeClient` are created once in `createClusterClient()` and shared across all concurrent reconcile goroutines. The controller-runtime client is goroutine-safe by design (it uses the API server as its synchronization point). No races found.

---

## 4c. Contracts and Validation

### CRD Spec Validation

All CRDs reviewed have kubebuilder validation markers. Key observations:

- `Tenant` requires `spec.plan` (enum: starter/pro/team/enterprise) and `spec.gitOps.repoName`.
- `Environment` requires `spec.tenantRef.name`, `spec.environment`, `spec.namespace.name`.
- `ClusterCredential` has no kubebuilder enum on `spec.scope.argocd` (bool, acceptable).

No missing required-field markers found on critical paths.

### Finding CODE-007 (LOW) — computePhase hardcodes "RouteReachable" string literal

**File:** `operators/internal/controller/application_controller.go:270`

```go
// Uses string literal where constant exists:
if c.Type == "RouteReachable" && c.Status == "False" {
// But reconcileRouteStatus at line 196 correctly uses:
conditions.SetCondition(&app.Status.Conditions, conditions.TypeRouteReachable, ...)
```

If `conditions.TypeRouteReachable` value changes, `computePhase` silently stops matching and ApplicationPhaseDown/Degraded are never returned.

### Status Subresource Usage

All controllers correctly use `r.Status().Update(ctx, obj)` or `r.Status().Patch(ctx, obj, patch)` for status mutations and `r.Update(ctx, obj)` for spec/metadata mutations. The ApplicationController uses `Patch` (lines 100-104) which is the preferred approach. Correct.

### Owner References

`reconcileEnvironmentCRs`: owner reference set correctly via `controllerutil.SetControllerReference(tenant, envCR, r.Scheme)` at line 270.
`reconcileExposedRoute`: owner reference set via `controllerutil.SetControllerReference(app, route, r.Scheme)` at line 153.
No orphaned resources found.

---

## 4d. Code Quality

### Finding CODE-003 (MEDIUM) — Harbor NewClient creates private http.Client bypassing httpclient package

**File:** `operators/internal/harbor/client.go:58-67`

```go
func NewClient(baseURL, username, password string) *Client {
    return &Client{
        baseURL: strings.TrimRight(baseURL, "/"),
        username: username,
        password: password,
        httpClient: &http.Client{
            Timeout: 30 * time.Second,  // No TLS config — uses Go default
        },
    }
}
```

`main.go:248` calls `harbor.NewClient(harborURL, harborUser, harborPass)` directly. All other provisioners (Gitea, ArgoCD via k8s dynamic client) use the shared httpclient package which respects `OPERATOR_INSECURE_SKIP_TLS`. Harbor alone diverges.

### Finding CODE-004 (MEDIUM) — isNotFoundError uses fragile string matching

**File:** `operators/internal/minio/provisioner.go:233-246`

```go
func isNotFoundError(err error) bool {
    if err == nil { return false }
    msg := err.Error()
    return strings.Contains(msg, "does not exist") ||
        strings.Contains(msg, "not found") ||
        strings.Contains(msg, "The specified user does not exist") ||
        strings.Contains(msg, "The canned policy does not exist")
}
```

The code comment acknowledges this is fragile. madmin-go error format changes can silently break deletion idempotency.

### Function Length / Cyclomatic Complexity

Hot spots by line count:
- `tenant_build.go:reconcileBuildNamespace()` — 76 lines (includes inner step table, acceptable)
- `tenant_build.go:reconcileBuildMinIOCredentials()` — 50 lines (includes recovery branch, acceptable)
- `platformbootstrap_controller.go` — multi-phase dispatch pattern, each phase delegated to sub-functions (clean)
- `clustercredential_controller.go:reconcileArgoCDClusterSecret()` — 60 lines (clear single responsibility)
- `harbor/client.go:EnsureRobotAccount()` — conditional create/renew logic, well-structured

No god functions found. Largest logical unit is `runSteps` in tenant_controller.go which is a clean abstraction.

### Resource Cleanup

All HTTP response bodies are drained and closed:
- `gitea/api.go`: `defer func() { _ = resp.Body.Close() }()` with `_, _ = io.ReadAll(resp.Body)` drain — correct.
- `harbor/client.go`: uses `drainAndClose(resp)` helper — correct.
- `gitrepositorybinding_controller.go:checkReachability`: `defer func() { _ = resp.Body.Close() }()` — correct.

Context cancel functions are deferred correctly in all `context.WithTimeout` usages:
- `clustercredential_controller.go:187`: `healthzCtx, healthzCancel := context.WithTimeout(ctx, constants.ConnectivityTimeout); defer healthzCancel()` — correct.
- `gitrepositorybinding_controller.go:254`: `reqCtx, cancel := context.WithTimeout(ctx, constants.ReachabilityTimeout); defer cancel()` — correct.

### Determinism

No `time.Sleep()`, `rand.Sleep()`, or non-deterministic delays found in reconcile paths. All delays use `RequeueAfter` with named constants.

---

## 4e. Static Analysis

`golangci-lint` is not in PATH on the review machine. Manual pattern-based checks performed:

**Error swallowing check:**
```
grep -rn "_ =" internal/ --include="*.go" | grep -v "_test.go" |
  grep -v "resp.Body\|io.Read\|io.Copy"
```
Result: Only `_ = statemachine.NewPreviewMachine` (COMP-002 blank identifier anchor — already registered). No production error swallowing found.

**context.Background/TODO in reconcile:**
```
grep -rn "context.Background()\|context.TODO()" internal/ --include="*.go" | grep -v "_test.go"
```
Result: 0 matches — all reconcile paths use the ctx propagated from controller-runtime.

**panic in non-init paths:**
Result: 1 match — `tenant_harbor.go:271` (registered as CODE-002).

**Freeform condition strings:**
Result: 2 matches — `EnvironmentCleanup` in tenant_deletion.go (from ARCH-003), `"RouteReachable"` in application_controller.go (CODE-007).

---

## 4f. Test Auditor Report

### Test Coverage by Component

| Package | Test Files | Test Quality |
|---|---|---|
| controller/ | 12 test files + integration test | Good — envtest covers real reconcile cycles |
| argocd/ | real_provisioner_test.go | Unit tests only (no envtest) — acceptable for dynamic client |
| harbor/ | harbor_test.go | Unit tests — httptest server mocking |
| minio/ | provisioner_test.go | Unit tests — madmin-go mock via interface |
| gitea/ | No test files | Gap — no tests for api.go |
| httpclient/ | No test files | Acceptable (single-function, no logic) |

### envtest Coverage Analysis

The integration test suite (`tenant_integration_test.go`, `environment_controller_test.go`, etc.) uses envtest (real K8s API via controller-runtime testenv). Tests cover:
- Happy path provisioning (Phase → Active)
- Finalizer patterns
- Deletion workflows
- GitRepo failure injection
- ArgoCD failure injection
- Concurrent reconcile behavior

### Test Gaps Identified

1. `gitea/api.go` has zero test coverage (no test file). The package handles all URL construction and HTTP response parsing for Gitea — a subtle error here affects all tenant provisioning.

2. `gitrepositorybinding_controller.go:checkReachability()` is tested with a mock HTTPClient but the mock does not simulate timeout scenarios.

3. `reconcileBuildMinIOCredentials` force-rotate branch (lines 369-392) has no direct test — only tested via integration test which does not exercise the "secret deleted, MinIO user exists" path.

4. No test exists for the `isRegistryTokenFresh` cache-hit path in `tenant_build.go:209-232`.

### Test Quality Notes

- Ginkgo `Ordered` container used correctly with `AfterAll` cleanup.
- `fake_provisioners_test.go` provides clean in-process fakes for Gitea and ArgoCD provisioners.
- `concurrent_safety_test.go` exists in operatortest — verifies concurrent provisioner calls are safe.
- The 2 Skipped specs are intentional (feature flagged, not flaky).

---

## Findings Summary Table

| ID | Severity | Area | Description |
|---|---|---|---|
| CODE-001 | HIGH | Test/Idempotency | Failing integration test: NetworkPolicy idempotency broken (185/186 pass) |
| CODE-002 | MEDIUM | Error Handling | panic() in reconcile hot path at tenant_harbor.go:271 |
| CODE-003 | MEDIUM | Code Quality | Harbor NewClient bypasses shared httpclient TLS config |
| CODE-004 | MEDIUM | Code Quality | MinIO isNotFoundError fragile string matching |
| CODE-005 | LOW | Concurrency | ArgoCD Update not Patch — 409 conflict under concurrent reconciles |
| CODE-006 | MEDIUM | Error Handling | tenant_deletion.go Phase 1 silently continues on environment delete failure |
| CODE-007 | LOW | Contracts | computePhase hardcodes "RouteReachable" string literal |
| CODE-008 | LOW | Timeout | gitea/api.go no per-request timeout (mitigated by transport timeout) |

---

## Positive Observations

The following are explicitly strong patterns worth documenting:

1. **Context propagation:** All reconcile functions correctly thread the incoming `ctx` through to all sub-operations. No `context.Background()` in production paths.

2. **Timeout discipline:** All HTTP timeouts are centralized in `constants/timeouts.go` with named constants. No inline `time.Duration` magic numbers.

3. **Finalizer pattern:** All controllers implement the verify-then-remove finalizer pattern correctly. No premature finalizer removal before cleanup.

4. **State machine usage:** TenantController and PlatformBootstrapController correctly use `tenantSM.Transition()` and `platformBootstrapSM.TryTransition()`. No direct phase assignment in these critical controllers.

5. **Re-fetch before status update:** Both `runSteps` (tenant_controller.go:190) and the main Reconcile function (line 156) re-fetch the resource before status update to avoid stale resourceVersion. This is the correct pattern.

6. **Error context wrapping:** Errors consistently use `fmt.Errorf("operation: %w", err)` throughout. No bare `return err` on external calls.

7. **Circuit breaker coverage:** Gitea, ArgoCD, and Harbor all have CB wrappers in main.go. MinIO is the known gap (ARCH-001).
