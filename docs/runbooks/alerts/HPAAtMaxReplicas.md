# Runbook: HPAAtMaxReplicas

**Severity:** warning  
**Component:** autoscaling  
**Task:** T-07

## O que dispara

```promql
kube_horizontalpodautoscaler_status_current_replicas
  == on(namespace, horizontalpodautoscaler)
kube_horizontalpodautoscaler_spec_max_replicas
```

HPA pegged em maxReplicas por 5min+. Ceiling atingido.

## Diagnóstico rápido

```bash
# 1. Qual HPA?
kubectl --context mgmt -n loopzero-system get hpa

# 2. Utilização atual + histórico
kubectl --context mgmt -n loopzero-system describe hpa <name>

# 3. Pods executando — todos saturados ou só alguns?
kubectl --context mgmt -n loopzero-system top pods -l <label-from-hpa-target>
```

## Decisão

| CPU% atual | maxReplicas | Ação |
|------------|-------------|------|
| > target (70%) | = 10 | Bump `maxReplicas` +50% (ex: 10→15) via Pulumi + re-apply |
| ≈ target (70%) | Qualquer | Load real — está no ceiling certo. Monitorar se não oscila |
| < target (<70%) | Qualquer | HPA não deveria estar a max. Checa requests/limits do pod — request muito alto? |

## Validação pós-bump

Rodar capacity bench específico do subsistema (ver `docs/plans/capacity-baselines.md`):

```bash
LOOPZERO_CAPACITY=1 go test ./infra/tests/capacity/ -run TestCapacity<Subsystem> -v \
  -args -rps=<current-peak-rate> -duration=2m
```

Se o bench passar em todos percentis com novo max → ajuste OK.

## Armadilhas

- **minReplicas baixo + burst fast:** HPA escalando mas não rápido o suficiente. Fix: bumpar minReplicas (ex: commit 982dbbd Gitea 2→4).
- **Resource limits do pod muito baixos:** HPA escala mas cada pod nem atinge 70% CPU antes de throttle. Bump resources limits/requests no deployment.
