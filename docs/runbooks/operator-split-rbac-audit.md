# Operator Split RBAC Audit

> Plan reference: `docs/plans/infra-normalize-multicell-plan.md` Phase 2.
> Source decision: `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` (conditions C10 + C20).
> Last updated: 2026-04-30.

This runbook documents every permission the 4 split operator ClusterRoles hold that the monolith ClusterRole does NOT hold (after wildcard expansion). Each entry has a justification block. Pairs 1:1 with `infra/scripts/rbac-subset-allowlist.txt` — adding to the allowlist without adding here is a process violation.

The CI gate `rbac-subset-check` (in `.github/workflows/ci.yml`) reads the allowlist; this runbook is the human-facing rationale.

---

## Methodology

The CI checker (`infra/scripts/rbac-subset-check.sh`) runs `helm template` on the monolith chart `infra/helm/tenant-controller` and the 4 split charts (`charts/loopzero-{tenant,app,preview,platform}-operator`). For each, it expands every `(apiGroups, resources, verbs)` rule into a set of triples and computes:

```
widenings = (split_union − monolith_with_wildcards) − allowlist
```

If `widenings` is non-empty, the gate fails with a list of unjustified additions. To unblock: add to allowlist + add a justification block here.

---

## Tenant-operator widenings

The MEET decision flagged 5 widenings during the meeting. These were known at the time of decision and represent permissions the tenant-operator legitimately needs that the monolith expresses via implicit wildcards or that the split makes explicit.

### scheduling.k8s.io/priorityclasses (get/list/watch/create/update)

**Why.** Tenant workload priority classes (Starter/Pro/Team/Enterprise per CLAUDE.md plan caps) are cluster-scoped resources created by the tenant-operator on tenant onboarding. The monolith implicitly had cluster-admin via the wildcard `loopzero.io/*` rule — the split makes the priority-class permission explicit + bounded.

**Verbs covered.** `get` (status check), `list/watch` (cache sync), `create` (initial provision), `update` (plan upgrade). Notably **NOT delete** — priority classes survive tenant lifecycle.

**Risk.** Low. Tenant-operator already creates non-tenant-scoped resources (namespaces, ClusterRoles) in this same RBAC.

**Audit owner.** `tenant-operator` reconcile path; see `operators/internal/tenant/tenant_priority.go`.

---

### core/configmaps + core/services (get/list/create/update/patch)

**Why.** RBAC escalation prevention helper resources. When tenant-operator creates a tenant ServiceAccount + Role + RoleBinding, K8s admission rejects bindings whose subject (the SA) doesn't already exist in a way the controller's identity can vouch for. Reading existing ConfigMaps + Services is part of the cache-sync that satisfies this admission check.

**Verbs covered.** Read-side (get/list) + create/update/patch. NOT delete (orphan-protection — tenant-operator does not own configmaps/services in non-tenant namespaces).

**Risk.** Low. The verbs are scoped via tenant-operator's reconcile loop to tenant namespaces only (operator-side enforcement, NOT RBAC-side); a future security audit could narrow this with a Role+RoleBinding per-tenant-namespace pattern.

**Audit owner.** `tenant-operator`; tracked as future hardening (no committed timeline).

---

### apps/deployments (get/list/create/update/patch)

**Why.** Same root cause as configmaps/services: RBAC escalation prevention. Tenant-operator creates Deployments in tenant namespaces only (per environment_rbac.go reconcile), but K8s admission requires the controller identity to be able to read Deployments cluster-wide for the cache.

**Verbs covered.** Read + create/update/patch. NOT delete (cleanup is via owner-ref garbage collection, not direct delete).

**Risk.** Medium. A bug in the operator reconcile loop could create a Deployment in a non-tenant namespace. Mitigated by:
1. tenant-operator's NamespaceSelector cache (cache scoping per ADR-0018).
2. ValidatingAdmissionPolicies for `loopzero.io/managed-by` label enforcement.

**Audit owner.** `tenant-operator`; ADR-0018 cross-reference for cache scoping. Condition C16 of MEET-INFRA-NORMALIZE-T3-operator-split mandates envtest verification (T-07).

---

### core/pods + core/pods/log (get/list/watch/patch)

**Why.** argo-workflow-executor Role propagation. Each tenant gets a per-namespace Role granting workflow-executor read on its own pods + pod logs (so the build pipeline can stream logs back to the user). The tenant-operator creates this Role; to verify the Role binding works, it lists pods cluster-wide on reconcile.

**Verbs covered.** Read-only verbs (get/list/watch) + `patch` (status updates on workflow pods after build completion).

**Risk.** Low to medium. Read-only verbs across all namespaces is broad but matches the monolith's effective permissions (monolith has `loopzero.io/*` wildcard which transitively grants pod read via owner-ref expansion; the split makes it explicit).

**Audit owner.** `tenant-operator`; argo-workflow-executor Role definition in `operators/internal/tenant/argo_workflow_executor_role.go`.

---

### argoproj.io workflows + workflowtemplates + clusterworkflowtemplates + workflowtaskresults + workflowtasksets

**Why.** Per-tenant loopzero-workflow-submitter Role provisioning. Tenants submit builds via the API → tenant-operator creates Workflow CRs in the tenant namespace (build pipeline). The cluster workflow templates are read for inheritance.

**Verbs covered.** Full CRUD (workflowtaskresults + workflowtasksets are created by the workflow itself; the operator needs read access to track build progress).

**Risk.** Medium. Argo's CRDs accept any namespace; if tenant-operator misroutes a workflow it could land in a wrong namespace. Mitigated by:
1. tenant-operator's reconcile loop pins `metadata.namespace` to tenant namespace.
2. NetworkPolicy allow-list per tenant.
3. ResourceQuota per tenant namespace caps blast.

**Audit owner.** `tenant-operator`; build pipeline ownership.

---

## App-operator widenings

The MEET decision did NOT flag app-operator widenings — they were discovered by the CI checker on first run (commit landing this runbook).

### networking.k8s.io/ingresses (get/list/watch)

**Why.** App-operator reconciles `Application` CRs which produce IngressRoutes (Traefik) for tenant traffic. To populate `Application.status.url` and detect when Ingress reaches Ready, the operator must read Ingress objects cluster-wide.

**Verbs covered.** Read-only (`get/list/watch`). The operator does NOT create Ingresses directly; ArgoCD applies them from Git.

**Risk.** Very low. Read-only across all namespaces is harmless.

**Audit owner.** `app-operator`; reconcile path in `operators/internal/app/exposure_controller.go`.

---

## What's NOT in the allowlist (and why)

Several monolith permissions are deliberately omitted from the splits per least-privilege:

- **loopzero-app-operator** does NOT have priorityclasses access (only tenant-op manages these).
- **loopzero-preview-operator** does NOT have argo workflow access (preview env reconcile uses ResourceQuota + NetworkPolicy, no build pipeline).
- **loopzero-platform-operator** does NOT have tenant CRD access (it manages ClusterCredential + GitRepoBinding + PlatformBootstrap, not tenants).

These omissions are validated by reverse subset:

```
expected_omissions = monolith − split_union
```

The current `expected_omissions` count is `107 − 60_in_allowlist = ~47` rules covering CRD-specific tenant permissions that legitimately don't fan out to app/preview/platform operators. This matches the operator domain decomposition.

---

## How to add a new widening

When a future PR introduces a new permission to a split operator that isn't in the monolith:

1. **First, ask: should the monolith get this too?** If the monolith was missing the permission and the split discovered the gap, the answer is usually yes — the split should NOT widen vs the monolith but the monolith should be updated. This avoids drift between the two RBAC surfaces while migration is in flight.
2. **If the split legitimately needs MORE than the monolith** (rare), document here:
   - Permission triple `(apiGroup, resource, verb)`.
   - Which split chart adds it.
   - Why (reference the controller code path).
   - Risk assessment (low/medium/high).
   - Mitigation (NetworkPolicy, ResourceQuota, ValidatingAdmissionPolicy, etc.).
   - Audit owner (controller/team responsible).
3. **Add the triple to** `infra/scripts/rbac-subset-allowlist.txt`.
4. **Verify** `bash infra/scripts/rbac-subset-check.sh --verbose` returns PASS.
5. **Get review** from `platform-security-engineer` per pre-release governance.

---

## Cross-references

- `infra/scripts/rbac-subset-check.sh` — the gate
- `infra/scripts/rbac-subset-allowlist.txt` — machine-readable allowlist
- `.github/workflows/ci.yml` — `rbac-subset` job that runs the gate on every PR
- `docs/decisions/MEET-INFRA-NORMALIZE-T3-operator-split.md` — conditions C10 + C20
- `docs/decisions/ADR-0021-infra-normalize-decisions.md` — D1 (forward-only operator topology)
- `docs/decisions/ADR-0018-tenant-namespace-sot.md` — cache scoping (referenced by C16)
- `infra/helm/tenant-controller/templates/rbac.yaml` — monolith ClusterRole (baseline)
- `charts/loopzero-{tenant,app,preview,platform}-operator/templates/clusterrole.yaml` — split ClusterRoles (under audit)
