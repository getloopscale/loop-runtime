# ROADMAP — HARDENING SPRINTS

**Objetivo:** Levar o LoopZero de "feature-complete" para "sobrevive à produção real".
**Base:** execution-plan-v2-2026-03-24.md + ROADMAP.md + project-scan
**Data:** 2026-03-24

---

## Regras Globais

1. **Uma variável de sistema por vez.** Nunca mudar runtime + infra + pipeline no mesmo sprint.
2. **3 deploys estáveis entre sprints.** Se falhar, não avançar.
3. **Rollback definido para cada ação.** Sem rollback = não faz.

---

## Sequência

```
Sprint 1: Runtime        → Wire 5 controllers (serializado, 1 por vez)
    ↓ GATE: 3 deploys estáveis
Sprint 2: Estabilização  → TLS fix + validação E2E
    ↓ GATE: 3 deploys estáveis, TLS 4/4
Sprint 3: Banco          → PG HA + failover testado durante deploy
    ↓ GATE: 3 deploys estáveis pós-failover
Sprint 4: Observabilidade → Métricas + SLOs + Dashboard
    ↓ GATE: "por que demorou?" respondível em 30s
Sprint 5: Pipeline       → Fix build failure + cache + queue depth
    ↓ GATE: 3 deploys estáveis, builds sem SIGTERM
Sprint 6: Operação       → status → logs (Loki) → rollback → secrets (ESO) (1 por vez)
    ↓ GATE: 3 deploys estáveis após cada feature
```

---

## Tabela de Sprints

| Sprint | Objetivo | Done |
|---|---|---|
| [01-runtime](./01-runtime/ROADMAP.md) | Ativar os 5 controllers implementados mas não registrados no main.go, para que todo estado declarado via CRD passe por reconcile loop real. | 7 controllers running + 3 deploys consecutivos estáveis com Tenant → Environment → namespace criado no runtime em <60s. |
| [02-estabilizacao](./02-estabilizacao/ROADMAP.md) | Corrigir os 3 certificados TLS falhando para que todos os serviços da plataforma sejam acessíveis via HTTPS. | 4/4 certificados Ready + ArgoCD, Gitea e Grafana acessíveis via HTTPS sem erro de certificado. |
| [03-banco](./03-banco/ROADMAP.md) | Eliminar PostgreSQL como single point of failure com HA real testado empiricamente durante deploy ativo. | PG com 2 nodes + failover executado durante deploy + API reconectou em <30s + zero locks orphanados + 3 deploys estáveis pós-failover. |
| [04-observabilidade](./04-observabilidade/ROADMAP.md) | Instrumentar cada etapa do deploy com métricas, definir SLOs com alertas, e criar dashboard para que "por que esse deploy demorou?" seja respondível em 30 segundos. | 8 métricas emitindo + 5 SLOs com alertas configurados + dashboard Grafana mostrando dados reais após 3 deploys. |
| [05-pipeline](./05-pipeline/ROADMAP.md) | Resolver a falha de build (SIGTERM), validar que cache funciona, e tornar a fila de builds visível e controlada por tenant. | Causa raiz do SIGTERM documentada e corrigida + cache reduz install-deps >50% + queue depth no dashboard + 3 builds sem falha. |
| [06-operacao](./06-operacao/ROADMAP.md) | Implementar os 4 comandos operacionais (status, logs, rollback, secrets) para que o usuário opere falhas sem depender de kubectl ou intervenção humana. Logs via **Grafana Loki** (não PostgreSQL). Secrets via **External Secrets Operator** (não controller custom). Dividido em 4 sub-sprints sequenciais: [6.1-status](./06-operacao/ROADMAP.md), [6A-logs](./06-operacao/06a-logs/ROADMAP.md), [6B-rollback](./06-operacao/06b-rollback/ROADMAP.md), [6C-secrets](./06-operacao/06c-secrets/ROADMAP.md). | `loopzero status` mostra estado consolidado + `loopzero logs` retorna output do build (Loki) + `loopzero rollback` reverte em <30s + `loopzero secret set` sincroniza para workload em <30s (ESO). |

---

## Detalhes

| Sprint | Confiança | Esforço | Pré-requisito |
|---|---|---|---|
| 01-runtime | ALTA | ~3h | Sistema base saudável |
| 02-estabilizacao | MÉDIA | ~2h | Sprint 1 GATE passou |
| 03-banco | MÉDIA | ~4h | Sprint 2 GATE passou |
| 04-observabilidade | ALTA | ~8h | Sprint 3 GATE passou |
| 05-pipeline | ALTA | ~4h | Sprint 4 GATE passou |
| 06-operacao | MÉDIA | ~50h (6.1: 2h, 6A: 12h, 6B: 20h, 6C: 16h). Timeline realista: 3-4 semanas (TDD + gates). | Sprint 5 GATE passou |

---

## Tabela de Confiança

| Item | Confiança | Justificativa |
|---|---|---|
| Wire controllers (1.1-1.3, 1.4) | ALTA | Código existe, testes passam, assinaturas simples |
| Wire PlatformBootstrap (1.5) | MÉDIA | Dependências externas (Gitea, ArgoCD, API URLs) |
| TLS fix | MÉDIA | Causa raiz desconhecida até investigar |
| PG HA upgrade | MÉDIA | Failover precisa ser testado empiricamente |
| Métricas + Dashboard | ALTA | Instrumentação sem side effects |
| SLO targets | MÉDIA | Estimativas — ajustar com dados reais |
| 6.1 loopzero status | MÉDIA | Handler existe, falta wiring no router + adapters |
| 6A loopzero logs (5 tasks) | MÉDIA | Grafana Loki + Promtail + MinIO (infra existente). API faz proxy de LogQL. Sem código custom de log storage. |
| 6B loopzero rollback (6 tasks) | BAIXA | State machines existem (rollback + release). Falta orchestrator + gitops read + endpoint. |
| 6C loopzero secret (5 tasks) | MÉDIA | External Secrets Operator (CNCF, Apache 2.0). Elimina controller custom + crypto custom. API cria K8s Secret + ExternalSecret CR. |
