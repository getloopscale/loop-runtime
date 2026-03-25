# Sub-Sprint 6B — `loopzero rollback`

**Ação:** Implementar rollback orchestrator com lock, state machine e gitops reverso.
**DoD:** `loopzero rollback` reverte para release anterior em <30s com concurrent rollback rejeitado (409).
**Confiança:** BAIXA
**Esforço:** ~20h (6 tasks sequenciais)
**Pré-requisito:** GATE-6A passou (`loopzero logs` funcional).
**Regra:** Uma camada por vez. Lock obrigatório. State machine obrigatória. Zero termos internos.

---

## Contexto Técnico

**O que existe:**
- **Rollback state machine** (`pkg/statemachine/rollback.go`) — VALIDATING → REVERTING → SYNCING → COMPLETED + 4 terminais de falha. 8 testes passando.
- **Release state machine** (`pkg/statemachine/release.go`) — Inclui transições ACTIVE → ROLLING_BACK → ROLLED_BACK. SUPERSEDED como terminal.
- **Release store** (`api/internal/db/release_store.go`) — Create, GetByID, UpdateStatus, GetActiveByEnv, GetActiveByEnvTx.
- **Environment target store** (`api/internal/db/environment_target_store.go`) — SetDesired, SetActive, Get. Pointer swap possível.
- **GitOps push** (`api/internal/gitops/gitea_pusher.go`) — Push idempotente (mesmo manifest → noop). Remove funciona.
- **ArgoCD sync** (`api/internal/argocd/sync_client.go`) — TriggerSync + WaitForSync com circuit breaker.
- **Environment lock** (`api/internal/lock/environment_lock.go`) — pg_advisory_xact_lock com 5s timeout.
- **Build orchestrator** (`api/internal/services/build_orchestrator.go`) — Referência de padrão para orquestração (DIP, lock, sync).

**O que NÃO existe:**
- `GetPreviousRelease()` no ReleaseStore
- Rollback orchestrator service
- Endpoint `POST /api/v1/projects/{name}/rollback`
- CLI command `loopzero rollback`
- Tabela/registro de rollback operations (decisão necessária)

**Decisões de design (pré-definidas):**

| Decisão | Escolha | Rejeitado | Justificativa |
|---------|---------|-----------|---------------|
| Onde vive o estado do rollback? | Release-centric (Release.Status = ROLLING_BACK → ROLLED_BACK) | Tabela separada de rollback operations | State machines já suportam. Sem tabela nova. Rollback é uma operação sobre Release, não entidade separada. |
| Onde buscar manifests antigos? | Git history no Gitea | Cache no Release record | Source of truth. Imutável. Auditável. GitOps push já funciona. |
| Rollback implícito ou explícito? | Ambos — default: release anterior. Flag `--to` para específico. | Apenas implícito | UX simples para caso comum, poder para caso complexo. |
| Semântica de rollback | Rollback **reativa** a release anterior (SUPERSEDED/ROLLED_BACK → ACTIVE). Não cria release nova. Campo `rollback_of` adicionado ao Release record para auditoria. | Criar release clone apontando para manifests antigos | Criar releases clone gera "releases fantasma" que poluem o histórico. Após A → B → rollback, o resultado é: A=ACTIVE, B=ROLLED_BACK. Segundo rollback reverte para B (a rolled_back mais recente). Auditável e previsível. |
| Efeitos não-reconhecíveis? | v1: sem verificação automática, mas CLI exibe **warning obrigatório** sobre migrations. `--yes` aceita automaticamente. | Verificação automática de migrations | Verificação automática é complexa (requer introspection do schema). Warning no CLI é suficiente para v1 — o desenvolvedor decide. |
| Concurrent rollback/deploy? | Lock serializa. Quem chegar primeiro ganha. Segundo recebe 409. | Fila de operações | Consistente com padrão existente de concurrent-build check. |

---

## Task 6B.1 — GetPreviousRelease + GetByIDForTenant

**Confiança:** ALTA
**Esforço:** ~2h

### O que fazer

1. **Adicionar** `GetPreviousRelease(ctx, tenantID, projectID, env)` ao ReleaseStore
2. **Adicionar** `GetByIDForTenant(ctx, tenantID, releaseID)` para busca segura por ID com tenant isolation
3. **Adicionar** `ListByEnv(ctx, tenantID, projectID, env, limit)` para listar releases disponíveis para rollback

### Queries

```sql
-- GetPreviousRelease: release anterior ao atualmente ativo
SELECT id, status, git_commit, created_at, ...
FROM releases
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
  AND status IN ('SUPERSEDED', 'ROLLED_BACK')
ORDER BY created_at DESC
LIMIT 1;

-- GetByIDForTenant: busca segura com tenant isolation
SELECT ... FROM releases WHERE id = $1 AND tenant_id = $2;

-- ListByEnv: releases disponíveis para rollback
SELECT ... FROM releases
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
  AND status IN ('ACTIVE', 'SUPERSEDED', 'ROLLED_BACK')
ORDER BY created_at DESC
LIMIT $4;
```

### TDD

1. Teste: 2 releases (ACTIVE + SUPERSEDED) → GetPreviousRelease retorna o SUPERSEDED
2. Teste: apenas 1 release (ACTIVE, sem anterior) → retorna nil (sem erro)
3. Teste: GetByIDForTenant com tenant correto → retorna release
4. Teste: GetByIDForTenant com tenant errado → retorna nil (tenant isolation)
5. Teste: ListByEnv retorna releases ordenados por data DESC

### Gate 6B.1
- [ ] 3 novos métodos no ReleaseStore
- [ ] Tenant isolation verificado
- [ ] 5 testes passam

---

## Task 6B.2 — Rollback orchestrator service

**Confiança:** MÉDIA
**Esforço:** ~6h

### O que fazer

1. **Criar** `api/internal/services/rollback_service.go`
2. **Seguir padrão** do BuildOrchestrator (DIP, interfaces, lock)

### Interface

```go
type RollbackOrchestratorDeps struct {
    Releases        RollbackReleaseStore
    EnvTargets      RollbackEnvTargetStore
    GitOps          RollbackGitOpsPusher
    Sync            RollbackSyncClient
    Lock            RollbackLocker
    Logger          *slog.Logger
}

type RollbackResult struct {
    ReactivatedRelease *Release  // release alvo reativada (para onde reverteu)
    RolledBackRelease  *Release  // release que estava ativa (sendo revertida)
}
```

### Fluxo

```
1. Adquire environment lock (pg_advisory_xact_lock)
   ↓
2. Verifica: existe release ACTIVE? Se não → erro BLOCKED
   ↓
3. Resolve target:
   - Se to_release_id fornecido → GetByIDForTenant (com tenant isolation)
   - Se não → GetPreviousRelease (mais recente SUPERSEDED ou ROLLED_BACK)
   - Se nenhum encontrado → erro REJECTED "No previous release"
   ↓
4. Busca manifests do target release no Gitea (git show {commit}:{path})
   ↓
5. Transiciona release atual: ACTIVE → ROLLING_BACK
   ↓
6. Push manifests antigos para Gitea (gitops repo)
   ↓
7. Trigger ArgoCD sync (com circuit breaker)
   ↓
8. Transiciona release atual: ROLLING_BACK → ROLLED_BACK
   ↓
9. Reativa release target: SUPERSEDED/ROLLED_BACK → ACTIVE
   (adiciona campo rollback_of = ID da release rolled_back para auditoria)
   ↓
10. Atualiza environment_target: active_release_id = target release
    desired_release_id = target release
```

**Semântica de rollback-de-rollback:**
Após A(ACTIVE) → B(deploy) → rollback: A=ACTIVE, B=ROLLED_BACK.
Se rollback novamente: target = B (mais recente ROLLED_BACK). Resultado: B=ACTIVE, A=ROLLED_BACK.
O comportamento é **toggle entre as duas releases mais recentes**.
Para rollback para release específica: `--to <id>`.

### Edge cases

| Caso | Comportamento | Teste |
|------|---------------|-------|
| Nenhuma release anterior | Retorna erro REJECTED | ✅ |
| Deploy em andamento (lock busy) | Retorna 409 (lock timeout) | ✅ |
| Manifests não encontrados no git | Retorna erro REVERT_FAILED | ✅ |
| ArgoCD sync falha | Retorna erro SYNC_FAILED | ✅ |
| Circuit breaker aberto | Retorna erro SYNC_FAILED com mensagem | ✅ |
| Dois rollbacks simultâneos | Primeiro ganha, segundo 409 | ✅ |

### TDD

1. Teste: happy path — 2 releases → rollback → release anterior reativada (SUPERSEDED → ACTIVE)
2. Teste: `to_release_id` específico → rollback para release escolhida (reativação)
3. Teste: sem release anterior → erro REJECTED
4. Teste: deploy em andamento (lock) → 409
5. Teste: ArgoCD sync falha → erro SYNC_FAILED, release fica ROLLBACK_FAILED
6. Teste: dois rollbacks simultâneos → 1× sucesso, 1× 409
7. Teste: manifests não encontrados → erro REVERT_FAILED
8. Teste: tenant isolation (rollback de projeto de outro tenant → 404)
9. Teste: rollback-de-rollback — A(ACTIVE) → B(deploy) → rollback(A ativa) → rollback(B ativa) → toggle correto
10. Teste: release reativada contém campo `rollback_of` com ID da release rolled_back

### Gate 6B.2
- [ ] Orchestrator implementado com DIP
- [ ] Lock adquirido para toda operação
- [ ] Rollback reativa release existente (não cria nova)
- [ ] Campo `rollback_of` presente na release reativada
- [ ] State machine transitions via pkg/statemachine (nunca direto)
- [ ] Rollback-de-rollback funciona (toggle)
- [ ] 10 testes passam

---

## Task 6B.3 — Gitea: buscar manifests de commit anterior

**Confiança:** MÉDIA
**Esforço:** ~3h

### O que fazer

1. **Adicionar** método ao GitOps pusher (ou novo client) para buscar conteúdo de arquivo em commit específico
2. **Endpoint Gitea:** `GET /api/v1/repos/{owner}/{repo}/raw/{filepath}?ref={commit_sha}`

### Interface

```go
type GitOpsReader interface {
    GetManifestsAtCommit(ctx context.Context, tenant, project, env, commitSHA string) ([]byte, error)
}
```

### TDD

1. Teste: commit existe → retorna YAML completo
2. Teste: commit não existe → erro claro (não panic)
3. Teste: path não existe no commit → erro "manifests not found at commit {sha}"

### Gate 6B.3
- [ ] Método busca manifests de qualquer commit
- [ ] Erro claro quando commit/path não existe
- [ ] 3 testes passam

---

## Task 6B.4 — API endpoint

**Confiança:** ALTA
**Esforço:** ~3h

### O que fazer

1. **Criar** `api/internal/routes/rollback.go`
2. **Endpoint:** `POST /api/v1/projects/{name}/rollback`
3. **Middleware:** Auth + Idempotency-Key + EnvironmentLock
4. **Wiring** no router em `main.go`

### Request

```json
{
  "environment": "production",
  "to_release_id": "rel-abc123"
}
```

`to_release_id` é opcional. Se omitido, reverte para release imediatamente anterior.

### Response (200)

```json
{
  "status": "completed",
  "rolled_back_release": "rel-newest",
  "reactivated_release": "rel-older",
  "environment": "production",
  "url": "https://my-app-production.dev.loopzero.example"
}
```

### Response (409 — concurrent operation)

```json
{
  "code": "DEPLOY_IN_PROGRESS",
  "message": "Another operation is in progress. Try again in a moment."
}
```

### Response (404 — no previous release)

```json
{
  "code": "NO_PREVIOUS_RELEASE",
  "message": "No previous release found to rollback to. Deploy at least twice before rolling back."
}
```

### TDD

1. Teste: POST com projeto válido → 200 com release revertida
2. Teste: POST com `to_release_id` → 200 rollback para release específica
3. Teste: projeto inexistente → 404
4. Teste: sem release anterior → 404 com código NO_PREVIOUS_RELEASE
5. Teste: sem auth → 401
6. Teste: concurrent operation → 409 com código DEPLOY_IN_PROGRESS
7. Teste: idempotency-key replay → mesma response
8. Teste: tenant isolation → 404 para projeto de outro tenant

### Gate 6B.4
- [ ] Endpoint funcional com lock + idempotency
- [ ] Wired no router (verificar em main.go)
- [ ] 8 testes passam

---

## Task 6B.5 — CLI command

**Confiança:** ALTA
**Esforço:** ~3h

### O que fazer

1. **Criar** `cli/src/commands/rollback.ts`
2. **Wiring** em `cli/src/index.ts`
3. **Flags:** `--env <name>` (default: production), `--to <release-id>`, `--json`, `--yes` (skip confirmation)

### Output human

```
Warning: Rollback does not revert database migrations.
  If the deployment you're reverting from included schema changes,
  the previous version may not be compatible with the current database.

Rolling back production...

  Previous: rel-abc123 (deployed 2h ago)
  Current:  rel-def456 (deployed 30m ago)

Rollback completed in 12s.
Active release: rel-abc123
URL: https://my-app-production.dev.loopzero.example
```

O warning aparece **sempre** (exceto com `--yes`). É a primeira coisa exibida antes de iniciar a operação.
Em modo interativo (sem `--yes`), aguarda confirmação `Continue? (y/N)` após o warning.

### Output JSON (`--json`)

```json
{
  "status": "completed",
  "rolled_back_release": "rel-def456",
  "active_release": "rel-abc123",
  "environment": "production",
  "duration_seconds": 12,
  "url": "https://my-app-production.dev.loopzero.example"
}
```

### Exit codes

| Code | Significado |
|------|-------------|
| 0 | Sucesso |
| 1 | Uso inválido |
| 3 | Rollback falhou (sync failure, manifests not found) |
| 4 | Auth error |

### TDD

1. Teste: rollback sucesso → exit 0, output contém release IDs
2. Teste: sem auth → exit 4, mensagem sugere `loopzero login`
3. Teste: sem loopzero.yaml → exit 1, mensagem sugere `loopzero init`
4. Teste: API retorna 409 → exit 3, mensagem "Another operation is in progress"
5. Teste: API retorna 404 (no previous) → exit 3, mensagem "No previous release"
6. Teste: `--json` → JSON válido com schema
7. Teste: `--to rel-abc123` → passa release_id para API
8. Teste: zero termos internos em qualquer cenário
9. Teste: rollback exibe warning sobre database migrations antes de executar
10. Teste: `--yes` pula confirmação mas warning ainda aparece no output

### Gate 6B.5
- [ ] `loopzero rollback` reverte para release anterior
- [ ] `--to <id>` funciona
- [ ] `--json` shape estável
- [ ] Warning sobre migrations exibido
- [ ] Zero termos internos
- [ ] 10 testes passam

---

## Task 6B.6 — Validação E2E

**Confiança:** MÉDIA
**Esforço:** ~3h

### O que fazer

1. **Teste E2E manual** (ou script):
   - Deploy versão A → confirma URL servindo A
   - Deploy versão B → confirma URL servindo B
   - `loopzero rollback` → confirma URL servindo A em <30s
   - `loopzero status` → confirma release anterior como ACTIVE
2. **Verificar**: release B marcada como ROLLED_BACK no DB
3. **Verificar**: release A reativada como ACTIVE (com `rollback_of` = B.id)
4. **Verificar**: ArgoCD Application synced com manifests de A
5. **Verificar**: rollback novamente → B reativada, A rolled_back (toggle funciona)

### Gate 6B.6
- [ ] Deploy A → Deploy B → Rollback → App na versão A em <30s
- [ ] Release B status = ROLLED_BACK
- [ ] `loopzero status` reflete estado correto
- [ ] 3 deploys + rollback estáveis

---

## Limitações Aceitas (v1)

| Limitação | Justificativa | Futuro |
|-----------|---------------|--------|
| Sem verificação automática de efeitos colaterais (migrations) | CLI exibe warning obrigatório. Desenvolvedor decide. Verificação automática requer schema introspection (complexo). | Adicionar `--acknowledge-effects` flag para CI sem warning |
| Sem rollback parcial (por app em monorepo) | Monorepo rollback é all-or-nothing | Granularidade por app se demanda existir |
| Sem SSE streaming do progresso | Rollback é rápido (<30s) | Adicionar se rollbacks ficarem lentos |
| Manifests via git history (network call) | Gitea é local (low latency) | Cache manifests no Release record |

---

## GATE-6B

- [ ] `loopzero rollback` reverte em <30s
- [ ] `loopzero rollback --to <id>` funciona
- [ ] Concurrent rollback/deploy → 409
- [ ] Sem release anterior → mensagem clara
- [ ] Release revertida marcada ROLLED_BACK
- [ ] `loopzero status` reflete estado pós-rollback
- [ ] Zero termos internos
- [ ] Testes passam (unit API + unit CLI + E2E)
- [ ] 3 deploys estáveis pós-rollback
