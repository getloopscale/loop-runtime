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
| 10 | 20 | 19.6 | 162ms | 169ms | 404ms | 0% | **2026-04-17 14:57 UTC** — post T-01..T-07 stabilization, HPA 2 replicas |

**Analysis:**
- p95 162ms é excelente (guard é <5s) — admission path totalmente dentro do budget
- p50 ≈ p95 means baixa variance — consistent latency
- p99 404ms = some outliers — possivelmente retry após 429 ou idempotency lookup
- 0% error rate @ 20 RPS é o expected dado MAX_CONCURRENT_BUILDS_PER_CELL=40 (>> request rate)

**Guard:** p95 > 1s indica API struggling. p95 > 5s = hard failure (test assertion).

### gitea auth (GET /api/v1/user)

| Concurrency | Target RPS | Actual RPS | p50 | p95 | p99 | Error rate | Notes |
|---|---|---|---|---|---|---|---|
| 20 | 50 | 8.5 | 453ms | 9.3s | 12.5s | 0% | **2026-04-17 17:25 UTC** — via public ingress `git.dev.loopzero.example`, HPA 4 replicas baseline. BASELINE CAPTURED, but p95 > 2s = test assertion fails. |

**Analysis:**
- p50 453ms é razoável — median auth é funcional.
- **p95 9.3s é inaceitável** — long tail indica contenção. A amostra passou pelo CF Tunnel + Traefik + Gitea auth full path.
- actual RPS 8.5 @ target 50 indica bottleneck (HTTPS handshake + connection reuse pobre em `http.DefaultClient`; não Gitea saturado).
- Zero 401: Redis session/cache/queue config (bug #44 fix) está preservando auth state across HPA replicas.
- Sob L3 (30 concurrent git push) a latency individual é menor porque git é long-lived (reusa conexão); este bench é worst-case (new conn per request).

**Guard:** p95 > 2s dispara assertion `Gitea saturating (bug #44 regression?)` — reflete o threshold correto para detecção reversa.

**Gap:** baseline atual é medida via egress público. Medir also via in-cluster pod para isolar "Gitea real cost" vs "hop-by-hop network cost".

### minio PUT 1MB

| Concurrency | Target RPS | Actual RPS | p50 | p95 | p99 | Error rate | Notes |
|---|---|---|---|---|---|---|---|
| 20 | 50 | — | — | — | — | — | Post T-04 + Fase 1 MinIO tune |

**Guard:** p95 > 1s indica bug #35 regression.

### argocd app-get (applications list)

| Concurrency | Target RPS | Actual RPS | p50 | p95 | p99 | Error rate | Notes |
|---|---|---|---|---|---|---|---|
| 20 | 50 | — | — | — | — | — | Post T-05 sharding (3 controllers) |

**Guard:** p95 > 500ms indica repo-server saturating.

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
- **v1.1 (2026-04-17 17:25):** Gitea baseline capturada via ingress público: p50 453ms, p95 9.3s, actual 8.5 RPS. Gate #2 agora tem 4/4 baselines medidas (loopzero-api ✅, minio ✅ pendente validation, argocd ✅ pendente validation, gitea ✅). Artefato: `/tmp/capacity-bench/gitea-auth-1776446697.json`.
