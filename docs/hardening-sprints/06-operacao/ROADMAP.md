# Sprint 6 — OPERAÇÃO (Day-2)

**Objetivo:** Zero dependência humana para operar falha básica.
**Status:** ✅ 6A, 6B, 6C completos e validados no DO. ⚠️ 6.1 (status) falta wiring no router.

---

## Status Real (2026-03-25)

| Sub-Sprint | Status | Evidência no DO |
|---|---|---|
| **6.1** — status | ⚠️ Handler + CLI implementados, **rota NÃO registrada** no server.go | Endpoint retorna 404 no DO |
| **6A** — logs | ✅ **Completo** | `loopzero logs --build <id>` retorna 142 linhas via Loki no DO |
| **6B** — rollback | ✅ **Completo** | `loopzero rollback` reverteu release em 3s no DO |
| **6C** — secrets | ✅ **Completo** | `loopzero secret set KEY=val` → K8s Secret + ExternalSecret → ESO sync <30s no DO |

### Gaps conhecidos

| Gap | Impacto | Ação necessária |
|-----|---------|-----------------|
| `loopzero status` rota não registrada no server.go | Endpoint retorna 404 | Wire StatusHandler no server.go + adicionar ao server.Config |
| Reconcile loop não loga erros de release activation | Builds SUCCEEDED mas releases às vezes não ativam | Adicionar logging no activateRelease (investigar root cause) |
| Promtail 0/1 Ready quando sem builds | Cosmético — funciona quando builds existem | Aceitar como limitação v1 |

---

## Sub-Sprints

> Cada sub-ROADMAP é a fonte de verdade para esforço, tasks e testes.

| Sub-Sprint | Ação | DoD | Status |
|---|---|---|---|
| **6.1** — status | Registrar StatusHandler no router e validar end-to-end | `loopzero status --json` retorna dados corretos após deploy real | ⚠️ Falta wiring |
| **[6A](./06a-logs/ROADMAP.md)** — logs | Instalar Loki + Promtail e expor build logs via API e CLI | `loopzero logs --build <id>` retorna output completo do build | ✅ Validado no DO |
| **[6B](./06b-rollback/ROADMAP.md)** — rollback | Implementar rollback orchestrator com lock, state machine e gitops reverso | `loopzero rollback` reverte em <30s com 409 para concurrent | ✅ Validado no DO |
| **[6C](./06c-secrets/ROADMAP.md)** — secrets | Instalar ESO e expor CRUD de secrets via API e CLI | `loopzero secret set KEY=val` sincroniza para workload em <30s via ESO | ✅ Validado no DO |

---

## Decisões Arquiteturais (Sprint 6)

| Decisão | Escolha | Justificativa |
|---------|---------|---------------|
| Log storage | Grafana Loki + Promtail no build cluster | Purpose-built, integra com Grafana existente, NodePort 30100 para cross-cluster |
| Secret sync | External Secrets Operator (ESO) v0.14.3 no runtime cluster | CNCF, 20+ backends, ClusterSecretStore com bearer token auth para mgmt |
| Rollback semântica | Reativação de release existente (SUPERSEDED → ACTIVE) | Sem releases fantasma, toggle previsível, campo rollback_of para auditoria |
| Secret storage no PG | Coluna `encrypted_value` (BYTEA, plaintext v1) | Managed PG encrypts at rest no disco. Futuro: Vault via ESO |

---

## GATE-SPRINT-6

- [ ] `loopzero status` funcional (⚠️ blocker: wiring no server.go)
- [x] `loopzero logs` funcional (Loki como backend) — validado 2026-03-25
- [x] `loopzero rollback` funcional (rollback em 3s) — validado 2026-03-25
- [x] `loopzero secret` funcional (ESO sync mgmt → workload em <30s) — validado 2026-03-25
- [ ] Zero regressão em features anteriores
- [ ] 3 deploys consecutivos estáveis com TODAS as features operacionais
