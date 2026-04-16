# Monorepo Validator — Deploy-Ready Validation

> Garantir que todo monorepo passa por validação completa ANTES de submeter ao
> build pipeline. Mensagens claras, educativas, com fix sugerido.

## Contexto

Problemas encontrados ao deployar aquim.ai (2026-04-08):
- Framework `react` não reconhecido no server → build falha 5min depois
- `render: static` não inferido para frontends → validação rejeita
- Context path mismatch com user Dockerfile + buildpack
- Nenhuma mensagem clara sobre o que está errado

## Regras do LoopZero (imutáveis)

1. **Frontend = SEMPRE static** (zero pods, CDN via CF Worker)
2. **Backend (server) = 1 pod** (container no runtime cluster)
3. **Worker = 1 pod** (container sem ingress)
4. **Max 2 pods por monorepo** (1 server + 1 worker, ou 2 servers)
5. **Frontend não conta como pod** (é static)
6. **User pode customizar** — mas o default deve funcionar sem config extra

## Stacks Suportados (loopzero-stacks)

| Template | Apps | Pods | Static |
|----------|------|------|--------|
| monorepo-turbo | api (server) + web (frontend) | 1 | 1 |
| monorepo-python | api (server) + worker (worker) | 2 | 0 |
| monorepo-go | api (server) + worker (worker) | 2 | 0 |
| monorepo-ruby | api (server) + worker (worker) | 2 | 0 |
| monorepo-java | api (server) + worker (worker) | 2 | 0 |
| monorepo-php | api (server) + worker (worker) | 2 | 0 |
| monorepo-rust | api (server) + worker (worker) | 2 | 0 |
| fullstack-nextjs | web (frontend) | 0 | 1 |
| node-express | api (server) | 1 | 0 |
| python-fastapi | api (server) | 1 | 0 |
| **Custom** | user-defined (max 2 pods) | ≤2 | ≤N |

## Validações Necessárias

### V1: Framework Recognition (CLI-side, pre-deploy)

**Quando:** `loopzero deploy` ou `loopzero validate`

**Regra:** Todo framework no loopzero.yaml DEVE ser reconhecido pelo server.

**Frameworks reconhecidos:**
- Node: `nextjs`, `next`, `vite`, `remix`, `nestjs`, `express`, `node`, `react`, `vue`, `svelte`, `astro`, `angular`, `fastify`
- Go: `gin`, `go`
- Python: `fastapi`, `django`, `python`
- Custom: `custom` (requer Dockerfile)

**Se não reconhecido:**
```
ERROR: Unknown framework 'sinatra' for app 'api'.

  Supported frameworks:
    Node.js: express, nestjs, fastify, nextjs, vite, react, vue, ...
    Go:      go, gin
    Python:  fastapi, django, python
    Custom:  custom (requires Dockerfile in app path)

  Fix: Set 'framework: custom' and provide a Dockerfile in apps/api/
```

**DoD:**
- [ ] CLI rejeita frameworks desconhecidos com erro claro
- [ ] Mensagem lista todos os frameworks suportados
- [ ] Sugere `framework: custom` + Dockerfile como fix
- [ ] Teste: `framework: sinatra` → error com mensagem educativa

### V2: Frontend = Static (auto-infer)

**Quando:** `normalizeApp()` no CLI

**Regra:** Se `type: frontend`, `render` DEVE ser `static`. Se não declarado, auto-setar.

**Mudança em `app-unit.ts`:**
```typescript
// ANTES: rendering default é "server"
let rendering: RenderMode = 'server';

// DEPOIS: frontends default para "static"
let rendering: RenderMode = type === 'frontend' ? 'static' : 'server';
```

**Se user tenta `render: server` em frontend:**
```
ERROR: App 'web': frontend apps must use static delivery.

  LoopZero serves frontends from CDN — zero pods, instant global delivery.
  Server-side rendering is not supported. Use 'render: static'.

  If you need a server (e.g., Next.js API routes), use 'type: server' instead.
```

**DoD:**
- [ ] `type: frontend` auto-seta `render: static` se não declarado
- [ ] `type: frontend` + `render: server` → error com mensagem clara
- [ ] Teste: loopzero.yaml sem `render` + `type: frontend` → `render: static` inferido
- [ ] Templates loopzero-stacks atualizados (remover `render: server` dos frontends)

### V3: Max Pods Validation

**Quando:** `loopzero deploy` ou `loopzero validate`

**Regra:** Máximo 2 pods por projeto. Frontend (static) não conta.

```typescript
const podApps = apps.filter(a => a.type === 'server' || a.type === 'worker');
if (podApps.length > 2) {
  error(`Too many server/worker apps (${podApps.length}). Maximum is 2.`);
}
```

**Mensagem:**
```
ERROR: Too many container apps (3). Maximum is 2 per project.

  Container apps (need a pod): api, worker, processor
  Static apps (CDN, no pod):   web

  Tip: Combine small services into one app, or split into separate projects.
  Starter plan: 1 project. Pro plan: 3 projects. Team plan: 10 projects.
```

**DoD:**
- [ ] Validação rejeita >2 pod apps
- [ ] Frontends não contam como pods
- [ ] Mensagem lista quais apps são pods e quais são static
- [ ] Sugere alternativa (combinar ou split)
- [ ] Teste: 3 servers → error. 2 servers + 1 frontend → OK.

### V4: Dockerfile Detection for Custom Framework

**Quando:** `loopzero deploy` (pre-upload) ou `loopzero validate`

**Regra:** Se `framework: custom` ou framework desconhecido + `build: buildpack`, DEVE existir Dockerfile no app path.

**Mudança:** `validate-config.ts` já verifica isso (line 72-76), mas:
1. Só verifica quando `build === 'dockerfile'` (não buildpack)
2. Não verifica no caso `framework: custom` + `build: buildpack`

**Mensagem:**
```
ERROR: App 'api' uses custom framework but no Dockerfile found at apps/api/Dockerfile.

  Options:
    1. Add a Dockerfile to apps/api/
    2. Change framework to a supported one (e.g., 'framework: express')

  Supported frameworks: express, nestjs, fastify, go, gin, fastapi, django, python
```

**DoD:**
- [ ] Detecta Dockerfile ausente para `framework: custom`
- [ ] Mensagem sugere framework correto ou adicionar Dockerfile
- [ ] Teste: `framework: custom` sem Dockerfile → error

### V5: Server-Side Framework Validation (API)

**Quando:** `POST /api/v1/builds` (antes de submeter ao pipeline)

**Regra:** API DEVE rejeitar frameworks que o pipeline não sabe construir.

**Mudança em `api/internal/routes/builds.go`:**
```go
for _, app := range units {
    strategy := resolveLanguageStrategy(app.Framework)
    if strategy == "custom" && app.Build != "dockerfile" {
        return error("framework '%s' requires build: dockerfile", app.Framework)
    }
}
```

**DoD:**
- [ ] API retorna 422 para framework não reconhecido + build=buildpack
- [ ] Mensagem sugere `build: dockerfile` ou framework correto
- [ ] Teste: `framework: sinatra` + `build: buildpack` → 422

### V6: Context Path Auto-Detection

**Quando:** `CreateBuildPlan()` no server

**Regra:** Se o app tem user Dockerfile no path E `build != "dockerfile"`, o plan deve auto-ajustar para `build: "dockerfile"` com context correto.

**Mudança em `build_plan.go`:**
```go
// Auto-detect: if user has Dockerfile in app path, treat as dockerfile build
if unit.Build == "buildpack" && unit.Path != "." && unit.Path != "" {
    // The loopzero-packs will find and use the user Dockerfile,
    // but context must match the Dockerfile's expectations.
    // When loopzero-packs copies Dockerfile.{app} to /workspace,
    // the context is /workspace (not /workspace/{path}).
    // This is correct for buildpack-generated Dockerfiles
    // but WRONG for user-provided ones.
    //
    // Fix: build_plan should NOT change this — instead, V4 validation
    // should force build: dockerfile when user Dockerfile exists.
}
```

**Decisão:** V4 (CLI validation) é a solução correta. Se Dockerfile existe no path, CLI deve auto-setar `build: dockerfile`. Não mudar no server.

**Mudança em `app-unit.ts`:**
```typescript
// Auto-detect Dockerfile in app path
if (build === 'buildpack' && raw.path && raw.path !== '.') {
    // Check if Dockerfile exists at app path (fs access)
    // If exists → auto-switch to 'dockerfile' build
}
```

**DoD:**
- [ ] CLI detecta Dockerfile no app path quando `build: buildpack`
- [ ] Auto-seta `build: dockerfile` com warning
- [ ] Teste: buildpack + Dockerfile presente → switch to dockerfile

### V7: Unified Validator Output

**Quando:** `loopzero validate` ou pré-deploy

**Output format:**
```
$ loopzero validate

  Validating loopzero.yaml...

  ✓ Project name: aquim-test
  ✓ Apps: 2 (api, web)
  ✓ Pods: 1 (api) — within limit (max 2)
  ✓ Static: 1 (web) — CDN delivery
  ✓ Frameworks: python (api), vite (web)
  ✓ Dockerfiles: api/Dockerfile found
  ✓ Build strategy: api=dockerfile, web=buildpack

  Ready to deploy. Run: loopzero deploy
```

**Error format:**
```
$ loopzero validate

  Validating loopzero.yaml...

  ✗ 2 errors found:

  1. App 'api': unknown framework 'sinatra'
     Fix: Use 'framework: custom' and add a Dockerfile to apps/api/

  2. App 'processor': exceeds pod limit
     Fix: Maximum 2 container apps per project. Combine services or split into separate projects.

  Run: loopzero validate --fix to see suggested loopzero.yaml changes
```

**DoD:**
- [ ] `loopzero validate` mostra summary claro
- [ ] Erros numerados com fix sugerido
- [ ] `--json` output com schema estável
- [ ] Roda automaticamente antes de `loopzero deploy`

## Sequenciamento

```
V2 (frontend=static auto) → V1 (framework check) → V4 (Dockerfile detect) → V3 (max pods)
     ↓
V5 (server-side check) → V6 (context auto-detect) → V7 (output format)
```

V2 é o mais impactante — resolve o problema #1 que tive com aquim.ai.

## Impacto nos loopzero-stacks templates

| Template | Mudança necessária |
|----------|-------------------|
| monorepo-turbo | `render: server` → remover (auto-inferred as static) |
| monorepo-go | `framework: custom` → adicionar Dockerfile nos apps |
| monorepo-ruby | `framework: custom` → adicionar Dockerfile (ou `framework: sinatra` se adicionarmos suporte) |
| monorepo-python | Remover `port: 8001` do worker |
| fullstack-nextjs | OK (mas `render: server` deveria ser removido — SSR via static ou container?) |
| Todos | Remover `commands` block (não reconhecido pelo CLI atual) |

## Frameworks a considerar adicionar

| Framework | Language | Detecção | Prioridade |
|-----------|---------|----------|------------|
| `sinatra` | Ruby | Gemfile + `sinatra` dep | Média (loopzero-stacks usa) |
| `rails` | Ruby | Gemfile + `rails` dep | Alta (popular) |
| `spring` | Java | pom.xml + spring-boot | Alta (loopzero-stacks usa) |
| `laravel` | PHP | composer.json + laravel | Média (loopzero-stacks usa) |
| `axum` | Rust | Cargo.toml + axum dep | Média (loopzero-stacks usa) |
| `slim` | PHP | composer.json + slim | Baixa |
