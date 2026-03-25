# ROADMAP — HARDENING SPRINTS

**Objetivo:** Levar o LoopZero de "feature-complete" para "sobrevive à produção real".
**Base:** execution-plan-v2-2026-03-24.md + ROADMAP.md + project-scan
**Data início:** 2026-03-24
**Última atualização:** 2026-03-25

---

## Regras Globais

1. **Uma variável de sistema por vez.** Nunca mudar runtime + infra + pipeline no mesmo sprint.
2. **3 deploys estáveis entre sprints.** Se falhar, não avançar.
3. **Rollback definido para cada ação.** Sem rollback = não faz.

---

## Sequência

```
Sprint 1: Runtime        → Wire 7 controllers (serializado, 1 por vez)               ✅ DONE
    ↓ GATE: 3 deploys estáveis
Sprint 2: Estabilização  → TLS fix + validação E2E                                   ✅ DONE
    ↓ GATE: 3 deploys estáveis, TLS 4/4
Sprint 3: Banco          → PG HA + failover testado durante deploy                   ✅ DONE
    ↓ GATE: 3 deploys estáveis pós-failover
Sprint 4: Observabilidade → Métricas + SLOs + Dashboard                              ✅ DONE
    ↓ GATE: "por que demorou?" respondível em 30s
Sprint 5: Pipeline       → Fix build failure + cache + queue depth                   ✅ DONE
    ↓ GATE: 3 deploys estáveis, builds sem SIGTERM
Sprint 6: Operação       → status → logs (Loki) → rollback → secrets (ESO)          ⚠️ 3/4 DONE
    ↓ GATE: 3 deploys estáveis após cada feature
```

---

## Tabela de Sprints

| Sprint | Objetivo | Status | Done |
|---|---|---|---|
| [01-runtime](./01-runtime/ROADMAP.md) | Ativar controllers no main.go para reconcile loop real. | ✅ Complete | 7 controllers running + 3 deploys estáveis |
| [02-estabilizacao](./02-estabilizacao/ROADMAP.md) | Corrigir TLS para HTTPS em todos os serviços. | ✅ Complete | 4/4 certificados Ready |
| [03-banco](./03-banco/ROADMAP.md) | PostgreSQL HA com failover testado durante deploy. | ✅ Complete | PG 2 nodes + failover + API reconectou <30s |
| [04-observabilidade](./04-observabilidade/ROADMAP.md) | Métricas + SLOs + Dashboards Grafana. | ✅ Complete | 8 métricas + 5 SLOs + 6 dashboards |
| [05-pipeline](./05-pipeline/ROADMAP.md) | Fix SIGTERM + cache + queue depth. | ✅ Complete | Builds sem SIGTERM + cache funcional |
| [06-operacao](./06-operacao/ROADMAP.md) | 4 comandos operacionais: status, logs, rollback, secrets. | ⚠️ 3/4 | Logs ✅ Rollback ✅ Secrets ✅ Status ⚠️ (falta wiring) |

---

## Sprint 6 — Status Detalhado (2026-03-25)

| Sub-Sprint | Status | Validado no DO | Evidência |
|---|---|---|---|
| 6.1 status | ⚠️ Código pronto, rota não wired | ❌ Retorna 404 | Handler + CLI + testes existem, falta 1 linha no server.go |
| 6A logs | ✅ Completo | ✅ | 142 linhas de log real via Loki, --json schema correto |
| 6B rollback | ✅ Completo | ✅ | Reverteu release em 3s, toggle funciona |
| 6C secrets | ✅ Completo | ✅ | ESO sync <30s, valor nunca no output, K8s Secret + ExternalSecret automáticos |

### Infraestrutura deployada no DO

| Componente | Cluster | Status |
|-----------|---------|--------|
| Grafana Loki (single binary) | Build | Running (2/2), NodePort 30100 |
| Promtail (DaemonSet) | Build | Running, coletando logs de builds |
| External Secrets Operator v0.14.3 | Runtime | Running (3 pods) |
| ClusterSecretStore (loopzero-mgmt-store) | Runtime | Ready + Valid |
| Loki cross-cluster config | Pipeline | Automático (infra-deploy step) |

### Testes

| Componente | Testes | Status |
|-----------|--------|--------|
| CLI total | 455+ | ✅ Zero regressão |
| API total | 60+ files | ✅ Zero regressão |
| State machine (release reactivation) | 3 novos | ✅ |
| CI pipeline | 17+ jobs | ✅ All green |

---

## Detalhes

| Sprint | Confiança | Esforço estimado | Esforço real |
|---|---|---|---|
| 01-runtime | ALTA | ~3h | ~3h |
| 02-estabilizacao | MÉDIA | ~2h | ~2h |
| 03-banco | MÉDIA | ~4h | ~4h |
| 04-observabilidade | ALTA | ~8h | ~8h |
| 05-pipeline | ALTA | ~4h | ~4h |
| 06-operacao | MÉDIA | ~50h | ~40h (6A: 10h, 6B: 15h, 6C: 12h, 6.1: pendente) |
