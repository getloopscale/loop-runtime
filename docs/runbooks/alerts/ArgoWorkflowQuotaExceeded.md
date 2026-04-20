# Runbook: ArgoWorkflowQuotaExceeded

**Severity:** warning  
**Component:** argo-workflows  
**Task:** T-07

## O que dispara

```promql
sum(rate(argo_workflows_k8s_request_total{status_code="403"}[5m])) > 0.1
```

Argo controller recebendo >0.1 403/s por 10min. Geralmente é `ResourceQuota` excedida (bug #41), não RBAC.

## Diagnóstico rápido

```bash
# 1. Checa quota atual
kubectl --context build -n argo get resourcequota argo-workflow-quota

# 2. Últimos erros do controller
kubectl --context build -n argo logs deploy/argo-workflows-workflow-controller --tail=30 \
  | grep -iE "exceeded quota|403"

# 3. Workflows stuck
kubectl --context build -n argo get wf --no-headers | awk '{print $2}' | sort | uniq -c
```

## Decisão

| Sinal | Causa | Ação |
|-------|-------|------|
| "exceeded quota: argo-workflow-quota, requested: limits.cpu=3" | Pico de workflows excede quota | Bump quota OU reduce concurrency upstream |
| 403 em pods:create, configmaps:get (RBAC) | RBAC gap novo | `kubectl auth can-i` + patch ClusterRole |
| 403 em namespaces:delete | Bug #37 (já fixed commit ef93a1b) | Verificar se fix persiste após `pulumi up` |

## Correções conhecidas

- **Commit 9ee9806:** quota requests.cpu 8→24, limits.cpu 16→48
- **Commit ef93a1b:** RBAC pods+namespaces delete (bugs #36/#37)

## Recuperação

1. Se quota ajustada: esperar 5min, verificar taxa voltou abaixo de 0.1/s
2. Se RBAC: confirmar `kubectl auth can-i --as=system:serviceaccount:... <verb> <resource>`
3. Limpar workflows failed antigos: `kubectl -n argo delete wf --field-selector status.phase=Failed`
