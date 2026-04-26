# Runbook: LoopZeroArgoCDSyncFailed

**Severity:** Warning
**Component:** ArgoCD + loopzero-api gitops pusher
**SLO impact:** Deploy success rate

## Alert expression

```
argocd_app_sync_total{phase="Failed"} - argocd_app_sync_total{phase="Failed"} offset 5m > 3
```

Triggered when an ArgoCD Application has 3+ sync failures in 5 min.

## What this means

ArgoCD is failing to apply manifests to the target cluster. Deploys appear successful at API level (build SUCCEEDED) but workloads are not running. This is the "false success" failure mode that `loopzero_false_success_total` tracks.

## Triage (first 5 min)

1. Identify failing app:
   ```
   kubectl -n argocd get applications -o jsonpath='{range .items[?(@.status.sync.status=="OutOfSync")]}{.metadata.name}{"\t"}{.status.sync.status}{"\t"}{.status.health.status}{"\n"}{end}'
   ```
2. Get sync error:
   ```
   kubectl -n argocd describe application <name> | grep -A 10 "Operation State\|Sync Result"
   ```
3. Check repo-server health:
   ```
   kubectl -n argocd logs deploy/argocd-repo-server --tail=200
   ```

## Diagnose

| Sync error | Likely cause |
|-----------|--------------|
| `the server could not find the requested resource` | CRD missing on target cluster |
| `forbidden` | RBAC mismatch (post-T1.1: scoped Role missing) |
| `error reconciling resource` | Resource in invalid state (e.g., NetworkPolicy with bad selector) |
| `webhook denied` | ValidatingAdmissionPolicy rejection (e.g., signed-image policy in T-INFRA-001 future fix) |
| `connection refused` to git | Gitea unreachable from argocd-repo-server |

## Mitigate

**Force resync after fixing root cause:**
```
kubectl -n argocd argocd app sync <name> --force --replace
```

**Re-apply chart for missing CRD:**
```
helm upgrade -n loopzero-system loopzero-crds charts/loopzero-crds
```

**Restart repo-server:**
```
kubectl -n argocd rollout restart deploy/argocd-repo-server
```

## Verify recovery

- ArgoCD app shows Synced + Healthy
- `loopzero_false_success_total` rate returns to 0
- New deploys complete in expected time

## Escalation

> 15 min unresolved: page on-call gitops/delivery engineer.

## Related

- Metric: `argocd_app_sync_total`, `loopzero_false_success_total`
- ArgoCD UI: `argocd.<base>/applications/<name>`
- Source: `api/internal/argocd/`, `api/internal/gitops/`
