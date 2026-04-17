# Capacity Baselines

Este documento registra baselines de capacity por subsistema, medidos via
`infra/tests/capacity/` (T-03 do plano `50k-immediate-focus.md`).

## Como rodar

```bash
export LOOPZERO_CAPACITY=1
export LOOPZERO_API_URL=https://api.dev.loopzero.example
export LOOPZERO_SERVICE_AUTH=$(kubectl --context mgmt get secret loopzero-api-secrets -o jsonpath='{.data.LOOPZERO_SERVICE_AUTH_SECRET}' | base64 -d)

# loopzero-api submit path
go test ./infra/tests/capacity/ -run TestCapacityLoopZeroAPI -v \
  -args -rps=50 -duration=60s -workers=20

# Gitea auth
export LOOPZERO_GITEA_URL=http://gitea-http.platform-system.svc.cluster.local:3000
export LOOPZERO_GITEA_USER=loopzero-admin
export LOOPZERO_GITEA_PASS=$(kubectl --context mgmt -n platform-system get secret gitea-admin-secret -o jsonpath='{.data.password}' | base64 -d)
go test ./infra/tests/capacity/ -run TestCapacityGiteaAuth -v \
  -args -rps=100 -duration=60s -workers=50
```

Resultados ficam em `/tmp/capacity-bench/{subsystem}-{timestamp}.json`.

## Convenções

- Todos bench são **opt-in** (`LOOPZERO_CAPACITY=1`) — nunca rodam em CI por default.
- Soft assertions falham somente em regressão crítica (p95 > 5s, error-rate > 50%).
- Cada subsystem tem seus próprios thresholds de sucesso documentados no teste.

## Baselines conhecidas

Serão preenchidos com primeiros runs validados após T-04 (HPAs) + T-05 (sharding) estabilizarem.

### loopzero-api POST /builds

| Concurrency | Target RPS | Actual RPS | p50 | p95 | p99 | Error rate | Notes |
|---|---|---|---|---|---|---|---|
| 20 | 50 | — | — | — | — | — | Post T-04 |

### gitea auth (GET /api/v1/user)

| Concurrency | Target RPS | Actual RPS | p50 | p95 | p99 | Error rate | Notes |
|---|---|---|---|---|---|---|---|
| 50 | 100 | — | — | — | — | — | Post T-04 (HPA 2-8) |

### minio PUT (via mc)

Pendente: implementação de `minio_test.go`.

### argocd sync

Pendente: implementação de `argocd_test.go`.

## Regressão

Quando um bench sai da baseline documentada:

1. Identifica o subsystem.
2. Checa alertas T-07 que cobrem o subsystem:
   - Gitea: `GiteaAuthLatencyHigh`
   - Argo: `ArgoWorkflowQuotaExceeded`
   - Storage: `PVCPendingSustained`
   - HPA: `HPAAtMaxReplicas`
3. Se alerta disparou: ok — alerta + bench corroboram.
4. Se não disparou: adicionar alerta novo (capacity gap → monitoring gap).

## Relação com L3/L4 E2E

Capacity bench é **complementar**, não substituto, ao E2E:

- **Bench detecta** saturation de subsystems individuais em isolamento.
- **E2E valida** integração + fluxo end-to-end.

Regra: bench primeiro, E2E depois. Se bench passa mas E2E falha, o bug está em integração/orquestração, não capacity. Se bench falha, conserta antes de rodar E2E.

## Changelog

- **v1 (2026-04-17):** Framework inicial (harness.go) + 2 bench suites (loopzero-api, gitea). Baselines ainda não capturadas — próximo passo é rodar pós-estabilização do dev cluster.
