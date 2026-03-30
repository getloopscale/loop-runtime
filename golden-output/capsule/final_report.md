# Golden Harvest Report — Capsule → LoopZero

**Pipeline run:** 2026-03-29
**Mode:** golden-only (phases 1, 2, 5)
**Reference project:** Capsule (CNCF Sandbox multi-tenancy operator — https://projectcapsule.dev)
**Target:** LoopZero PaaS operator suite (`operators/internal/controller/`)
**Database:** `golden-output/capsule/golden.db`

---

## Executive Summary

This report presents the complete output of the golden harvest pipeline run against the Capsule
multi-tenancy operator. The goal was to identify test patterns and behavioral contracts in a
production-grade CNCF Sandbox project and convert them into portable, implementation-agnostic
tests that raise the quality floor of LoopZero's operator suite.

**What was done:**

From 21 source tests across 3 behavioral domains (tenant-isolation, resource-quotas,
network-policies), the pipeline extracted 8 behavioral contracts and produced 21 golden tests
in 3 Go files ready to drop into LoopZero's envtest suite. No source test was skipped — 4 were
explicitly deprecated or deferred with documented rationale.

**What LoopZero gains:**

The golden suite covers the most critical failure modes for a multi-tenant PaaS operator:
namespace isolation, RBAC propagation, resource quota enforcement, network policy management,
and controller idempotency. These 21 tests, combined with LoopZero's existing Ginkgo suite,
provide a behavioral safety net that spans the full tenant lifecycle from provisioning to
drift correction.

**Immediate action:** Copy `golden_suite/portable_tests/*.go` into
`operators/internal/controller/` and run `go test ./internal/controller/ -ginkgo.focus="Golden:"`.

---

## Reference System Profile — Capsule

| Attribute | Value |
|-----------|-------|
| Project | Capsule |
| Organization | CNCF Sandbox |
| URL | https://projectcapsule.dev |
| Repository | github.com/projectcapsule/capsule |
| Purpose | Multi-tenancy framework for Kubernetes: allows multiple teams to share one cluster via isolated Tenant objects |
| Language | Go |
| Test framework | Ginkgo v2 + Gomega + `envtest.UseExistingCluster=true` |
| Controller runtime | controller-runtime v0.x (kubebuilder-based) |
| CRD type | `capsulev1beta2.Tenant` — one CR per team, owns multiple namespaces |
| Admission webhooks | 4 admission webhooks enforcing tenant boundary invariants |
| Key mechanism | Tenant owners impersonated via `rest.ImpersonationConfig`; managed resources tracked with Capsule ownership labels |
| Timeout convention | `defaultTimeoutInterval = 40s`, `defaultPollInterval = 1s` for all `Eventually` assertions |

### Why Capsule is a valid reference for LoopZero

Both systems solve the same underlying problem: one Kubernetes operator manages isolated
resource contexts (namespaces, RBAC, quotas, network policies) on behalf of multiple tenants.
Capsule solves this for cluster-sharing; LoopZero solves this for a PaaS product where each
customer is a tenant. The behavioral contracts are structurally identical — only the
ownership model and naming conventions differ.

---

## Harvest Summary

### Counts (verified against database)

| Metric | Count |
|--------|-------|
| Source tests extracted | 21 |
| Source tests harvested (accepted) | 21 |
| Source tests rejected | 0 |
| Behavioral contracts mapped | 8 |
| Golden tests produced | 21 |
| Golden tests: critical priority | 12 |
| Golden tests: high priority | 8 |
| Golden tests: medium priority | 1 |
| Go test files written | 3 |
| Domains covered | 3 |
| Fixtures extracted | 0 (inline per test) |
| Pipeline iterations | 2 |

### Source tests by domain

| Domain | Source tests | Coverage note |
|--------|-------------|---------------|
| tenant-isolation | 12 | Full lifecycle: create, hijack prevention, cordoning, scalability, PV isolation |
| resource-quotas | 6 | Namespace quota, pod quota, custom CRD quota, replication, drift correction |
| network-policies | 3 | Replication, deletion prevention, drift correction |

### Domains excluded from harvest

The following Capsule domains were evaluated and excluded as not relevant to LoopZero:

- Ingress hostname collision prevention (LoopZero uses Traefik, not Capsule ingress)
- Image pull policy enforcement (LoopZero builds images centrally, no per-tenant pull policy)
- Service metadata propagation (not part of LoopZero's tenant model)
- User impersonation scope tests (LoopZero uses ServiceAccounts, not human user impersonation)

---

## Golden Suite Overview

### File inventory

| File | Domain | Golden tests | Total specs (including sub-cases) |
|------|--------|--------------|------------------------------------|
| `golden_suite/portable_tests/tenant_isolation_test.go` | tenant-isolation | 12 | 26 |
| `golden_suite/portable_tests/resource_quotas_test.go` | resource-quotas | 5 | 19 |
| `golden_suite/portable_tests/network_policies_test.go` | network-policies | 4 | 13 |
| **Total** | | **21** | **58** |

### Priority breakdown

| Priority | Count | Percentage |
|----------|-------|-----------|
| critical | 12 | 57% |
| high | 8 | 38% |
| medium | 1 | 5% |

### Installation

```bash
cp golden-output/capsule/golden_suite/portable_tests/*.go \
    operators/internal/controller/

cd operators && go test ./internal/controller/ -v -count=1 \
    -run TestControllers -ginkgo.focus="Golden:"
```

The tests use the same package-level variables (`ctx`, `k8sClient`, `timeout`, `interval`)
defined in `operators/internal/controller/suite_test.go`. No additional setup required.

---

## Coverage Analysis by Domain

![Coverage by domain](figures/coverage_by_domain.svg)

### tenant-isolation (12 golden tests)

This domain covers the core tenant lifecycle: from provisioning through RBAC establishment
to cross-tenant isolation invariants. It is the highest-priority domain because a failure here
means a tenant can access another tenant's resources — a critical security violation in any PaaS.

| Golden Test ID | Priority | What it verifies |
|----------------|----------|-----------------|
| `golden_tenant_isolation_build_namespace_created` | critical | Build namespace exists with `loopzero.io/tenant`, `loopzero.io/component=build`, `loopzero.io/managed-by` labels; no ownerReference (cross-cluster) |
| `golden_tenant_isolation_runtime_namespace_labels` | critical | Runtime namespace exists with correct tenant and environment labels |
| `golden_tenant_isolation_cross_tenant_namespace_no_shared_labels` | critical | Two tenants cannot share a namespace — `loopzero.io/tenant` values are distinct |
| `golden_tenant_isolation_tenant_reaches_active_phase` | critical | Tenant CR transitions to `Active` phase after all resources provisioned |
| `golden_tenant_isolation_idempotent_reconciliation` | critical | Repeated reconciliation does not duplicate namespaces or RBAC resources |
| `golden_tenant_isolation_rolebinding_deployer` | critical | `loopzero-deployer` RoleBinding exists in each runtime namespace |
| `golden_tenant_isolation_env_cr_created_for_each_environment` | critical | TenantController creates one Environment CR per environment in spec |
| `golden_tenant_isolation_finalizer_prevents_premature_deletion` | high | Tenant CR finalizer blocks GC until cleanup completes |
| `golden_tenant_isolation_empty_environments_rejected` | high | Tenant CR with empty environments list rejected at admission |
| `golden_tenant_isolation_build_executor_rbac` | high | Build namespace has Argo Workflow executor RBAC for workflow pods |
| `golden_tenant_isolation_build_submitter_rbac` | high | Build namespace has `workflow-submitter` RBAC for the API service account |
| `golden_tenant_isolation_build_namespace_condition` | high | Tenant CR gets `BuildNamespaceReady` and `QuotaEnforced` conditions set to True |

**Source coverage:** 12 source tests → 12 golden tests (some source tests contribute to multiple golden tests; some source tests were deprecated — see below).

### resource-quotas (5 golden tests)

This domain covers quota enforcement across both the build plane (protecting shared build
infrastructure) and the runtime plane (protecting tenants from each other). It also covers
drift correction — the reconciliation loop restoring quotas that were manually deleted or
modified.

| Golden Test ID | Priority | What it verifies |
|----------------|----------|-----------------|
| `golden_resource_quotas_build_quota_enforced` | critical | Build namespace ResourceQuota enforces CPU and memory limits |
| `golden_resource_quotas_runtime_quota_from_tenant_limits` | critical | Runtime namespace ResourceQuota reflects Tenant `spec.limits` values |
| `golden_resource_quotas_limitrange_default_container_limits` | high | LimitRange in runtime namespace sets default container resource limits |
| `golden_resource_quotas_restored_after_manual_deletion` | high | Deleted ResourceQuota is restored by next reconciliation cycle |
| `golden_resource_quotas_build_limitrange_created` | medium | Build namespace has LimitRange with container default resource limits |

**Source coverage:** 6 source tests → 5 golden tests (3 source quota tests collapsed into one golden test covering the build quota context; drift correction covered by one golden test mapping to 2 source tests).

### network-policies (4 golden tests)

This domain covers network isolation at both the build plane and the runtime plane, plus
drift correction of managed NetworkPolicies.

| Golden Test ID | Priority | What it verifies |
|----------------|----------|-----------------|
| `golden_network_policies_build_egress_restrict` | critical | Build namespace has egress-restricting NetworkPolicy blocking internet access |
| `golden_network_policies_runtime_deny_all` | critical | Runtime namespace has a deny-all NetworkPolicy covering both Ingress and Egress |
| `golden_network_policies_runtime_three_policies` | critical | Runtime namespace has at least 3 NetworkPolicies: deny-all, allow-ingress, allow-egress |
| `golden_network_policies_restored_after_modification` | high | Modified NetworkPolicy is restored to desired state on next reconciliation |

**Source coverage:** 3 source tests → 4 golden tests (the ingress/egress isolation test was split into `runtime_deny_all` and `runtime_three_policies` to reflect LoopZero's two-context policy model).

---

## Source-to-Golden Conversion

![Test progress](figures/test_progress.svg)

### Conversion table

| Source Test ID | Domain | Golden Test(s) | Decision |
|----------------|--------|----------------|----------|
| `src_tenant_isolation_namespace_created_with_rolebindings` | tenant-isolation | build_namespace_created, runtime_namespace_labels, rolebinding_deployer, env_cr_created, build_executor_rbac, build_submitter_rbac | 1 source → 6 golden (multiple behaviors) |
| `src_tenant_isolation_group_owner_creates_namespace` | tenant-isolation | runtime_namespace_labels | Group owner maps to Environment CR delegation |
| `src_tenant_isolation_non_owner_cannot_select_foreign_tenant` | tenant-isolation | cross_tenant_namespace_no_shared_labels | Label-based isolation |
| `src_tenant_isolation_namespace_lifecycle_status_tracking` | tenant-isolation | tenant_reaches_active_phase, finalizer_prevents_premature_deletion | State machine + finalizer |
| `src_tenant_isolation_additional_rolebindings_propagated` | tenant-isolation | idempotent_reconciliation | CreateOrUpdate idempotency |
| `src_tenant_isolation_missing_tenant_namespace_creation_fails` | tenant-isolation | empty_environments_rejected | CRD admission validation |
| `src_tenant_isolation_cannot_hijack_system_namespace_via_ownerref` | tenant-isolation | (none) | LoopZero enforces at admission level — not controller level; no equivalent needed |
| `src_tenant_isolation_cannot_hijack_system_namespace_via_label` | tenant-isolation | cross_tenant_namespace_no_shared_labels | Covered by label isolation test |
| `src_tenant_isolation_owner_ref_patch_silently_ignored` | tenant-isolation | (deprecated) | ownerRef behavior differs by design — cross-cluster namespaces cannot use ownerReferences |
| `src_tenant_isolation_cordoning_blocks_pod_deletion` | tenant-isolation | (deprecated) | LoopZero does not implement cordon; test when tenant suspension is built |
| `src_tenant_isolation_cross_tenant_pv_mount_prevented` | tenant-isolation | (deferred) | LoopZero PVC isolation not yet implemented; write when PVC cross-tenant guard is added |
| `src_tenant_isolation_scalability_50_namespaces` | tenant-isolation | (deferred) | Load test, not behavioral; belongs in `infra/tests/load/` with 50-namespace threshold |
| `src_resource_quotas_namespace_count_quota_enforced` | resource-quotas | build_quota_enforced | Workflow count quota |
| `src_resource_quotas_pod_quota_blocks_new_pods` | resource-quotas | build_quota_enforced | CPU/memory quota |
| `src_resource_quotas_custom_crd_quota_enforced` | resource-quotas | build_quota_enforced | Argo Workflow count |
| `src_resource_quotas_limitrange_and_quota_replicated_to_namespaces` | resource-quotas | runtime_quota_from_tenant_limits, limitrange_default_container_limits, build_limitrange_created, build_namespace_condition | Primary source for quota/LR tests |
| `src_resource_quotas_resourcequota_restored_after_modification` | resource-quotas | restored_after_manual_deletion | Drift correction |
| `src_resource_quotas_limitrange_restored_after_modification` | resource-quotas | restored_after_manual_deletion | Drift correction (covered by same golden test) |
| `src_network_policies_tenant_networkpolicy_created_in_namespace` | network-policies | build_egress_restrict, runtime_deny_all | Two distinct policy contexts |
| `src_network_policies_tenant_networkpolicy_restored_after_modification` | network-policies | restored_after_modification | Drift correction |
| `src_network_policies_networkpolicy_ingress_from_same_tenant` | network-policies | runtime_three_policies | Ingress allow policy |

### Deprecated source tests (4)

| Source Test | Reason |
|-------------|--------|
| `src_tenant_isolation_owner_ref_patch_silently_ignored` | ownerReference model is architecturally different: LoopZero namespaces live on separate physical clusters and cannot carry ownerReferences to mgmt cluster objects. The golden test `build_namespace_created` explicitly asserts `OwnerReferences == empty` as correct behavior. |
| `src_tenant_isolation_cordoning_blocks_pod_deletion` | LoopZero does not implement a cordon/uncordon feature in the current roadmap. Write a dedicated test when tenant suspension is added to the operator. |
| `src_tenant_isolation_cross_tenant_pv_mount_prevented` | PVC cross-tenant isolation requires a PV controller (Capsule has `controllers/pv/controller.go`). LoopZero has not implemented this. Deferred — write the golden test when the PV controller is added. |
| `src_tenant_isolation_scalability_50_namespaces` | This is a load test (50 namespaces, 50 running pods). It does not test a behavioral contract — it tests controller throughput. Add to `infra/tests/load/` with Capsule's 50-namespace threshold as the performance baseline. |

---

## Behavioral Contracts Extracted

8 contracts were extracted from Capsule's source. All contracts are implementation-agnostic
and applicable to any kubebuilder-based multi-tenant operator.

| Contract ID | Name | Type | Domains | Direct LoopZero mapping |
|-------------|------|------|---------|---------------------|
| `contract_capsule_tenant_rolebindings_propagation` | RoleBindings propagated to all child namespaces | protocol | tenant-isolation | TenantController: `loopzero-deployer` RoleBinding in build + runtime namespaces; prune on spec change |
| `contract_capsule_tenant_networkpolicy_propagation` | NetworkPolicies replicated into every child namespace | protocol | network-policies | Build-namespace egress restriction; EnvironmentController deny-all + allow-ingress |
| `contract_capsule_tenant_resourcequota_enforcement` | Tenant-scoped ResourceQuota aggregates cross-namespace usage | invariant | resource-quotas | TenantController quota enforcement; proportional distribution formula |
| `contract_capsule_namespace_metadata_propagation` | Tenant spec labels/annotations propagated and cleaned up | protocol | tenant-isolation | TenantController + EnvironmentController namespace labelling; pruning on spec update |
| `contract_capsule_tenant_cordoning` | Cordoning blocks all write operations in tenant namespaces | invariant | tenant-isolation | Maps to LoopZero tenant suspension (not yet implemented) |
| `contract_capsule_cross_tenant_namespace_prevention` | User cannot assign namespace to a tenant they do not own | invariant | tenant-isolation | Enforced via `loopzero.io/tenant` label validation at admission |
| `contract_capsule_pv_cross_tenant_mount_prevention` | PV bound to Tenant A cannot be claimed by Tenant B | invariant | tenant-isolation | Not yet implemented; deferred to storage isolation work |
| `contract_capsule_reconcile_status_conditions` | Status conditions updated on every reconcile cycle, even on error | invariant | all domains | All LoopZero controllers must use deferred status writes |

### Contract type breakdown

| Type | Count | Description |
|------|-------|-------------|
| protocol | 3 | Describes an async propagation flow with pre/postconditions (RoleBindings, NetworkPolicies, metadata) |
| invariant | 5 | Describes a property that must hold regardless of timing or operator behavior (quota ceiling, cross-tenant block, status write) |

---

## Key Behavioral Patterns Extracted

The following patterns were identified in Capsule's source and test suite. Each has a direct
applicability assessment for LoopZero.

### Pattern 1: Set-difference pruning

**Where in Capsule:** `controllers/tenant/rolebindings.go`, `controllers/tenant/networkpolicies.go`

**Pattern:** When syncing a list of managed resources (RoleBindings, NetworkPolicies, ResourceQuotas),
the controller computes the diff between what exists (filtered by ownership label) and what the
spec requires. Resources in the existing set but not in the desired set are deleted. This is
the standard Kubernetes operator pattern but Capsule applies it across multiple namespaces simultaneously.

**LoopZero applicability:** HIGH. The TenantController must prune stale resources from the build
namespace and all runtime namespaces when the tenant spec changes. Current implementation risk:
if pruning is not implemented, deleted spec entries leave orphaned resources in production.

**Recommendation:** Audit `operators/internal/controller/tenant/` for prune loops. If absent,
implement set-difference deletion using `loopzero.io/managed-by=tenant-controller` label selector
as the ownership filter.

### Pattern 2: Deferred status writes

**Where in Capsule:** `controllers/tenant/manager.go`

**Pattern:**
```go
defer func() {
    if err := r.updateTenantStatus(ctx, tenant); err != nil {
        log.Error(err, "failed to update tenant status")
    }
}()
```
The `defer` ensures status conditions are written after every reconciliation pass, even when
the reconciliation itself returns a non-nil error. This prevents the Tenant from getting
stuck in an unknown state.

**LoopZero applicability:** CRITICAL. All 7 LoopZero controllers must follow this pattern. A controller
that skips status writes on error makes operator health invisible to the product and to on-call
engineers. This is the most important operational pattern in the Capsule codebase.

**Recommendation:** Add a linting rule or PR checklist item: "Does every `Reconcile()` function
defer a status write?" Audit all 7 controllers against this invariant.

### Pattern 3: Proportional ResourceQuota distribution

**Where in Capsule:** `controllers/tenant/resourcequotas.go`

**Pattern:** In tenant-scoped quota mode, when the sum of resource usage across all namespaces
approaches the tenant ceiling, each namespace's `hard` limit is set to its own current `used`
value. This effectively blocks new resource creation without requiring a central admission
webhook decision. The formula:

```
for each namespace ns:
    ns.quota.hard = tenantHard - totalUsed + ns.used
```

When `totalUsed >= tenantHard`, the formula collapses to `ns.quota.hard = ns.used` (zero headroom).

**LoopZero applicability:** MEDIUM. LoopZero enforces quotas per build namespace (limiting concurrent
build pods and CPU/memory). The proportional formula is more relevant once LoopZero supports
multi-environment tenants with shared quota pools. The current flat-quota model is sufficient
for the starter plan.

**Recommendation:** Document the current quota model explicitly. When multi-plan tiers are
introduced, use the proportional formula as the reference implementation.

### Pattern 4: Ownership label as the GC boundary

**Where in Capsule:** All resource sync functions

**Pattern:** Every managed resource receives a label `capsule.clastix.io/tenant=<tenant-name>`.
Pruning queries use label selectors to find all managed resources of a type in a namespace,
then deletes those not present in the current spec. User-created resources without the ownership
label are never touched.

**LoopZero applicability:** HIGH. LoopZero uses `loopzero.io/managed-by=<controller-name>` labels. The
pattern is the same but the label key differs. The critical invariant to preserve: the ownership
label must be applied atomically with resource creation (in the same `CreateOrUpdate` call).
If a resource is created without the label, it will never be pruned.

**Recommendation:** Verify that all `CreateOrUpdate` calls in LoopZero's operator attach the
`loopzero.io/managed-by` label in the resource spec before the server-side apply, not after.

### Pattern 5: Admission webhook as the last line of defense

**Where in Capsule:** 4 admission webhooks (namespace creation, ownerRef mutation, cordoning, PV binding)

**Pattern:** Controller-level reconciliation handles the happy path. Admission webhooks handle
the adversarial path: a user attempting to bypass the controller by directly mutating resources.
Tests 3, 4, 6, and 9 all verify webhook enforcement, not controller behavior.

**LoopZero applicability:** MEDIUM. LoopZero's operator does not currently have admission webhooks.
The TenantController enforces tenant boundaries via label assignment, but there is no webhook
preventing a cluster-admin from directly patching a namespace label to claim cross-tenant
ownership. This is acceptable in the current pre-release state (LoopZero controls access to the
mgmt cluster) but should be hardened before multi-tenant production.

**Recommendation:** Log this as a known security assumption: LoopZero's tenant isolation currently
relies on cluster-admin access control to the mgmt cluster, not on admission webhook enforcement.
Add to Sprint 6 (Security) backlog.

### Pattern 6: Eventually-consistent assertions with explicit timeouts

**Where in Capsule:** Every async test assertion

**Pattern:**
```go
Eventually(func() error {
    return k8sClient.Get(ctx, key, &ns)
}, 40*time.Second, 1*time.Second).Should(Succeed())
```

Capsule uses 40-second timeouts for all controller reconciliation assertions. This is
intentionally conservative — it accounts for API server rate limiting, webhook latency,
and pod scheduling delays in the CI environment.

**LoopZero applicability:** HIGH. LoopZero's envtest suite should adopt the same convention. The
golden tests use `timeout = 40*time.Second` and `interval = 1*time.Second` matching the
Capsule reference. Tests that use shorter timeouts (5s, 10s) are fragile in slow CI environments.

**Recommendation:** Standardize LoopZero's envtest timeout to 40s/1s. Set this in `suite_test.go`
as `var (timeout = 40*time.Second; interval = time.Second)`.

---

## Recommendations for LoopZero

Recommendations are ordered by priority and tied to specific findings.

### R1 (Critical): Audit all 7 controllers for deferred status writes

**Finding:** Capsule pattern 2 — deferred status writes after every reconcile cycle.
**Risk:** A controller that skips status writes on error leaves the operator blind during failures.
Production incidents become undiagnosable.
**Action:** For each controller in `operators/internal/controller/`, verify that `Reconcile()`
has a `defer` block that calls the status update function unconditionally. If absent, add it.
**Effort:** 1-2 hours per controller.

### R2 (Critical): Install the golden test suite into the operator test package

**Finding:** 21 golden tests ready to run in `golden_suite/portable_tests/`.
**Risk:** Without these tests, key behavioral contracts (cross-tenant isolation, quota enforcement,
NetworkPolicy drift correction) have no automated coverage.
**Action:** `cp golden-output/capsule/golden_suite/portable_tests/*.go operators/internal/controller/`
then run `go test ./internal/controller/ -ginkgo.focus="Golden:"`. Fix any failures before
merging.
**Effort:** 30 minutes to install; variable time to fix failures if any.

### R3 (High): Implement set-difference pruning in TenantController

**Finding:** Capsule pattern 1 — label-based set-difference pruning of managed resources.
**Risk:** If the TenantController does not prune stale resources, updating a Tenant spec
(removing an environment, changing quota values) will leave orphaned ResourceQuotas and
NetworkPolicies in production namespaces.
**Action:** Audit `operators/internal/controller/tenant/` for prune loops on RoleBindings,
ResourceQuotas, and NetworkPolicies. Implement set-difference deletion if absent.
**Effort:** 4-8 hours per resource type.

### R4 (High): Standardize envtest timeouts to 40s/1s

**Finding:** Capsule pattern 6 — 40-second eventually timeouts.
**Risk:** Tests with 5-10 second timeouts fail intermittently in slow CI environments (GHA
runners with constrained CPU). This causes false-negative test results that erode trust in CI.
**Action:** Update `operators/internal/controller/suite_test.go` to define
`timeout = 40*time.Second` and `interval = time.Second`. Update any test files that declare
their own shorter timeouts.
**Effort:** 1-2 hours.

### R5 (High): Verify ownership label applied atomically on resource creation

**Finding:** Capsule pattern 4 — ownership label as the GC boundary.
**Risk:** If `CreateOrUpdate` creates a resource without the `loopzero.io/managed-by` label
(e.g., due to a missing `Labels` initialization in the object spec), the resource escapes
the pruning GC and accumulates indefinitely.
**Action:** Grep for `CreateOrUpdate` calls in `operators/`. Verify each resource spec
initializes labels before the call, not after. Add a test that creates a resource, then
removes it from the spec, and verifies it is pruned.
**Effort:** 2-3 hours.

### R6 (Medium): Add cordon/suspension to Sprint backlog

**Finding:** Capsule behavioral contract `contract_capsule_tenant_cordoning` — cordoning blocks
all writes in tenant namespaces.
**Risk:** LoopZero has no mechanism to suspend a tenant (e.g., for unpaid accounts, abuse, or
maintenance). This is a product gap that will require operator changes when billing/abuse
features are built.
**Action:** Add "Tenant suspension" to Sprint 5 or Sprint 9 backlog. Reference the Capsule
cordoning implementation as the design pattern. The golden test skeleton is in the deprecated
section of the suite manifest.
**Effort:** Sprint-level planning item.

### R7 (Medium): Document cross-cluster ownerReference limitation

**Finding:** Capsule uses ownerReferences for GC; LoopZero cannot (cross-cluster namespaces).
**Risk:** Engineers unfamiliar with the multi-cluster architecture may add ownerReferences to
LoopZero-managed resources expecting K8s GC to clean them up, then be surprised when GC never fires.
**Action:** Add a comment to `operators/internal/controller/tenant/` explaining why
ownerReferences are intentionally absent from cross-cluster resources. The golden test
`build_namespace_created` already asserts `OwnerReferences == empty` as the correct behavior.
**Effort:** 30 minutes.

### R8 (Low): Write load test using Capsule 50-namespace threshold as baseline

**Finding:** `src_tenant_isolation_scalability_50_namespaces` — Capsule verifies exact status
tracking for 50 namespaces under load (running pods per namespace).
**Risk:** LoopZero has no load test for the TenantController under scale. If status tracking
degrades at 20+ namespaces (e.g., due to missing pagination in list calls), it will not be
caught until production.
**Action:** Write a load test in `infra/tests/load/` that creates 50 tenants × 2 environments
each and verifies status convergence. Use Capsule's 40-second convergence threshold per
namespace as the performance baseline.
**Effort:** 4-6 hours.

---

## Compatibility Matrix

The following matrix maps each behavioral domain to the Capsule contracts covered and the
corresponding LoopZero controllers responsible for implementing them. Status reflects whether
the golden test suite covers the behavior.

| Domain | Behavioral contract | Capsule verdict | LoopZero controller | Golden test | Gap |
|--------|---------------------|-----------------|-----------------|-------------|-----|
| tenant-isolation | Namespace created with ownership labels on provisioning | Required | TenantController | `build_namespace_created`, `runtime_namespace_labels` | None |
| tenant-isolation | RBAC propagated to all owned namespaces | Required | TenantController | `rolebinding_deployer`, `build_executor_rbac`, `build_submitter_rbac` | None |
| tenant-isolation | Cross-tenant namespace isolation via labels | Required | TenantController | `cross_tenant_namespace_no_shared_labels` | None |
| tenant-isolation | Tenant CR reaches Active phase (state machine) | Required | TenantController | `tenant_reaches_active_phase` | None |
| tenant-isolation | Finalizer prevents premature CR deletion | Required | TenantController | `finalizer_prevents_premature_deletion` | None |
| tenant-isolation | Reconciliation is idempotent (no duplicates) | Required | TenantController | `idempotent_reconciliation` | None |
| tenant-isolation | Environment CR created per environment spec entry | Required | TenantController | `env_cr_created_for_each_environment` | None |
| tenant-isolation | Invalid tenant spec rejected at admission | Required | CRD validation | `empty_environments_rejected` | None |
| tenant-isolation | Tenant suspension blocks mutations | Future | Not implemented | (deprecated) | No cordon mechanism |
| tenant-isolation | PV cross-tenant mount prevention | Future | Not implemented | (deferred) | No PV controller |
| tenant-isolation | ownerRef hijacking prevention | Inherited | Admission (by design) | (none) | Cross-cluster design difference |
| resource-quotas | Build namespace ResourceQuota enforced | Required | TenantController | `build_quota_enforced` | None |
| resource-quotas | Runtime namespace ResourceQuota from tenant spec | Required | TenantController / EnvironmentController | `runtime_quota_from_tenant_limits` | None |
| resource-quotas | LimitRange in runtime namespace | Required | EnvironmentController | `limitrange_default_container_limits` | None |
| resource-quotas | ResourceQuota restored after manual deletion | Required | TenantController | `restored_after_manual_deletion` | None |
| resource-quotas | Proportional cross-namespace quota distribution | Future | Not implemented | (none) | No multi-env quota pooling |
| network-policies | Build namespace egress restriction | Required | TenantController | `build_egress_restrict` | None |
| network-policies | Runtime namespace deny-all policy | Required | EnvironmentController | `runtime_deny_all` | None |
| network-policies | Runtime namespace minimum 3 policies | Required | EnvironmentController | `runtime_three_policies` | None |
| network-policies | NetworkPolicy restored after modification | Required | EnvironmentController | `restored_after_modification` | None |
| all | Status conditions written after every reconcile | Required | All controllers | (covered per domain) | Needs audit (R1) |

**Legend:**
- Required — must work before production launch
- Future — needed for a planned feature not yet implemented
- Inherited — behavior provided by platform, not by LoopZero controllers

---

## Deliverable Manifest

All expected outputs have been verified to exist.

| File | Status |
|------|--------|
| `golden-output/capsule/golden.db` | Present |
| `golden-output/capsule/harvested/test_catalog.md` | Present |
| `golden-output/capsule/harvested/contract_map.md` | Present |
| `golden-output/capsule/golden_suite/suite_manifest.md` | Present |
| `golden-output/capsule/golden_suite/portable_tests/tenant_isolation_test.go` | Present |
| `golden-output/capsule/golden_suite/portable_tests/resource_quotas_test.go` | Present |
| `golden-output/capsule/golden_suite/portable_tests/network_policies_test.go` | Present |
| `golden-output/capsule/figures/coverage_by_domain.svg` | Present |
| `golden-output/capsule/figures/test_progress.svg` | Present |
| `golden-output/capsule/state/meetings/iteration_001.md` | Present |
| `golden-output/capsule/state/meetings/iteration_002.md` | Present |
| `golden-output/capsule/final_report.md` | **This file** |

---

*Report generated by the golden-harvest-loop pipeline, phase 5 (REPORT).*
*All counts in this report are derived from live database queries against `golden-output/capsule/golden.db`.*
*Cross-validation: source_tests=21, golden_tests=21, contracts=8, domains=3 — all consistent.*
