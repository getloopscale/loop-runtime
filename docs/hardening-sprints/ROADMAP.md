# ROADMAP — HARDENING SPRINTS

**Objetivo:** Levar o LoopZero de "feature-complete" para "sobrevive à produção real".
**Base:** execution-plan-v2-2026-03-24.md + ROADMAP.md + project-scan
**Data início:** 2026-03-24
**Última atualização:** 2026-03-27

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
Sprint 7: Segurança      → SecurityContext + PSA + NetworkPolicy enforce             🔲 TODO
    ↓ GATE: 3 deploys estáveis, pods non-root, PSA restricted
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
| [07-workload-security](./07-workload-security/ROADMAP.md) | Hardening de segurança do cluster workload. | 🔲 TODO | 7 tasks: SecurityContext, PSA, NetworkPolicy, SA, PG egress, MinIO, digest |

**Feature sprints** (parallel track, product features):

| Sprint | Objetivo | Status | Done |
|---|---|---|---|
| [10-static-frontend-delivery](../sprints/10-static-frontend-delivery/sprint.md) | Frontends estáticos via CF Worker + DO Spaces (zero pods). Cloudflare Tunnel para DDoS/WAF. | ✅ Complete | Monorepo (API+Web+Worker) no DO. Frontend via CF edge. BuildKit single-container. RELEASING state. CF KV routing. |

---

## Sprint 10 — Status Detalhado (2026-03-27)

| Componente | Status | Validado no DO | Evidência |
|---|---|---|---|
| BuildKit single-container | ✅ Completo | ✅ | Eliminou false failure do sidecar. Rootless. |
| RELEASING state machine | ✅ Completo | ✅ | Crash recovery: RUNNING→RELEASING→SUCCEEDED |
| Static asset upload (S3) | ✅ Completo | ✅ | `aws s3 sync --acl public-read` no Argo Workflow |
| CF Tunnel | ✅ Completo | ✅ | `*.dev.loopzero.example` via tunnel, zero IP público |
| CF Worker (static routing) | ✅ Completo | ✅ | KV lookup → DO Spaces fetch, SPA fallback, security headers |
| CF KV auto-write | ✅ Completo | ✅ | API escreve routing entry no build completion |
| Flat hostname format | ✅ Completo | ✅ | `{tenant}-{project}-{app}-{env}.dev.loopzero.example` |
| Pulumi IaC (CF provider) | ✅ Completo | — | `@pulumi/cloudflare`: Worker + KV + Route + CDN |
| CI/CD (GitHub Actions) | ✅ Completo | — | `CLOUDFLARE_API_TOKEN` secret adicionado ao workflow |

### Infraestrutura deployada no DO (Sprint 10)

| Componente | Localização | Status |
|-----------|-------------|--------|
| Cloudflare Tunnel (c35b9d63) | CF edge → runtime cluster | Active, all traffic via CF |
| CF Worker (loopzero-static-delivery) | CF edge | Deployed, route `*.dev.loopzero.example/*` |
| CF KV (loopzero-routing) | CF edge | Active, 28992ed1... |
| DO Spaces CDN (loopzero-static-assets) | sfo3.cdn.digitaloceanspaces.com | Enabled, TTL 3600s |
| Advanced Certificate (*.dev.loopzero.example) | CF | Active |
| nginx-s3-gateway | runtime cluster | Running (fallback, pode ser removido) |

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
| 07-workload-security | ALTA | ~6-8h | — |
| 10-static-frontend | ALTA | ~30h | ~25h (BuildKit: 5h, RELEASING: 3h, S3: 4h, CF Tunnel: 5h, Worker: 4h, Pulumi: 4h) |
