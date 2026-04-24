# Runbook: GiteaAuthLatencyHigh

**Severity:** warning  
**Component:** gitea  
**Task:** T-07 (plan 50k-immediate-focus)

## O que dispara

```promql
histogram_quantile(0.95,
  sum by (le) (rate(gitea_http_request_duration_seconds_bucket[5m]))
) > 2
```

Gitea p95 auth latency > 2s por 5min. Leading indicator de CPU saturation — histórico: bug #44 (L3 c=30 causou 401 cascade quando auth ficou 7s).

## Diagnóstico rápido

```bash
# 1. CPU e replicas atuais
kubectl --context mgmt -n loopzero-system get hpa gitea

# 2. Pods Gitea (top + ages)
kubectl --context mgmt -n loopzero-system top pods -l app=gitea
kubectl --context mgmt -n loopzero-system get pods -l app=gitea

# 3. Logs de auth recente
kubectl --context mgmt -n loopzero-system logs -l app=gitea --tail=50 | grep -iE "401|auth|slow"
```

## Decisão

| Sinal | Causa provável | Ação |
|-------|----------------|------|
| HPA não escalou (`CURRENT` = `MIN`) | HPA broken ou cpu% não atingiu target | `kubectl describe hpa gitea` — checa ScalingActive, última recomendação |
| HPA escalou até `MAX` e latência alta | maxReplicas insuficiente | Bump `maxReplicas` no Pulumi e re-apply |
| HPA OK mas latência alta em TODAS replicas | DB/Redis Gitea saturando | Verifica gitea-redis pod + PG conn pool |
| Latência spike inicial + HPA scaling | Burst excedeu minReplicas | Bump `minReplicas` (padrão: ≥ concurrency esperada / 8) |

## Correções conhecidas

- **Commit bbec947:** resources 500m/512Mi → 3CPU/2Gi + HPA 2-8
- **Commit 982dbbd:** `minReplicas: 2 → 4` para absorber burst inicial
- **Runbook histórico:** `docs/plans/scale-50k-l3-readiness.md` bug #44

## Recuperação

1. Validar latência voltou (<2s p95 em 5min):
   ```
   kubectl --context mgmt -n loopzero-system exec -it deploy/prometheus-server -c prometheus-server -- \
     sh -c 'wget -q -O- "http://localhost:9090/api/v1/query?query=histogram_quantile(0.95,sum by (le) (rate(gitea_http_request_duration_seconds_bucket[5m])))"'
   ```
2. Documentar incidente em `docs/plans/capacity-baselines.md` se thresholds precisam ajuste permanente.
