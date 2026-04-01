# Plano: `loopzero build` Subcommands

> **Objetivo:** Dar visibilidade sobre builds ao developer via CLI. Listar builds
> recentes, inspecionar detalhes de um build especifico, e cancelar builds em andamento.
>
> **Escopo:** 3 subcomandos (`list`, `inspect`, `cancel`), 1 nova rota API, queries DB
> adicionais, testes CLI + API.
>
> **Classificacao:** T1 (list/inspect sao read-only, single-component cada).
> `cancel` ja existe no backend (T0 — apenas CLI wiring).
>
> **Status:** DRAFT
>
> **Principio:** "Builds sao o heartbeat do developer. Se nao consigo ver meus builds,
> nao confio na plataforma."

---

## Contexto

Hoje o developer so ve builds indiretamente: `loopzero deploy` mostra o build em tempo real,
`loopzero status` mostra o ultimo build. Nao ha como listar historico, inspecionar detalhes
de um build especifico (duracao, erro, apps, commit), ou cancelar um build sem conhecer
a API diretamente.

### O Que Existe Hoje

**API (backend):**
- `POST /api/v1/builds` — submete build (usado por `loopzero deploy`)
- `GET /api/v1/builds/{id}` — retorna status basico (build_id, project_id, environment, status, commit_sha, created_by)
- `POST /api/v1/builds/{id}/cancel` — cancela build (valida tenant ownership, verifica terminal state)
- `GET /api/v1/builds/{id}/logs` — retorna build logs via Loki
- `GET /api/v1/builds/{id}/stream` — SSE stream de eventos

**DB (build_store.go):**
- `GetByID(ctx, id)` — retorna Build completo (id, tenant_id, project_id, environment, status, commit_sha, correlation_id, created_by, created_at, started_at, finished_at, error_message, duration_seconds, workflow_id)
- `ListByProject(ctx, tenantID, projectID, limit, offset)` — lista builds por projeto, ordenados por created_at DESC
- `HasActiveBuild(ctx, tx, tenantID, projectID, env)` — verifica se ha build ativo
- `FindActiveBuilds(ctx)` — lista todos os builds ativos (QUEUED, RUNNING, RELEASING)
- `CancelActiveBuilds(ctx, tx, tenantID, projectID, env)` — cancela builds ativos em batch

**CLI:**
- `loopzero deploy` — submete e acompanha build
- `loopzero status` — mostra ultimo build + release ativa
- `loopzero logs --build <id>` — mostra logs de build
- Nenhum comando `loopzero build` existe

### O Que Falta

| Funcionalidade | API | DB | CLI |
|---------------|-----|-----|-----|
| Listar builds por projeto | Precisa nova rota | `ListByProject` existe, precisa filtro por env | `loopzero build list` |
| Inspecionar build | `GET /builds/{id}` existe, resposta incompleta | `GetByID` retorna tudo, rota expoe subset | `loopzero build inspect` |
| Cancelar build | `POST /builds/{id}/cancel` existe | Logica completa no orchestrator | `loopzero build cancel` |

---

## Subcomando 1: `loopzero build list`

### Sintaxe

```
loopzero build list [--env <environment>] [--limit <n>] [--json]
```

O projeto e lido automaticamente de `loopzero.yaml` (padrao existente em `loopzero status`, `loopzero deploy`).

### Flags

| Flag | Default | Descricao |
|------|---------|-----------|
| `--env` | (todos) | Filtra por environment. Se omitido, mostra builds de todos os environments. |
| `--limit` | `10` | Numero maximo de builds retornados. Max: 50. |
| `--json` | `false` | Saida JSON (stability contract). |

### Exit Codes

| Code | Condicao |
|------|----------|
| `0` | Sucesso (inclusive lista vazia) |
| `1` | Erro de uso (loopzero.yaml ausente, flag invalida) |
| `4` | Nao autenticado |

### Output Humano

```
Builds for my-project (production):

  ID          STATUS     COMMIT   STARTED              DURATION
  bld_a1b2c3  SUCCEEDED  f3c9a01  2026-03-31 14:23:01  24s
  bld_d4e5f6  FAILED     b2a8c12  2026-03-31 13:50:44  18s
  bld_g7h8i9  CANCELLED  c1d2e3f  2026-03-31 12:10:00  —

3 builds
```

Sem builds:
```
No builds found for my-project. Run `loopzero deploy` to create your first build.
```

### Output JSON

```json
{
  "builds": [
    {
      "build_id": "bld_a1b2c3",
      "project": "my-project",
      "environment": "production",
      "status": "SUCCEEDED",
      "commit_sha": "f3c9a01",
      "created_by": "github|12345",
      "created_at": "2026-03-31T14:23:01Z",
      "started_at": "2026-03-31T14:23:05Z",
      "finished_at": "2026-03-31T14:23:25Z",
      "duration_seconds": 24,
      "error": null
    }
  ],
  "total": 3
}
```

**Stability contract:** campos `build_id`, `project`, `environment`, `status`, `commit_sha`, `created_at` sao imutaveis. Novos campos podem ser adicionados.

### API: Nova Rota `GET /api/v1/projects/{name}/builds`

A rota `GET /api/v1/builds/{id}` ja existe para build individual. Para listar builds de um projeto, precisamos de uma rota nova que segue o padrao RESTful de `GET /api/v1/projects/{name}/status`.

**Request:**
```
GET /api/v1/projects/{name}/builds?env=production&limit=10&offset=0
Authorization: Bearer <github-token>
```

**Response (200):**
```json
{
  "builds": [...],
  "total": 42
}
```

**Alternativa descartada:** Reutilizar `GET /api/v1/builds?project=X` — inconsistente com o padrao existente onde projeto e path param (`/projects/{name}/status`).

### Backend: Mudancas Necessarias

**1. Nova query: `ListByProjectAndEnv` (build_store.go)**

`ListByProject` existe mas nao filtra por environment. O status handler faz filtro em memoria (linha 147 de status.go), o que e aceitavel para 1 build mas nao para listas paginadas.

```go
// ListByProjectAndEnv returns builds for a project, optionally filtered by environment.
// If env is empty, returns builds for all environments.
func (s *PostgresBuildStore) ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]Build, int, error)
```

SQL:
```sql
-- When env is provided:
SELECT ... FROM builds
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3
ORDER BY created_at DESC
LIMIT $4 OFFSET $5;

SELECT COUNT(*) FROM builds
WHERE tenant_id = $1 AND project_id = $2 AND environment = $3;

-- When env is empty:
SELECT ... FROM builds
WHERE tenant_id = $1 AND project_id = $2
ORDER BY created_at DESC
LIMIT $3 OFFSET $4;

SELECT COUNT(*) FROM builds
WHERE tenant_id = $1 AND project_id = $2;
```

Retorna `([]Build, totalCount, error)` para que o CLI possa mostrar "3 builds" ou "Showing 10 of 42".

**2. Nova handler: `BuildListHandler` (routes/build_list.go)**

Separado do `BuildHandler` existente (SRP — handler de listagem vs handler de mutacao).

```go
type BuildListHandler struct {
    builds BuildListStore
}

type BuildListStore interface {
    ListByProjectAndEnv(ctx context.Context, tenantID, projectID, env string, limit, offset int) ([]BuildListItem, int, error)
}
```

**3. Wire no server.go:**

```go
r.Get("/api/v1/projects/{name}/builds", cfg.BuildListHandler.List)
```

### CLI: Implementacao

**Arquivo:** `cli/src/commands/build-list.ts`

Segue o padrao de `StatusCommand`:
- DI via `BuildListDeps` interface
- Auth check → loopzero.yaml check → API call → format output
- `exitWithError` / `handleError` pattern

```typescript
export interface BuildListDeps {
  getToken: () => string | null;
  findLoopZeroYaml: () => LoopZeroYamlResult | null;
  listBuilds: (project: string, env: string | undefined, limit: number) => Promise<BuildListResponse>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}
```

### Testes

**API (Go):**
- `TestBuildListHandler_Success` — retorna builds filtrados por env
- `TestBuildListHandler_AllEnvs` — retorna builds de todos os envs quando env omitido
- `TestBuildListHandler_EmptyList` — retorna lista vazia (200, nao 404)
- `TestBuildListHandler_Unauthorized` — retorna 401
- `TestBuildListHandler_LimitValidation` — rejeita limit > 50 ou limit < 1
- `TestBuildListHandler_TenantIsolation` — nao retorna builds de outro tenant

**DB (Go):**
- `TestListByProjectAndEnv_WithEnv` — filtra corretamente
- `TestListByProjectAndEnv_WithoutEnv` — retorna todos
- `TestListByProjectAndEnv_Pagination` — limit/offset funciona
- `TestListByProjectAndEnv_TotalCount` — count correto independente de limit

**CLI (Jest):**
- `test_build_list_success` — formata tabela corretamente
- `test_build_list_empty` — mensagem amigavel
- `test_build_list_json` — JSON shape correto
- `test_build_list_auth_required` — exit code 4
- `test_build_list_no_config` — exit code 1
- `test_build_list_env_filter` — passa --env para API

### DoD — `loopzero build list`

- [ ] Query `ListByProjectAndEnv` implementada com testes
- [ ] Handler `BuildListHandler.List` implementado com testes
- [ ] Rota `GET /api/v1/projects/{name}/builds` registrada no server.go
- [ ] CLI `BuildListCommand` implementado com testes
- [ ] Comando registrado no CLI entrypoint
- [ ] `--json` output segue stability contract
- [ ] Exit codes: 0 (sucesso), 1 (uso), 4 (auth)
- [ ] Zero termos internos na saida
- [ ] `task test:unit` passa
- [ ] `task lint` passa

---

## Subcomando 2: `loopzero build inspect <build-id>`

### Sintaxe

```
loopzero build inspect <build-id> [--json]
```

Nao requer `loopzero.yaml` — o build-id e suficiente para identificar o build.
Tenant ownership e validado pelo backend (SEC-4).

### Flags

| Flag | Default | Descricao |
|------|---------|-----------|
| `--json` | `false` | Saida JSON (stability contract). |

### Exit Codes

| Code | Condicao |
|------|----------|
| `0` | Sucesso |
| `1` | Erro de uso (build-id ausente) |
| `4` | Nao autenticado |

Nota: build nao encontrado retorna exit code `1` (usage error — ID invalido), nao `3` (deploy failure).

### Output Humano

```
Build bld_a1b2c3

  Project:      my-project
  Environment:  production
  Status:       SUCCEEDED
  Commit:       f3c9a01b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f
  Started by:   github|12345
  Created:      2026-03-31 14:23:01
  Started:      2026-03-31 14:23:05
  Finished:     2026-03-31 14:23:25
  Duration:     24s

  Apps:
    api         server   port 3000
    web         static
    worker      worker

View logs: loopzero logs --build bld_a1b2c3
```

Build com erro:
```
Build bld_d4e5f6

  Project:      my-project
  Environment:  production
  Status:       FAILED
  Commit:       b2a8c12...
  Error:        Dockerfile syntax error on line 12

View logs: loopzero logs --build bld_d4e5f6
```

### Output JSON

```json
{
  "build_id": "bld_a1b2c3",
  "project": "my-project",
  "environment": "production",
  "status": "SUCCEEDED",
  "commit_sha": "f3c9a01b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6e7f",
  "created_by": "github|12345",
  "created_at": "2026-03-31T14:23:01Z",
  "started_at": "2026-03-31T14:23:05Z",
  "finished_at": "2026-03-31T14:23:25Z",
  "duration_seconds": 24,
  "error": null,
  "apps": [
    { "name": "api", "type": "server", "port": 3000 },
    { "name": "web", "type": "frontend", "rendering": "static" },
    { "name": "worker", "type": "worker" }
  ]
}
```

### API: Expandir `GET /api/v1/builds/{id}`

A rota ja existe (`BuildHandler.Get`), mas retorna um subset limitado (`BuildStatusResponse`):

```go
// Atual — incompleto
type BuildStatusResponse struct {
    BuildID     string `json:"build_id"`
    ProjectID   string `json:"project_id"`
    Environment string `json:"environment"`
    Status      string `json:"status"`
    CommitSHA   string `json:"commit_sha"`
    CreatedBy   string `json:"created_by"`
}
```

**Mudanca:** Expandir `BuildStatusResponse` para incluir timestamps, duracao, erro, e apps:

```go
type BuildStatusResponse struct {
    BuildID         string         `json:"build_id"`
    ProjectID       string         `json:"project_id"`
    Environment     string         `json:"environment"`
    Status          string         `json:"status"`
    CommitSHA       string         `json:"commit_sha"`
    CreatedBy       string         `json:"created_by"`
    CreatedAt       string         `json:"created_at"`
    StartedAt       string         `json:"started_at,omitempty"`
    FinishedAt      string         `json:"finished_at,omitempty"`
    DurationSeconds *int           `json:"duration_seconds,omitempty"`
    Error           string         `json:"error,omitempty"`
    Apps            []BuildAppInfo `json:"apps,omitempty"`
}

type BuildAppInfo struct {
    Name      string `json:"name"`
    Type      string `json:"type"`
    Port      int    `json:"port,omitempty"`
    Rendering string `json:"rendering,omitempty"`
}
```

**Impacto:** Adicao de campos (nao breaking change). Campos existentes nao mudam. Consumers que fazem `JSON.parse` continuam funcionando — campos novos sao ignorados se nao usados.

**Onde mudar:** `BuildOrchestratorService.GetBuildStatus` precisa retornar os campos adicionais. O orchestrator ja tem acesso ao build store que retorna `db.Build` completo e ao `GetBuildApps`.

### Backend: Mudancas Necessarias

**1. Expandir `GetBuildStatus` no orchestrator (services/build_orchestrator.go)**

O orchestrator ja chama `buildStore.GetByID()` que retorna o `db.Build` completo. So precisa mapear os campos adicionais para `BuildStatusResponse` e chamar `buildStore.GetBuildApps()` para os apps.

**2. Atualizar `BuildStatusResponse` em routes/builds.go**

Adicionar os campos novos (listed above). Zero mudanca na rota ou no handler — so no response struct.

### CLI: Implementacao

**Arquivo:** `cli/src/commands/build-inspect.ts`

Mais simples que `build list` — nao precisa de `loopzero.yaml`.

```typescript
export interface BuildInspectDeps {
  getToken: () => string | null;
  getBuild: (buildId: string) => Promise<BuildDetail>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}
```

### Testes

**API (Go):**
- `TestGetBuildStatus_FullResponse` — retorna todos os campos novos
- `TestGetBuildStatus_WithApps` — inclui app specs
- `TestGetBuildStatus_FailedBuild` — inclui error message
- `TestGetBuildStatus_NullOptionals` — omits null fields corretamente

**CLI (Jest):**
- `test_build_inspect_success` — formata output corretamente
- `test_build_inspect_failed_build` — mostra erro
- `test_build_inspect_json` — JSON shape correto
- `test_build_inspect_not_found` — exit code 1
- `test_build_inspect_auth_required` — exit code 4
- `test_build_inspect_missing_id` — exit code 1

### DoD — `loopzero build inspect`

- [ ] `BuildStatusResponse` expandido com timestamps, duracao, erro, apps
- [ ] `GetBuildStatus` no orchestrator mapeia campos novos
- [ ] Testes API atualizados para novos campos
- [ ] CLI `BuildInspectCommand` implementado com testes
- [ ] Comando registrado no CLI entrypoint
- [ ] `--json` output segue stability contract
- [ ] Exit codes: 0 (sucesso), 1 (uso/not found), 4 (auth)
- [ ] Zero termos internos na saida (workflow_id, tenant_id, correlation_id nao expostos)
- [ ] `task test:unit` passa
- [ ] `task lint` passa

---

## Subcomando 3: `loopzero build cancel <build-id>`

### Sintaxe

```
loopzero build cancel <build-id> [--json]
```

Nao requer `loopzero.yaml` — o build-id e suficiente.

### Flags

| Flag | Default | Descricao |
|------|---------|-----------|
| `--json` | `false` | Saida JSON (stability contract). |

### Exit Codes

| Code | Condicao |
|------|----------|
| `0` | Build cancelado com sucesso |
| `1` | Erro de uso (build-id ausente, build nao encontrado, build ja finalizado) |
| `4` | Nao autenticado |

Nota: cancelar um build ja finalizado (SUCCEEDED, FAILED, CANCELLED) retorna exit code `1` com mensagem clara, nao `0` silencioso.

### Output Humano

Sucesso:
```
Build bld_a1b2c3 cancelled.
```

Build ja finalizado:
```
Build bld_a1b2c3 already finished (SUCCEEDED). Nothing to cancel.
```

Build nao encontrado:
```
Build bld_xyz123 not found.
```

### Output JSON

Sucesso:
```json
{
  "build_id": "bld_a1b2c3",
  "status": "cancelled"
}
```

Build ja finalizado:
```json
{
  "code": "BUILD_ALREADY_TERMINAL",
  "error": "Build bld_a1b2c3 already finished (SUCCEEDED). Nothing to cancel.",
  "exit_code": 1
}
```

### API: Ja Existe

`POST /api/v1/builds/{id}/cancel` esta 100% implementado:
- Auth check (middleware)
- Tenant ownership (SEC-4)
- Terminal state check (retorna `ErrInvalidTransition`)
- Argo Workflow cancellation (orchestrator)
- State machine transition BUILD_CANCEL → CANCELLED

Zero mudanca no backend. Apenas CLI wiring.

### CLI: Implementacao

**Arquivo:** `cli/src/commands/build-cancel.ts`

```typescript
export interface BuildCancelDeps {
  getToken: () => string | null;
  cancelBuild: (buildId: string) => Promise<{ status: string }>;
  writeTo: (msg: string) => void;
  writeError: (msg: string) => void;
}
```

O `cancelBuild` faz `POST /api/v1/builds/{id}/cancel` e mapeia erros:
- 404 → `BUILD_NOT_FOUND`
- 409 (ErrInvalidTransition) → `BUILD_ALREADY_TERMINAL`
- 401 → `AUTH_REQUIRED`

### Testes

**CLI (Jest) — nao precisa de testes API novos:**
- `test_build_cancel_success` — exit code 0, mensagem correta
- `test_build_cancel_json` — JSON shape correto
- `test_build_cancel_not_found` — exit code 1
- `test_build_cancel_already_terminal` — exit code 1, mensagem amigavel
- `test_build_cancel_auth_required` — exit code 4
- `test_build_cancel_missing_id` — exit code 1

### DoD — `loopzero build cancel`

- [ ] CLI `BuildCancelCommand` implementado com testes
- [ ] Comando registrado no CLI entrypoint
- [ ] `--json` output segue stability contract
- [ ] Exit codes: 0 (sucesso), 1 (uso/not found/terminal), 4 (auth)
- [ ] Zero termos internos na saida
- [ ] Mensagem amigavel para build ja finalizado
- [ ] `task test:unit` passa
- [ ] `task lint` passa

---

## CLI Entrypoint: Registrar `loopzero build`

**Arquivo:** `cli/src/index.ts` (ou equivalente Commander setup)

```typescript
const build = program.command('build').description('Manage builds');

build
  .command('list')
  .description('List recent builds')
  .option('--env <environment>', 'Filter by environment')
  .option('--limit <n>', 'Max builds to show', '10')
  .option('--json', 'Output as JSON')
  .action(async (opts) => { /* wire BuildListCommand */ });

build
  .command('inspect <build-id>')
  .description('Show build details')
  .option('--json', 'Output as JSON')
  .action(async (buildId, opts) => { /* wire BuildInspectCommand */ });

build
  .command('cancel <build-id>')
  .description('Cancel a running build')
  .option('--json', 'Output as JSON')
  .action(async (buildId, opts) => { /* wire BuildCancelCommand */ });
```

---

## Ordem de Implementacao

```
1. DB: ListByProjectAndEnv query + testes           (backend, T1)
2. API: BuildListHandler + rota + testes             (backend, T1)
3. API: Expandir BuildStatusResponse + testes        (backend, T1 — additive only)
4. CLI: BuildListCommand + testes                    (cli, T1)
5. CLI: BuildInspectCommand + testes                 (cli, T1)
6. CLI: BuildCancelCommand + testes                  (cli, T0 — backend ja existe)
7. CLI: Registrar `loopzero build` no entrypoint         (cli, T0)
8. Integration: testar E2E os 3 subcomandos          (cross-component)
```

Tasks 1-3 sao backend, podem ser feitas em sequencia numa sessao.
Tasks 4-7 sao CLI, podem ser feitas em sequencia numa segunda sessao.
Task 8 e validacao final.

---

## Agenda de Reuniao (se necessario)

Nenhuma task e T2/T3 individualmente. Porem, se o conjunto for avaliado como T2
(novo contrato API `GET /projects/{name}/builds`), estas questoes devem ser discutidas:

### Q1: Paginacao

A query `ListByProjectAndEnv` retorna `total` count. Devemos implementar paginacao
completa agora (cursor-based ou offset-based) ou apenas `limit`?

**Recomendacao:** Offset-based com `limit` + `offset` params. Cursor-based e YAGNI —
builds sao append-only e ordenados por created_at DESC, offset funciona bem.

### Q2: Filtro por Status

Deve `loopzero build list` aceitar `--status FAILED` para filtrar?

**Recomendacao:** Nao na v1. YAGNI. Se necessario, adicionar depois como query param
sem breaking change.

### Q3: Overlap com `loopzero build cancel` e futuro `loopzero cancel`

Sprint 4 menciona `loopzero cancel` (generico). `loopzero build cancel` e mais especifico
e consistente com o namespace `loopzero build`. Se `loopzero cancel` for implementado depois,
pode ser alias ou wrapper.

**Recomendacao:** Implementar `loopzero build cancel` agora. Se `loopzero cancel` surgir, faz
delegate para `loopzero build cancel` internamente.

### Q4: Campo `project` vs `project_id` no JSON

O backend usa `project_id` (que na verdade e o nome do projeto, nao UUID — inconsistencia
conhecida documentada em "Known Systemic Patterns"). No JSON de saida do CLI, devemos
usar `project` (user-facing) ou `project_id` (consistente com API)?

**Recomendacao:** `project` na saida CLI (user-facing). O campo `project_id` da API
e mapeado para `project` no JSON response. Isso segue o padrao de `loopzero status` que
ja usa `project`.

---

## Checklist Global

### Gate: `loopzero build` Complete

- [ ] `loopzero build list` — funcional com output humano e JSON
- [ ] `loopzero build inspect` — funcional com output humano e JSON
- [ ] `loopzero build cancel` — funcional com output humano e JSON
- [ ] Todos os testes passam (`task test:unit`, `task lint`)
- [ ] Zero termos internos em qualquer saida CLI
- [ ] Exit codes consistentes com spec (0/1/4 apenas)
- [ ] `--json` shapes sao stability contracts documentados
- [ ] Changelog atualizado

### O Que NAO Fazer

- [ ] NAO implementar `loopzero build retry` (YAGNI — nao especificado)
- [ ] NAO implementar filtro por status (YAGNI — adicionar depois se necessario)
- [ ] NAO implementar cursor-based pagination (YAGNI — offset funciona)
- [ ] NAO modificar `loopzero deploy` (escopo separado)
- [ ] NAO expor workflow_id, tenant_id, correlation_id na saida CLI (Rule 3)

---

## Referencias

| Documento | Relevancia |
|-----------|------------|
| `api/internal/routes/builds.go` | Handlers existentes (Create, Get, Cancel) |
| `api/internal/db/build_store.go` | Queries existentes (GetByID, ListByProject, FindActiveBuilds) |
| `api/internal/routes/status.go` | Padrao de rota por projeto (`/projects/{name}/status`) |
| `cli/src/commands/status.ts` | Padrao CLI (DI, auth check, loopzero.yaml, exitWithError) |
| `cli/src/commands/deploy.ts` | Padrao CLI (build submission, SSE, error handling) |
| `api/internal/server/server.go` | Registro de rotas |
