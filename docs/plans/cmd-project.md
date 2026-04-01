# Plano: `loopzero project` — Subcomandos de gestao de projetos

> **Objetivo:** Consolidar operacoes de projeto sob `loopzero project` com subcomandos `list`, `create`, `delete`, `inspect`.
>
> **Escopo:** CLI (4 subcomandos), API (2 endpoints novos), Backend (1 query nova), refactor do `project-delete` existente.
>
> **Classificacao:** T2 (toca contratos CLI, rotas API, store DB)
>
> **Status:** DRAFT — pendente aprovacao em meeting

---

## Contexto

### O que ja existe

| Componente | Status | Localizacao |
|-----------|--------|-------------|
| `loopzero project delete <name>` | Funcional | `cli/src/commands/project-delete.ts`, registrado como subcomando de `project` em `cli/src/index.ts:741-790` |
| `POST /api/v1/projects` | Funcional | `api/internal/routes/projects.go:107` — cria projeto |
| `DELETE /api/v1/projects/{name}` | Funcional | `api/internal/routes/projects.go:164` — deleta projeto |
| `DELETE /api/v1/projects/{name}/environments/{env}` | Funcional | `api/internal/routes/projects.go:219` — deleta de env especifica |
| `GET /api/v1/projects/{name}/status` | Funcional | `api/internal/routes/status.go` — status com builds e release ativa |
| `ProjectStore.CreateProject` | Funcional | `api/internal/db/project_store.go:31` |
| `ProjectStore.GetByName` | Funcional | `api/internal/db/project_store.go:55` |
| `ProjectStore.DeleteProject` | Funcional | `api/internal/db/project_store.go:80` |
| `ProjectStore.ListByTenant` | **NAO EXISTE** | Necessario para `loopzero project list` |
| `GET /api/v1/projects` | **NAO EXISTE** | Necessario para `loopzero project list` |
| `GET /api/v1/projects/{name}` | **NAO EXISTE** | Necessario para `loopzero project inspect` |
| `loopzero init` cria projeto via API | Funcional | `cli/src/commands/init.ts:251` — chama `POST /api/v1/projects` |

### Schema do banco

```sql
-- migration 000002_auth_tenants.up.sql
CREATE TABLE IF NOT EXISTS projects (
    id         TEXT PRIMARY KEY,                       -- proj-{8hex}
    tenant_id  TEXT NOT NULL REFERENCES tenants (id),
    name       TEXT NOT NULL,
    created_by TEXT NOT NULL REFERENCES users (id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, name)
);
```

---

## Decisao pendente: `loopzero init` vs `loopzero project create`

### Opcoes

| Opcao | Descricao | Pros | Contras |
|-------|-----------|------|---------|
| **A: `loopzero init` continua criando projetos** | `loopzero project create` e um alias explicito, `loopzero init` mantem o fluxo atual (detecta + gera yaml + registra no backend) | Zero breaking change, DX mais simples para onboarding | Dois caminhos para criar projeto, confusao sobre qual usar |
| **B: `loopzero project create` e o unico caminho** | `loopzero init` apenas gera `loopzero.yaml` localmente, sem chamar API. Projeto so existe apos `loopzero project create` ou primeiro `loopzero deploy` | Separacao clara de responsabilidades, `init` e local-only | Breaking change no fluxo existente, step extra para usuario novo |
| **C: `loopzero init` cria, `loopzero project create` e para headless/CI** | `loopzero init` continua criando (fluxo interativo). `loopzero project create` permite criar sem diretorio local (ex: CI pipeline que cria projeto antes de checkout) | Cada comando tem caso de uso distinto | Dois caminhos, mas justificados |

**Recomendacao:** Opcao C. `loopzero init` e o fluxo natural de onboarding (detecta monorepo, gera yaml, registra). `loopzero project create` atende CI/automacao onde nao ha diretorio local. Nenhum breaking change.

**Deve ser discutido em meeting** antes de implementar.

---

## Subcomandos

### 1. `loopzero project list`

**Sintaxe:**

```
loopzero project list [--json]
```

**Flags:**

| Flag | Tipo | Default | Descricao |
|------|------|---------|-----------|
| `--json` | boolean | false | Saida em formato JSON |

**Exit codes:** `0` sucesso, `1` erro de uso, `4` auth error

**Saida humana:**

```
Projects in team "acme":

  NAME            CREATED
  api-gateway     2026-03-15
  web-app         2026-03-20
  worker-jobs     2026-03-28

3 projects
```

**Saida JSON (`--json`):**

```json
{
  "projects": [
    {
      "name": "api-gateway",
      "id": "proj-a1b2c3d4",
      "created_at": "2026-03-15T10:30:00Z"
    }
  ],
  "total": 3
}
```

**API:** `GET /api/v1/projects` (novo)

- Auth: obrigatorio (token + tenant isolation)
- Response 200: `{ "projects": [...], "total": N }`
- Sem paginacao na v1 (projetos por tenant sao dezenas, nao milhares)

**Backend:**

- Nova query `ListByTenant(ctx, tenantID) ([]ProjectResult, error)` no `ProjectStore`
- SQL: `SELECT id, name, tenant_id, created_at FROM projects WHERE tenant_id = $1 ORDER BY name ASC`
- Adicionar `created_at` ao `ProjectResult` (campo novo, nao quebra contrato existente)

**Testes:**

- CLI: token ausente retorna exit 4; lista vazia mostra "No projects"; lista com N projetos mostra tabela; `--json` retorna shape correta
- API: retorna 401 sem auth; retorna lista vazia para tenant sem projetos; retorna projetos apenas do tenant autenticado (isolamento)
- DB: `ListByTenant` retorna projetos ordenados por nome; nao retorna projetos de outro tenant

**DoD — DB (`ListByTenant`):**
- [ ] `ListByTenant(ctx, tenantID)` implementado em `ProjectStore`
- [ ] SQL usa `ORDER BY name ASC`
- [ ] Retorna `[]ProjectResult` com `CreatedAt` preenchido
- [ ] Teste: retorna projetos ordenados por nome
- [ ] Teste: nao retorna projetos de outro tenant (isolamento)
- [ ] Teste: retorna lista vazia para tenant sem projetos

**DoD — API (`GET /api/v1/projects`):**
- [ ] Handler `ProjectHandler.List` implementado em `routes/projects.go`
- [ ] Rota registrada no `server.go`
- [ ] Auth middleware ativo (401 sem token)
- [ ] Response 200 com shape `{ "projects": [...], "total": N }`
- [ ] Tenant isolation: retorna apenas projetos do tenant autenticado
- [ ] Teste: retorna 401 sem auth
- [ ] Teste: retorna lista vazia para tenant sem projetos
- [ ] Teste: retorna projetos com shape correta

**DoD — CLI (`loopzero project list`):**
- [ ] `ProjectListCommand` implementado em `cli/src/commands/project-list.ts`
- [ ] Auth check (exit 4 se nao autenticado)
- [ ] Lista vazia mostra "No projects" (exit 0)
- [ ] Lista com N projetos mostra tabela formatada
- [ ] `--json` retorna shape `{ "projects": [...], "total": N }`
- [ ] Zero termos internos em qualquer string
- [ ] Testes: token ausente (exit 4), lista vazia, lista com projetos, `--json` shape
- [ ] `npm run build` passa

---

### 2. `loopzero project create <name>`

**Sintaxe:**

```
loopzero project create <name> [--json]
```

**Flags:**

| Flag | Tipo | Default | Descricao |
|------|------|---------|-----------|
| `--json` | boolean | false | Saida em formato JSON |

**Exit codes:** `0` sucesso, `1` erro de uso (nome invalido, faltando), `4` auth error

**Saida humana:**

```
Created project "api-gateway".

Next steps:
  cd your-repo && loopzero init --name api-gateway
  loopzero deploy
```

Caso projeto ja exista:

```
Project "api-gateway" already exists.
```
(exit 0 — idempotente, nao e erro)

**Saida JSON (`--json`):**

```json
{
  "name": "api-gateway",
  "id": "proj-a1b2c3d4",
  "created": true
}
```

Se ja existe:

```json
{
  "name": "api-gateway",
  "created": false,
  "message": "Project already exists"
}
```

**API:** `POST /api/v1/projects` (ja existe)

- Ja trata 409 para projeto duplicado
- Nenhuma mudanca necessaria no backend
- CLI deve tratar 409 como sucesso (idempotente) com `created: false`

**Validacao (Rule 3 — termos internos):**

- Nome validado como RFC 1123 (mesma logica de `init.ts:sanitizeProjectName`)
- Mensagens de erro usam linguagem de produto: "project name", "team", nunca "tenant"

**Testes:**

- CLI: cria projeto com sucesso; nome vazio retorna exit 1; nome invalido (caracteres especiais) retorna exit 1; sem auth retorna exit 4; projeto duplicado retorna exit 0; `--json` shape correta
- Reutiliza endpoint existente — testes de API ja cobrem

**DoD — CLI (`loopzero project create`):**
- [ ] `ProjectCreateCommand` implementado em `cli/src/commands/project-create.ts`
- [ ] Auth check (exit 4 se nao autenticado)
- [ ] Nome vazio retorna exit 1 com mensagem de uso
- [ ] Nome invalido (caracteres especiais) retorna exit 1
- [ ] Criacao com sucesso mostra confirmacao + next steps (exit 0)
- [ ] Projeto duplicado (409) tratado como idempotente (exit 0, `created: false`)
- [ ] `--json` retorna shape `{ "name", "id", "created" }` para ambos os cenarios
- [ ] Validacao RFC 1123 no nome (mesma logica de `init.ts:sanitizeProjectName`)
- [ ] Zero termos internos em qualquer string (Rule 3)
- [ ] Testes: sucesso, nome vazio (exit 1), nome invalido (exit 1), sem auth (exit 4), duplicado (exit 0), `--json` shape
- [ ] `npm run build` passa

---

### 3. `loopzero project delete <name>`

**Sintaxe:**

```
loopzero project delete <name> [--env <name>] [--yes] [--json]
```

**Flags:**

| Flag | Tipo | Default | Descricao |
|------|------|---------|-----------|
| `--env` | string | (todos) | Deleta apenas de um ambiente especifico |
| `--yes` | boolean | false | Pula confirmacao (obrigatorio em CI) |
| `--json` | boolean | false | Saida em formato JSON |

**Exit codes:** `0` sucesso, `1` erro de uso, `3` falha na delecao, `4` auth error

**Refactor necessario:**

O comando ja existe e funciona (`cli/src/commands/project-delete.ts`). Ja esta registrado como subcomando de `project` em `index.ts:741-790`. **Nenhuma mudanca estrutural necessaria** — o refactor e minimo:

1. Mover logica de `project-delete.ts` para `project/delete.ts` (organizacao por pasta, opcional)
2. Garantir que `--json` herda do parent command se nao passado explicitamente

**API:** `DELETE /api/v1/projects/{name}` e `DELETE /api/v1/projects/{name}/environments/{env}` (ja existem)

**Testes:** Ja existem para `ProjectDeleteCommand`. Verificar cobertura de `--json` output shape.

**DoD — CLI (`loopzero project delete` refactor):**
- [ ] (Opcional) Logica movida de `project-delete.ts` para `project/delete.ts`
- [ ] `--json` herda do parent command se nao passado explicitamente
- [ ] Testes existentes continuam passando
- [ ] Cobertura de `--json` output shape verificada e adicionada se ausente
- [ ] `--env` flag funciona corretamente
- [ ] `--yes` flag pula confirmacao
- [ ] Zero termos internos em qualquer string
- [ ] `npm run build` passa

---

### 4. `loopzero project inspect <name>`

**Sintaxe:**

```
loopzero project inspect <name> [--env <name>] [--json]
```

**Flags:**

| Flag | Tipo | Default | Descricao |
|------|------|---------|-----------|
| `--env` | string | production | Ambiente para mostrar detalhes |
| `--json` | boolean | false | Saida em formato JSON |

**Exit codes:** `0` sucesso, `1` erro de uso, `4` auth error

**Saida humana:**

```
Project: api-gateway
Team:    acme
Created: 2026-03-15

Environment: production

  Current deployment:
    Version:  rel-a1b2c3d4
    Commit:   abc1234
    Deployed: 2026-03-28 14:30 UTC
    Status:   active

  Recent builds:
    BUILD           STATUS     COMMIT   DURATION   CREATED
    bld-x1y2z3w4    succeeded  abc1234  24s        2026-03-28 14:30
    bld-a9b8c7d6    failed     def5678  18s        2026-03-27 10:15
    bld-e5f6g7h8    succeeded  ghi9012  31s        2026-03-26 09:00

  URL: https://api-gateway.production.dev.loopzero.example
```

Projeto nao encontrado:

```
Project "unknown-app" not found.
```
(exit 1)

**Saida JSON (`--json`):**

```json
{
  "project": {
    "name": "api-gateway",
    "id": "proj-a1b2c3d4",
    "created_at": "2026-03-15T10:30:00Z"
  },
  "environment": "production",
  "active_release": {
    "id": "rel-a1b2c3d4",
    "status": "active",
    "commit": "abc1234",
    "created_at": "2026-03-28T14:30:00Z"
  },
  "recent_builds": [
    {
      "id": "bld-x1y2z3w4",
      "status": "succeeded",
      "commit": "abc1234",
      "duration_seconds": 24,
      "created_at": "2026-03-28T14:30:00Z"
    }
  ],
  "url": "https://api-gateway.production.dev.loopzero.example"
}
```

**API:** `GET /api/v1/projects/{name}` (novo)

- Query params: `?env=production` (default: production)
- Response 200: projeto + release ativa + builds recentes (5 ultimos)
- Response 404: projeto nao encontrado
- Reutiliza `StatusBuildStore.ListByProject` e `StatusReleaseStore.GetActiveByEnv` que ja existem para o endpoint `/status`

**Backend:**

- Novo handler `ProjectHandler.Get` em `routes/projects.go`
- Reutiliza stores existentes (`ProjectStore.GetByName`, `StatusBuildStore.ListByProject`, `StatusReleaseStore.GetActiveByEnv`)
- `ProjectHandler` precisa receber referencia aos build/release stores (injecao via config ou setter como `SetDeleter`)

**Overlap com `loopzero status`:**

`loopzero status` ja mostra informacoes similares (release ativa + builds recentes). Diferenca semantica:

| Comando | Proposito | Perspectiva |
|---------|-----------|-------------|
| `loopzero status` | "Meu deploy esta ok?" | Operacional — rapido, focado no estado atual |
| `loopzero project inspect` | "Me conta tudo sobre este projeto" | Informacional — detalhado, inclui metadados do projeto |

`loopzero project inspect` agrega os dados de `status` + metadados do projeto (id, created_at, team). Na pratica, `inspect` e um superset de `status` para um ambiente especifico.

**Testes:**

- CLI: projeto nao encontrado retorna exit 1; auth ausente retorna exit 4; mostra release ativa; mostra builds recentes; `--json` shape correta; sem release ativa mostra "No active deployment"
- API: retorna 404 para projeto inexistente; retorna 401 sem auth; retorna dados corretos com release ativa; retorna dados sem release ativa; tenant isolation (nao retorna projeto de outro tenant)
- DB: nenhuma query nova — reutiliza stores existentes

**DoD — API (`GET /api/v1/projects/{name}`):**
- [ ] Handler `ProjectHandler.Get` implementado em `routes/projects.go`
- [ ] Rota registrada no `server.go`
- [ ] Query param `?env=production` (default: production)
- [ ] Response 200: projeto + release ativa + builds recentes (5 ultimos)
- [ ] Response 404: projeto nao encontrado
- [ ] `ProjectHandler` recebe referencia aos build/release stores (injecao)
- [ ] Auth middleware ativo (401 sem token)
- [ ] Tenant isolation (nao retorna projeto de outro tenant)
- [ ] Teste: retorna 404 para projeto inexistente
- [ ] Teste: retorna 401 sem auth
- [ ] Teste: retorna dados corretos com release ativa
- [ ] Teste: retorna dados sem release ativa
- [ ] Teste: tenant isolation

**DoD — CLI (`loopzero project inspect`):**
- [ ] `ProjectInspectCommand` implementado em `cli/src/commands/project-inspect.ts`
- [ ] Auth check (exit 4 se nao autenticado)
- [ ] Projeto nao encontrado retorna exit 1 com mensagem sugestiva
- [ ] Mostra release ativa com version, commit, deployed, status
- [ ] Mostra builds recentes em tabela formatada
- [ ] Mostra URL do servico
- [ ] Sem release ativa mostra "No active deployment"
- [ ] `--env` flag filtra por ambiente (default: production)
- [ ] `--json` retorna shape com `project`, `environment`, `active_release`, `recent_builds`, `url`
- [ ] Zero termos internos em qualquer string (Rule 3)
- [ ] Testes: projeto nao encontrado (exit 1), sem auth (exit 4), com release ativa, sem release ativa, `--json` shape
- [ ] `npm run build` passa

---

## Resumo de mudancas

### CLI (`cli/`)

| Mudanca | Arquivo | Tipo |
|---------|---------|------|
| Novo `ProjectListCommand` | `cli/src/commands/project-list.ts` | Novo |
| Novo `ProjectCreateCommand` | `cli/src/commands/project-create.ts` | Novo |
| Novo `ProjectInspectCommand` | `cli/src/commands/project-inspect.ts` | Novo |
| Registrar subcomandos em `project` | `cli/src/index.ts` | Editar |
| (Opcional) mover `project-delete.ts` para `project/delete.ts` | `cli/src/commands/` | Refactor |

**DoD — Wire CLI:**
- [ ] Imports adicionados em `index.ts`
- [ ] Subcomandos `list`, `create`, `inspect` registrados sob `project`
- [ ] `loopzero project list --help` funciona
- [ ] `loopzero project create --help` funciona
- [ ] `loopzero project inspect --help` funciona
- [ ] `loopzero project delete` continua funcionando (sem regressao)

### API (`api/`)

| Mudanca | Arquivo | Tipo |
|---------|---------|------|
| `GET /api/v1/projects` handler (`List`) | `api/internal/routes/projects.go` | Editar |
| `GET /api/v1/projects/{name}` handler (`Get`) | `api/internal/routes/projects.go` | Editar |
| Registrar rotas no router | `api/internal/server/server.go` | Editar |
| Injetar build/release stores no `ProjectHandler` | `api/internal/server/server.go` | Editar |

### Backend (`api/internal/db/`)

| Mudanca | Arquivo | Tipo |
|---------|---------|------|
| `ListByTenant(ctx, tenantID)` query | `api/internal/db/project_store.go` | Editar |
| Adicionar `CreatedAt` ao `ProjectResult` | `api/internal/routes/projects.go` | Editar |
| Atualizar `ProjectStore` interface | `api/internal/routes/projects.go` | Editar |

**DoD — Backend:**
- [ ] `CreatedAt` adicionado ao `ProjectResult` (ponteiro `*time.Time` para backward compat)
- [ ] `ListByTenant` implementado no `PostgresProjectStore`
- [ ] Interface `ProjectStore` atualizada com `ListByTenant`
- [ ] Todos os call sites existentes continuam compilando
- [ ] `go test ./api/...` passa

### Nenhuma migration necessaria

O schema ja tem `created_at` e `updated_at`. A query `ListByTenant` usa campos existentes.

---

## Conformidade com Rule 3 (termos internos)

| Proibido | Substituto usado |
|----------|------------------|
| tenant_id | team |
| ArgoCD | (nunca mencionado) |
| GitOps | (nunca mencionado) |
| CRD | (nunca mencionado) |
| Gitea | (nunca mencionado) |
| MinIO | (nunca mencionado) |

Todas as mensagens de erro sugerem proxima acao:

- "Not authenticated. Run: loopzero login"
- "Project name is required. Usage: loopzero project create <name>"
- "Project \"X\" not found. Run: loopzero project list"

---

## Ordem de implementacao

```
Fase 1: Backend + API (sem mudanca de CLI)
  1. Adicionar CreatedAt ao ProjectResult
  2. Implementar ListByTenant no PostgresProjectStore
  3. Implementar ProjectHandler.List (GET /api/v1/projects)
  4. Implementar ProjectHandler.Get (GET /api/v1/projects/{name})
  5. Registrar rotas no server.go
  6. Testes unitarios para queries e handlers

Fase 2: CLI
  7. ProjectListCommand + testes
  8. ProjectCreateCommand + testes
  9. ProjectInspectCommand + testes
  10. Registrar subcomandos em index.ts

Fase 3: Validacao
  11. task test:unit passa
  12. task lint passa
  13. Grep por termos internos nas strings do CLI
  14. Validar --json shapes
```

**DoD — Fase 1:**
- [ ] `CreatedAt` no `ProjectResult` sem quebrar call sites existentes
- [ ] `ListByTenant` com teste de isolamento por tenant
- [ ] `GET /api/v1/projects` retorna 200 com shape correta
- [ ] `GET /api/v1/projects/{name}` retorna 200/404 conforme esperado
- [ ] Rotas registradas no `server.go`
- [ ] `go test ./api/...` passa

**DoD — Fase 2:**
- [ ] `ProjectListCommand` implementado e testado
- [ ] `ProjectCreateCommand` implementado e testado
- [ ] `ProjectInspectCommand` implementado e testado
- [ ] Subcomandos registrados em `index.ts`
- [ ] `npm test` passa
- [ ] `npm run build` passa

**DoD — Fase 3:**
- [ ] `task test:unit` passa (Go + TS)
- [ ] `task lint` passa (golangci-lint + ESLint)
- [ ] Zero termos internos encontrados via grep nas strings CLI
- [ ] `--json` shapes validadas contra spec §0.3
- [ ] CHANGELOG.md atualizado com entries em `[Unreleased]`

---

## Riscos

| Risco | Probabilidade | Mitigacao |
|-------|---------------|-----------|
| `CreatedAt` adicionado ao `ProjectResult` quebra testes existentes que fazem scan sem esse campo | Media | Manter campo opcional (ponteiro `*time.Time`) ou atualizar todos os call sites |
| `ProjectHandler.Get` duplica logica de `StatusHandler.Get` | Alta | Compor — `inspect` chama internamente os mesmos stores que `status` usa |
| Paginacao necessaria no futuro para `list` | Baixa (dezenas de projetos por tenant) | Implementar sem paginacao agora (YAGNI), schema JSON permite adicionar `cursor`/`next` depois |

---

## Checklist Global — Gate: Complete

- [ ] `loopzero project list` mostra projetos com output correto (tabela + contagem)
- [ ] `loopzero project list --json` retorna shape `{ "projects": [...], "total": N }`
- [ ] `loopzero project create <name>` cria projeto e retorna confirmacao
- [ ] `loopzero project create <name> --json` retorna shape `{ "name", "id", "created" }`
- [ ] `loopzero project create` trata duplicado como idempotente (exit 0)
- [ ] `loopzero project delete` continua funcionando (refactor sem regressao)
- [ ] `loopzero project inspect <name>` mostra detalhes do projeto com release ativa e builds recentes
- [ ] `loopzero project inspect <name> --json` retorna shape com `project`, `active_release`, `recent_builds`, `url`
- [ ] `GET /api/v1/projects` implementado com tenant isolation
- [ ] `GET /api/v1/projects/{name}` implementado com 200/404
- [ ] `ListByTenant` implementado no `ProjectStore` com teste de isolamento
- [ ] All: `--json` output matches spec §0.3
- [ ] All: exit codes corretos (0/1/3/4)
- [ ] All: zero termos internos em output (Rule 3)
- [ ] `npm test` passa
- [ ] `go test ./api/...` passa
- [ ] `task lint` passa
- [ ] `task validate:manifests` passa
- [ ] CHANGELOG.md atualizado com entries em `[Unreleased]`
