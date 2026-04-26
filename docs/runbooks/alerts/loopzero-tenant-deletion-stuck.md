# Runbook: LoopZeroTenantDeletionStuck

**Severity:** Warning
**Component:** loopzero-tenant-controller (deletion lifecycle)
**SLO impact:** Tenant lifecycle, billing accuracy

## Alert expression

```
time() - loopzero_tenant_deletion_started_timestamp{tenant=~".+"} > 600
  and loopzero_tenant_phase{tenant=~".+", phase="Deleting"} == 1
```

Triggered when a tenant is in Deleting phase for more than 10 min.

## What this means

The tenant deletion finalizer is stuck. Resources may be orphaned; tenant will not actually be removed; billing/quota counters may continue.

## Triage (first 5 min)

1. Identify stuck tenant:
   ```
   kubectl get tenants -A -o jsonpath='{range .items[?(@.status.phase=="Deleting")]}{.metadata.name}{"\t"}{.status.phase}{"\n"}{end}'
   ```
2. Look at finalizers and conditions:
   ```
   kubectl describe tenant <name> | grep -A 30 "Conditions:\|Finalizers:"
   ```
3. Get controller log for this tenant:
   ```
   kubectl -n loopzero-system logs deploy/loopzero-tenant-controller --tail=500 | grep <tenant>
   ```

## Diagnose

| Phase | Symptom | Likely cause |
|-------|---------|--------------|
| Phase 0 (DeleteApplications) | NotFound errors | Already cleaned — this is the idempotency fix in MEET-052 (should not block) |
| Phase 1 (DeleteEnvironments) | Soft hang | CODE-006/VAL-005: Phase 1 swallows errors → infinite requeue. Fix by T2.4 |
| Phase 2 (Cleanup external) | CB open | Gitea/ArgoCD/MinIO CB is open → wait for recovery |
| Phase 3 (FinalizeNamespaces) | Namespace stuck Terminating | Resource has finalizer; check `kubectl get all,resourcequotas,networkpolicies -n <ns>` |
| Phase 4 (RemoveFinalizer) | Status update conflict | Eventually resolves; usually transient |

## Mitigate

**Soft hang (most common per VAL-005):**
- Identify the failing operation in logs.
- If transient (CB-related): wait 5-10 min for retry.
- If persistent: file bug for T2.4 fix; manual remediation below.

**Stuck namespace (Phase 3):**
```
# List blocking finalizers
kubectl get ns <loopzero-ns> -o jsonpath='{.spec.finalizers}'

# If a CR finalizer is stuck (rare, only after platform bug)
kubectl patch <resource> <name> -n <ns> --type=merge -p '{"metadata":{"finalizers":[]}}'
```

**Last resort (manual finalizer removal — only with on-call approval):**
```
kubectl patch tenant <name> --type=merge -p '{"metadata":{"finalizers":[]}}'
```
> WARNING: bypasses cleanup; resources may be orphaned. Document why in incident.

## Verify recovery

- Tenant CR removed
- Tenant namespaces gone (`kubectl get ns | grep loopzero-<tenant>`)
- ArgoCD Application removed
- Gitea repo archived (per gitops setting)

## Escalation

After 30 min: page on-call platform engineer. Capture full state for forensics.

## Related

- Metric: `loopzero_tenant_phase`, `loopzero_tenant_deletion_started_timestamp`
- Source: `operators/internal/controller/tenant_deletion.go`, `tenant_deletion_backoff_test.go`
- Findings: VAL-005 / CODE-006 (T2.4 fix in flight)
- Plan ref: `docs/plans/operators-deep-review-remediation-plan.md` T2.4
