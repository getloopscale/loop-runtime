# Alert Runbooks

Runbooks operacionais para alertas do Prometheus. Cada alert tem:

1. Query que dispara
2. Diagnóstico rápido (comandos)
3. Tabela de causas prováveis → ações
4. Correções conhecidas (commits + bugs históricos)
5. Procedimento de recuperação

## Inventário

### T-07 Capacity alerts (plan 50k-immediate-focus.md)

| Alert | Severity | File |
|-------|:--------:|------|
| [GiteaAuthLatencyHigh](GiteaAuthLatencyHigh.md) | warning | Gitea p95 auth > 2s — leading indicator de CPU saturation |
| [ArgoWorkflowQuotaExceeded](ArgoWorkflowQuotaExceeded.md) | warning | Argo 403 rate > 0.1/s — normalmente ResourceQuota, não RBAC |
| [HPAAtMaxReplicas](HPAAtMaxReplicas.md) | warning | HPA pegged no max → ceiling atingido |
| [PVCPendingSustained](PVCPendingSustained.md) | warning | PVC pending > 2min — histórico DO CSI rate limit |
| [TenantReconcileBacklog](TenantReconcileBacklog.md) | warning | Controller queue > 50 — operator saturated |
| [BuildFailureRateHigh](BuildFailureRateHigh.md) | critical | > 10% builds failing — tabela de modos conhecidos |

### TLS alerts (plan edge-routing-tls)

Runbooks pendentes. Prometheus rules já ativos:

- `LoopZeroCertApproachingRotationCap` — 180 dias para rotação
- `LoopZeroClusterIssuerNotReady` — issuer não pronto 15min+
- `LoopZeroCertStuckNotReady` — certificate não pronto 30min+

### Platform alerts

Runbooks pendentes para:
- `LoopZeroAPIDown`, `LoopZeroDeployFailing`, `HarborCanaryFailing` (etc)

### Top 10 critical alerts (T2.7 / VAL-002 — added 2026-04-26)

Runbooks adicionados para fechar parte do gap de VAL-002 (33 de 47 alertas sem runbook):

| Alert | Severity | File |
|-------|:--------:|------|
| LoopZeroDBPoolSaturation | warning | [loopzero-db-pool-saturation.md](loopzero-db-pool-saturation.md) |
| LoopZeroReconcilerUnhealthy | warning | [loopzero-reconciler-unhealthy.md](loopzero-reconciler-unhealthy.md) |
| LoopZeroAppCrashLoop | warning | [loopzero-app-crash-loop.md](loopzero-app-crash-loop.md) |
| LoopZeroAppDeployStuck | warning | [loopzero-app-deploy-stuck.md](loopzero-app-deploy-stuck.md) |
| LoopZeroBuildCircuitBreakerOpen | warning | [loopzero-build-cb-open.md](loopzero-build-cb-open.md) |
| LoopZeroTenantDeletionStuck | warning | [loopzero-tenant-deletion-stuck.md](loopzero-tenant-deletion-stuck.md) |
| LoopZeroPlatformBootstrapStuck | warning | [loopzero-platformbootstrap-stuck.md](loopzero-platformbootstrap-stuck.md) |
| LoopZeroArgoCDSyncFailed | warning | [loopzero-argocd-sync-failed.md](loopzero-argocd-sync-failed.md) |
| LoopZeroCertRenewalFailed | warning | [loopzero-cert-renewal-failed.md](loopzero-cert-renewal-failed.md) |
| LoopZeroPulumiDrift | warning | [loopzero-pulumi-drift.md](loopzero-pulumi-drift.md) |

> **Observabilidade nota (T2.6 / OBS-001):** LoopZeroReconcilerUnhealthy referenciava
> métrica `loopzero_reconciler_errors_total` que nunca era emitida. O runbook
> referencia o nome correto `loopzero_operator_reconcile_errors_total`. A correção
> no `prometheusrule-warning.yaml` aguarda governance approval (T2.6 bloqueado).

## Princípios

1. **Tudo o que dispara deve ter runbook.** Zero-runbook é technical debt operacional.
2. **Runbook é código** — versionado em git, revisado em PR.
3. **Modo de falha conhecido → fila documentada.** Cada bug encontrado adiciona linha ao runbook correspondente.
4. **Fix persistente → atualiza runbook + technical-debt.md.**
