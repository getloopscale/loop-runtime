# Runbook: LoopZeroBuildCircuitBreakerOpen

**Severity:** Warning
**Component:** loopzero-api + loopzero-tenant-controller (CB wrappers)
**SLO impact:** Build pipeline SLO

## Alert expression

```
loopzero_tenant_cb_state{name=~"gitea|argocd|minio|harbor"} == 2
```

State `2` = Open. Triggered when any external provisioner CB has opened (5 consecutive failures).

## What this means

The CB has stopped retrying calls to a downstream system to prevent retry storms. Provisioning operations targeting that system will fail-fast (return 503) until CB closes.

## Triage (first 5 min)

1. Identify circuit: `kubectl -n loopzero-system port-forward svc/loopzero-api 9090; curl localhost:9090/metrics | grep loopzero_tenant_cb_state`
2. Identify external system from circuit name (gitea, argocd, minio, harbor, **minio post-T1.4**).
3. Verify external system health directly:
   - Gitea: `curl -sS https://git.<base>/api/v1/version`
   - ArgoCD: `kubectl -n argocd get pods`
   - MinIO: `kubectl -n loopzero-system port-forward svc/minio 9000; curl -sS localhost:9000/minio/health/live`
   - Harbor: `curl -sS https://harbor.<base>/api/v2.0/health`

## Diagnose

| External | Common failure modes |
|----------|---------------------|
| Gitea | DB pool saturation, disk full on git data PVC |
| ArgoCD | Repo server crashloop, redis unreachable |
| MinIO | Disk full, replication lag |
| Harbor | DB connection issue, garbage collection running |

## Mitigate

**The CB will auto-close after 120s of recovery.** Do NOT manually flip the CB.

| External | Action |
|----------|--------|
| Gitea | Restart gitea pod if DB stuck; expand PVC if disk full |
| ArgoCD | `kubectl -n argocd rollout restart statefulset argocd-application-controller` |
| MinIO | Check `kubectl -n loopzero-system describe pvc minio-data`; drain if disk full |
| Harbor | Wait for GC; check Harbor logs for upstream errors |

## Verify recovery

- `loopzero_tenant_cb_state` returns to 0 (closed) within 5-10 min
- Failed builds during open window can be retried via `loopzero deploy`

## Escalation

If CB does not close after 30 min of healthy upstream: file bug — CB may be stuck open due to clock issue or counter overflow.

## Related

- Metric: `loopzero_tenant_cb_state` (0=closed, 1=half-open, 2=open)
- Source: `pkg/circuitbreaker/`, `operators/internal/{gitea,argocd,minio,harbor}/circuitbreaker_provisioner.go`
- ADR: T1.4 (ARCH-001 wires MinIO CB)
