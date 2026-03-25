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
Sprint 6: Operação       → status → logs → rollback → secrets (1 por vez)
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
| [06-operacao](./06-operacao/ROADMAP.md) | Implementar os 4 comandos operacionais (status, logs, rollback, secrets) para que o usuário opere falhas sem depender de kubectl ou intervenção humana. | `loopzero status` mostra estado consolidado + `loopzero logs` retorna output do build + `loopzero rollback` reverte em <30s + `loopzero secret set` sincroniza para workload em <30s. |

---

## Detalhes

| Sprint | Confiança | Esforço | Pré-requisito |
|---|---|---|---|
| 01-runtime | ALTA | ~3h | Sistema base saudável |
| 02-estabilizacao | MÉDIA | ~2h | Sprint 1 GATE passou |
| 03-banco | MÉDIA | ~4h | Sprint 2 GATE passou |
| 04-observabilidade | ALTA | ~8h | Sprint 3 GATE passou |
| 05-pipeline | ALTA | ~4h | Sprint 4 GATE passou |
| 06-operacao | BAIXA-MÉDIA | ~40h+ | Sprint 5 GATE passou |

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
| loopzero status | MÉDIA | Design claro, implementação nova |
| loopzero logs | BAIXA | Cross-cluster streaming difícil |
| loopzero rollback | BAIXA | State machine + gitops reverso + lock |
| loopzero secret | BAIXA | Controller novo + segurança |
