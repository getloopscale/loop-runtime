# Autoscaling Topology — LoopZero Platform

**Status:** live em dev cluster (DO SFO3).
**Source of truth:** `infra/pulumi/src/components/mgmt-bootstrap.ts` + `infra/helm/argocd/values.yaml`.
**Último update:** 2026-04-17 (T-04 + T-05 do plano `50k-immediate-focus.md`).

## Visão geral

```
╔═══════════════════════════════════════════════════════════════════════════════╗
║                              MGMT CLUSTER                                     ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║                                                                               ║
║  APP PLANE (LoopZero services)                                                    ║
║  ┌────────────────┬─────────┬─────────┬──────────────┐                        ║
║  │ Deployment     │ HPA min │ HPA max │ Trigger      │                        ║
║  ├────────────────┼─────────┼─────────┼──────────────┤                        ║
║  │ loopzero-api       │   2     │   10    │ CPU 70%      │                        ║
║  │ loopzero-cloud     │   1     │    5    │ CPU 70%      │                        ║
║  │ loopzero-dashboard │   1     │    3    │ CPU 80%      │                        ║
║  └────────────────┴─────────┴─────────┴──────────────┘                        ║
║                                                                               ║
║  GITOPS / REGISTRY / AUTH PLANE                                               ║
║  ┌────────────────────────┬─────────┬─────────┬──────────────┐                ║
║  │ Deployment             │ HPA min │ HPA max │ Trigger      │                ║
║  ├────────────────────────┼─────────┼─────────┼──────────────┤                ║
║  │ gitea                  │   4 ★   │    8    │ CPU 70%      │                ║
║  │ harbor-core            │   1     │    5    │ CPU 70%      │                ║
║  │ harbor-registry        │   1     │    8    │ CPU 70%      │                ║
║  │ harbor-jobservice      │   1     │    3    │ CPU 70%      │                ║
║  │ argocd-server          │   1     │    3    │ CPU 70%      │                ║
║  │ argocd-repo-server     │   2     │   10    │ CPU 70%      │                ║
║  │ ory-hydra              │   1     │    3    │ CPU 70%      │                ║
║  │ ory-kratos             │   1     │    3    │ CPU 70%      │                ║
║  │ mgmt-traefik           │   2     │    6    │ CPU 70%      │                ║
║  └────────────────────────┴─────────┴─────────┴──────────────┘                ║
║                                                                               ║
║  STATEFUL SCALING                                                             ║
║  ┌──────────────────────────────────┬──────────┬─────────────────────┐        ║
║  │ StatefulSet                      │ Replicas │ Mode                │        ║
║  ├──────────────────────────────────┼──────────┼─────────────────────┤        ║
║  │ argocd-application-controller    │     3    │ Sharding (hash)     │        ║
║  │ minio                            │     1    │ Standalone (⚠ L4)   │        ║
║  │ gitea-redis (master)             │     1    │ Single              │        ║
║  └──────────────────────────────────┴──────────┴─────────────────────┘        ║
║                                                                               ║
║  FIXED (no autoscaling)                                                       ║
║  - argocd-dex-server, argocd-applicationset-controller, argocd-redis          ║
║    argocd-notifications-controller: 1 each                                    ║
║  - cert-manager, external-secrets-operator, k8s-monitoring: cluster-level     ║
║                                                                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝

★ Gitea minReplicas=4 — elevated from default 2 after L3 v8 lesson:
  reactive HPA não absorve burst inicial (commit 982dbbd).

╔═══════════════════════════════════════════════════════════════════════════════╗
║                             BUILD CLUSTER                                     ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║                                                                               ║
║  argo-workflows-workflow-controller: 1 (single, não HPA)                      ║
║    → GAP: sharding em L4+ (candidato T3 meeting)                              ║
║  argo-workflows-server: 1 (UI, baixa carga)                                   ║
║    → GAP: HPA 1-3 futuro                                                      ║
║                                                                               ║
║  Node autoscaling: 2 default + 3 overflow + 2 c30-prep (+ DOKS autoscaler)    ║
║  Local-path-provisioner: per-node hostPath (bypass DO CSI rate limit)         ║
║                                                                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝

╔═══════════════════════════════════════════════════════════════════════════════╗
║                           RUNTIME CLUSTER                                     ║
╠═══════════════════════════════════════════════════════════════════════════════╣
║                                                                               ║
║  User apps: cada tenant gerencia seu próprio HPA (Deployment tenant-app)      ║
║  Traefik (ingress): 2 static. GAP: HPA 2-6 (planejado T-04).                  ║
║                                                                               ║
║  Node autoscaling: DOKS autoscaler + overflow pools                           ║
║                                                                               ║
╚═══════════════════════════════════════════════════════════════════════════════╝
```

## HPA behavior (comum a todos)

```yaml
behavior:
  scaleUp:
    stabilizationWindowSeconds: 30      # reacts fast to load burst
    policies:
      - type: Percent
        value: 100                      # can double replicas per 30s
        periodSeconds: 30
  scaleDown:
    stabilizationWindowSeconds: 300     # slow to scale down — avoid flapping
    policies:
      - type: Percent
        value: 25                       # max 25% removal per 60s
        periodSeconds: 60
```

**Rationale:**
- `scaleUp` fast: burst arrivals (deploy waves) are the primary risk; responding in 30s vs 5min is critical.
- `scaleDown` slow: holding capacity after burst is cheap; re-scaling on transient gaps is expensive.

## Lições de L3 v8

1. **minReplicas must cover expected burst.** Reactive HPA has ~60s latency from CPU spike to new pod Ready. A burst arriving in <60s will overwhelm whatever `minReplicas` is. Rule of thumb:
   `minReplicas(dep) ≥ expected_concurrent_requests / reqs_per_pod_sustained`
2. **Sharding ≠ HPA.** ArgoCD application-controller uses sharding (stateful hash-based distribution), not HPA. Adicionar replicas requires `ARGOCD_CONTROLLER_REPLICAS` env var too.
3. **Build cluster has different scaling needs.** Workflows são efêmeros (per-build pods). O controller é constante. Não aplicar padrão "app server" HPA — pesquisar argo-workflows sharding guide.

## Gaps conhecidos (backlog)

| Gap | Impacto | Prioridade |
|-----|:-------:|:----------:|
| argo-workflow-controller sharding | Blocks L4+ (>500 concurrent wfs) | High — T3 meeting |
| MinIO distributed mode | Block L4+ scale of source bundles | High — plan existe `minio-distributed-migration.md` |
| Runtime Traefik HPA | User app ingress lag sob L4 burst | Medium |
| gitea-redis: HA master-replica | Stateful; upgrade complexity | Low |

## Observability

Alerts T-07 cobrem os sinais:

- `HPAAtMaxReplicas` — ceiling atingido
- `GiteaAuthLatencyHigh` — leading indicator de saturação specific
- `TenantReconcileBacklog` — operator throughput (não HPA, mas correlata)

Dashboard: Grafana "Capacity Elasticity" (pendente criação — gap T-07 DoD).

## Como atualizar este diagrama

Qualquer mudança em HPA min/max deve:
1. Atualizar Pulumi source (`mgmt-bootstrap.ts`)
2. Atualizar tabela acima (em mesma PR)
3. Executar `tools/capture-capacity-baselines.sh` pós-deploy para confirmar baseline
4. Atualizar `capacity-baselines.md` se baseline mudar significativamente
