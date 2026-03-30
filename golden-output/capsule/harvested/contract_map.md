# Capsule Behavioral Contract Map

Extracted from: `referencias/capsule/`
Date: 2026-03-29
Domain: `capsule`
Contracts extracted: 8

---

## Purpose

This map describes the behavioral contracts extracted from the Capsule multi-tenancy controller. The contracts are implementation-agnostic statements of what Capsule promises to do. They are directly relevant to LoopZero's TenantController, EnvironmentController, and build namespace NetworkPolicy — all of which share the same structural responsibilities.

---

## Contract Index

| ID | Name | Type | Relevant to LoopZero |
|----|------|------|-----------------|
| `contract_capsule_tenant_rolebindings_propagation` | RoleBindings propagated to all child namespaces | `protocol` | TenantController RBAC across build+runtime clusters |
| `contract_capsule_tenant_networkpolicy_propagation` | NetworkPolicies replicated into every child namespace | `protocol` | Build namespace egress restriction, EnvironmentController isolation |
| `contract_capsule_tenant_resourcequota_enforcement` | Tenant-scoped ResourceQuota aggregates cross-namespace usage | `invariant` | TenantController quota enforcement |
| `contract_capsule_namespace_metadata_propagation` | Tenant spec labels/annotations propagated and cleaned up | `protocol` | TenantController and EnvironmentController namespace labelling |
| `contract_capsule_tenant_cordoning` | Cordoning blocks all write operations in tenant namespaces | `invariant` | Tenant suspension flow |
| `contract_capsule_cross_tenant_namespace_prevention` | User cannot assign namespace to a tenant they do not own | `invariant` | Tenant isolation at admission |
| `contract_capsule_pv_cross_tenant_mount_prevention` | PV bound to Tenant A cannot be claimed by Tenant B | `invariant` | Storage isolation between tenants |
| `contract_capsule_reconcile_status_conditions` | Status conditions updated on every reconcile cycle | `invariant` | All LoopZero controllers — operator health visibility |

---

## Contract Details

### 1. RoleBindings Propagated to All Child Namespaces

**ID:** `contract_capsule_tenant_rolebindings_propagation`
**Type:** `protocol`

**Preconditions:**
- A Tenant object exists with at least one entry in `AdditionalRoleBindings`
- One or more namespaces are listed in `tenant.Status.Namespaces`

**Postconditions:**
- A RoleBinding exists in every tenant namespace for each `AdditionalRoleBindings` entry
- Each RoleBinding references the ClusterRole declared in the spec entry
- Each RoleBinding binds the subjects declared in the spec entry
- Each RoleBinding carries the label `capsule.clastix.io/tenant=<tenant-name>`
- Each RoleBinding is owned by the Tenant (ownerReference set)
- RoleBindings removed from the spec are deleted from all namespaces (pruning)

**Invariants:**
- Sync is idempotent — re-running with unchanged spec produces no diff
- Only Capsule-managed RoleBindings (those with the managed label) are subject to pruning

**Source evidence:**
- `referencias/capsule/internal/controllers/tenant/rolebindings.go`
- `referencias/capsule/e2e/additional_role_bindings_test.go`

---

### 2. NetworkPolicies Replicated Into Every Child Namespace

**ID:** `contract_capsule_tenant_networkpolicy_propagation`
**Type:** `protocol`

**Preconditions:**
- A Tenant object exists with at least one entry in `spec.networkPolicies.items`
- One or more namespaces are listed in `tenant.Status.Namespaces`

**Postconditions:**
- A NetworkPolicy named `capsule-<tenant>-<i>` exists in every tenant namespace for each spec entry `i`
- The NetworkPolicy spec exactly matches the Tenant spec entry (ingress, egress, podSelector, policyTypes)
- Each NetworkPolicy carries the label `capsule.clastix.io/tenant=<tenant-name>`
- Each NetworkPolicy is owned by the Tenant (ownerReference set)
- NetworkPolicies from removed spec entries are deleted from all namespaces

**Invariants:**
- Reconciliation is idempotent — unchanged spec causes no mutations
- Only Capsule-managed NetworkPolicies are pruned; user-created policies are not touched

**Source evidence:**
- `referencias/capsule/internal/controllers/tenant/networkpolicies.go`
- `referencias/capsule/e2e/tenant_resources_test.go`

---

### 3. Tenant-Scoped ResourceQuota Aggregates Cross-Namespace Usage

**ID:** `contract_capsule_tenant_resourcequota_enforcement`
**Type:** `invariant`

**Preconditions:**
- A Tenant exists with at least one ResourceQuota entry in `spec.resourceQuota.items`
- One or more namespaces are listed in `tenant.Status.Namespaces`

**Postconditions:**
- A ResourceQuota named `capsule-<tenant>-<i>` exists in every tenant namespace for each spec entry `i`
- In Tenant-scoped mode: when `sum(used) >= hard`, each namespace quota `hard` is set to its own current `used` value (new resource creation blocked)
- In Tenant-scoped mode: when `sum(used) < hard`, each namespace quota `hard = tenant hard - total used + that namespace used`
- In Namespace-scoped mode: each namespace quota `hard` equals the spec hard value unchanged
- Attempting to create resources beyond quota is rejected by Kubernetes admission

**Invariants:**
- In tenant-scoped mode, the sum of all namespace hard quotas never exceeds the tenant-level hard quota
- ResourceQuota objects are owned by the Tenant and carry the Capsule managed label

**Source evidence:**
- `referencias/capsule/internal/controllers/tenant/resourcequotas.go`
- `referencias/capsule/e2e/resource_quota_exceeded_test.go`

---

### 4. Namespace Labels and Annotations Propagated and Cleaned Up From Tenant Spec

**ID:** `contract_capsule_namespace_metadata_propagation`
**Type:** `protocol`

**Preconditions:**
- A Tenant exists with `NamespaceOptions.AdditionalMetadata` or `AdditionalMetadataList` configured
- The target namespace is owned by the Tenant

**Postconditions:**
- All declared labels are present on the namespace
- All declared annotations are present on the namespace
- Labels with namespace selectors that do not match are NOT applied
- Template variables (`{{ tenant.name }}`, `{{ namespace }}`) are resolved in values
- `capsule.clastix.io/tenant` is always set to the tenant name (cannot be overridden)
- `kubernetes.io/metadata.name` is always set to the namespace name (cannot be overridden)
- Metadata removed from the Tenant spec is removed from the namespace on the next reconciliation
- Tenant status records which labels/annotations are managed per namespace

**Invariants:**
- When `ManagedMetadataOnly=false`: user-defined namespace metadata is preserved alongside managed metadata
- When `ManagedMetadataOnly=true`: only managed metadata exists on the namespace after reconciliation
- A reconciliation error from invalid metadata sets `Ready=False` without modifying existing namespace metadata

**Source evidence:**
- `referencias/capsule/internal/controllers/tenant/namespaces.go`
- `referencias/capsule/e2e/namespace_additional_metadata_test.go`

---

### 5. Cordoning a Tenant Blocks All Write Operations in Its Namespaces

**ID:** `contract_capsule_tenant_cordoning`
**Type:** `invariant`

**Preconditions:**
- A Tenant exists with at least one owned namespace
- A resource (e.g. Pod) exists within a tenant namespace

**Postconditions (Cordoned=true):**
- All owned namespaces have the cordoned label set
- Tenant status `CordonedCondition=True` with reason `Cordoned`
- Delete/create/update operations by tenant owners are rejected

**Postconditions (Cordoned=false):**
- The cordoned label is absent from all owned namespaces
- Tenant status `CordonedCondition=False` with reason `Active`
- Write operations by tenant owners succeed normally

**Invariants:**
- Cordoning does not delete any existing resources — it only blocks future mutations
- The controller re-labels namespaces synchronously on cordoned state change

**Source evidence:**
- `referencias/capsule/e2e/tenant_cordoning_test.go`
- `referencias/capsule/internal/controllers/tenant/manager.go`

---

### 6. User Cannot Assign a Namespace to a Tenant They Do Not Own

**ID:** `contract_capsule_cross_tenant_namespace_prevention`
**Type:** `invariant`

**Preconditions:**
- A Tenant X exists
- A user who is an owner of a different Tenant Y (or no Tenant) attempts to create a namespace
- The namespace creation request includes the label `capsule.clastix.io/tenant=X`

**Postconditions:**
- The namespace creation is rejected
- Tenant X namespace list is unchanged

**Invariants:**
- Only users listed as owners of Tenant X can create namespaces that belong to Tenant X

**Source evidence:**
- `referencias/capsule/e2e/selecting_non_owned_tenant_test.go`

---

### 7. PersistentVolume Bound to One Tenant Cannot Be Claimed by Another

**ID:** `contract_capsule_pv_cross_tenant_mount_prevention`
**Type:** `invariant`

**Preconditions:**
- Tenant A has a namespace with a PVC bound to PV X
- The controller has labelled PV X with `capsule.clastix.io/tenant=<tenant-A>`
- Tenant B has a separate namespace
- A user in Tenant B attempts to create a PVC explicitly naming PV X

**Postconditions:**
- The PVC creation in the Tenant B namespace is rejected
- PV X remains exclusively available to Tenant A

**Invariants:**
- PersistentVolumes are labelled with the tenant that first bound them
- Once labelled, a PV cannot be claimed by a namespace outside that tenant

**Source evidence:**
- `referencias/capsule/e2e/preventing_pv_cross_tenant_mount_test.go`
- `referencias/capsule/internal/controllers/pv/controller.go`

---

### 8. Status Conditions Updated on Every Reconcile Cycle

**ID:** `contract_capsule_reconcile_status_conditions`
**Type:** `invariant`

**Preconditions:**
- A Tenant object has been enqueued for reconciliation

**Postconditions:**
- `Tenant.Status.Conditions[Ready]=True` when all sub-reconciliations succeed with reason `Succeeded`
- `Tenant.Status.Conditions[Ready]=False` with error message when any sub-reconciliation fails with reason `Failed`
- Each namespace entry in `tenant.Status.Spaces` carries a per-namespace Ready condition
- `Tenant.Status.State=Active` when `spec.cordoned=false`
- `Tenant.Status.State=Cordoned` when `spec.cordoned=true`
- Status update is attempted even when the reconciliation returns a non-nil error (deferred write)

**Invariants:**
- Status is written using optimistic concurrency (retry on conflict) — no lost updates
- A failed status write surfaces as an error to controller runtime and triggers backoff retry

**Source evidence:**
- `referencias/capsule/internal/controllers/tenant/manager.go`
- `referencias/capsule/internal/controllers/tenant/namespaces.go`
- `referencias/capsule/e2e/namespace_additional_metadata_test.go`

---

## Relevance to LoopZero Controllers

### TenantController (`operators/internal/controller/tenant/`)

| Contract | What LoopZero must implement |
|----------|--------------------------|
| RoleBindings propagation | Create RoleBindings for tenant owner in all build + runtime namespaces; prune on spec change |
| NetworkPolicy propagation | Apply build-namespace egress restriction policies; prune removed policies |
| ResourceQuota enforcement | Enforce per-tenant quota across all project namespaces |
| Namespace metadata propagation | Apply tenant labels to all owned namespaces; clean up on removal |
| Status conditions | Write Ready condition after every reconcile pass, even on error |

### EnvironmentController (`operators/internal/controller/environment/`)

| Contract | What LoopZero must implement |
|----------|--------------------------|
| NetworkPolicy propagation | Isolate environment namespaces from each other using NetworkPolicies |
| Namespace metadata propagation | Apply environment labels (e.g. `loopzero/tenant`, `loopzero/environment`) to the namespace |
| Status conditions | Reflect reconciliation outcome on the Environment CR status |

### Build Namespace NetworkPolicy

| Contract | What LoopZero must implement |
|----------|--------------------------|
| NetworkPolicy propagation | Build namespaces must carry a NetworkPolicy restricting egress (no lateral movement to other tenants' namespaces or internal cluster services) |
| Cross-tenant isolation | Build pods must not reach namespaces belonging to other tenants |

