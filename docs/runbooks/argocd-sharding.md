# Runbook: ArgoCD Application Controller Sharding

**Context:** T-05 (plan 50k-immediate-focus.md). ArgoCD
`application-controller` distribui Applications entre N pods via hash
consistente. Usado para escalar reconcile throughput quando N Apps cresce.

## Estado atual (dev cluster)

- StatefulSet: `argocd-application-controller`
- Replicas: **3**
- Env: `ARGOCD_CONTROLLER_REPLICAS=3`
- Algoritmo: `legacy` (default — hash-based, estável)

## Quando re-shardar

### Critérios para bump replicas +1

- p95 sync latency > 60s por 10+ min
- `workqueue_depth{name="app_operation"} > 100` sustentado
- N Apps > 300 (heurística: ~100 Apps/controller)
- CPU do controller > 60% sustentado (memory típico é alto, CPU é o sinal)

### Critérios para scale-down

- p95 sync latency < 10s por 30 min
- N Apps < 50
- CPU < 15% sustentado

## Procedimento — bump replicas

```bash
# 1. Atualiza Helm values
# infra/helm/argocd/values.yaml
#   controller:
#     replicas: 3 → 4
#     env:
#       - name: ARGOCD_CONTROLLER_REPLICAS
#         value: "4"     # ← deve bater com replicas

# 2. Live apply (sem rebuild)
kubectl --context mgmt -n platform-system scale statefulset argocd-application-controller --replicas=4
kubectl --context mgmt -n platform-system set env statefulset/argocd-application-controller \
  ARGOCD_CONTROLLER_REPLICAS=4

# 3. Espera novo pod subir
kubectl --context mgmt -n platform-system rollout status statefulset/argocd-application-controller --timeout=5m

# 4. Commit no Helm values para persistir
git add infra/helm/argocd/values.yaml
git commit -m "argocd: controller replicas X → Y"

# 5. Valida distribuição (cada pod reconciliando ~N/replicas Apps)
kubectl --context mgmt -n platform-system port-forward svc/argocd-metrics 9001 &
curl -s localhost:9001/metrics | grep argocd_cluster_app_info | awk '{print $2}' | sort | uniq -c
```

## Procedimento — scale-down

**⚠ ATENÇÃO:** Reduzir replicas faz redistribuição de Apps. Sync latency
pode ficar alto temporariamente enquanto novos owners tomam cache.
Prefira fazer fora de janela de pico.

```bash
# Bump down: 4 → 3
kubectl --context mgmt -n platform-system scale statefulset argocd-application-controller --replicas=3
kubectl --context mgmt -n platform-system set env statefulset/argocd-application-controller \
  ARGOCD_CONTROLLER_REPLICAS=3
```

## Diagnóstico de problemas comuns

### Syncs parados em Unknown

```bash
# Ver qual pod controla o app
kubectl --context mgmt -n platform-system logs argocd-application-controller-0 --tail=50 | grep <app-name>
kubectl --context mgmt -n platform-system logs argocd-application-controller-1 --tail=50 | grep <app-name>
kubectl --context mgmt -n platform-system logs argocd-application-controller-2 --tail=50 | grep <app-name>

# Se nenhum pod é responsável: ARGOCD_CONTROLLER_REPLICAS não está set — apps sem owner
# Fix: confirmar env var no StatefulSet, restartar pods
```

### Hotspot — um pod sobrecarregado

Sharding legacy usa hash simples — pode dar hotspots se Apps têm nomes similares.

Fix: migrar para `round-robin`:

```yaml
# infra/helm/argocd/values.yaml
controller:
  sharding:
    algorithm: round-robin
```

⚠ round-robin exige que TODOS os controllers restartem simultaneamente. Usar apenas em janela de manutenção.

### Perda de shard ownership em restart

Se um pod crashar, suas Apps ficam temporariamente sem owner. Durante o restart (~60s):
- Novos sync events são ignorados
- Apps em Progressing podem virar Unknown

Mitigação: `controller.pdb` (PodDisruptionBudget) com `maxUnavailable: 1`. Já configurado via Helm.

## Medições (L3 baseline)

| Replicas | N Apps | p50 sync | p95 sync | Controller CPU |
|---|---|---|---|---|
| 1 | 100 | >15min stuck | >15min stuck | 80%+ |
| **3** | **100** | **TBD (L3 v10)** | **TBD** | **TBD** |

*Capturar após L3 v10 stabilizar via:*

```bash
kubectl --context mgmt -n platform-system port-forward deploy/prometheus-server 9090 &
curl -s "localhost:9090/api/v1/query?query=histogram_quantile(0.95,rate(argocd_app_reconcile_bucket[5m]))" | jq
```

## Referências

- Commit T-05: 15ce9be (live apply + Helm values)
- Plan: `docs/plans/50k-immediate-focus.md` T-05
- Topology: `docs/plans/autoscaling-topology.md` (stateful scaling section)
- ArgoCD docs: https://argo-cd.readthedocs.io/en/stable/operator-manual/high_availability/
