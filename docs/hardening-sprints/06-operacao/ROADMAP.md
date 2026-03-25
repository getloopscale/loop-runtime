# Sprint 6 — OPERAÇÃO (Day-2)

**Objetivo:** Zero dependência humana para operar falha básica.
**Confiança:** MÉDIA (status), BAIXA (logs, rollback, secrets)
**Esforço:** ~50h (4 sub-sprints sequenciais)
**Timeline realista:** 3-4 semanas. TDD + gates de 3 deploys estáveis entre cada sub-sprint adicionam overhead significativo. ~2-3h de progresso real por sessão de trabalho. Não pressionar para pular gates.
**Pré-requisito:** GATE-SPRINT-5 passou. Sistema comprovadamente estável.
**Regra:** Uma feature por vez. 3 deploys estáveis entre cada feature. Gate entre sub-sprints.

---

## Estrutura de Sub-Sprints

```
6.1: status   → Wire handler + CLI (já implementado, falta wiring)
    ↓ GATE: loopzero status funcional, 3 deploys estáveis
6A:  logs     → Loki + Promtail + API + CLI (5 tasks)
    ↓ GATE: loopzero logs funcional, 3 deploys estáveis
6B:  rollback → Store queries + orchestrator + gitops read + API + CLI (6 tasks)
    ↓ GATE: loopzero rollback <30s, 3 deploys estáveis
6C:  secrets  → ESO + API mutations + ExternalSecret CRs + CLI (5 tasks)
    ↓ GATE: loopzero secret sync <30s, 3 deploys estáveis
```

## Sub-Sprints

> Cada sub-ROADMAP é a fonte de verdade para esforço, tasks e testes.
> Este arquivo contém apenas resumo e gates.

| Sub-Sprint | Ação | DoD | Esforço | Confiança | ROADMAP |
|---|---|---|---|---|---|
| **6.1** — status | Registrar StatusHandler no router e validar end-to-end | `loopzero status --json` retorna dados corretos de build, release e sync após deploy real | ~2h | MÉDIA | (abaixo neste arquivo) |
| **[6A](./06a-logs/ROADMAP.md)** — logs | Instalar Loki + Promtail e expor build logs via API e CLI | `loopzero logs --build <id>` retorna output completo do build com steps identificados | ~12h | MÉDIA | [06a-logs/ROADMAP.md](./06a-logs/ROADMAP.md) |
| **[6B](./06b-rollback/ROADMAP.md)** — rollback | Implementar rollback orchestrator com lock, state machine e gitops reverso | `loopzero rollback` reverte para release anterior em <30s com concurrent rollback rejeitado (409) | ~20h | BAIXA | [06b-rollback/ROADMAP.md](./06b-rollback/ROADMAP.md) |
| **[6C](./06c-secrets/ROADMAP.md)** — secrets | Instalar ESO e expor CRUD de secrets via API e CLI | `loopzero secret set KEY=val` sincroniza para workload em <30s via ESO, valor nunca visível em output | ~16h | MÉDIA | [06c-secrets/ROADMAP.md](./06c-secrets/ROADMAP.md) |

---

## Ordem (NÃO inverter)

1. **status** — read-only, zero risco, valida dados existentes
2. **logs** — read-only, Loki como backend (não PostgreSQL)
3. **rollback** — mutação de estado, requer state machine + gitops reverso + lock
4. **secrets** — ESO para cross-cluster sync (não controller custom)

---

## Decisões Arquiteturais (Sprint 6)

| Decisão | Escolha | Rejeitado | Justificativa |
|---------|---------|-----------|---------------|
| Log storage | Grafana Loki + MinIO | PostgreSQL `build_logs` table | PG é banco OLTP, não log store. Logs competiriam com advisory locks. Loki integra com Grafana existente e usa MinIO existente. |
| Log shipping | Promtail (DaemonSet) | Captura no reconcile loop | Automático, sem código custom, captura de todos os pods (não apenas terminais). Habilita `--follow` real-time via Loki tail. |
| Secret sync | External Secrets Operator (ESO) | AppSecretController custom | ESO é CNCF, 4.2k+ stars, Apache 2.0, suporta 20+ backends. Elimina ~800 linhas de código custom (controller + crypto + migration). |
| Secret encryption | Delega ao backend (v1: K8s Secrets, futuro: Vault) | AES-256-GCM custom (pkg/crypto) | Não reinventar crypto. ESO permite trocar backend sem mudar código. |
| AppSecret CRD | Remover — usar ExternalSecret CR nativo do ESO | Manter como wrapper | KISS. CRD custom que faz wrap de outro CRD é complexidade sem valor. |
| Secret storage no PG | Coluna `value` (plaintext). Encryption at rest delegada ao PG gerenciado (DigitalOcean managed PG encrypts at rest). | `encrypted_value BYTEA` com `pgcrypto` | v1 não implementa crypto custom. Coluna não pode se chamar "encrypted" sem encryption. Renomear para `value`. Futuro: migrar para Vault via ESO elimina PG como store de secrets. |
| Rollback semântica | Rollback **reativa** a release anterior (SUPERSEDED → ACTIVE), sem criar release nova. Campo `rollback_of` no release record para auditoria. | Criar nova release clone | Evita proliferação de releases fantasma. Segundo rollback tem semântica clara: reverte para a release rolled_back mais recente. |

---

## Task 6.1 — `loopzero status`

**Ação:** Registrar StatusHandler no router e validar end-to-end.
**DoD:** `loopzero status --json` retorna dados corretos de build, release e sync após deploy real.
**Confiança:** MÉDIA
**Esforço:** ~2h
**Por que primeiro:** Read-only. Agrega dados que já existem em 3 stores (builds, releases, environment_targets) + ArgoCD. Zero risco de corrupção.

**Estado atual:** Handler (`api/internal/routes/status.go`) e CLI (`cli/src/commands/status.ts`) implementados com testes passando. **Falta apenas wiring no router** (`api/internal/server/server.go`).

### API

**Endpoint:** `GET /api/v1/projects/{name}/status?env={env}`

**Response agrega:**
- Último build (id, status, duração, erro se houver)
- Release ativo (id, version/commit, quando ativou)
- ArgoCD sync status (synced/out-of-sync/unknown)
- Pod health (running/desired counts)

**Arquivos:**
- `api/internal/routes/status.go` — handler existente (186 linhas)
- `api/internal/server/server.go` — **WIRE AQUI** (seguir padrão de ProjectHandler)
- `api/internal/db/` — queries nos stores existentes (BuildStore, ReleaseStore, EnvironmentTargetStore)

### CLI

**Comando:** `loopzero status [--env production] [--json]`

**Output human:**
```
Environment: production
Last deploy:  2 hours ago (build-a1b2c3d4)
Release:      rel-e5f6g7h8 (active since 14:30)
Sync:         Synced
Pods:         2/2 running
```

**Exit codes:** 0 (success), 1 (usage), 4 (auth)

### TDD

1. Teste: `GET /api/v1/projects/my-app/status?env=production` → 200 com campos obrigatórios
2. Teste: projeto inexistente → 404
3. Teste: CLI `--json` schema
4. Teste: CLI exit codes
5. **Teste de integração: endpoint responde no server real** (previne "implemented but not wired")

**Gate 6.1:**
- [ ] `loopzero status` mostra dados corretos após deploy
- [ ] `--json` retorna schema estável
- [ ] Testes passam (unit + CLI + **integração de wiring**)
- [ ] Endpoint verificado no server.go (não apenas handler isolado)
- [ ] 3 deploys estáveis com `loopzero status` verificável

---

## GATE-SPRINT-6

- [ ] `loopzero status` funcional
- [ ] `loopzero logs` funcional (Loki como backend)
- [ ] `loopzero rollback` funcional (rollback em <30s)
- [ ] `loopzero secret` funcional (ESO sync mgmt → workload)
- [ ] Zero regressão em features anteriores
- [ ] 3 deploys consecutivos estáveis com TODAS as features operacionais

**Este sprint encerra o hardening. O sistema está pronto para produção real.**
