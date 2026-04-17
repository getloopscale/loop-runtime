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

## Princípios

1. **Tudo o que dispara deve ter runbook.** Zero-runbook é technical debt operacional.
2. **Runbook é código** — versionado em git, revisado em PR.
3. **Modo de falha conhecido → fila documentada.** Cada bug encontrado adiciona linha ao runbook correspondente.
4. **Fix persistente → atualiza runbook + technical-debt.md.**
