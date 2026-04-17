# Runbook: BuildFailureRateHigh

**Severity:** critical  
**Component:** build-pipeline  
**Task:** T-07

## O que dispara

```promql
(
  sum(rate(loopzero_builds_total{status="FAILED"}[5m]))
  /
  sum(rate(loopzero_builds_total[5m]))
) > 0.10
```

> 10% dos builds recentes falharam (5min window). Indica regressão em algum step da pipeline.

## Diagnóstico rápido

```bash
# 1. Últimos workflows failed
kubectl --context build -n argo get wf --sort-by=.metadata.creationTimestamp \
  | grep -E "Failed|Error" | tail -5

# 2. Qual STEP falhou mais?
kubectl --context build -n argo get wf -o json \
  | jq -r '.items[] | select(.status.phase=="Failed") | .status.nodes | to_entries[] | select(.value.phase=="Failed" or .value.phase=="Error") | .value.displayName' \
  | sort | uniq -c | sort -rn

# 3. DB: últimos 5 builds FAILED + error_message
kubectl --context mgmt -n platform-system run psql-failures --rm -i --restart=Never \
  --image=postgres:16-alpine --timeout=30s \
  -- psql "$DB_URL" -c "SELECT id, tenant_id, error_message, finished_at FROM builds WHERE status='FAILED' ORDER BY finished_at DESC LIMIT 5;"
```

## Modos de falha conhecidos

| Padrão do erro | Diagnóstico | Bug ref | Fix |
|----------------|-------------|---------|-----|
| `cosign sign-image ... permission denied /home/user/.sigstore` | TUF cache write fail | Bug #39 | Commit bd78f00 (HOME=/tmp) |
| `mirror-harbor-api: 401 Unauthorized` | Harbor creds expired/missing | MEET-048 ongoing | Non-blocking (continueOn: failed) |
| `restore-cache: Pod was active ... deadline` | MinIO saturation | Bug #35/#18 | Commit f1cf228 (timeout 600s + resources bump) |
| `SYNC_FAILED` (Release) | ArgoCD AppProject missing | Bug #42 | `tools/admin-evict-tenants.sh` antes de novo run |
| `CELL_AT_CAPACITY` 20/20 | Admission gate (bug #43 wiring) | Bug #43 | Commit f094fa1 (env var plumbed) |
| `Max duration limit exceeded` em build-image | BuildKit timeout sob load | Open | Investigar retryStrategy MaxDuration |

## Decisão

1. **Qual step falhou mais?** Usar comando jq acima — se um step domina (>50% das falhas), foca nele.
2. **É padrão conhecido (acima)?** Aplica fix correspondente ou verifica se regrediu.
3. **É novo?** Abre bug, adiciona ao `docs/plans/scale-50k-technical-debt.md`.

## Correções persistentes

Cada fix deve ter:
- Commit (código/config)
- Entrada em `scale-50k-technical-debt.md`
- Adicionada a este runbook (tabela de modos de falha)
- Se aplicável: alert específico adicional (ex: cosign step errors)

## Recuperação

```bash
# Se um único step domina — retry manual dos workflows recentes falhados
kubectl --context build -n argo get wf -l status.phase=Failed -o name \
  | xargs -I {} kubectl --context build -n argo argo retry {}
```

Se taxa não cair em 15min após fix: escalation via SRE Guardian.
