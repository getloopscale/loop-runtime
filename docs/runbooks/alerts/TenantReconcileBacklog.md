# Runbook: TenantReconcileBacklog

**Severity:** warning  
**Component:** operators  
**Task:** T-07

## O que dispara

```promql
workqueue_depth{
  name=~"tenant|environment|application|exposure"
} > 50
```

Workqueue de um dos 4 controllers core acima de 50 items por 5min.

## Diagnóstico rápido

```bash
# 1. Qual controller?
kubectl --context mgmt -n platform-system logs -l app=tenant-controller --tail=100 \
  | grep -iE "reconcile|requeue|queue"

# 2. Throughput atual (reconciles/s)
kubectl --context mgmt -n platform-system port-forward deploy/prometheus-server 9090 &
curl -s "localhost:9090/api/v1/query?query=rate(controller_runtime_reconcile_total%5B1m%5D)" | jq

# 3. Leader ativo?
kubectl --context mgmt -n platform-system get lease -o wide | grep controller
```

## Decisão

| Sinal | Causa provável | Ação |
|-------|----------------|------|
| Queue crescendo linear | Burst > controller throughput | Adicionar CPU/memory do pod; longer-term: sharding (gap #3 do plano) |
| Queue crescendo step-wise | Downstream API slow (Gitea, ArgoCD) | Correlaciona com outros alerts (`GiteaAuthLatencyHigh`) |
| Queue alta mas estável | Convergiu num novo steady-state — possivelmente healthy | Monitorar 15min. Se não cai → ação |
| Reconcile errors > 0 | Código retornando err | `kubectl logs` para pattern |

## Controller sharding (solução de longo prazo)

Documentado em `docs/plans/50k-immediate-focus.md` gap #3. Sharding via workqueue com consistent-hash pelo label `loopzero.io/shard`. Requer T3 meeting.

## Correções conhecidas

- **Commit ef93a1b:** RBAC pods:delete (bug #36) — antes, reconcile loopa com 403
- **Commit 982dbbd:** Gitea minReplicas 2→4 — reduz downstream latency (reduz queue growth)

## Recuperação imediata

```bash
# Bump controller CPU temporariamente
kubectl --context mgmt -n platform-system set resources deploy/<controller> \
  --limits=cpu=2000m,memory=1Gi --requests=cpu=500m,memory=256Mi

# Watch queue drain
kubectl --context mgmt -n platform-system port-forward deploy/prometheus-server 9090 &
watch "curl -s 'localhost:9090/api/v1/query?query=workqueue_depth' | jq"
```
