# Capsule E2E Test Catalog

**Source project:** projectcapsule/capsule
**Reference path:** `/home/paulo/loopzero/referencias/capsule/e2e/`
**Harvested:** 2026-03-29
**Total tests extracted:** 18
**Framework:** Ginkgo v2 + Gomega + envtest (UseExistingCluster=true)
**Language:** Go

---

## Test Framework Notes

- Suite bootstraps with `envtest.Environment{UseExistingCluster: ptr.To(true)}` — tests run against a live cluster, not fake clients.
- All tenant owners are impersonated via `ownerClient(api.UserSpec)` which sets `rest.ImpersonationConfig`.
- `defaultTimeoutInterval = 40s`, `defaultPollInterval = 1s` for all `Eventually` assertions.
- Namespaces are tagged with `env=e2e` and cleaned up in `AfterSuite`.
- `CheckForOwnerRoleBindings` verifies that both `admin` and `capsule-namespace-deleter` RoleBindings exist for each owner.
- `VerifyTenantRoleBindings` verifies all role bindings across all tenant namespaces match the spec.

---

## Domain: tenant-isolation

### TEST 1 — `namespace_created_by_owner_gets_rolebindings`
**File:** `e2e/new_namespace_test.go`
**Test ID:** `src_tenant_isolation_namespace_created_with_rolebindings`
**Type:** e2e

**Behavioral contract:** When a tenant owner (User) creates a namespace, that namespace appears in the tenant namespace list AND RoleBindings for all tenant owners (User, Group, ServiceAccount) are automatically created in the namespace.

**Assertions:**
- `NamespaceCreation(ns, owner.UserSpec).Should(Succeed())`
- `TenantNamespaceList(tnt).Should(ContainElements(ns.GetName()))`
- For each owner: `CheckForOwnerRoleBindings(ns, owner, nil).Should(Succeed())`

**Fixtures:** Tenant with User + Group + ServiceAccount owners

---

### TEST 2 — `group_owner_creates_namespace_with_rolebindings`
**File:** `e2e/new_namespace_test.go`
**Test ID:** `src_tenant_isolation_group_owner_creates_namespace`
**Type:** e2e

**Behavioral contract:** When a Group-type tenant owner creates a namespace, the namespace is registered in the tenant and RoleBindings for all owners (including groups) are present.

**Assertions:**
- `NamespaceCreation(ns, groupOwner.UserSpec).Should(Succeed())`
- `TenantNamespaceList(tnt).Should(ContainElements(ns.GetName()))`
- For each owner: `CheckForOwnerRoleBindings(ns, owner, nil).Should(Succeed())`

**Fixtures:** Tenant with Group owner

---

### TEST 3 — `tenant_owner_cannot_claim_system_namespace_via_ownerreference_patch`
**File:** `e2e/namespace_hijacking_test.go`
**Test ID:** `src_tenant_isolation_cannot_hijack_system_namespace_via_ownerref`
**Type:** e2e

**Behavioral contract:** A tenant owner cannot claim ownership of an off-limits namespace (e.g., kube-system) by patching its ownerReferences to point to their tenant. The patch must be rejected.

**Assertions:**
- `cs.CoreV1().Namespaces().Patch(kube-system, StrategicMergePatch, tenantOwnerRef)` → `Expect(err).To(HaveOccurred())`

**Fixtures:** Tenant with User+ServiceAccount owners, kube-system namespace

---

### TEST 4 — `tenant_owner_cannot_claim_system_namespace_via_label_patch`
**File:** `e2e/namespace_hijacking_test.go`
**Test ID:** `src_tenant_isolation_cannot_hijack_system_namespace_via_label`
**Type:** e2e

**Behavioral contract:** A tenant owner cannot take ownership of kube-system by patching the tenant label onto it. The webhook must block the label patch on system namespaces.

**Assertions:**
- `cs.CoreV1().Namespaces().Patch(kube-system, StrategicMergePatch, tenantLabel)` → `Expect(err).To(HaveOccurred())`

**Fixtures:** Tenant with User+ServiceAccount owners, kube-system namespace

---

### TEST 5 — `tenant_owner_patch_ownerref_to_random_tenant_is_rejected_and_original_preserved`
**File:** `e2e/namespace_hijacking_test.go`
**Test ID:** `src_tenant_isolation_owner_ref_patch_silently_ignored`
**Type:** e2e

**Behavioral contract:** Even if a tenant owner patches a namespace ownerReference to point to a non-existent random tenant, the original tenant ownerReference must be preserved and the fake ownerReference must not be stored.

**Assertions:**
- Patch with random UID+Name ownerRef: `Expect(err).ToNot(HaveOccurred())` (API accepts)
- Verify random ownerRef NOT stored: `Expect(hasSpecificOwnerRef).To(BeFalse())`
- Verify original ownerRef STILL present: `Expect(hasOriginReference).To(BeTrue())`

**Fixtures:** Tenant, NewNamespace per owner

---

### TEST 6 — `user_without_ownership_cannot_create_namespace_in_foreign_tenant`
**File:** `e2e/selecting_non_owned_tenant_test.go`
**Test ID:** `src_tenant_isolation_non_owner_cannot_select_foreign_tenant`
**Type:** e2e

**Behavioral contract:** A user who is not an owner of a tenant cannot create a namespace claiming to belong to that tenant. The creation must be rejected even when the correct tenant label is present.

**Assertions:**
- `cs.CoreV1().Namespaces().Create(ns with tenant label)` → `Expect(err).To(HaveOccurred())`

**Fixtures:** Tenant owned by "undefined", namespace labeled with that tenant, user "dale" (not an owner)

---

### TEST 7 — `tenant_status_tracks_namespace_lifecycle_create_and_delete`
**File:** `e2e/namespace_status_test.go`
**Test ID:** `src_tenant_isolation_namespace_lifecycle_status_tracking`
**Type:** e2e

**Behavioral contract:** The tenant status accurately tracks namespace count (Size) and per-namespace Ready conditions as namespaces are created and deleted. When a namespace is deleted, its instance is removed from tenant status and Size decrements.

**Assertions:**
- After ns1 created: `t.Status.Size == 1`, instance `Ready=True`
- After ns2 created: `t.Status.Size == 2`
- After ns1 deleted: `t.Status.Size == 1`, ns1 instance is `nil`
- After ns2 deleted: `t.Status.Size == 0`, ns2 instance is `nil`

**Fixtures:** Tenant, 2 namespaces created sequentially

---

### TEST 8 — `cordoned_tenant_blocks_mutations_and_uncordoning_restores_access`
**File:** `e2e/tenant_cordoning_test.go`
**Test ID:** `src_tenant_isolation_cordoning_blocks_pod_deletion`
**Type:** e2e

**Behavioral contract:** When a tenant is cordoned, all mutation operations within its namespaces are blocked (pod deletion fails), namespace receives a cordoned label, and tenant condition reflects cordoned state. When uncordoned, operations are restored and the cordoned label is removed.

**Assertions:**
- Pre-cordon: pod deletion succeeds
- `tnt.Spec.Cordoned = true` → `ns.Labels` has `meta.CordonedLabel`
- Tenant condition: `CordonedCondition=True`, reason=Cordoned
- Cordoned: pod deletion fails
- `tnt.Spec.Cordoned = false` → `ns.Labels` does NOT have `meta.CordonedLabel`
- Tenant condition: `CordonedCondition=False`, reason=Active
- Uncordoned: pod deletion succeeds

**Fixtures:** Tenant, namespace, pod, ownerClient

---

### TEST 9 — `user_without_tenant_assignment_cannot_create_namespace`
**File:** `e2e/missing_tenant_test.go`
**Test ID:** `src_tenant_isolation_missing_tenant_namespace_creation_fails`
**Type:** e2e

**Behavioral contract:** A user whose tenant object does not exist in the cluster cannot create namespaces. The admission webhook must reject namespace creation for users with no valid tenant assignment.

**Assertions:**
- Tenant object NOT created in cluster (only declared in memory)
- `cs.CoreV1().Namespaces().Create(ns)` → `Expect(err).ShouldNot(Succeed())`

**Fixtures:** In-memory Tenant spec (not applied), ownerClient impersonating the owner

---

### TEST 10 — `additional_rolebindings_from_tenant_spec_propagated_to_all_namespaces`
**File:** `e2e/additional_role_bindings_test.go`
**Test ID:** `src_tenant_isolation_additional_rolebindings_propagated`
**Type:** e2e

**Behavioral contract:** When a tenant spec declares additional role bindings, those RoleBindings must exist in each namespace owned by that tenant, binding the specified subjects to the specified ClusterRole.

**Assertions:**
- `VerifyTenantRoleBindings(t)` — checks all RoleBindings in all tenant namespaces match spec

**Fixtures:** Tenant with `AdditionalRoleBindings: [{ClusterRoleName: "crds-rolebinding", Subjects: [system:authenticated group]}]`

---

### TEST 11 — `persistentvolume_from_one_tenant_cannot_be_mounted_by_another_tenant`
**File:** `e2e/preventing_pv_cross_tenant_mount_test.go`
**Test ID:** `src_tenant_isolation_cross_tenant_pv_mount_prevented`
**Type:** e2e

**Behavioral contract:** When a PersistentVolume is created by a PVC in tenant A, it is labeled with the tenant name. Any attempt by tenant B to claim that same PV by name in a PVC must fail consistently, preventing cross-tenant storage access.

**Assertions:**
- `pv.Labels["capsule.clastix.io/tenant"]` == `tnt1.Name`
- `Consistently(create PVC in tnt2 namespace with VolumeName=pv.Name).Should(HaveOccurred())`

**Fixtures:** 2 Tenants (tnt1=jessica, tnt2=leto), PVC + Pod in tnt1, PVC with explicit VolumeName in tnt2

---

### TEST 12 — `tenant_status_remains_consistent_through_50_namespace_scale_up_and_down`
**File:** `e2e/scalability_test.go`
**Test ID:** `src_tenant_isolation_scalability_50_namespaces`
**Type:** e2e

**Behavioral contract:** The tenant controller must correctly track namespace count and per-namespace status for up to 50 namespaces under realistic workload (running pods). Status.Size must be exact at each step during both scale-up and scale-down sequences.

**Assertions:**
- After each of 50 namespace creations: `Status.Size == i+1`, instance has `Ready=True`
- After each of 50 namespace deletions: `Status.Size == amount-i-1`, instance is `nil`

**Fixtures:** Tenant, 50 namespaces (scale-0..scale-49), 1 pause-container deployment per namespace

---

## Domain: resource-quotas

### TEST 13 — `namespace_creation_blocked_when_tenant_namespace_quota_exceeded`
**File:** `e2e/overquota_namespace_test.go`
**Test ID:** `src_resource_quotas_namespace_count_quota_enforced`
**Type:** e2e

**Behavioral contract:** When a tenant has a namespace quota of 3, creating a 4th namespace must fail. The first 3 namespaces succeed and are tracked; the 4th is rejected by the admission webhook.

**Assertions:**
- Namespaces 1-3 creation: `.Should(Succeed())`
- Namespace 4 creation: `.ShouldNot(Succeed())`

**Fixtures:** Tenant with `NamespaceOptions.Quota = 3`, owner "bob"

---

### TEST 14 — `pod_quota_reached_blocks_additional_pod_creation`
**File:** `e2e/resource_quota_exceeded_test.go`
**Test ID:** `src_resource_quotas_pod_quota_blocks_new_pods`
**Type:** e2e

**Behavioral contract:** When 10 pods (the quota limit) are already scheduled across the tenant namespaces, creating an additional pod must fail. The ResourceQuota status shows Hard.Pods == Used.Pods before the failed creation attempt.

**Assertions:**
- 2 namespaces × 5-replica deployment = 10 pods → `rq.Status.Hard.Pods() == rq.Status.Used.Pods()` (quota saturated)
- Additional pod creation: `.ShouldNot(Succeed())`

**Fixtures:** Tenant with `ResourceQuota.Items[{pods: 10}]` + `LimitRange` (pod/container/PVC min/max), 2 namespaces

---

### TEST 15 — `custom_resource_quota_blocks_crd_instances_beyond_tenant_limit`
**File:** `e2e/custom_resource_quota_test.go`
**Test ID:** `src_resource_quotas_custom_crd_quota_enforced`
**Type:** e2e

**Behavioral contract:** When a tenant has a custom resource quota for a CRD type (e.g., 3 Foo instances), creating more than 3 instances across all tenant namespaces must be rejected, and the tenant status annotations must reflect the current usage.

**Assertions:**
- 3 Foo instances created (one per namespace): `.ShouldNot(HaveOccurred())`
- Additional Foo in each namespace: `.Should(HaveOccurred())`
- Tenant annotations: `quota.resources.capsule.clastix.io/foos.test.clastix.io_v1 == "3"` and `used.resources.capsule.clastix.io/foos.test.clastix.io_v1 == "3"`

**Fixtures:** Tenant with annotation `quota.resources.capsule.clastix.io/foos.test.clastix.io_v1: "3"`, CRD `foos.test.clastix.io`, dynamic client

---

### TEST 16 — `limitrange_resourcequota_networkpolicy_replicated_to_all_tenant_namespaces`
**File:** `e2e/tenant_resources_test.go`
**Test ID:** `src_resource_quotas_limitrange_and_quota_replicated_to_namespaces`
**Type:** e2e

**Behavioral contract:** When a tenant specifies LimitRanges, NetworkPolicies, and ResourceQuotas in its spec, exactly those resources must exist in each tenant namespace with the exact spec declared, named according to the `capsule-{tenantname}-{index}` naming convention.

**Assertions:**
- For each namespace in [bim, bum, bam]:
  - `LimitRange "capsule-tenant-resources-0"` exists with spec matching tenant
  - `NetworkPolicy "capsule-tenant-resources-0"` exists with spec matching tenant
  - `ResourceQuota "capsule-tenant-resources-0/1/2"` exists with spec matching tenant
  - Namespace annotation `scheduler.alpha.kubernetes.io/node-selector = kubernetes.io/os=linux`

**Fixtures:** Tenant with LimitRanges + NetworkPolicies + ResourceQuota + NodeSelector, 3 namespaces

---

### TEST 17 — `managed_limitrange_restored_when_modified_by_third_party`
**File:** `e2e/tenant_resources_changes_test.go`
**Test ID:** `src_resource_quotas_limitrange_restored_after_modification`
**Type:** e2e

**Behavioral contract:** Tenant owners cannot delete managed LimitRanges. When any actor clears a LimitRange spec, the controller must reconcile it back to the exact spec defined in the tenant. Deletion of managed LimitRanges by tenant owners is also blocked.

**Assertions:**
- Tenant owner delete: `Expect(err).To(HaveOccurred())`
- Admin clears `lr.Spec.Limits = []` → `Eventually(lr.Spec).Should(Equal(originalSpec))`

**Fixtures:** Tenant with LimitRanges, 4 namespaces (fire/walk/with/me)

---

### TEST 18 — `managed_resourcequota_restored_when_modified_by_third_party`
**File:** `e2e/tenant_resources_changes_test.go`
**Test ID:** `src_resource_quotas_resourcequota_restored_after_modification`
**Type:** e2e

**Behavioral contract:** Tenant owners cannot delete managed ResourceQuotas. When any actor clears a ResourceQuota hard limits, the controller must reconcile it back to the exact spec defined in the tenant spec. This prevents tenants from bypassing resource limits.

**Assertions:**
- Tenant owner delete: `Expect(err).To(HaveOccurred())`
- Admin clears `rq.Spec.Hard = {}` → `Eventually(rq.Spec).Should(Equal(originalSpec))`

**Fixtures:** Tenant with ResourceQuota, 4 namespaces

---

## Domain: network-policies

### TEST 19 — `tenant_networkpolicy_spec_replicated_as_k8s_networkpolicy_in_each_namespace`
**File:** `e2e/tenant_resources_test.go`
**Test ID:** `src_network_policies_tenant_networkpolicy_created_in_namespace`
**Type:** e2e

**Behavioral contract:** Each NetworkPolicy declared in the tenant spec is created in every tenant namespace with the exact same spec (ingress/egress rules, pod selectors, policy types). The NetworkPolicy name follows the pattern `capsule-{tenantname}-{index}`.

**Assertions:**
- `k8sClient.Get(NamespacedName{Name: "capsule-tenant-resources-0", Namespace: name}, np).Should(Succeed())`
- `np.Spec.Should(Equal(tenantSpec.NetworkPolicies.Items[0]))`

**Fixtures:** Tenant with NetworkPolicies spec, 3 namespaces

---

### TEST 20 — `tenant_networkpolicy_restored_when_tenant_owner_modifies_or_deletes_it`
**File:** `e2e/tenant_resources_changes_test.go`
**Test ID:** `src_network_policies_tenant_networkpolicy_restored_after_modification`
**Type:** e2e

**Behavioral contract:** Tenant owners cannot delete managed NetworkPolicies. If an admin modifies a managed NetworkPolicy (clears its rules), the controller reconciles it back to the tenant-declared spec. This ensures tenant network isolation cannot be bypassed by direct resource modification.

**Assertions:**
- Tenant owner delete: `Expect(err).To(HaveOccurred())`
- Admin clears `np.Spec.Egress = []` and `np.Spec.Ingress = []`
- `Eventually(np.Spec).Should(Equal(originalSpec))`

**Fixtures:** Tenant with NetworkPolicies, 4 namespaces, ownerClient for deletion attempt

---

### TEST 21 — `networkpolicy_allows_ingress_from_same_tenant_namespaces_and_pods`
**File:** `e2e/tenant_resources_test.go`
**Test ID:** `src_network_policies_networkpolicy_ingress_from_same_tenant`
**Type:** e2e

**Behavioral contract:** The NetworkPolicy created in each tenant namespace allows ingress only from pods in the same tenant (by namespace label selector) and from a private IP range, while allowing egress to all public IPs excluding the private range. This enforces intra-tenant connectivity with external isolation.

**Policy spec:**
- Ingress `from`: `NamespaceSelector{capsule.clastix.io/tenant: tenant-resources}` + `PodSelector{}` + `IPBlock{192.168.0.0/12}`
- Egress `to`: `IPBlock{0.0.0.0/0 except 192.168.0.0/12}`
- `PolicyTypes: [Ingress, Egress]`

**Assertions:**
- NetworkPolicy exists in each namespace with exact spec matching the above

**Fixtures:** Tenant with above NetworkPolicies spec, 3 namespaces

---

## Summary by Domain

| Domain | Tests | Key behaviors verified |
|--------|-------|------------------------|
| `tenant-isolation` | 12 | Namespace → tenant tracking, owner RoleBindings, namespace hijacking prevention, cordoning, cross-tenant PV prevention, scalability |
| `resource-quotas` | 7 | Namespace count quota, pod quota, custom CRD quota, LimitRange/ResourceQuota replication and reconciliation |
| `network-policies` | 3 | NetworkPolicy replication, deletion prevention, reconciliation after modification |

## Key Patterns for Golden Test Conversion

1. **Impersonation pattern:** `ownerClient(api.UserSpec{Name, Kind})` → sets `rest.ImpersonationConfig`; translates directly to any RBAC-aware K8s test client.

2. **Eventually pattern:** All async assertions use `Eventually(func, 40s, 1s)` — translate to controller reconciliation wait loops.

3. **Naming convention:** Capsule names managed resources `capsule-{tenantname}-{index}`. LoopZero may use different naming but the behavioral contract (resources exist with exact spec) is portable.

4. **Reconciliation loop tests** (tests 17, 18, 20): These test the idempotency of the controller — modifying a managed resource triggers reconciliation back to desired state. Critical behavioral contract for LoopZero's operator.

5. **Webhook enforcement tests** (tests 3, 4, 6, 9): These test admission webhooks. LoopZero's operator uses different enforcement mechanisms but the behavioral contracts are identical: certain operations must be blocked.

6. **Status tracking tests** (tests 7, 12): Tenant status must accurately reflect the current namespace count and per-namespace conditions. This maps directly to LoopZero's CRD status subresource requirements.
