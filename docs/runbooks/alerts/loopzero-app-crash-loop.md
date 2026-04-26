# Runbook: LoopZeroAppCrashLoop

**Severity:** Warning
**Component:** Tenant workload
**SLO impact:** Tenant app availability

## Alert expression

```
increase(kube_pod_container_status_restarts_total{namespace=~"loopzero-.*-(production|staging)"}[10m]) > 3
```

Triggered when a tenant pod restarts 3+ times in 10 min.

## What this means

A user app is failing on startup or during runtime. ArgoCD will mark the Application Degraded; subsequent deploys may be blocked. Customer-impacting if production.

## Triage (first 5 min)

1. Identify pod: `kubectl get pods -A -l loopzero.io/managed-by=loopzero --field-selector=status.phase!=Running | head`
2. Get last termination reason:
   ```
   kubectl -n <ns> describe pod <pod> | grep -A5 "Last State"
   ```
3. Capture logs from previous container:
   ```
   kubectl -n <ns> logs <pod> --previous --tail=200
   ```

## Diagnose

| Reason | Likely cause |
|--------|--------------|
| `OOMKilled` | Memory limit too tight → adjust `loopzero.yaml` resources or scale plan |
| `Error` (exit 1) | App startup error → check logs for stack trace |
| `CrashLoopBackOff` | Startup loop → check liveness probe |
| `ImagePullBackOff` | Image not found / unauthorized → check cosign signing + admission policy |

## Mitigate

**Quick rollback** (if recent deploy):
```
loopzero rollback --project <name> --env production
```

**Increase memory** (in `loopzero.yaml`, then redeploy):
```yaml
apps:
  api:
    resources:
      memory: 1Gi
```

**Disable liveness probe temporarily** (only if probe is wrong, not the app):
```yaml
livenessProbe: null
```

## Verify recovery

- Restart count stable for 15 min
- ArgoCD Application status = Healthy
- App responds 200 on `/health`

## Escalation

If crash continues after rollback: customer-facing impact → notify customer success; capture logs for engineering bug.

## Related

- Metric: `kube_pod_container_status_restarts_total`
- CLI: `loopzero logs`, `loopzero rollback`, `loopzero status`
