# Golden Test Suite Manifest — Capsule → LoopZero

Generated: 2026-03-29
Source reference: Capsule multi-tenancy operator (https://projectcapsule.dev)
Target: LoopZero PaaS operator suite (`operators/internal/controller/`)

---

## Overview

This suite contains 21 golden tests normalized from 21 Capsule source tests across 3 domains.
Each golden test is implementation-agnostic: it tests observable Kubernetes behavior (namespace
labels, RBAC resources, ResourceQuotas, NetworkPolicies, status conditions) without coupling
to internal Capsule or LoopZero implementation details.

All tests use Ginkgo v2 + Gomega and are compatible with LoopZero's envtest setup.

---

## File Index

| File | Domain | Golden Tests | Priority |
|------|--------|-------------|----------|
| `portable_tests/tenant_isolation_test.go` | tenant-isolation | 11 | critical/high |
| `portable_tests/resource_quotas_test.go` | resource-quotas | 6 | critical/high/medium |
| `portable_tests/network_policies_test.go` | network-policies | 4 | critical/high |

**Total: 21 golden tests** (15 critical/high + 6 medium)

---

## How to Install

Copy the `portable_tests/` directory into LoopZero's controller test package:

```bash
cp -r golden-output/capsule/golden_suite/portable_tests/*.go \
    operators/internal/controller/
```

The tests use the same package-level variables (`ctx`, `k8sClient`, `timeout`, `interval`)
defined in `operators/internal/controller/suite_test.go`. No additional setup is required.

Run only the golden tests:

```bash
cd operators && go test ./internal/controller/ -v -count=1 \
    -run TestControllers -ginkgo.focus="Golden:"
```

---

## Behavioral Contracts Covered

| BC ID | Capsule Contract | LoopZero Equivalent | Priority |
|-------|-----------------|-----------------|----------|
| BC-001 | Namespace + owner RoleBinding created when tenant owns namespace | Build namespace + runtime namespace + Environment CR + deployer RoleBinding | critical |
| BC-002 | ResourceQuota propagated to all tenant namespaces | build-quota in build-ns + tenant-quota in runtime-ns | critical |
| BC-003 | Invalid tenant spec rejected at admission | Empty environments list rejected by CRD minItems:1 | high |
| BC-004 | Drift correction: deleted/modified resources restored by reconcile loop | ResourceQuota + NetworkPolicy restored after manual deletion/modification | high |
| BC-005 | NetworkPolicy created in every tenant namespace | deny-all + allow-ingress-traefik + allow-egress in runtime-ns + build-egress-restrict in build-ns | critical |
| BC-006 | Ingress only from same-tenant namespaces | allow-ingress-traefik restricts source to ingress controller | high |
| BC-007 | Cross-tenant namespace isolation (no shared labels) | loopzero.io/tenant label uniquely identifies namespace ownership | critical |
| BC-008 | State machine: tenant lifecycle tracking | TenantPhase transitions Provisioning → Active | critical |

---

## Test Registry (Golden Tests)

### Domain: tenant-isolation (11 tests)

| ID | Name | Priority | Status |
|----|------|----------|--------|
| `golden_tenant_isolation_build_namespace_created` | Tenant provisioning creates build namespace with tenant isolation labels | critical | active |
| `golden_tenant_isolation_runtime_namespace_labels` | Tenant provisioning creates runtime namespace with tenant and environment labels | critical | active |
| `golden_tenant_isolation_cross_tenant_namespace_no_shared_labels` | Two tenants cannot share the same namespace via label collision | critical | active |
| `golden_tenant_isolation_tenant_reaches_active_phase` | Tenant CR transitions to Active phase after all resources are provisioned | critical | active |
| `golden_tenant_isolation_finalizer_prevents_premature_deletion` | Tenant CR finalizer prevents garbage collection until cleanup completes | high | active |
| `golden_tenant_isolation_idempotent_reconciliation` | Repeated reconciliation does not duplicate namespaces or RBAC resources | critical | active |
| `golden_tenant_isolation_rolebinding_deployer` | Tenant provisioning creates deployer RoleBinding in each runtime namespace | critical | active |
| `golden_tenant_isolation_env_cr_created_for_each_environment` | TenantController creates an Environment CR for each environment in spec | critical | active |
| `golden_tenant_isolation_empty_environments_rejected` | Tenant CR with empty environments list is rejected at admission | high | active |
| `golden_tenant_isolation_build_executor_rbac` | Build namespace has Argo Workflow executor RBAC for workflow pods | high | active |
| `golden_tenant_isolation_build_submitter_rbac` | Build namespace has workflow-submitter RBAC for the API to submit builds | high | active |
| `golden_tenant_isolation_build_namespace_condition` | Tenant CR gets BuildNamespaceReady and QuotaEnforced conditions set to True | high | active |

### Domain: resource-quotas (6 tests)

| ID | Name | Priority | Status |
|----|------|----------|--------|
| `golden_resource_quotas_build_quota_enforced` | Build namespace ResourceQuota enforces CPU and memory limits | critical | active |
| `golden_resource_quotas_runtime_quota_from_tenant_limits` | Runtime namespace ResourceQuota reflects Tenant spec.limits values | critical | active |
| `golden_resource_quotas_limitrange_default_container_limits` | LimitRange in runtime namespace sets default container resource limits | high | active |
| `golden_resource_quotas_restored_after_manual_deletion` | Deleted ResourceQuota is restored by next reconciliation | high | active |
| `golden_resource_quotas_build_limitrange_created` | Build namespace has LimitRange with container default resource limits | medium | active |

### Domain: network-policies (4 tests)

| ID | Name | Priority | Status |
|----|------|----------|--------|
| `golden_network_policies_build_egress_restrict` | Build namespace has egress-restricting NetworkPolicy that blocks internet access | critical | active |
| `golden_network_policies_runtime_deny_all` | Runtime namespace has a deny-all NetworkPolicy covering both Ingress and Egress | critical | active |
| `golden_network_policies_runtime_three_policies` | Runtime namespace has at least three NetworkPolicies: deny-all, allow-ingress, allow-egress | critical | active |
| `golden_network_policies_restored_after_modification` | Modified NetworkPolicy is restored to desired state on next reconciliation | high | active |

---

## Normalization Decisions

### 1. Capsule Tenant → LoopZero Tenant CR

**Coupling removed:** Capsule `capsulev1beta2.Tenant` with `.Spec.Owners[]` (User, Group, ServiceAccount).
**Replacement:** LoopZero `loopzerov1.Tenant` with `.Spec.Environments[]` and `.Spec.Limits`.
**Rationale:** LoopZero's tenancy model is team-based, not user-based. RBAC is created via
dedicated ServiceAccounts (loopzero-deployer) rather than binding external user identities.

### 2. Capsule namespace creation by owner → LoopZero Environment CR delegation

**Coupling removed:** Capsule tests used `ownerClient` to create namespaces as a tenant owner.
**Replacement:** LoopZero TenantController creates Environment CRs; EnvironmentController provisions namespaces.
**Rationale:** LoopZero namespace provisioning is declarative (CR-driven), not imperative (client-driven).
The observable output is the same: namespace exists with correct labels and RBAC.

### 3. Capsule GlobalTenantResource → LoopZero per-context resources

**Coupling removed:** Capsule `GlobalTenantResource` propagates ResourceQuotas and NetworkPolicies.
**Replacement:** Two separate resource contexts in LoopZero: build namespace (TenantController) and
runtime namespace (EnvironmentController). Tests verify each context independently.
**Rationale:** LoopZero's build isolation concern (no internet exfiltration) is orthogonal to
runtime tenant isolation. Merging them into one abstraction would hide the security intent.

### 4. Capsule owner cross-namespace access → LoopZero loopzero.io/tenant label isolation

**Coupling removed:** Capsule `CheckForOwnerRoleBindings()` helper (internal to Capsule test suite).
**Replacement:** Direct API server assertions using `k8sClient.Get()` and label selector queries.
**Rationale:** No dependency on reference implementation internals. Any operator that produces
the correct label values and resource names satisfies the behavioral contract.

### 5. Capsule cordoning → marked as deprecated (no LoopZero equivalent)

**Decision:** `src_tenant_isolation_cordoning_blocks_pod_deletion` has no behavioral equivalent
in LoopZero (LoopZero does not implement a cordon/uncordon feature). This source test was NOT normalized.
A separate golden test for tenant suspension should be written when LoopZero implements that state.

### 6. Capsule scalability (50 namespaces) → deferred

**Decision:** `src_tenant_isolation_scalability_50_namespaces` is a load test, not a behavioral
contract. It was not normalized into a golden test. A dedicated load test should be written
in `infra/tests/load/` using the Capsule behavioral threshold as a reference target.

### 7. ownerReference absence on namespaces

**Behavioral difference explicitly documented:** Capsule sets ownerReferences to enable
K8s GC. LoopZero cannot — namespaces live on different physical clusters. The golden test
`golden_tenant_isolation_build_namespace_created` asserts `OwnerReferences == empty` as
the correct LoopZero behavior, not a missing feature.

---

## Tolerance Definitions Used

| Tolerance | Usage in this suite |
|-----------|-------------------|
| `exact_match` | Label values, resource names, namespace names, condition statuses |
| `structural_match` | RoleBinding shape (kind+name of roleRef, subject count) |
| `set_match` | PolicyTypes list (order is undefined), environment names |
| `pattern_match` | Error message contents (ContainSubstring, not exact equality) |
| `numeric_tolerance` | Minimum policy count (>= 3, not == 3) |

---

## Source Test Coverage

| Source Test ID | Domain | Golden Test(s) | Notes |
|----------------|--------|----------------|-------|
| `src_tenant_isolation_namespace_created_with_rolebindings` | tenant-isolation | build_namespace_created, runtime_namespace_labels, rolebinding_deployer, env_cr_created, build_executor_rbac, build_submitter_rbac | Multiple behaviors from one source |
| `src_tenant_isolation_group_owner_creates_namespace` | tenant-isolation | runtime_namespace_labels | Group owner → Environment CR delegation |
| `src_tenant_isolation_non_owner_cannot_select_foreign_tenant` | tenant-isolation | cross_tenant_namespace_no_shared_labels | Label-based isolation |
| `src_tenant_isolation_namespace_lifecycle_status_tracking` | tenant-isolation | tenant_reaches_active_phase, finalizer_prevents_premature_deletion | State machine + finalizer |
| `src_tenant_isolation_additional_rolebindings_propagated` | tenant-isolation | idempotent_reconciliation | CreateOrUpdate idempotency |
| `src_tenant_isolation_missing_tenant_namespace_creation_fails` | tenant-isolation | empty_environments_rejected | CRD admission validation |
| `src_tenant_isolation_cannot_hijack_system_namespace_via_ownerref` | tenant-isolation | (no golden test — LoopZero enforces at admission level, not controller level) | |
| `src_tenant_isolation_cannot_hijack_system_namespace_via_label` | tenant-isolation | cross_tenant_namespace_no_shared_labels | Covered by label isolation test |
| `src_tenant_isolation_owner_ref_patch_silently_ignored` | tenant-isolation | (deprecated — ownerRef behavior differs by design) | |
| `src_tenant_isolation_cordoning_blocks_pod_deletion` | tenant-isolation | (deprecated — LoopZero does not implement cordon) | |
| `src_tenant_isolation_cross_tenant_pv_mount_prevented` | tenant-isolation | (deferred — LoopZero PVC isolation not yet implemented in envtest) | |
| `src_tenant_isolation_scalability_50_namespaces` | tenant-isolation | (deferred — load test, not behavioral) | |
| `src_resource_quotas_namespace_count_quota_enforced` | resource-quotas | build_quota_enforced | workflow count quota |
| `src_resource_quotas_pod_quota_blocks_new_pods` | resource-quotas | build_quota_enforced | CPU/memory quota |
| `src_resource_quotas_custom_crd_quota_enforced` | resource-quotas | build_quota_enforced | Argo workflow count |
| `src_resource_quotas_limitrange_and_quota_replicated_to_namespaces` | resource-quotas | runtime_quota_from_tenant_limits, limitrange_default_container_limits, build_limitrange_created, build_namespace_condition | Primary source for quota/LR tests |
| `src_resource_quotas_resourcequota_restored_after_modification` | resource-quotas | restored_after_manual_deletion | Drift correction |
| `src_resource_quotas_limitrange_restored_after_modification` | resource-quotas | restored_after_manual_deletion | Drift correction (covered together) |
| `src_network_policies_tenant_networkpolicy_created_in_namespace` | network-policies | build_egress_restrict, runtime_deny_all | Two policy contexts |
| `src_network_policies_tenant_networkpolicy_restored_after_modification` | network-policies | restored_after_modification | Drift correction |
| `src_network_policies_networkpolicy_ingress_from_same_tenant` | network-policies | runtime_three_policies | Ingress allow policy |
