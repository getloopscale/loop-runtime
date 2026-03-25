# Sprint 6 — OPERAÇÃO (Day-2)

**Objetivo:** Zero dependência humana para operar falha básica.
**Confiança:** MÉDIA (status), BAIXA (logs, rollback, secrets)
**Esforço:** ~40h+ (features novas — implementação TDD completa)
**Pré-requisito:** GATE-SPRINT-5 passou. Sistema comprovadamente estável.
**Regra:** Uma feature por vez. 3 deploys estáveis entre cada feature.

---

## Ordem (NÃO inverter)

1. **status** — read-only, zero risco, valida dados existentes
2. **logs** — read-only, mas cross-cluster streaming é difícil
3. **rollback** — mutação de estado, requer state machine + gitops reverso
4. **secrets** — controller NOVO + segurança, maior risco

---

## Task 6.1 — `loopzero status`

**Confiança:** MÉDIA
**Esforço:** ~6h
**Por que primeiro:** Read-only. Agrega dados que já existem em 3 stores (builds, releases, environment_targets) + ArgoCD. Zero risco de corrupção.

### API

**Endpoint:** `GET /api/v1/projects/{name}/status?env={env}`

**Response agrega:**
- Último build (id, status, duração, erro se houver)
- Release ativo (id, version/commit, quando ativou)
- ArgoCD sync status (synced/out-of-sync/unknown)
- Pod health (running/desired counts)

**Arquivos:**
- `api/internal/routes/status.go` — novo handler
- `api/internal/db/` — queries nos stores existentes (BuildStore, ReleaseStore, EnvironmentTargetStore)
- `api/internal/argocd/` — query ArgoCD Application status

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

**Gate 6.1:**
- [ ] `loopzero status` mostra dados corretos após deploy
- [ ] `--json` retorna schema estável
- [ ] Testes passam (unit + CLI)
- [ ] 3 deploys estáveis com `loopzero status` verificável

---

## Task 6.2 — `loopzero logs`

**Confiança:** BAIXA
**Esforço:** ~8h

**Por que BAIXA confiança:**
- Cross-cluster log streaming (API no mgmt → pods no build cluster)
- Pods efêmeros (deletados após workflow TTL)
- Long-lived connection para `--follow`
- Edge cases: pod deleted durante streaming, network interruption

### API

**Endpoint:** `GET /api/v1/builds/{id}/logs?follow=true`

**Lógica:**
- Build RUNNING: proxy de pod logs via build cluster client (`corev1.PodLogOptions{Follow: true}`)
- Build terminal + pod existe: pod logs sem follow
- Build terminal + pod deletado: retorna logs do SSE event store (`sse_events` table)
- Build terminal + sem dados: `404` com mensagem "logs expired"

**Arquivos:**
- `api/internal/routes/logs.go` — novo handler
- `api/internal/argo/` — usar build cluster client para PodLog

### CLI

**Comando:** `loopzero logs --build <id> [--follow] [--json]`

**Exit codes:** 0 (success), 1 (usage), 4 (auth)

### TDD

1. Teste: logs de build terminal → retorna content
2. Teste: build inexistente → 404
3. Teste: `--follow` com build ativo → streaming (mock pod logs)
4. Teste: CLI exit codes

**Limitação aceita:** Logs de builds antigos podem não estar disponíveis. Mensagem clara: "Build logs expired after X hours."

**Gate 6.2:**
- [ ] `loopzero logs --build <id>` retorna output
- [ ] `--follow` funciona com build ativo
- [ ] Build antigo sem logs → mensagem clara (não erro genérico)
- [ ] 3 deploys estáveis

---

## Task 6.3 — `loopzero rollback`

**Confiança:** BAIXA
**Esforço:** ~12h

**Por que BAIXA confiança:**
- Pointer swap em `environment_targets` precisa ser atômico (dentro do lock)
- Gitops-push do manifest anterior pode falhar se foi garbage-collected
- Release state machine: ACTIVE → ROLLING_BACK → ROLLED_BACK (transitions específicas)
- ArgoCD sync pode falhar (circuit breaker)
- Testes de rollback são complexos (precisam de 2 deploys + rollback + verificação)

### API

**Endpoint:** `POST /api/v1/rollback`

**Body:** `{ "project": "...", "environment": "...", "to_release_id": "..." }` (optional — default: release anterior)

**Fluxo:**
1. Adquire environment lock
2. Busca release anterior em `environment_targets` (ou `to_release_id` se especificado)
3. Verifica que manifests do release anterior existem no Gitea
4. Cria novo Release record apontando para manifests anteriores
5. Gitops-push dos manifests
6. Triggera ArgoCD sync
7. Pointer swap: `desired_release_id = novo release`
8. Release anterior fica SUPERSEDED, novo fica ACTIVE

**Arquivos:**
- `api/internal/routes/rollback.go` — novo handler
- `api/internal/services/rollback_service.go` — lógica de rollback
- Reutiliza: `lock/`, `gitops/`, `argocd/`, `db/release_store.go`, `db/environment_target_store.go`

### CLI

**Comando:** `loopzero rollback [--env production] [--to <release-id>] [--json]`

**Exit codes:** 0 (success), 1 (usage), 3 (deploy failure), 4 (auth)

### TDD

1. Teste: deploy A → deploy B → rollback → app na versão A
2. Teste: rollback com `--to <id>` específico
3. Teste: rollback de release inexistente → 404
4. Teste: rollback durante build ativo → 409 (environment locked)
5. Teste: dois rollbacks simultâneos → 1× 200, 1× 409

**Gate 6.3:**
- [ ] Deploy A → Deploy B → `loopzero rollback` → app rodando versão A em <30s
- [ ] Release B → SUPERSEDED
- [ ] `--to <id>` funciona
- [ ] Concurrent rollback → 409
- [ ] 3 deploys estáveis pós-rollback

---

## Task 6.4 — `loopzero secret`

**Confiança:** BAIXA
**Esforço:** ~16h+

**Por que por último e BAIXA:**
- AppSecretController NÃO EXISTE — precisa ser implementado do zero
- Cross-namespace Secret sync (mgmt → workload) é operação sensível
- Risco de segurança se mal implementado
- Rotação precisa ser atômica (app não pode ver estado intermediário)
- Maior escopo de implementação de todos os itens

### Controller (NOVO)

**AppSecretController** reconcilia AppSecret CRs:
1. Lê Secret do namespace mgmt (referenciado em `spec.secretRef`)
2. Cria/atualiza Secret no namespace workload (`status.targetNamespace`)
3. Calcula hash SHA256 do conteúdo (`status.workloadHash`)
4. Detecta drift: `status.hashMatch = (mgmtHash == workloadHash)`
5. Rotação: se hash muda, atualiza Secret no workload

**Arquivos:**
- `operators/internal/controller/appsecret_controller.go` — NOVO
- `operators/internal/controller/appsecret_controller_test.go` — NOVO (TDD)
- `operators/cmd/main.go` — wire `SetupAppSecretController`

### API

- `POST /api/v1/secrets` — cria Secret no mgmt namespace + AppSecret CR
- `DELETE /api/v1/secrets/{name}` — remove
- `GET /api/v1/secrets` — já existe (list names only)

### CLI

- `loopzero secret set <name>=<value> [--env] [--app]`
- `loopzero secret list [--env] [--app]`
- `loopzero secret delete <name> [--env] [--app]`

**Segurança:**
- Valores NUNCA em logs, NUNCA em `--json` output, NUNCA em git
- `loopzero secret list` retorna apenas nomes
- Audit log de quem mudou qual secret (tabela `audit_log`)

### TDD

1. Teste: controller — criar AppSecret CR → Secret criado no workload namespace
2. Teste: controller — mudar valor no mgmt → workload atualizado em <30s
3. Teste: controller — deletar AppSecret → Secret removido do workload
4. Teste: API — POST /secrets → Secret + AppSecret CR criados
5. Teste: CLI — `loopzero secret set` → valor nunca aparece no output
6. Teste: CLI — `loopzero secret list` → apenas nomes

**Gate 6.4:**
- [ ] Controller implementado e testado (envtest)
- [ ] `loopzero secret set DB_URL=<value>` → Secret no mgmt + sync para workload
- [ ] `loopzero secret list` → apenas nomes
- [ ] Rotação: set com novo valor → sincronizado em <30s
- [ ] Valores nunca em logs ou output
- [ ] 3 deploys estáveis pós-secrets

---

## GATE-SPRINT-6

- [ ] `loopzero status` funcional
- [ ] `loopzero logs` funcional (com limitação documentada para builds antigos)
- [ ] `loopzero rollback` funcional (rollback em <30s)
- [ ] `loopzero secret` funcional (sync mgmt → workload)
- [ ] Zero regressão em features anteriores
- [ ] 3 deploys consecutivos estáveis com TODAS as features operacionais

**Este sprint encerra o hardening. O sistema está pronto para produção real.**
