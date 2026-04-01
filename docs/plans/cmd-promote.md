# Plano: `loopzero promote` — Promover release entre ambientes

> **Objetivo:** Permitir que o usuario promova uma release ACTIVE de um ambiente para outro sem rebuild. A imagem ja construida e reutilizada; apenas os manifests sao re-gerados para o ambiente destino.
>
> **Escopo:** API (service + route + DB), CLI (command), testes (unit + integration + E2E)
>
> **Classificacao:** T2 (toca API contracts, state machine, DB schema, CLI output)
>
> **Status:** DRAFT — aguardando meeting review
>
> **Sintaxe:** `loopzero promote staging production [--project <name>]`

---

## Contexto

### Problema

Hoje, para levar uma release de staging para production, o usuario precisa rodar `loopzero deploy` novamente. Isso dispara um novo build (Argo Workflows + BuildKit), mesmo que a imagem ja esteja pronta. O promote elimina esse rebuild: pega a release ACTIVE do ambiente source, re-gera os manifests para o ambiente target (hostname, namespace, TLS), faz gitops push, e aciona sync.

### O que ja existe

| Mecanismo | Status | Localizacao |
|-----------|--------|-------------|
| PromotionMachine (state machine) | OK — 7 states, 6 events | `pkg/statemachine/promotion.go` |
| RollbackOrchestrator (padrao analogo) | OK — lock + delivery + finalize | `api/internal/services/rollback_orchestrator.go` |
| ContainerDeliveryActivator | OK — gitops push + ArgoCD sync | `api/internal/services/container_delivery.go` |
| StaticDeliveryActivator | OK — KV write + S3 | `api/internal/services/static_delivery.go` |
| Manifest renderer | OK — `manifests.Render(RenderInput)` | `api/internal/manifests/renderer.go` |
| RollbackHandler (padrao de route) | OK — project lookup + service call | `api/internal/routes/rollback.go` |
| RollbackCommand (padrao de CLI) | OK — auth + yaml + confirmation + JSON | `cli/src/commands/rollback.ts` |
| Release model | OK — `db.Release` com apps, artifacts, git_commit | `api/internal/db/release_store.go` |
| Environment lock | OK — `pg_advisory_xact_lock` | `api/internal/lock/` |
| ActivateReleaseKernel | OK — single source of truth para ativacao | `api/internal/services/release_activator.go` |

### O que precisa ser criado

```
1. PromotionOrchestrator (service)           — api/internal/services/promotion_orchestrator.go
2. DB: promotions table + store              — api/internal/db/promotion_store.go + migration
3. PromotionHandler (route)                  — api/internal/routes/promote.go
4. PromoteCommand (CLI)                      — cli/src/commands/promote.ts
5. Testes (unit + integration)               — api/tests/unit/ + cli/tests/unit/
6. Wiring (main.go + adapters)               — api/cmd/ + api/internal/adapters/
7. E2E validation                            — infra/tests/e2e/
```

---

## Design Decisions (requerem meeting)

### D1 — Promote cria uma NOVA release no target env

Promote nao reutiliza a release do source. Ele cria uma nova release no target environment, referenciando o mesmo build (mesma imagem). Motivo: cada environment tem sua propria cadeia de releases, com estados independentes. Uma release ACTIVE em staging nao pode ser simultaneamente ACTIVE em production — sao namespaces, hostnames, e TLS certs diferentes.

**Proposta:**
- Nova release no target com `build_id` apontando para o build original
- Campo `promoted_from_release_id` na nova release (rastreabilidade)
- Campo `promoted_from_env` na nova release

### D2 — Source env DEVE ter release ACTIVE

Se o source environment nao tem nenhuma release ACTIVE, o promote falha com erro claro. Nao ha sentido em promover algo que nao esta rodando.

**Pergunta para meeting:** Devemos exigir que a release esteja HEALTHY (ArgoCD healthy) alem de ACTIVE? Ou basta estar ACTIVE?

**Proposta:** Apenas ACTIVE. O usuario pode querer promover uma release que funciona em staging mesmo que o health check do ArgoCD esteja com flapping. Health check e responsabilidade do target env apos sync.

### D3 — Target env com release ACTIVE existente: supersede normalmente

Se o target env ja tem uma release ACTIVE, o fluxo de ativacao (ActivateReleaseKernel) ja faz supersede da release anterior. Nao precisa de logica especial — o mesmo kernel usado por deploy e rollback trata isso.

### D4 — Sem cross-project promotion

Promote opera dentro do mesmo project. `loopzero promote staging production` promove o project do `loopzero.yaml` atual. Cross-project promotion nao esta no escopo (YAGNI).

### D5 — Promotions table vs campo na releases

**Opcao A:** Tabela `promotions` separada com tracking do fluxo (state machine, timestamps).
**Opcao B:** Campos `promoted_from_release_id` e `promoted_from_env` na tabela `releases`.

**Proposta:** Opcao A — tabela separada. Motivo:
- O state machine de promotion (VALIDATING -> COMMITTING -> SYNCING -> COMPLETED) e diferente do release state machine
- Permite auditoria: quem promoveu, quando, de onde para onde
- A release criada no target e uma release normal — apenas referenciada pela promotion
- Analogia: rollback tambem nao cria campos extras na release, ele e um fluxo que manipula releases existentes

### D6 — Promote para static apps

Para apps com `rendering: static`, promote deve re-escrever a entry no CF KV para o subdomain do target environment, apontando para os mesmos assets no S3 (mesmo build-id, mesmo s3Prefix). Nao ha necessidade de re-upload — os assets sao imutaveis por build-id.

---

## Schema do banco

### Migration: `000012_promotions.up.sql`

```sql
CREATE TABLE IF NOT EXISTS promotions (
    id              TEXT PRIMARY KEY,
    tenant_id       TEXT NOT NULL,
    project_id      TEXT NOT NULL,
    source_env      TEXT NOT NULL,
    target_env      TEXT NOT NULL,
    source_release_id TEXT NOT NULL,
    target_release_id TEXT,           -- preenchido apos criacao da release no target
    status          TEXT NOT NULL DEFAULT 'VALIDATING',
    correlation_id  TEXT NOT NULL DEFAULT '',
    created_by      TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at    TIMESTAMPTZ,
    error_message   TEXT,
    CONSTRAINT fk_promotions_source_release FOREIGN KEY (source_release_id) REFERENCES releases(id)
);

CREATE INDEX idx_promotions_tenant_project ON promotions (tenant_id, project_id);
CREATE INDEX idx_promotions_status ON promotions (status);
```

---

## Task 1 — PromotionStore (DB layer)

**Arquivo:** `api/internal/db/promotion_store.go`

**Modelo:**

```go
type Promotion struct {
    ID              string     `json:"id"`
    TenantID        string     `json:"tenant_id"`
    ProjectID       string     `json:"project_id"`
    SourceEnv       string     `json:"source_env"`
    TargetEnv       string     `json:"target_env"`
    SourceReleaseID string     `json:"source_release_id"`
    TargetReleaseID string     `json:"target_release_id"`
    Status          string     `json:"status"`
    CorrelationID   string     `json:"correlation_id"`
    CreatedBy       string     `json:"created_by"`
    CreatedAt       time.Time  `json:"created_at"`
    CompletedAt     *time.Time `json:"completed_at,omitempty"`
    ErrorMessage    string     `json:"error_message,omitempty"`
}
```

**Interface:**

```go
type PromotionStore interface {
    Create(ctx context.Context, tx DBTX, promo *Promotion) error
    GetByID(ctx context.Context, id string) (*Promotion, error)
    UpdateStatus(ctx context.Context, tx DBTX, id string, event string) (*Promotion, error)
    SetTargetReleaseID(ctx context.Context, tx DBTX, id, releaseID string) error
    SetErrorMessage(ctx context.Context, tx DBTX, id, msg string) error
}
```

**State machine:** usa `statemachine.NewPromotionMachine()` para validar transicoes.

**TDD:** Testes unitarios para Create, UpdateStatus (transicoes validas e invalidas), SetTargetReleaseID.

**DoD:**
- [ ] Migration `000012_promotions.up.sql` criada e aplicavel (`task db:migrate` passa)
- [ ] Modelo `Promotion` struct definido com todos os campos
- [ ] `PromotionStore` interface definida (Create, GetByID, UpdateStatus, SetTargetReleaseID, SetErrorMessage)
- [ ] Implementacao concreta usando pgx pool
- [ ] `UpdateStatus` usa `statemachine.NewPromotionMachine()` para validar transicoes
- [ ] Teste unitario: Create insere registro corretamente
- [ ] Teste unitario: UpdateStatus com transicao valida (VALIDATING → COMMITTING) funciona
- [ ] Teste unitario: UpdateStatus com transicao invalida retorna erro
- [ ] Teste unitario: SetTargetReleaseID atualiza campo corretamente
- [ ] Teste unitario: SetErrorMessage atualiza campo corretamente
- [ ] `go test ./...` passa no modulo api
- [ ] `task lint` passa

---

## Task 2 — PromotionOrchestrator (service)

**Arquivo:** `api/internal/services/promotion_orchestrator.go`

**Padrao:** segue exatamente o RollbackOrchestrator — DIP com interfaces, advisory lock no target env.

### Dependencias (DIP)

```go
type PromotionOrchestratorDeps struct {
    Promotions   PromotionStore
    Releases     PromotionReleaseStore  // GetActiveByEnvTx, Create, UpdateStatus
    EnvTargets   PromotionEnvTargetStore
    GitPusher    PromotionGitOpsPusher
    Sync         PromotionSyncClient
    Locker       EnvironmentLocker
    KVClient     StaticRoutingClient     // CF KV para static apps (nil = disabled)
    Logger       *slog.Logger
    BaseDomain   string
    RegistryURL  string
    S3Endpoint   string
    S3BucketName string
}
```

### Request/Response

```go
type PromoteRequest struct {
    TenantID      string
    ProjectID     string
    ProjectName   string
    SourceEnv     string
    TargetEnv     string
    CorrelationID string
    UserID        string
}

type PromoteResult struct {
    PromotionID     string
    SourceReleaseID string
    TargetReleaseID string
    SourceEnv       string
    TargetEnv       string
    URL             string
}
```

### Fluxo `Promote(ctx, req) (*PromoteResult, error)`

```
Phase 1 (NO lock): Validate
  1. Buscar release ACTIVE no source env
     → Se nao existe: return ErrNoActiveRelease
  2. Validar que source_env != target_env
     → Se iguais: return ErrSameEnvironment (novo erro)
  3. Criar registro Promotion (VALIDATING)
  4. Transition: VALIDATING → COMMITTING (ValidationPassed)

Phase 2 (LOCK no target env): Create release + push manifests
  5. Extrair apps da source release (json.Unmarshal release.Apps)
  6. Re-gerar manifests para target env:
     - Mesmo ImageRef (mesma imagem, sem rebuild)
     - Novo Environment (namespace, hostname)
     - Novo ReleaseID
     manifests.Render(RenderInput{
         TenantID:    req.TenantID,
         ProjectID:   req.ProjectID,
         Environment: req.TargetEnv,  // <-- target
         BuildID:     sourceRelease.BuildID,
         ReleaseID:   newRelease.ID,
         BaseDomain:  deps.BaseDomain,
         Apps:        targetApps,     // mesmas apps, mesmas imagens
     })
  7. Criar nova Release no target env (status READY, build_id do source)
  8. Push manifests para Gitea (gitops repo)
  9. Transition promotion: COMMITTING → SYNCING (CommitCompleted)

Phase 3 (NO lock): Sync
  10. Trigger ArgoCD sync (triggerSyncWithRetry)
      argoAppName = "{tenantID}-{targetEnv}"
  11. Se sync trigger falha: transition → SYNC_FAILED

Phase 4 (LOCK no target env): Activate
  12. ActivateReleaseKernel (mesma funcao de deploy e rollback)
      - Supersede release anterior no target
      - Set envtarget desired + active
      - Emit deploy.completed SSE
  13. Transition promotion: SYNCING → COMPLETED (SyncCompleted)
  14. Set promotion.TargetReleaseID
  15. Set promotion.CompletedAt

Static apps (adicional):
  - Se app tem rendering: static, write CF KV entry para target env subdomain
    apontando para mesmo buildId/s3Prefix do source
```

### Tratamento de erro

```go
func (o *PromotionOrchestrator) failPromotion(ctx context.Context, promotionID, event, errMsg string) error {
    // Transition to terminal error state (REJECTED, COMMIT_FAILED, or SYNC_FAILED)
    // Set error message
    // Return original error
}
```

**TDD:** Testes unitarios cobrindo:
- Happy path: source ACTIVE, manifests gerados, push ok, sync ok, release ativada
- Source sem release ACTIVE → ErrNoActiveRelease
- Source == target → ErrSameEnvironment
- GitOps push falha → COMMIT_FAILED
- Sync falha → SYNC_FAILED
- Advisory lock timeout no target → 409
- Target ja tem release ACTIVE → supersede funciona normalmente

**DoD:**
- [ ] `PromotionOrchestrator` struct criado com `PromotionOrchestratorDeps`
- [ ] Metodo `Promote(ctx, PromoteRequest) (*PromoteResult, error)` implementado
- [ ] Phase 1 (no lock): busca release ACTIVE no source, valida source != target, cria Promotion
- [ ] Phase 2 (lock no target): cria release, re-gera manifests, push gitops
- [ ] Phase 3 (no lock): trigger ArgoCD sync
- [ ] Phase 4 (lock no target): ActivateReleaseKernel (supersede + envtarget + SSE)
- [ ] Static app support: CF KV write para target env subdomain
- [ ] `failPromotion()` trata transicoes para estados terminais de erro
- [ ] Teste unitario: happy path — source ACTIVE, manifests ok, sync ok, release ativada
- [ ] Teste unitario: source sem release ACTIVE → ErrNoActiveRelease
- [ ] Teste unitario: source == target → ErrSameEnvironment
- [ ] Teste unitario: gitops push falha → COMMIT_FAILED
- [ ] Teste unitario: sync falha → SYNC_FAILED
- [ ] Teste unitario: advisory lock timeout no target → 409
- [ ] Teste unitario: target com release ACTIVE existente → supersede funciona
- [ ] `go test ./...` passa no modulo api
- [ ] `task lint` passa

---

## Task 3 — API endpoint

**Arquivo:** `api/internal/routes/promote.go`

**Endpoint:** `POST /api/v1/projects/{name}/promote`

**Padrao:** identico ao RollbackHandler.

### Request body

```json
{
  "source_env": "staging",
  "target_env": "production"
}
```

### Response (200 OK)

```json
{
  "status": "completed",
  "promotion_id": "prm-abc123",
  "source_env": "staging",
  "target_env": "production",
  "source_release": "rel-xyz789",
  "target_release": "rel-def456",
  "url": "https://myproject-production.dev.loopzero.example"
}
```

### Error responses

| Code | Condition | Body |
|------|-----------|------|
| 400 | Missing source_env or target_env | `{ "code": "MISSING_FIELD", "message": "..." }` |
| 400 | source_env == target_env | `{ "code": "SAME_ENVIRONMENT", "message": "..." }` |
| 401 | No auth | `{ "code": "UNAUTHORIZED", "message": "..." }` |
| 404 | Project not found | `{ "code": "PROJECT_NOT_FOUND", "message": "..." }` |
| 404 | No active release in source | `{ "code": "NO_ACTIVE_RELEASE", "message": "..." }` |
| 409 | Lock timeout (another operation) | `{ "code": "DEPLOY_IN_PROGRESS", "message": "..." }` |
| 500 | Internal error | `{ "code": "INTERNAL_ERROR", "message": "..." }` |

### Handler

```go
type PromoteHandler struct {
    projects   PromoteProjectStore
    service    PromoteService
    baseDomain string
}

func (h *PromoteHandler) Create(w http.ResponseWriter, r *http.Request) {
    // 1. Auth check (middleware.GetUserContext)
    // 2. Parse project name from URL
    // 3. Decode request body (source_env, target_env)
    // 4. Validate source_env != target_env
    // 5. Lookup project by name
    // 6. Call service.Promote(ctx, PromoteInput{...})
    // 7. Return 200 with result or typed error
}
```

### Wiring

Em `api/cmd/` ou `api/internal/adapters/`, registrar:
```go
r.Post("/api/v1/projects/{name}/promote", promoteHandler.Create)
```

**TDD:** Testes unitarios para:
- Happy path → 200
- Missing fields → 400
- Project not found → 404
- No active release → 404
- Lock timeout → 409
- Auth missing → 401

**DoD:**
- [ ] `PromoteHandler` struct criado com `PromoteProjectStore` e `PromoteService` interfaces
- [ ] Endpoint `POST /api/v1/projects/{name}/promote` registrado
- [ ] Request body parsed: `source_env`, `target_env`
- [ ] Validacao: missing fields → 400, source == target → 400
- [ ] Auth middleware ativo na rota
- [ ] Idempotency middleware ativo na rota
- [ ] Correlation-ID forwarded ao service
- [ ] Response 200 com shape documentada (promotion_id, source/target env/release, url)
- [ ] Error responses tipadas: 400, 401, 404, 409, 500 (conforme tabela)
- [ ] Teste unitario: happy path → 200
- [ ] Teste unitario: missing fields → 400
- [ ] Teste unitario: same environment → 400
- [ ] Teste unitario: project not found → 404
- [ ] Teste unitario: no active release → 404
- [ ] Teste unitario: lock timeout → 409
- [ ] Teste unitario: auth missing → 401
- [ ] `go test ./...` passa no modulo api
- [ ] `task lint` passa

---

## Task 4 — CLI command

**Arquivo:** `cli/src/commands/promote.ts`

**Sintaxe:**

```
loopzero promote <source-env> <target-env> [options]

Options:
  --project <name>   Project name (default: from loopzero.yaml)
  --json             Output as JSON
  --yes              Skip confirmation prompt
```

### Exemplos

```bash
loopzero promote staging production
loopzero promote staging production --project my-api
loopzero promote staging production --json
loopzero promote staging production --yes   # skip confirmation in CI
```

### Exit codes

| Code | Condition |
|------|-----------|
| 0 | Promotion completed successfully |
| 1 | Usage error (bad arguments, missing flags, no loopzero.yaml) |
| 3 | Promotion failed (sync failed, commit failed) |
| 4 | Auth error (not logged in) |

### Human output

```
Promoting staging → production for project my-api...

  Warning: Promote does not revert database migrations.
    If the target environment has a different schema version,
    the promoted release may not be compatible.

  Promote staging → production? [y/N] y

  Source:  staging (release rel-xyz789)
  Target:  production

  Generating manifests for production...  done
  Pushing deployment configuration...     done
  Waiting for deployment to be live...    done

  Promotion completed in 12s

  URL: https://my-api-production.dev.loopzero.example
```

### JSON output

```json
{
  "ok": true,
  "promotion_id": "prm-abc123",
  "source_env": "staging",
  "target_env": "production",
  "source_release_id": "rel-xyz789",
  "target_release_id": "rel-def456",
  "url": "https://my-api-production.dev.loopzero.example"
}
```

### Error JSON

```json
{
  "ok": false,
  "error": {
    "code": "NO_ACTIVE_RELEASE",
    "message": "No active release in staging. Deploy to staging first: loopzero deploy --env staging"
  }
}
```

### Rule 3 compliance

Nenhum termo interno: sem "ArgoCD", "GitOps", "gitops-push", "Gitea", etc. Mensagens usam linguagem de produto:
- "Pushing deployment configuration" (nao "gitops push to Gitea")
- "Waiting for deployment to be live" (nao "ArgoCD sync")
- "Generating manifests" (nao "rendering K8s YAML")

### Implementacao

```typescript
export interface PromoteDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  promote: (project: string, sourceEnv: string, targetEnv: string) => Promise<PromoteApiResponse>;
  confirm?: (message: string) => Promise<boolean>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
  isCI?: boolean;
}

export interface PromoteOptions {
  project?: string;
  json?: boolean;
  yes?: boolean;
}

export class PromoteCommand {
  constructor(private readonly deps: PromoteDeps) {}

  async execute(sourceEnv: string, targetEnv: string, opts: PromoteOptions = {}): Promise<number> {
    // 1. Auth check
    // 2. Find loopzero.yaml (or use --project)
    // 3. Validate args (source != target)
    // 4. Show migration warning
    // 5. Confirmation prompt (unless --yes or CI)
    // 6. Call API
    // 7. Display result (human or JSON)
    // 8. Return exit code
  }
}
```

**TDD:** Testes unitarios cobrindo:
- Happy path (human output + JSON output)
- Auth error → exit 4
- No loopzero.yaml → exit 1
- Missing args → exit 1
- Same env → exit 1
- API error (no active release) → exit 3
- API error (lock timeout) → exit 3
- Confirmation denied → exit 0
- CI mode sem --yes → exit 1
- JSON output shape (field names are stability contract)
- No internal terms in any output string

**DoD:**
- [ ] `PromoteCommand` class implementada com DI via `PromoteDeps`
- [ ] Auth check: sem token → exit 4
- [ ] `loopzero.yaml` lookup: sem arquivo e sem `--project` → exit 1
- [ ] Validacao: source == target → exit 1
- [ ] Validacao: args faltando → exit 1
- [ ] Migration warning exibido antes da confirmacao
- [ ] Confirmation prompt: `--yes` ou `CI=true` pula, negacao → exit 0
- [ ] Human output: progress steps + URL final (conforme template)
- [ ] `--json` output: shape com `ok`, `promotion_id`, `source_env`, `target_env`, `source_release_id`, `target_release_id`, `url`
- [ ] Error JSON shape: `ok: false`, `error.code`, `error.message`
- [ ] Exit codes corretos: 0 (sucesso), 1 (usage), 3 (falha), 4 (auth)
- [ ] Zero internal terms em qualquer string (Rule 3)
- [ ] CI mode: sem spinners, sem color
- [ ] Teste unitario: happy path (human output)
- [ ] Teste unitario: happy path (JSON output)
- [ ] Teste unitario: auth error → exit 4
- [ ] Teste unitario: no loopzero.yaml → exit 1
- [ ] Teste unitario: missing args → exit 1
- [ ] Teste unitario: same env → exit 1
- [ ] Teste unitario: API error (no active release) → exit 3
- [ ] Teste unitario: API error (lock timeout) → exit 3
- [ ] Teste unitario: confirmation denied → exit 0
- [ ] Teste unitario: CI mode sem --yes → exit 1
- [ ] Teste unitario: JSON output shape (field names stability contract)
- [ ] Teste unitario: no internal terms in any output string
- [ ] `npm test` passa
- [ ] `npm run build` passa

---

## Task 5 — Novos typed errors

**Arquivo:** `pkg/errors/errors.go`

```go
var ErrSameEnvironment = NewLoopZeroError(
    "SAME_ENVIRONMENT",
    "Source and target environments must be different",
    400,
)
```

`ErrNoActiveRelease` ja existe (usado pelo rollback).

**DoD:**
- [ ] `ErrSameEnvironment` definido em `pkg/errors/errors.go` com code, message, HTTP 400
- [ ] Teste unitario: `ErrSameEnvironment` tem code `SAME_ENVIRONMENT` e status 400
- [ ] `go test ./...` passa no modulo pkg
- [ ] `task lint` passa

---

## Task 6 — Wiring (integracao)

**Arquivos:**
- `api/cmd/` ou `api/internal/server/` — registrar rota
- `api/internal/adapters/` — criar adapter para PromoteService interface

**DoD:**
- [ ] PromotionStore criado com pool do pgx
- [ ] PromotionOrchestrator criado com todas as deps injetadas
- [ ] PromoteHandler registrado em `POST /api/v1/projects/{name}/promote`
- [ ] Middleware de auth ativo na rota
- [ ] Middleware de idempotency ativo na rota
- [ ] CLI command registrado no Commander
- [ ] `curl -X POST /api/v1/projects/myapp/promote` funciona (wiring API confirmado)
- [ ] `loopzero promote staging production` funciona end-to-end (wiring CLI confirmado)
- [ ] `npm run build` passa (CLI)
- [ ] `go test ./...` passa (API)
- [ ] `task lint` passa

**Padrao "implemented but not wired":** verificar TODOS os call sites apos implementacao.

---

## Task 7 — Testes de integracao

**Arquivo:** `api/tests/integration/promote_test.go` (ou `api/tests/unit/promote_test.go`)

**Cenarios criticos:**

1. **Concurrent promote + deploy:** promote no target env enquanto deploy esta rodando no target
   - Esperado: um dos dois recebe 409 (lock contention)

2. **Promote + rollback race:** promote em andamento, usuario faz rollback no target
   - Esperado: um dos dois recebe 409

3. **Promote de release com static app:** source tem app com `rendering: static`
   - Esperado: CF KV entry criada para target env, mesmos assets

4. **Promote quando target ja tem ACTIVE:** promote deve supersede a release anterior
   - Esperado: release anterior fica SUPERSEDED, nova fica ACTIVE

5. **Double promote (idempotency):** mesmo Idempotency-Key
   - Esperado: segunda chamada retorna mesmo resultado sem side effects

**DoD:**
- [ ] Teste: concurrent promote + deploy no mesmo target → exatamente um 409
- [ ] Teste: concurrent promote + rollback no mesmo target → exatamente um 409
- [ ] Teste: promote de release com static app → CF KV entry criada para target
- [ ] Teste: promote quando target ja tem ACTIVE → release anterior fica SUPERSEDED
- [ ] Teste: double promote com mesmo Idempotency-Key → mesma resposta, sem side effects
- [ ] Todos os testes deterministicos (zero flaky)
- [ ] `go test ./...` passa no modulo api
- [ ] `task lint` passa

---

## Task 8 — E2E validation

**Prerequisitos:** ambiente local (k3d) ou DigitalOcean dev.

**Fluxo de validacao:**

```bash
# 1. Deploy para staging
loopzero deploy --env staging

# 2. Confirmar que esta ACTIVE
loopzero status --env staging

# 3. Promover para production
loopzero promote staging production

# 4. Confirmar que production esta ACTIVE com mesma imagem
loopzero status --env production

# 5. Confirmar que staging continua ACTIVE (promote nao afeta source)
loopzero status --env staging

# 6. Verificar URL de production
curl -s https://myproject-production.dev.loopzero.example/health

# 7. Rollback no production (verificar que funciona apos promote)
loopzero rollback --env production
```

**DoD:**
- [ ] Deploy para staging funciona (`loopzero deploy --env staging`)
- [ ] `loopzero status --env staging` mostra release ACTIVE
- [ ] `loopzero promote staging production` completa com sucesso e mostra URL
- [ ] `loopzero status --env production` mostra release ACTIVE com mesma imagem do staging
- [ ] `loopzero status --env staging` continua ACTIVE (promote nao afeta source)
- [ ] URL de production responde (`curl` retorna 200)
- [ ] `loopzero rollback --env production` funciona apos promote
- [ ] Validado em ambiente DigitalOcean (dev) — nao apenas local
- [ ] `loopzero promote staging production --json` retorna shape correta

---

## Ordem de implementacao

```
Task 5 (typed errors)           ← pre-requisito simples, T0
    ↓
Task 1 (PromotionStore + migration) ← DB layer, T1
    ↓
Task 2 (PromotionOrchestrator)  ← service layer, depende de Task 1
    ↓
Task 3 (API endpoint)           ← route layer, depende de Task 2
    ↓
Task 6 (wiring)                 ← integracao, depende de Tasks 1-3
    ↓
Task 4 (CLI command)            ← pode ser paralelo com Task 3 apos API spec definida
    ↓
Task 7 (integration tests)      ← depende de Tasks 1-6
    ↓
Task 8 (E2E validation)         ← depende de tudo
```

---

## Riscos e mitigacoes

| Risco | Impacto | Mitigacao |
|-------|---------|-----------|
| Manifests gerados para target diferem do esperado (hostnames, namespaces) | Release nao funciona no target | Teste unitario: renderizar mesma app para staging vs production, validar diff |
| Source release tem apps com secrets que nao existem no target env namespace | Pods nao iniciam | Validacao: verificar se os secrets referenciados existem no target namespace (ESO sync) |
| CF KV write para static app no target falha | Static app nao acessivel pelo hostname de production | Tratar como non-fatal (mesmo padrao do deploy) — assets ainda acessiveis via cluster |
| Promote durante deploy no source env | Release ACTIVE muda entre validacao e push | Snapshot do source release ID na fase 1, usar ID fixo nas fases seguintes |
| ArgoCD Application nao existe para target env | Sync falha | Validar que environment existe no target antes de promover (EnvironmentController deve ter criado) |

---

## Meeting questions

As seguintes questoes requerem decisao formal antes da implementacao:

1. **Promote cria nova release ou reutiliza?** (proposta: nova release — D1)
2. **Source env deve estar HEALTHY alem de ACTIVE?** (proposta: apenas ACTIVE — D2)
3. **Target env com release ACTIVE existente?** (proposta: supersede normal — D3)
4. **Cross-project promotion?** (proposta: nao, YAGNI — D4)
5. **Tabela separada ou campos na releases?** (proposta: tabela `promotions` — D5)
6. **Promote deve ser sincrono (como rollback) ou assincrono (como deploy)?** Rollback e sincrono (~3s). Deploy e assincrono (SSE streaming). Promote esta entre os dois — o push e rapido mas o sync pode demorar. Proposta: sincrono com timeout de 150s (mesmo padrao do rollback), porque nao ha build para esperar.
7. **Promote deve ter streaming (SSE)?** Proposta: nao na v1. O fluxo e rapido o suficiente para ser sincrono. Se necessario, adicionar SSE depois (YAGNI).

---

## Gate: Complete

Checklist global — todas as condicoes devem ser verdadeiras antes de marcar `loopzero promote` como DONE.

- [ ] `task test:unit` passa (CLI + API + pkg)
- [ ] `task lint` passa (golangci-lint + ESLint)
- [ ] `task validate:manifests` passa
- [ ] Migration aplicavel e idempotente (`task db:migrate`)
- [ ] Rota `POST /api/v1/projects/{name}/promote` funciona com auth + idempotency
- [ ] `withEnvironmentLock` usado em todas as mutacoes do target env
- [ ] `loopzero promote staging production` funciona end-to-end (CLI → API → gitops → sync → ACTIVE)
- [ ] `loopzero promote staging production --json` retorna shape correta (stability contract)
- [ ] Exit codes corretos: 0 (sucesso), 1 (usage), 3 (falha), 4 (auth)
- [ ] Zero internal terms em qualquer output CLI (Rule 3)
- [ ] State machine transitions validadas via `PromotionMachine` (nao diretas)
- [ ] Concurrent promote + deploy/rollback produz exatamente um 409
- [ ] Idempotency-Key: mesma key → mesma resposta sem side effects
- [ ] Static app promote: CF KV entry escrita para target env subdomain
- [ ] Rollback funciona apos promote no target env
- [ ] Validado em ambiente DigitalOcean (dev) — nao apenas testes locais
- [ ] CHANGELOG.md atualizado em `[Unreleased]` > `Added`
- [ ] Nenhum padrao "implemented but not wired" — todos os call sites verificados
