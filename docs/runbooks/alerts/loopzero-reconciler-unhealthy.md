# Runbook: LoopZeroReconcilerUnhealthy

**Severity:** Warning
**Component:** loopzero-tenant-controller (operators)
**SLO impact:** Tenant provisioning, environment lifecycle

## Alert expression

```
increase(loopzero_operator_reconcile_errors_total[5m]) > 5
```

Triggered when the operator reconcile loop produces 5+ errors in 5 minutes.

> **Note (OBS-001 fix):** prior to T2.6 this alert referenced `loopzero_reconciler_errors_total` which is never emitted. Confirm Prometheus rule uses the corrected name `loopzero_operator_reconcile_errors_total`.

## What this means

The operator is failing to reconcile one or more CRs at elevated rate. Tenant provisioning may be stuck; Environment + ResourceQuota + NetworkPolicy creation may be inconsistent.

## Triage (first 5 min)

1. Identify failing controller: `kubectl -n loopzero-system logs -l app=loopzero-tenant-controller --tail=200 | grep -i error`
2. Group errors by reason:
   ```
   kubectl -n loopzero-system logs deploy/loopzero-tenant-controller --tail=500 | grep "error during reconcile" | jq -r '.error' | sort | uniq -c | sort -rn
   ```
3. Check controller liveness: `kubectl -n loopzero-system get pods -l app=loopzero-tenant-controller`

## Diagnose

| Pattern | Likely cause |
|---------|--------------|
| `connection refused` to gitea/argocd/minio/harbor | External system down → check provisioner CB state metric `loopzero_tenant_cb_state` |
| `forbidden: secrets is forbidden` | RBAC misconfiguration (post T1.1 namespace-scoped Role missing) |
| `failed to update status: the object has been modified` | Status conflict — usually transient, resolves on retry |
| `webhook unavailable` | API server webhook config drift; check `kubectl get validatingwebhookconfigurations` |

## Mitigate

| Cause | Action |
|-------|--------|
| External provisioner down | Wait for CB to close; verify external system; do NOT manually retry |
| RBAC missing | Re-apply chart: `helm upgrade -n loopzero-system loopzero-tenant-controller charts/loopzero-tenant-controller` |
| Status conflict storm | Check controller workqueue depth; consider scaling MaxConcurrentReconciles down |
| Webhook drift | `kubectl rollout restart deploy loopzero-tenant-controller -n loopzero-system` |

## Verify recovery

- `loopzero_operator_reconcile_errors_total` rate < 1/min sustained
- Tenant CRs that were stuck transition to Active phase
- No new errors of the same type in logs

## Escalation

> 30 min unresolved or repeated firing within 1 hour: page on-call operator engineer.

## Related

- Metric: `loopzero_operator_reconcile_errors_total`
- Source: `operators/internal/controller/`
- ADR: T2.6 (OBS-001 fix)
