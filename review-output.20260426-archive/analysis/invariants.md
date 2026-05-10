# System Invariants — operators/

**Defined:** 2026-04-26 (Phase 7 Validation)
**Total invariants:** 15
**Validated:** 5 | **Violated:** 10 | **Untested:** 0

---

## Summary Table

| ID | Name | Status | Violation Finding | Test Coverage |
|---|---|---|---|---|
| INV-001 | tenant_active_requires_all_conditions | validated | — | tenant_integration_test.go:45-50 |
| INV-002 | environment_resources_on_runtime_cluster | validated | — | environment_controller.go:35-37 comment + tests |
| INV-003 | state_machine_authority_for_supported_controllers | violated | ARCH-002, COMP-002 | Partial (tenant + env covered, exposure/app/preview not) |
| INV-004 | circuit_breaker_coverage_all_external_provisioners | violated | ARCH-001 | No test for missing MinIO CB |
| INV-005 | finalizer_guard_before_cleanup | validated | — | tenant_integration_test.go:365-388 |
| INV-006 | secure_by_default_tls_verification | violated | COMP-007, SEC-001 | No test for TLS default verification |
| INV-007 | idempotent_reconcile_for_existing_resources | violated | CODE-001 | 1 FAILING TEST: tenant_integration_test.go:266 |
| INV-008 | inv_tenant_namespace_isolation | validated | — | tenant_isolation_test.go:250-311 |
| INV-009 | inv_status_via_state_machine | violated | ARCH-002, COMP-002 | Partial (tenant/env/bootstrap covered) |
| INV-010 | inv_finalizer_completes | violated | VAL-005, CODE-006 | Phase 1 error path: NOT TESTED |
| INV-011 | inv_owner_refs_set | validated | — | reconcileEnvironmentCRs uses IsControlledBy |
| INV-012 | inv_secret_namespace_scoped | violated | INFRA-001 | No test for RBAC scope |
| INV-013 | inv_no_panic_in_reconcile | violated | CODE-002 | No test for panic path |
| INV-014 | inv_circuit_breaker_on_external | violated | ARCH-001, ARCH-004 | No test for unwrapped MinIO/HTTP |
| INV-015 | inv_alert_metrics_emitted | violated | VAL-001 | No CI validation of alert expressions |

---

## Detailed Invariant Definitions

### INV-001: tenant_active_requires_all_conditions
**Status:** VALIDATED

**Assertion:** A Tenant CR may only reach `Phase=Active` if all required conditions are True. The state machine enforces this: `TenantAllReady` event is only emitted when `conditions.AllConditionsTrue()` returns true.

**Code location:** `operators/internal/controller/tenant_controller.go:144-149`
```go
if conditions.AllConditionsTrue(tenant.Status.Conditions, r.requiredProvisioningConditions()...) &&
    tenant.Status.Phase == loopzerov1.TenantPhaseProvisioning {
    tenant.Status.Phase = loopzerov1.TenantPhase(
        tenantSM.Transition(statemachine.TenantProvisioning, statemachine.TenantAllReady),
    )
}
```

**Test:** `tenant_integration_test.go:45-50` — Eventually waits for Active phase and verifies all condition types are True.

---

### INV-002: environment_resources_on_runtime_cluster
**Status:** VALIDATED

**Assertion:** All tenant runtime resources (namespace, RBAC, NetworkPolicy, ResourceQuota) are created exclusively via `r.RuntimeClient`, never via the mgmt cluster client.

**Code location:** `operators/internal/controller/environment_controller.go:35-37`
```
// Multi-cluster design:
//   - r.Client        → mgmt cluster (Environment CRDs, status)
//   - r.RuntimeClient → runtime cluster (namespaces, RBAC, network policies)
```

**Test:** `environment_controller_test.go` uses separate fake clients for mgmt and runtime clusters.

---

### INV-003: state_machine_authority_for_supported_controllers
**Status:** VIOLATED

**Assertion:** Phase transitions for Tenant, Environment, PlatformBootstrap CRs go through `pkg/statemachine` functions. Direct `Status.Phase =` assignment is forbidden per CLAUDE.md Rule 8.

**Violations:**
- `exposure_controller.go:96,116,128,148,161` — 5 direct assignments (ExposedRoute has no state machine)
- `application_controller.go:205` — computePhase sets phase directly (LoopZeroApplication has no state machine)
- `preview_controller.go:79,133,155,169` — 4 direct assignments (state machine imported as blank identifier)

**Finding references:** ARCH-002, COMP-002

**Remediation options:**
1. Add ExposedRouteMachine and ApplicationMachine to `pkg/statemachine/`
2. OR explicitly document ExposedRoute and LoopZeroApplication as "computed-phase" controllers in CLAUDE.md with rationale (no transition enforcement needed for these simpler lifecycles)
3. For Preview: wire the existing `PreviewMachine` properly when the controller is registered (COMP-001)

---

### INV-004: circuit_breaker_coverage_all_external_provisioners
**Status:** VIOLATED

**Assertion:** Every external system client injected into any reconciler must be wrapped in a circuit breaker. Unwrapped clients cause retry storms during system degradation.

**Violations:**
- MinIO `AdminProvisioner` returned directly from `setupMinIO()` at `cmd/main.go:221-231` — no CB wrapper
- `GitRepositoryBindingController` makes direct `http.Client.Do()` calls at `gitrepositorybinding_controller.go:251-290` — no CB protection

**Compliant implementations (positive reference):**
- Gitea: `NewCircuitBreakerProvisioner(gitProvisioner, gitCB, logger)` at `main.go:133`
- ArgoCD: `NewCircuitBreakerProvisioner(argoCDProvisioner, argoCB, logger)` at `main.go:149`
- Harbor: `NewCircuitBreakerProvisioner(harborProvisioner, harborCB, logger)` at `main.go:253`

**Finding references:** ARCH-001, ARCH-004

---

### INV-005: finalizer_guard_before_cleanup
**Status:** VALIDATED

**Assertion:** Cleanup resources are never deleted before the finalizer is confirmed present. The finalizer is added atomically with a resource version check on first reconcile.

**Code location:** `operators/internal/controller/tenant_controller.go:111-118` — AddFinalizer + r.Update + r.Get (re-fetch) before any provisioner calls.

**Test:** `tenant_integration_test.go:365-388` — verifies finalizer present before and during deletion, verifies it is removed only after all cleanup completes.

---

### INV-006: secure_by_default_tls_verification
**Status:** VIOLATED

**Assertion:** All operator HTTP clients must verify TLS by default. InsecureSkipVerify=true must require explicit opt-in.

**Violation:** `operators/internal/httpclient/client.go:28-34` — `skipTLSVerify()` returns `true` (skip verification) when `OPERATOR_INSECURE_SKIP_TLS` is unset.

**Finding references:** COMP-007, SEC-001

**Fix:** Invert the default — return `false` (enforce TLS) unless `OPERATOR_INSECURE_SKIP_TLS=true` is explicitly set.

---

### INV-007: idempotent_reconcile_for_existing_resources
**Status:** VIOLATED

**Assertion:** Running the same reconcile N times for an unchanged resource produces identical state each time.

**Violation:** `tenant_integration_test.go:266` — FAILING TEST. After 3 consecutive Tenant reconciles (spec updates), `countResources()` reports NetworkPolicy count as 3 instead of 0. Policies accumulate instead of remaining stable.

**Finding reference:** CODE-001

**Root cause:** `reconcileBuildNetworkPolicy()` creates policies in the build namespace via `r.BuildClient`, but `countResources()` in the test counts them in the runtime namespace — but the real issue is that spec-triggered reconciles in EnvironmentController create additional NetworkPolicies in the runtime namespace without idempotency guards.

---

### INV-008: inv_tenant_namespace_isolation
**Status:** VALIDATED

**Assertion:** TenantController reconcile for Tenant A cannot read, modify, or delete resources belonging to Tenant B. Cross-tenant GC uses `metav1.IsControlledBy` UID check.

**Code location:** `operators/internal/controller/tenant_controller.go:280-296` — `reconcileEnvironmentCRs` checks `metav1.IsControlledBy(env, tenant)` before GC.

**Test:** `tenant_isolation_test.go:250-311` — verifies that reconciling Tenant A does not affect Tenant B's namespaces or resources.

---

### INV-009: inv_status_via_state_machine
**Status:** VIOLATED

**Assertion:** Every Phase field update in a CR goes through `pkg/statemachine.TryTransition()` or `Transition()`.

**Compliant controllers:** TenantController, EnvironmentController, PlatformBootstrapController
**Non-compliant controllers:** ExposedRoute (5 direct assignments), LoopZeroApplication (computePhase), PreviewController (4 direct assignments)

**Note:** ExposedRoute and LoopZeroApplication violations may be acceptable if documented explicitly — these CRDs have simpler lifecycles without invalid-transition risk. PreviewController is more concerning because the state machine was intentionally imported but then bypassed.

---

### INV-010: inv_finalizer_completes
**Status:** VIOLATED (partial)

**Assertion:** Finalizer cleanup eventually completes — no infinite requeue without a bounded retry limit and escalation path.

**Phase 2 status:** COMPLIANT — `DeletionMaxAttempts=10` with exponential backoff. After exhaustion: `TenantDeletionStuckTotal.Inc()` and `RequeueDeletionStuck (1h)`.

**Phase 1 status:** NON-COMPLIANT — Environment CR delete failures are logged but not returned. The loop continues with `childEnvCount++` already incremented. Infinite 5-second requeue on persistent Phase 1 failures. No retry cap. No metric emitted.

**Finding reference:** VAL-005, CODE-006

---

### INV-011: inv_owner_refs_set
**Status:** VALIDATED

**Assertion:** Environment CRs created by TenantController have ownerRef pointing to the parent Tenant CR. Kubernetes GC will clean up orphaned Environment CRs when the Tenant is deleted.

**Code location:** `operators/internal/controller/tenant_controller.go:254-275` — `reconcileEnvironmentCRs` sets ownerRef via `controllerutil.SetControllerReference`. `isChild` check uses ownerRef UID matching.

---

### INV-012: inv_secret_namespace_scoped
**Status:** VIOLATED

**Assertion:** Operator RBAC never grants cluster-wide delete on `core/v1 Secrets`. Secret operations are scoped to specific namespaces.

**Violation:** `charts/loopzero-tenant-controller/templates/clusterrole.yaml` grants `get/list/watch/create/update/delete` on `core/v1 Secrets` at cluster scope with no namespace restriction.

**Finding reference:** INFRA-001

**Correct pattern:** `charts/loopzero-tenant-controller/templates/platformbootstrap_role.yaml` demonstrates namespace-scoped `Role` per namespace.

---

### INV-013: inv_no_panic_in_reconcile
**Status:** VIOLATED

**Assertion:** No `panic()` call exists in any code path reachable from `Reconcile()`. Errors in reconcile hot paths must be returned, not panicked.

**Violation:** `operators/internal/controller/tenant_harbor.go:271`
```go
panic(fmt.Sprintf("harbor: marshal docker config: %v", err))
```

Called from `ensureHarborRobotAccounts()` which is in the Tenant reconcile step list. `json.Marshal` errors on structs with string fields are loopzeroretically impossible but the `panic()` call exists.

**Finding reference:** CODE-002

**Fix:** Change `buildDockerConfigJSON` signature to `(string, error)`. Return `fmt.Errorf("build docker config: %w", err)` instead of panic.

---

### INV-014: inv_circuit_breaker_on_external
**Status:** VIOLATED

**Assertion:** Every call to an external system (Gitea, ArgoCD, Harbor, MinIO, any HTTP service) from a reconcile loop is wrapped in a circuit breaker. Equivalent to ARCH-001/ARCH-004.

**Violation summary:**
1. MinIO `AdminProvisioner` in `cmd/main.go:221-231` — not wrapped
2. `GitRepositoryBindingController` inline HTTP at `gitrepositorybinding_controller.go:251-290` — not wrapped

**Finding references:** ARCH-001, ARCH-004

---

### INV-015: inv_alert_metrics_emitted
**Status:** VIOLATED

**Assertion:** Every `expr:` in a PrometheusRule references a metric name that is emitted (`prometheus.MustRegister`) somewhere in the codebase.

**Violations:** 12 alerts reference metrics that do not exist in any Go file:
- `loopzero_reconciler_errors_total` (real: `loopzero_operator_reconcile_errors_total`)
- `loopzero_secret_sync_hash_mismatch_total` (no AppSecret controller)
- `loopzero_break_glass_sessions_active`, `loopzero_build_egress_connectivity`, `loopzero_build_started_timestamp`, `loopzero_build_status`, `loopzero_fallback_active`, `loopzero_git_desired_state_divergence`, `loopzero_gitea_last_backup_timestamp`, `loopzero_git_mirror_status`, `loopzero_tenant_pods_pending_duration_seconds`, `loopzero_tenant_quota_usage_ratio`

**Finding reference:** VAL-001

**Remediation:** Add a CI job that extracts metric names from all PrometheusRule `expr:` fields and cross-references against `prometheus.NewCounter/NewGauge/NewHistogram` definitions in Go source files.
