---
type: runbook
title: "Delete guardrail — protected platform resources"
---

# Runbook — delete guardrail on protected platform resources

**Alerts:** `PlatformDeleteGuardrailTriggered` (warning) · `PlatformDeleteGuardrailDenied` (critical)
**Policy:** `loopzero-protect-platform-deletes` (mgmt cluster)
**Plan:** `.claude/knowledge-base/plans/d4-vap-delete-guardrail-plan.md`
**ADR:** `docs/decisions/2026-08-07-capi-byoc-cluster-lifecycle-adr.md` (D4)

## What this guardrail does

Refuses `DELETE` of the mgmt boundary resources — `Application`, `AppProject`, `Tenant`,
`Environment`, `ClusterCredential` — **and of the CRDs that define them** — when the caller
is not one of the platform identities.

Deleting any of these triggers the ArgoCD cascade finalizer, which deletes the resources
they manage. Deleting the *CRD* is worse: Kubernetes garbage-collects every instance at
once, and no per-instance rule is ever consulted. That is the exact sequence of the incident
this policy exists to prevent.

## Which identities are exempt, and why

| Identity | Why |
|---|---|
| `system:serviceaccount:loopzero-system:loopzero-tenant-operator` | Deletes Applications/AppProjects and Environment CRs during teardown |
| `system:serviceaccount:loopzero-system:loopzero-api` | Evicts tenants |
| `loopzero-app-operator`, `loopzero-platform-operator`, `loopzero-preview-operator` | Listed so a future teardown path does not deadlock |
| `system:serviceaccount:kube-system:namespace-controller` | The namespace GC. **Removing this exemption deadlocks namespace deletion (B33).** |
| `system:kube-controller-manager` | Cluster-internal GC |
| `system:masters` group | Operator break-glass |

## `PlatformDeleteGuardrailTriggered` (warning)

Fires on any evaluation, in either Warn or Deny mode.

1. Identify who was caught:
   ```bash
   kubectl --context <mgmt> get --raw /metrics \
     | grep 'apiserver_validating_admission_policy_check_total{.*loopzero-protect-platform-deletes'
   ```
   The metric gives counts, not identities. For the identity, read the apiserver audit log
   or the warning returned to the caller.

2. **Is the caught identity a legitimate platform component?**
   - **Yes** → the exemption list is incomplete. This is a defect in the policy, not in the
     caller. Add the identity to `matchConditions.skip-platform-identities` in
     `infra/helm/admission-policies/templates/vap-protect-platform-deletes.yaml` **and** in
     the published chart copy. **Do not flip to Deny until this is fixed.**
   - **No** → the guardrail did its job. Confirm with whoever ran the delete whether it was
     intentional.

## `PlatformDeleteGuardrailDenied` (critical)

A delete was actually refused. Same triage as above, with one addition: if a legitimate
teardown is blocked **right now**, you have two options.

**Preferred — break-glass for one operation.** `system:masters` is exempt, so an operator
with cluster-admin can perform the delete directly. This leaves the guardrail in place.

**Last resort — disable the policy.** Only if the guardrail is broadly blocking legitimate
work:

```bash
# revert to Warn (keeps observability, stops blocking)
helm upgrade admission-policies-mgmt infra/helm/admission-policies \
  -f infra/helm/admission-policies/values.yaml \
  -f infra/helm/admission-policies/values-mgmt.yaml \
  --set vaps.protectPlatformDeletesAction=Warn

# or disable entirely
  --set vaps.protectPlatformDeletes=false
```

No image rebuild is needed — this is a values change by design (plan ADR D5), so the
rollback path is always available under incident pressure.

## The failure mode to watch for

A `ValidatingAdmissionPolicy` with `failurePolicy: Fail` on `DELETE` that does not exempt
the cluster's own garbage collector **deadlocks namespaces in `Terminating` forever**. Five
namespaces were lost to this (B33). Symptom:

```
ContentDeletionFailed: ... is forbidden: ValidatingAdmissionPolicy ...
```

If you see it, disable the policy immediately (command above) and treat the missing
exemption as the bug.

## Verifying the guardrail actually works

```bash
# 1. a non-exempt identity must be refused
kubectl --as=system:serviceaccount:default:probe delete crd <a-loopzero-crd>
#    expect: Forbidden ... ValidatingAdmissionPolicy 'loopzero-protect-platform-deletes' denied

# 2. a non-LoopZero CRD must NOT be guarded
kubectl --as=system:serviceaccount:default:probe delete crd <some-other-crd>
#    expect: deleted
```

The probe identity needs RBAC to delete CRDs — otherwise RBAC refuses first and you have
tested nothing. A `Forbidden` reading `cannot delete resource ... at the cluster scope` is
RBAC; a `Forbidden` naming the policy is the guardrail.
