# Roadmap Executável — Sprint 2: `loopzero deploy` Happy Path

> **Spec:** §23 Sprint 2 · **Semanas:** 6-9
> **Princípio:** Convergência primeiro — a vertical slice prova que o sistema funciona antes de qualquer polish.

---

## Índice

- [Fase 0 — Pré-requisitos de Infra](#fase-0--pré-requisitos-de-infra)
- [Fase 1 — Vertical Slice: CLI → API → Build → Release → URL](#fase-1--vertical-slice-cli--api--build--release--url)
- [Fase 2 — Serialização e Concorrência](#fase-2--serialização-e-concorrência)
- [Fase 3 — SSE Streaming e Observabilidade do Build](#fase-3--sse-streaming-e-observabilidade-do-build)
- [Fase 4 — CLI Polish e CI Mode](#fase-4--cli-polish-e-ci-mode)
- [Fase 5 — Testes de Convergência e Hardening](#fase-5--testes-de-convergência-e-hardening)
- [Grafo de Dependências](#grafo-de-dependências)
- [Exit Gate](#exit-gate)
- [Known Limitations](#known-limitations)

---

## Decisões de Arquitetura e Contratos

Respostas explícitas a perguntas que devem estar respondidas antes de implementar.

### DA-1: Default environment é `production` (permanente — stability contract)

`loopzero deploy` sem `--env` deploya em `production`. Isso é permanente e faz parte do CLI stability contract (§0.2). Todos os componentes (CLI, API, testes) devem usar este default.

### DA-2: `--no-wait` sempre retorna exit 0 se submit aceito

`--no-wait` retorna imediatamente após o submit (202). Exit codes 2/3/5 **não são emitidos** neste modo — o CLI não espera o resultado. Mensagem ao retornar: `"Deploy started (build-xxxxx). Run: loopzero status to check progress."` Isso é by-design, não um bug.

### DA-3: Exit code 5 (partial failure) somente com 2+ apps

Exit code 5 (partial failure) ocorre apenas quando **2 ou mais apps** são solicitadas e pelo menos uma falha. Se apenas 1 app é solicitada (explicitamente via `--app` ou monorepo com 1 app), falha → exit code 2 (build) ou 3 (deploy).

### DA-4: `hashtext()` inclui tenant_id — isolamento multi-tenant garantido

O lock key é `hashtext('{tenant}:{project}:{env}')` — tenant_id é parte da chave. Dois tenants com mesmo project+env geram hashes diferentes. O collision risk (~0.001%) é cross-key (birthday paradox), não cross-tenant por design. Se collision ocorrer, o impacto é false serialization (liveness), não data corruption (safety). Spec §4.2 confirma.

### DA-5: Schema JSON do deploy e relação build_id/deploy_id

**Relação entre IDs (spec §9):**
- `build_id`: `build-{8hex}` — identifica a execução do build (usado em `loopzero logs --build`, SSE stream, API)
- `release_id`: `rel-{8hex}` — identifica o release criado a partir do build (usado internamente, DB, environment_targets)
- `deploy_id`: `d-{4hex}` — **alias user-facing do release_id** (usado em CLI output, `--json`, `loopzero inspect`, `loopzero rollback`). Gerado como truncamento/derivação do release_id para DX

O `deploy_id` no schema §0.3 é o que o usuário vê. O `build_id` é o que o CLI usa para conectar ao SSE stream. O `release_id` é interno.

**Schema JSON do deploy response (spec §0.3):**
```json
{
  "deploy_id": "d-8f3a",
  "status": "success|partial_failure",
  "duration_seconds": 97,
  "apps": [
    {
      "name": "api",
      "status": "deployed|failed|skipped",
      "url": "https://...",
      "build_duration_seconds": 45,
      "error": "error message if failed"
    }
  ]
}
```
Todas as tasks (S2.8, S2.12, S2.17) referenciam este schema. O campo `build_log` aparece em partial failure: `"build_log": "loopzero logs --build b-4a2c"` — usa o `build_id` curto `b-{4hex}` (DA-11).

**Fluxo de IDs:**
1. CLI gera Idempotency-Key (UUID)
2. API cria Build (`build-{8hex}`) → retorna `build_id` + `stream_url`
3. Build completa → API cria Release (`rel-{8hex}`) com `deploy_id` derivado (`d-{4hex}`)
4. SSE evento `deploy.completed` inclui `deploy_id` (user-facing) e `build_id` (para logs)

### DA-6: Health probe na URL antes de marcar ACTIVE

Após ArgoCD confirmar sync, a API faz HTTP GET na URL gerada (timeout: 30s, 3 retries com backoff 5s). Apenas se HTTP 200-399: Release → ACTIVE. Se probe falha: Release → SYNC_FAILED com reason `HEALTH_PROBE_FAILED`. Sem isso, a URL pode estar no ingress mas o container em CrashLoopBackOff.

### DA-7: Circuit breaker in-process é known limitation para v1

Cada réplica da API tem seu próprio circuit breaker state. Uma réplica pode ter circuit OPEN enquanto outra está CLOSED. Para v1 (single replica) isso é aceitável. Para multi-replica: upgrade para PG-backed state é tech debt explícito. Documentado em Known Limitations.

### DA-8: Mensagem de secrets faltando não sugere comando inexistente

Quando `loopzero deploy` detecta secrets faltando (Sprint 2), a mensagem é:
```
Missing secrets for env "production": DATABASE_URL, JWT_SECRET
Set secrets before deploying. See: https://docs.loopzero.example/secrets
```
NÃO sugere `loopzero secret set` (que não existe até Sprint 3). Será atualizada na Sprint 3 quando o comando existir.

### DA-9: `--force` NÃO existe em `loopzero deploy` (Sprint 2)

Na spec, `--force` é definido exclusivamente para:
- `loopzero rollback` — acknowledge irreversible side effects (§5.5)
- CI mode confirmations — skip `[Y/n]` prompts (§0.3)

`loopzero deploy` **não tem flag `--force`** na Sprint 2. Se no futuro for necessário (ex: cancelar build ativo e resubmeter), será adicionado com semântica explícita. Flags sem comportamento definido não entram no stability contract.

### DA-10: `environment` é case-sensitive, lowercase only

Nomes de environment são lowercase (`production`, `staging`, `pr-142`). Validação:
- `PRODUCTION`, `Production` → 400 `"Environment names must be lowercase."`
- Regex: `^[a-z][a-z0-9-]{0,62}$` (RFC 1123 subdomain label compatible)
- Validado tanto no CLI (antes do request) quanto na API (defense in depth)

### DA-11: IDs user-facing — `deploy_id` e `build_id` curto

**IDs internos → aliases user-facing (spec §0.3):**

| Interno | User-facing | Formato | Exemplo |
|---------|-------------|---------|---------|
| `release_id` (`rel-{8hex}`) | `deploy_id` | `d-{4hex}` | `rel-8f3a2b1c` → `d-8f3a` |
| `build_id` (`build-{8hex}`) | `build_id` curto | `b-{4hex}` | `build-4a2c8f1b` → `b-4a2c` |

- Aliases user-facing usados em toda saída de CLI, JSON, `loopzero inspect`, `loopzero rollback`, `loopzero logs --build`
- IDs internos completos nunca aparecem em user-facing output
- **Escopo de unicidade de `deploy_id`:** único dentro de `(tenant, project, environment)`. Não é globalmente único (65.536 valores). Os comandos `loopzero rollback` e `loopzero inspect` resolvem pelo contexto do diretório atual (tenant+project via `loopzero.yaml`, env via `--env` ou default). Se ambíguo (collision), API retorna o mais recente
- **Escopo de unicidade de `build_id` curto:** mesmo escopo — `(tenant, project, environment)`

### DA-12: git bundle contém apenas arquivos rastreados pelo Git

`git bundle create` empacota apenas conteúdo tracked pelo Git. `node_modules/`, `dist/`, arquivos não-committed não são incluídos. Isso é correto: o build remoto instala dependências via `install-deps` step. Se o bundle não contém `package.json` ou similar, o build falha com mensagem clara no step de install.

### DA-13: Idempotency-Key TTL expirado = request novo

Keys expiram após 24h (spec §4.3). Key expirada é tratada como request novo (não replay). O CLI gera UUID v4 novo a cada invocação — nunca reutiliza keys. TTL expirado só afeta scripts que reutilizam keys manualmente (edge case aceitável).

### DA-14: SSE event para health probe — feedback durante verificação

Durante o health probe (DA-6), a API emite evento SSE intermediário:
- `release.health_check` com `status: "checking"` antes do probe
- `release.health_check` com `status: "ok"` ou `status: "failed"` após resultado
O CLI renderiza: `⠋ Checking service health...` → `✓ Service healthy [8s]`
Sem isso, o usuário vê silêncio de até 30s entre sync e ACTIVE.

### DA-15: Mecanismo de callback Argo → API

O Argo Workflow notifica a API ao completar via HTTP callback no step `finalize`:

**Endpoint:** `POST /api/v1/builds/:id/callback`
- Auth: `Authorization: Bearer {LOOPZERO_ARGO_CALLBACK_SECRET}` (shared secret via env var — Argo não tem contexto OAuth)
- Body: `{ status: "succeeded"|"failed", apps: [{ name, image_digest, url, duration_ms, error? }] }`
- Idempotente: callback duplicado (retry do Argo) não cria Release duplicada — verificar se build já transitou para terminal state
- **Validação:** callback com `status: "succeeded"` e `apps: []` (vazio) → 400 `"Callback with succeeded status must include at least one app"`. Build permanece RUNNING (aguarda retry ou timeout)
- Retry: o step `finalize` faz retry 3× com backoff 5s se API retornar 5xx

**Resposta assíncrona (EC-R):** O callback retorna **202 Accepted imediatamente** após validar o payload e enfileirar `OnBuildCompleted` para execução em goroutine separada. O step `finalize` do Argo NÃO fica bloqueado aguardando sync + probe (que pode levar até 150s). Sem isso, o step `finalize` ficaria preso pelo Lock 2 (DA-17) e o Argo poderia timeout o step antes da ativação completar.

**Por que HTTP callback e não polling/webhook:**
- Argo Workflows tem suporte nativo a `http` template steps no `finalize`
- Polling desperdiça recursos e adiciona latência
- O step `finalize` roda `always` (mesmo em falha) — garante notificação
- Resposta 202 (async) garante que o step completa rápido independente do Lock 2

**Race condition (EC-N):** Se Argo completa antes de `SubmitBuild` transicionar para RUNNING, o callback encontra Build em QUEUED. `OnBuildCompleted` deve: verificar estado atual, se QUEUED → fazer `QUEUED → RUNNING → SUCCEEDED/FAILED` atomicamente (duas transições em sequência). Teste obrigatório.

### DA-16: Release state PENDING — semântica e transições

`Release PENDING` = release aguardando ArgoCD sync confirmation.

**Quem transiciona:** `BuildOrchestrator.OnBuildCompleted` faz `READY → PENDING` ao chamar `TriggerSync`.
**Evento SSE:** `release.pending` — CLI renderiza: `⠋ Deploying...`
**Transições saindo de PENDING:**
- `PENDING → ACTIVE`: sync ok + probe ok
- `PENDING → SYNC_FAILED`: sync timeout (2min) ou probe failure
- `PENDING → CANCELLED`: **somente via `ACTIVATION_TIMEOUT` na Sprint 2**. Cancel de build não retrocede um Release já em PENDING — o Release segue seu ciclo (ACTIVE ou SYNC_FAILED). Cancel explícito de Release fica para Sprint 4 (rollback/cancel)

**Timeout do ciclo completo (activation):** `ACTIVATION_TIMEOUT` = 150s (sync 120s max + probe 30s max). Se o ciclo completo (sync + probe) exceder 150s → Release SYNC_FAILED com reason `ACTIVATION_TIMEOUT`. Evento SSE: `release.sync_failed`. CLI exit code 3.

### DA-17: Dois locks separados no deploy flow — spec §10.1

O deploy flow usa **dois lock acquisitions separados** (spec §10.1 linhas 710):
1. **Lock 1 (steps 4-11):** submit — check concurrent build, create Build, submit Argo. Lock liberado quando a transação comete. Build roda SEM lock.
2. **Lock 2 (steps 14a-14g):** activation — update environment_targets, sync ArgoCD, health probe. Lock held até ACTIVE/SYNC_FAILED.

**Implicação:** entre Lock 1 e Lock 2, outras operações podem adquirir o lock. O Lock 2 pode ficar aberto por até 150s (ACTIVATION_TIMEOUT). Durante esse tempo, nenhuma outra mutação no mesmo `(tenant, project, env)` é possível. Para Sprint 2 (single user, demo), aceitável. O circuit breaker (DA-7/S2.20) evita que ArgoCD degradado cause locks presos por 2min em todos os tenants.

**O passo 3 de OnBuildCompleted (criar Release READY, gitops-push) acontece DENTRO do Lock 2** — o lock é adquirido antes de criar a Release, não depois. Sequência: adquire lock → cria Release → gitops-push → READY → PENDING → sync → probe → ACTIVE → libera lock.

### DA-18: Mensagem de CLI para 409 DEPLOY_IN_PROGRESS

Quando a API retorna `409 DEPLOY_IN_PROGRESS`, o CLI exibe:
```
A deploy is already in progress for this environment.
Run: loopzero status to check progress.
```
Exit code: **3** (deploy failure — não é usage error). Sem termos internos (sem mention de "lock" ou "advisory").

### DA-19: `--app` aceita múltiplos valores

`loopzero deploy --app api --app worker` deploya apenas as apps especificadas. Validação:
- Cada app especificada DEVE existir no `loopzero.yaml`
- Se qualquer app não existe → exit 1 **antes de qualquer upload** (fail fast, não parcial)
- Sem `--app`: todas as apps do `loopzero.yaml`

---

## Fase 0 — Pré-requisitos de Infra

> Objetivo: garantir que o TenantController e a infra de Sprint 1 estão deployados e operacionais no cluster. Sem isso, nenhum namespace existe e nenhum E2E roda.

### Task S2.0: Tiltfile + Deploy TenantController via Tilt

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §19 — Local Infrastructure, §7.3.1 — TenantController
**Depende de:** nenhuma (Sprint 1 prerequisite — controller já implementado, precisa estar rodando)
**Estimativa:** M

#### O que implementar
- `Tiltfile` na raiz do repositório:
  - Build e deploy do operator (TenantController) no mgmt cluster
  - Build e deploy do API server
  - Hot reload para ambos
  - Port-forward para API
  - Dependências: PostgreSQL, Gitea, MinIO (via Helm)
- Validar que ao rodar `tilt up`:
  - TenantController está rodando e reconciliando
  - API server está acessível
  - CRDs estão instalados

#### Critérios de aceite
- [ ] `tilt up` levanta operator + API + dependências
- [ ] **Startup order via `resource_deps`:** PostgreSQL → Gitea → MinIO → API server → TenantController (API não sobe antes do PG estar ready)
- [ ] TenantController reconcilia Tenant CR → cria namespaces, Gitea repo, RBAC
- [ ] API health check retorna 200
- [ ] Hot reload funciona (mudar código → redeploy automático)
- [ ] `task dev:up` levanta clusters k3d e dependências Helm. `tilt up` é executado separadamente para o dev loop (não chamado por `dev:up` — Tilt fica em foreground)
- [ ] `tilt up` em cluster limpo: zero crash loops de API durante startup
- [ ] **`LOOPZERO_ARGO_CALLBACK_SECRET` (DA-15):** gerado e injetado no API server e nos WorkflowTemplate parameters durante `tilt up`. Local dev: valor fixo `dev-callback-secret`. Produção: gerado por Helm

#### Teste primeiro (TDD)
- Validação manual: `task dev:up && tilt up`, criar Tenant CR, verificar namespaces
- Teste de fumaça: `curl localhost:<port>/api/v1/health` → 200
- Teste de startup order: derrubar PG, rodar `tilt up`, verificar que API aguarda PG (não crashloopa)

#### Riscos conhecidos
- Tiltfile não existe ainda — é a primeira vez que o dev loop completo é configurado
- Dependências Helm podem ter conflitos de versão
- `resource_deps` requer health checks nos recursos dependentes (PG readiness probe etc)

---

### Task S2.0b: ClusterCredentialsOperator — Escopo e Deploy

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.3 — ClusterCredentialsOperator
**Depende de:** S2.0
**Estimativa:** M

#### O que implementar
- **Esclarecer escopo:** O CRD `ClusterCredential` já existe em `operators/api/v1/clustercredential_types.go`. O que falta é o controller que reconcilia:
  - Lê kubeconfig do Secret referenciado em `spec.secretRef`
  - Valida conectividade com o cluster target
  - Distribui credentials para ArgoCD (cria ArgoCD cluster secret)
  - Atualiza `status.connected` e `status.lastCheckedAt`
- `operators/internal/controller/clustercredential_controller.go`:
  - Reconcile: read Secret → test connection → create/update ArgoCD cluster secret
  - Periodic recheck: a cada 5 minutos, revalida conectividade
  - Se cluster unreachable: `status.connected = false`, condition `Connected=False`
- Adicionar ao Tiltfile para deploy automático

#### Critérios de aceite
- [ ] Controller reconcilia ClusterCredential CR
- [ ] ArgoCD cluster secret criado com credentials válidas
- [ ] Connectivity check periódico funciona
- [ ] Cluster unreachable → status.connected = false, condition `Connected=False`
- [ ] **`spec.secretRef` apontando para Secret inexistente → condition `SecretNotFound=True`, requeue a cada 30s, sem panic**
- [ ] Testes com envtest

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/clustercredential_controller_test.go`
- Testes: reconcile happy path, cluster unreachable, periodic recheck, ArgoCD secret creation, **Secret inexistente → requeue sem crash**

#### Riscos conhecidos
- Cross-cluster connectivity em ambiente local (k3d) — pode exigir configuração de rede entre clusters
- Secret referenciado pode ser deletado externamente — controller deve ser resiliente

---

## Fase 1 — Vertical Slice: CLI → API → Build → Release → URL

> Objetivo: provar que `loopzero deploy` gera um app rodando com URL real. Um único app, happy path, sem concorrência, sem streaming sofisticado. Se isso não funciona, nada mais importa.
>
> **Limitação temporária:** Nas Fases 1-2, o CLI exibe apenas `"Deploy submitted (build-xxxxx)..."` sem progresso em tempo real. O streaming SSE com spinners e step-by-step só é integrado na Fase 3. Isso é intencional — primeiro validar o pipeline, depois o streaming.

### Task S2.1: Migration — partial unique index e builds_apps

**Componente:** api
**Agent:** api-engineer
**Spec:** §4.2 — Environment Lock, §9 — Data Model
**Depende de:** nenhuma
**Estimativa:** P

#### O que implementar
- Migration `000003_deploy_support.up.sql`:
  - `CREATE UNIQUE INDEX idx_one_active_build_per_env ON builds (tenant_id, project_id, environment) WHERE status IN ('QUEUED', 'RUNNING');` (spec §4.2)
  - Tabela `build_apps` para registrar apps individuais de cada build (name, status, duration, error, image_digest, url)
  - Adicionar coluna `apps` (JSONB) em `releases` para armazenar `AppRelease[]`
- Não criar migration down (forward-only, conforme CLAUDE.md)

#### Critérios de aceite
- [ ] Migration aplica sem erros em banco limpo
- [ ] Partial unique index impede dois builds QUEUED/RUNNING para mesmo (tenant, project, env)
- [ ] Permite dois builds SUCCEEDED/FAILED para mesmo (tenant, project, env)
- [ ] Teste de migration valida constraints

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/migration_003_test.go`
- Testar: index permite insert quando não há build ativo, bloqueia quando há, permite após transição para SUCCEEDED

#### Riscos conhecidos
- Nenhum — operação aditiva, não afeta schema existente

---

### Task S2.2: withEnvironmentLock

**Componente:** api
**Agent:** api-engineer
**Spec:** §4.2 — Environment Lock (advisory lock wrapper)
**Depende de:** nenhuma
**Estimativa:** P

#### O que implementar
- Arquivo: `api/internal/lock/environment_lock.go`
- Função `WithEnvironmentLock(ctx, db, tenant, project, env, fn) (T, error)`:
  - Abre transação `READ COMMITTED`
  - `SET LOCAL lock_timeout = '5s'`
  - `SELECT pg_advisory_xact_lock(hashtext($1))` onde `$1 = tenant:project:env`
  - Executa `fn(tx)` dentro da transação
  - Commit automático no sucesso, rollback no erro
- Retornar erro tipado `ErrOperationInProgress` quando lock timeout (mapeado para 409 no handler)
- Código de detecção de `lock_timeout` do PostgreSQL (error code `55P03`)
- **Comentário obrigatório no código:** explicar por que `pg_advisory_xact_lock` (transaction-scoped) e NUNCA `pg_advisory_lock` (session-scoped) — session-scoped persiste em connection pool reuse → deadlocks

#### Critérios de aceite
- [ ] Lock adquirido e liberado corretamente no happy path
- [ ] Timeout de 5s retorna `ErrOperationInProgress`
- [ ] Lock liberado automaticamente em panic/crash (rollback)
- [ ] Usa `pg_advisory_xact_lock` (NUNCA `pg_advisory_lock`)
- [ ] Comentário no código explicando por que `xact` e não `session`
- [ ] Dois tenants diferentes, mesmo project+env → locks independentes (isolamento multi-tenant via DA-4)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/environment_lock_test.go`
- Testes com banco real (testcontainers/pgx direto):
  1. Lock adquire e libera
  2. Dois locks concorrentes (mesmo key) → segundo espera e adquire após o primeiro liberar
  3. Lock com timeout → retorna ErrOperationInProgress
  4. Panic dentro do fn → lock liberado
  5. **Dois tenants diferentes, mesmo project+env → ambos adquirem lock simultaneamente (sem conflito)**

#### Riscos conhecidos
- Requer PostgreSQL real nos testes (não pode ser mockado — por design)
- `hashtext()` tem collision risk aceitável (~0.001% com 2000 envs, spec §4.2). Impacto: false serialization (liveness), não data corruption (safety)

---

### Task S2.3: BuildStore e ReleaseStore

**Componente:** api
**Agent:** api-engineer
**Spec:** §9 — Data Model, §5.1 — Build SM, §5.2 — Release SM
**Depende de:** S2.1
**Estimativa:** M

#### O que implementar
- `api/internal/db/build_store.go`:
  - `Create(ctx, tx, build) error` — insere build com status QUEUED
  - `GetByID(ctx, id) (*Build, error)`
  - `HasActiveBuild(ctx, tx, tenantID, projectID, env) (bool, error)` — query com `FOR UPDATE NOWAIT` dentro do lock
  - `UpdateStatus(ctx, tx, id, status, fields) error` — atualiza via state machine (import do pkg)
  - `ListByProject(ctx, tenantID, projectID, limit, offset) ([]Build, error)`
- `api/internal/db/release_store.go`:
  - `Create(ctx, tx, release) error` — insere release com status READY
  - `GetByID(ctx, id) (*Release, error)`
  - `UpdateStatus(ctx, tx, id, status, fields) error`
  - `GetActiveByEnv(ctx, tenantID, projectID, env) (*Release, error)`
- Tipos Go que espelham as tabelas existentes (migration 000001)

#### Critérios de aceite
- [ ] CRUD completo para builds e releases
- [ ] `HasActiveBuild` usa `FOR UPDATE NOWAIT` (spec §4.2)
- [ ] Status transitions validadas via state machine (nunca update direto)
- [ ] Testes contra banco real

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/build_store_test.go`, `api/tests/unit/release_store_test.go`
- Testes: create, get, update status com transição válida, transição inválida panic, HasActiveBuild retorna true/false

#### Riscos conhecidos
- State machine vive em `operators/internal/statemachine/`. A API precisa importar. Opções: (a) `go.work` resolve via workspace, (b) extrair para `pkg/statemachine`. Verificar qual é mais limpo. Se (b), criar task auxiliar

---

### Task S2.4: EnvironmentTargetStore

**Componente:** api
**Agent:** api-engineer
**Spec:** §9 — environment_targets table, §3 — Source of Truth
**Depende de:** S2.1
**Estimativa:** P

#### O que implementar
- `api/internal/db/environment_target_store.go`:
  - `SetDesired(ctx, tx, tenantID, projectID, env, releaseID, updatedBy, correlationID) error` — UPSERT
  - `SetActive(ctx, tx, tenantID, projectID, env, releaseID) error`
  - `Get(ctx, tenantID, projectID, env) (*EnvironmentTarget, error)`
- Operações dentro de transação (chamadas dentro do lock)

#### Critérios de aceite
- [ ] UPSERT funciona (insert na primeira vez, update nas seguintes)
- [ ] Get retorna nil quando não existe (não erro)
- [ ] Todas as operações recebem tx (não criam transação própria)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/environment_target_store_test.go`
- Testes: set desired, set active, get existente, get inexistente

#### Riscos conhecidos
- Nenhum — tabela já existe na migration 000001

---

### Task S2.5: SSE Event Journal — Store

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.1 — SSE reconnection protocol
**Depende de:** nenhuma
**Estimativa:** P

#### O que implementar
- `api/internal/sse/event_store.go`:
  - `Append(ctx, buildID, eventType, data) (eventID int64, error)`
  - `GetAfter(ctx, buildID, afterID) ([]SSEEvent, error)` — para replay com Last-Event-ID
  - `Prune(ctx, olderThan time.Duration) (int64, error)` — cleanup de eventos antigos
- Tipo `SSEEvent { ID int64, BuildID string, EventType string, Data json.RawMessage, CreatedAt time.Time }`

#### Critérios de aceite
- [ ] Append retorna ID sequencial
- [ ] GetAfter retorna eventos ordenados por ID
- [ ] GetAfter com ID inexistente retorna lista vazia (não erro)
- [ ] Prune remove eventos antigos

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/sse_event_store_test.go`
- Testes: append, get after, ordering, prune

#### Riscos conhecidos
- Nenhum — tabela sse_events já existe na migration 000001

---

### Task S2.6: Source Upload — Code Ingestion (Mode B)

**Componente:** api
**Agent:** api-engineer
**Spec:** §10.1 step 0b — Code Ingestion (CLI push)
**Depende de:** nenhuma
**Estimativa:** M

#### O que implementar
- Route: `POST /api/v1/source/upload`
  - Recebe git bundle do CLI (multipart ou raw body)
  - Valida `loopzero.yaml` no bundle (estrutura, campos obrigatórios)
  - **Validação de app types:** cada app DEVE ter `port` (número válido) se `type: server`, OU estar marcado como `type: worker`/`type: frontend`. Ambiguidade (sem port e sem type) → 422 com campo específico
  - Push bundle para Gitea source repo do tenant/project
  - Retorna commit SHA e lista de apps detectados
- Interface `SourceStore` (DIP) para abstrair Gitea:
  - `PushBundle(ctx, tenantID, projectID, bundle io.Reader) (commitSHA string, error)`
- Implementação fake para testes: armazena em memória
- **Limite de tamanho do bundle:** 500MB max (rejeitar com 413 Payload Too Large)
- **Contrato do bundle (DA-12):** bundle contém apenas arquivos tracked pelo Git. `node_modules/`, `dist/` não são incluídos. O build remoto instala dependências no step `install-deps`.

#### Critérios de aceite
- [ ] Upload aceita bundle válido e retorna SHA
- [ ] Rejeita bundle sem `loopzero.yaml` → 422
- [ ] Rejeita `loopzero.yaml` com campos obrigatórios faltando → 422
- [ ] Rejeita `loopzero.yaml` com 0 apps (`apps: {}` ou ausente) → 422 `"No apps defined in loopzero.yaml"`
- [ ] Rejeita app sem `port` e sem `type: worker/frontend` → 422 com campo e mensagem
- [ ] Tamanho máximo 500MB → 413
- [ ] Interface permite substituição por fake em testes

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/source_upload_test.go`
- Testes: upload válido, loopzero.yaml ausente, loopzero.yaml inválido, 0 apps, app type ambíguo (sem port nem worker), bundle muito grande (>500MB)

#### Riscos conhecidos
- Integração real com Gitea precisa do cluster rodando — testes unitários usam fake, integração na Fase 5

---

### Task S2.7: BuildOrchestrator — Core do Deploy

**Componente:** api
**Agent:** api-engineer
**Spec:** §10.1 steps 4-14 — Build submission + activation com serialização
**Depende de:** S2.2, S2.3, S2.4, S2.5, S2.6
**Estimativa:** G

#### O que implementar
- `api/internal/services/build_orchestrator.go`:
  - `SubmitBuild(ctx, req SubmitBuildRequest) (*SubmitBuildResponse, error)`:
    1. Adquire environment lock via `WithEnvironmentLock`
    2. Valida tenant ativo
    3. Verifica build concorrente (`HasActiveBuild`) — DENTRO do lock
    4. Cria Build record (QUEUED)
    5. Gera ephemeral credentials (interface, fake por enquanto)
    6. Submete Argo Workflow (interface `WorkflowSubmitter`)
    7. Transiciona Build para RUNNING
    8. Emite evento SSE (build.started)
    9. Retorna build ID + stream URL
  - `OnBuildCompleted(ctx, buildID, result)` — chamado pelo callback endpoint (DA-15):
    1. **Race condition guard (DA-15 EC-N):** se Build está em QUEUED (Argo completou antes de SubmitBuild transicionar para RUNNING), fazer `QUEUED → RUNNING → SUCCEEDED/FAILED` atomicamente
    2. Transiciona Build para SUCCEEDED ou FAILED
    3. **Adquire environment lock (Lock 2 — DA-17).** Tudo abaixo roda dentro do lock:
    4. Cria Release (READY), emite gitops-push, transiciona Release `READY → PENDING` (DA-16), emite SSE `release.pending`
    5. **Ativa release (spec §10.1 step 14):**
       a. Atualiza `environment_targets.desired_release_id`
       b. Notifica ArgoCD sync
       c. **Health probe:** HTTP GET na URL gerada (timeout 30s, 3 retries, backoff 5s) — somente para apps `type: server` e `type: frontend`
       d. Sync success + probe ok → Release ACTIVE, previous → SUPERSEDED
       e. Sync timeout (2min) → Release SYNC_FAILED
       f. Probe failure → Release SYNC_FAILED (reason: `HEALTH_PROBE_FAILED`)
       g. Libera environment lock
    6. **Emite eventos SSE para cada transição, incluindo DA-14:**
       - `release.health_check { status: "checking" }` antes do probe
       - `release.health_check { status: "ok"|"failed", duration_ms }` após resultado
  - **Health probe URL (DA-6 detalhamento):**
    - URL primária: gerada pelo ingress, port principal declarado no `loopzero.yaml`
    - Path: `healthCheckPath` do `loopzero.yaml` se definido, senão `/health` se existir (HEAD request), senão `/`
    - Aceita HTTP 200-399 como sucesso
    - Workers (`type: worker`): skip probe (sem URL)
    - Frontends (`type: frontend`): probe na URL do ingress (mesma URL entregue ao usuário — `https://{env}.{project}.{tenant}.loopzero.example`). O ingress faz proxy para o MinIO bucket. Probe no MinIO direto causaria false negative (bucket policy pode retornar 403).
  - **Build timeout:** configurable via env var `LOOPZERO_BUILD_TIMEOUT` (default: 300s). Background goroutine monitora. Se timeout: Build → FAILED (reason: `BUILD_TIMEOUT`), evento SSE `build.failed`, CLI exit code 2
  - **Activation timeout (DA-16):** `ACTIVATION_TIMEOUT` = 150s para ciclo completo (sync + probe). Excedido → Release SYNC_FAILED (reason: `ACTIVATION_TIMEOUT`)
- Interface `WorkflowSubmitter`:
  - `Submit(ctx, WorkflowSpec) (workflowID string, error)`
  - `Cancel(ctx, workflowID) error`
- Interface `GitOpsPusher`:
  - `Push(ctx, PushRequest) (commitSHA string, error)`
- Interface `HealthProber`:
  - `Probe(ctx, url string, path string) error` — HTTP GET, expects 200-399

#### Critérios de aceite
- [ ] Happy path: submit → QUEUED → RUNNING → SUCCEEDED → Release READY → PENDING → ACTIVE
- [ ] Concurrent build check DENTRO do lock (NUNCA fora — spec §4.2 race condition)
- [ ] Build concorrente retorna 409 DEPLOY_IN_PROGRESS
- [ ] Lock timeout retorna 409 OPERATION_IN_PROGRESS
- [ ] Eventos SSE emitidos em cada transição
- [ ] Interfaces para workflow, gitops, health probe (não acoplamento direto)
- [ ] **Health probe na URL antes de ACTIVE** (DA-6)
- [ ] Health probe failure → SYNC_FAILED com reason HEALTH_PROBE_FAILED
- [ ] Workers (type: worker) skip health probe (sem URL)
- [ ] Frontends: probe na URL do ingress (não MinIO direto)
- [ ] **Build timeout** → FAILED com reason BUILD_TIMEOUT
- [ ] **Activation timeout (DA-16)** → SYNC_FAILED com reason ACTIVATION_TIMEOUT
- [ ] `environment_targets` atualizado atomicamente dentro do lock
- [ ] **Race condition (DA-15 EC-N):** callback com Build em QUEUED → transição atômica QUEUED→RUNNING→terminal sem panic
- [ ] **Callback duplicado (DA-15):** build já em terminal state → no-op, retorna resultado existente
- [ ] Release anterior → SUPERSEDED quando novo release fica ACTIVE
- [ ] SSE `release.pending` emitido na transição READY → PENDING (DA-16)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/build_orchestrator_test.go`
- Testes:
  1. Submit happy path (fakes para workflow, gitops, health prober)
  2. Submit com build concorrente → 409
  3. Submit com lock timeout → 409
  4. OnBuildCompleted success → release READY → PENDING → probe ok → ACTIVE
  5. OnBuildCompleted success → probe fails → SYNC_FAILED (HEALTH_PROBE_FAILED)
  6. OnBuildCompleted failure → build FAILED, sem release
  7. Build timeout → FAILED (BUILD_TIMEOUT)
  8. Activation timeout → SYNC_FAILED (ACTIVATION_TIMEOUT)
  9. Worker app → skip health probe
  10. Frontend app → probe na URL do ingress (não MinIO)
  11. Environment targets updated atomicamente
  12. **Callback com Build em QUEUED (EC-N) → QUEUED→RUNNING→SUCCEEDED sem panic**
  13. **Callback duplicado → no-op**
  14. Release anterior transiciona para SUPERSEDED
  15. **Callback com `apps: []` + status succeeded → 400, build permanece RUNNING (DA-15)**
  16. Lock 2 liberado após ACTIVE ou SYNC_FAILED (DA-17) — não antes

#### Riscos conhecidos
- Orquestração complexa — muitos pontos de falha. Manter o teste cobrindo cada decisão
- State machine do build vive em `operators/` — pode ser necessário extrair para `pkg/` (ver S2.3)
- Health probe adiciona latência (~30s max) ao deploy. Aceitável para garantia de liveness
- Race condition EC-N é real em ambiente local com builds rápidos — teste obrigatório

---

### Task S2.8: POST /api/v1/builds — Route

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2 — POST /builds, §10.1 steps 1-3, §0.3 — JSON schema (DA-5), DA-10, DA-11
**Depende de:** S2.7
**Estimativa:** M

#### O que implementar
- `api/internal/routes/builds.go`:
  - `POST /api/v1/builds`:
    - Requer auth middleware (já existe)
    - Requer `Idempotency-Key` header (middleware já existe)
    - Body: `{ project_id, environment, apps?: string[] }`
    - **Validação de `environment` (DA-10):** lowercase only, regex `^[a-z][a-z0-9-]{0,62}$`. `PRODUCTION` → 400 `"Environment names must be lowercase."`
    - Valida input
    - Chama `BuildOrchestrator.SubmitBuild`
    - Retorna 202 Accepted com `{ build_id, stream_url }` — **submit response** (não final deploy response)
    - **JSON response shape documentada conforme DA-5/DA-11.** Final deploy response (com `deploy_id` = `d-{4hex}` derivado do release_id, urls, durations) é emitido como último evento SSE e capturado pelo CLI para `--json` output
  - `GET /api/v1/builds/:id`:
    - Retorna status do build com apps, conforme schema §0.3
  - `POST /api/v1/builds/:id/cancel`:
    - Requer `Idempotency-Key`
    - Cancela build via orchestrator
  - `POST /api/v1/builds/:id/callback` **(DA-15)**:
    - Auth: `Authorization: Bearer {LOOPZERO_ARGO_CALLBACK_SECRET}` (não OAuth)
    - Body: `{ status: "succeeded"|"failed", apps: [{ name, image_digest, url, duration_ms, error? }] }`
    - **Retorna 202 Accepted imediatamente** (DA-15 EC-R). `OnBuildCompleted` roda em goroutine separada — não bloqueia o step `finalize` do Argo
    - Validação síncrona (antes do 202): auth, build existe, payload válido
    - Idempotente: duplicado (build já em terminal state) → 200 no-op
    - Erros síncronos: 401 (auth), 404 (build não existe), 400 (payload inválido/apps vazio)

#### Critérios de aceite
- [ ] POST retorna 202 com `{ build_id, stream_url }`
- [ ] Idempotency-Key replay funciona (mesmo key = mesma response)
- [ ] Idempotency-Key mismatch → 422
- [ ] Auth ausente → 401 (exit code 4)
- [ ] Body inválido → 400 (exit code 1)
- [ ] Build concorrente → 409
- [ ] GET retorna build com apps e status — JSON schema match §0.3
- [ ] Cancel transiciona build
- [ ] **Callback endpoint (DA-15):** auth via shared secret, body válido → chama OnBuildCompleted
- [ ] Callback auth inválida → 401
- [ ] Callback build inexistente → 404
- [ ] Callback duplicado (build já terminal) → no-op 200
- [ ] **Callback com `apps: []` + status succeeded → 400 (DA-15)**
- [ ] **Environment validation (DA-10):** `PRODUCTION` → 400

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/builds_route_test.go`
- Testes: happy path 202, idempotency replay, mismatch 422, auth 401, body inválido 400, concorrente 409, GET JSON shape, callback happy path, callback auth invalid, callback duplicate, callback empty apps → 400, environment uppercase → 400

#### Riscos conhecidos
- Nenhum — segue padrão dos routes existentes (auth, projects)

---

### Task S2.9: SSE Streaming — GET /builds/:id/stream

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.1 — SSE reconnection protocol, §7.2 — GET /builds/:id/stream
**Depende de:** S2.5, S2.8
**Estimativa:** M

#### O que implementar
- `api/internal/sse/stream_handler.go`:
  - `GET /api/v1/builds/:id/stream`:
    - Set headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
    - Se `Last-Event-ID` header presente: replay eventos do journal
    - Se journal expirou: enviar evento `JOURNAL_EXPIRED`
    - **Conexão após build terminal:** envia todos os eventos do journal e fecha stream imediatamente (não fica aberta)
    - **Circuit OPEN (S2.20):** quando ArgoCD circuit está OPEN, emitir evento SSE `release.sync_failed` com `reason: "PLATFORM_DEGRADED"` e fechar stream. CLI exibe: `"Deploy service is temporarily unavailable. Your build succeeded — the deployment will complete automatically. Run: loopzero status to check."` Exit code 3.
    - Poll por novos eventos (interval 1s) ou use PostgreSQL LISTEN/NOTIFY
    - Enviar eventos no formato SSE: `id: {id}\nevent: {type}\ndata: {json}\n\n`
    - Encerrar quando build atinge terminal state
    - Heartbeat a cada 15s (`:heartbeat\n\n`)
    - **Último evento SSE do build:** `deploy.completed` com payload que contém o deploy response completo (schema §0.3 DA-5)
- Tipos de evento SSE (lista completa):
  - `build.started`, `build.step.started`, `build.step.completed`, `build.step.failed`
  - `build.succeeded`, `build.failed`, `build.cancelled`
  - `release.created`, `release.pending` (DA-16), `release.health_check` (DA-14), `release.active`, `release.sync_failed`
  - `release.superseded` — emitido quando release anterior é marcado SUPERSEDED (para auditoria/replay)
  - `deploy.completed` — final event com deploy response completo

#### Critérios de aceite
- [ ] Stream envia eventos em formato SSE correto
- [ ] `Last-Event-ID` replay funciona
- [ ] Journal expirado → evento JOURNAL_EXPIRED
- [ ] Stream encerra no terminal state
- [ ] **Conexão após terminal state:** replay completo + close imediato
- [ ] Heartbeat a cada 15s
- [ ] Client disconnect não causa goroutine leak
- [ ] `deploy.completed` event contém deploy response schema §0.3
- [ ] **`release.health_check` (DA-14):** `{status: "checking"}` emitido antes do probe; `{status: "ok"|"failed", duration_ms}` emitido após resultado
- [ ] **`release.pending` (DA-16):** emitido na transição READY → PENDING
- [ ] **`release.superseded`:** emitido quando release anterior marcado SUPERSEDED

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/sse_stream_test.go`
- Testes: stream happy path, replay com Last-Event-ID, journal expirado, heartbeat, disconnect cleanup, **conexão após terminal state (replay + close)**, deploy.completed event shape, **release.health_check format (DA-14)**, **release.pending emitido (DA-16)**, **release.superseded emitido**

#### Riscos conhecidos
- SSE com Go requer `http.Flusher` — verificar compatibilidade com middlewares
- Goroutine lifecycle management — cleanup no disconnect é crítico

---

### Task S2.10: Argo WorkflowTemplate — `loopzero-build-nodejs-v1`

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §7.4 — Argo Workflows (build engine)
**Depende de:** nenhuma
**Estimativa:** G

#### O que implementar
- `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml`:
  - WorkflowTemplate com steps da spec §7.4:
    - `setup`: clone-repo, restore-cache, detect-changeset
    - `parallel-1`: security-scan, install-deps
    - `build`: build-apps
    - `publish`: upload-frontend (MinIO), build-image (Kaniko, signed)
    - `release`: generate-manifests, validate-manifests (kubeconform), sign-manifests (cosign), gitops-push
    - `finalize` (always): save-cache, emit-summary, **notify-api** (`POST /api/v1/builds/:id/callback` — DA-15, retry 3× backoff 5s)
  - Parametrizado: tenant, project, env, commit SHA, apps list, credentials, **api_callback_url, callback_secret**
  - Resource limits em todos os containers
  - Non-root security context
  - Retry: steps de rede têm retry (3x, backoff exponencial)
- Git commit format com trailers: `Release-ID`, `Correlation-ID`, `Build-ID`
- Cache keys conforme spec §7.4 (incluem tenant para isolamento)

#### Critérios de aceite
- [ ] Template aplica sem erros (`kubectl apply --dry-run=server`)
- [ ] `kubeconform --strict` passa
- [ ] Todos os containers têm resource limits
- [ ] Todos os containers rodam non-root
- [ ] Cache keys incluem tenant para isolamento (spec §7.4)
- [ ] gitops-push produz commit com trailers corretos
- [ ] Template é parametrizado (não hardcoded)
- [ ] **`finalize` step chama callback API (DA-15):** `POST /api/v1/builds/:id/callback` com resultado
- [ ] **`finalize` step roda `always`** — mesmo em falha de steps anteriores
- [ ] **Callback retry:** 3× com backoff 5s se API retornar 5xx
- [ ] Template funciona com bundle de 1 commit (shallow clone — repo pode não ter histórico profundo)

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/unit/workflow_template_test.go`
- Testes: template rendering com diferentes parâmetros, validação de resource limits, security context, cache key format, finalize step com callback URL, finalize roda em falha

#### Riscos conhecidos
- Kaniko e cosign precisam de configuração de registry — pode exigir secrets no build cluster
- MinIO endpoint precisa ser acessível do build cluster

---

### Task S2.11: ArgoCD App-of-Apps e Sync Config

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §7.5 (ArgoCD), §10.1 step 14
**Depende de:** S2.0b (ClusterCredentialsOperator precisa estar rodando para ArgoCD acessar workload cluster)
**Estimativa:** M

#### O que implementar
- ArgoCD Application para projetos tenant:
  - Template Helm para `Application` CRD
  - Source: Gitea GitOps repo path para `(tenant/project/env)`
  - Destination: workload cluster namespace
  - Sync policy: manual (API trigga sync via ArgoCD API)
  - Health check configuration
- ArgoCD RBAC: cada tenant só vê seus apps
- **Dependência:** ClusterCredential reconciliado (S2.0b) → ArgoCD tem credentials para workload cluster

#### Critérios de aceite
- [ ] Application CRD template renderiza corretamente
- [ ] `kubeconform --strict` passa
- [ ] Sync policy é manual (não auto-sync — API controla)
- [ ] RBAC impede cross-tenant access
- [ ] ArgoCD acessa workload cluster via ClusterCredential

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/unit/argocd_app_test.go`
- Testes: template rendering, RBAC rules, namespace isolation

#### Riscos conhecidos
- ArgoCD multi-tenancy é complexo — RBAC policies precisam ser testadas end-to-end

---

### Task S2.12: CLI `loopzero deploy` — Comando Base

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §0.1 Workflow 1, §25 CLI Reference — deploy, DA-2, DA-3, DA-5
**Depende de:** S2.8
**Estimativa:** M

#### O que implementar
- `cli/src/commands/deploy.ts`:
  - **Busca `loopzero.yaml`:** procura no diretório atual e sobe até a raiz do Git repo (`git rev-parse --show-toplevel`). Se não encontrar em nenhum nível → exit code 1 `"No loopzero.yaml found. Run: loopzero init"`
  - Valida estrutura
  - **Valida `.git` existe:** sem `.git` → exit code 1 com mensagem `"Run from inside a git repository."`
  - Detecta HEAD commit (`git rev-parse HEAD`)
  - Empacota código via `git bundle create --depth=1` (apenas commit atual, não todo o histórico)
  - Upload: `POST /api/v1/source/upload`
  - **Ctrl+C durante upload:** aborta o upload, cleanup do bundle temp file, exit limpo. Se Ctrl+C após submit (durante stream): fecha SSE connection, exit 130 (standard SIGINT). Build continua server-side.
  - Gera `Idempotency-Key` (UUID v4)
  - Submit: `POST /api/v1/builds` com Idempotency-Key
  - Conecta ao SSE stream: `GET /builds/:id/stream`
  - Exibe progresso (Fase 1: output simples; Fase 3: spinners)
  - Exibe URLs finais no sucesso
  - Flags: `--env` (default: production — DA-1), `--app` (single service), `--no-wait`, `--json`
  - **Sem `--force` na Sprint 2 (DA-9)** — será adicionado se/quando necessário com semântica explícita
  - **Validação de `--env` (DA-10):** lowercase only. `--env PRODUCTION` → exit 1 `"Environment names must be lowercase."`
  - **Validação de `loopzero.yaml`:** pelo menos 1 app definido. 0 apps → exit 1 `"No apps defined in loopzero.yaml. Add at least one app to deploy."`
- Exit codes:
  - 0: success (e `--no-wait` com submit aceito — DA-2)
  - 1: usage error (bad args, no `.git`, no `loopzero.yaml`)
  - 2: build failure (single app, ou `--app` com falha)
  - 3: deploy failure (sync, 409 DEPLOY_IN_PROGRESS — DA-18)
  - 4: auth error
  - 5: partial failure (2+ apps solicitadas, pelo menos uma falhou — DA-3)
- **`--no-wait` behavior (DA-2):** retorna 0 após submit aceito (202). Mensagem: `"Deploy started (build-xxxxx). Run: loopzero status to check progress."` Exit codes 2/3/5 não são emitidos neste modo.

#### Critérios de aceite
- [ ] `loopzero deploy` sem flags funciona (defaults: env=production, all apps)
- [ ] Sem `.git` → exit code 1 com `"Run from inside a git repository."`
- [ ] `--env staging` envia environment correto
- [ ] `--app api` deploya apenas uma app
- [ ] `--no-wait` retorna 0 após submit com mensagem informativa (DA-2)
- [ ] `--json` output match spec §0.3 deploy schema (DA-5), `deploy_id` = `d-{4hex}` (DA-11)
- [ ] Exit codes corretos para cada cenário, incluindo DA-3 (partial failure somente com 2+ apps)
- [ ] Nenhum termo interno na saída (Rule 3)
- [ ] CI mode (`CI=true`): sem spinners, sem cores, sem confirmações
- [ ] Git bundle usa `--depth=1` (não envia todo o histórico — DA-12)
- [ ] `--env UPPERCASE` → exit 1 (DA-10)
- [ ] 0 apps no loopzero.yaml → exit 1

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/deploy.test.ts`
- Testes:
  1. Happy path: submit + stream + URLs
  2. Build failure → exit 2
  3. Deploy failure → exit 3
  4. Auth error → exit 4
  5. Partial failure (2+ apps) → exit 5
  6. Single app failure → exit 2 (não 5, conforme DA-3)
  7. `--json` output shape (DA-5), deploy_id format (DA-11)
  8. `--no-wait` retorna 0 com mensagem (DA-2)
  9. No `.git` → exit 1
  10. No internal terms in output strings
  11. CI mode formatting
  12. `--env PRODUCTION` → exit 1 (DA-10)
  13. 0 apps no loopzero.yaml → exit 1
  14. loopzero.yaml busca ascendente (subdirectory → repo root)
  15. Ctrl+C durante upload → cleanup, exit limpo
  16. Ctrl+C durante stream → close connection, build continua server-side

#### Riscos conhecidos
- SSE client em TypeScript — verificar lib (eventsource ou nativa)
- `git bundle create --depth=1` pode não estar disponível em todas as versões de Git — validar

---

## Fase 2 — Serialização e Concorrência

> Objetivo: provar que concorrência é segura. Dois deploys simultâneos → exatamente um 202, um 409.

### Task S2.13: Concurrent Build Rejection — Integration Test

**Componente:** api
**Agent:** test-engineer
**Spec:** §4.4 — Conflict Matrix, §20 — Test Contracts
**Depende de:** S2.7, S2.8, S2.15 (IdempotencyStore PG — testes devem usar store real, não fake)
**Estimativa:** M

#### O que implementar
- Teste de integração com PostgreSQL real:
  1. Dois goroutines chamam `POST /builds` simultaneamente para mesmo (tenant, project, env)
  2. Exatamente um recebe 202, outro recebe 409
  3. Verificar que o 409 tem `error_code: DEPLOY_IN_PROGRESS`
  4. Verificar que apenas um Build record existe com status QUEUED/RUNNING
- Teste com `Idempotency-Key`:
  1. Mesma key + mesmo payload → mesma response (202)
  2. Mesma key + payload diferente → 422

#### Critérios de aceite
- [ ] Concorrência: exatamente um 202 + um 409 (determinístico, não flaky)
- [ ] Idempotency replay retorna response cached
- [ ] Idempotency mismatch retorna 422
- [ ] Lock liberado após primeiro build finalizar → segundo build pode submeter

#### Teste primeiro (TDD)
- Arquivo: `api/tests/integration/concurrent_builds_test.go`
- Requer: PostgreSQL real (testcontainers ou banco local)

#### Riscos conhecidos
- Testes de concorrência podem ser flaky se não sincronizados corretamente — usar barriers/channels

---

### Task S2.14: Lock Release on Crash

**Componente:** api
**Agent:** test-engineer
**Spec:** §4.2 — advisory lock crash recovery, §12 — Failure Modes
**Depende de:** S2.2, S2.7
**Estimativa:** P

#### O que implementar
- Teste de integração:
  1. Goroutine A adquire lock e simula crash (fecha conexão abruptamente)
  2. Goroutine B tenta adquirir lock imediatamente após
  3. B deve adquirir sem esperar 5s (lock auto-released por PG)
- Teste de rollback automático:
  1. Função dentro do lock retorna error
  2. Transação faz rollback
  3. Lock liberado
  4. Próxima operação adquire lock

#### Critérios de aceite
- [ ] Crash do holder → lock liberado imediatamente
- [ ] Error na função → rollback + lock liberado
- [ ] Próxima operação não precisa esperar timeout

#### Teste primeiro (TDD)
- Arquivo: `api/tests/integration/lock_crash_recovery_test.go`

#### Riscos conhecidos
- Simular crash de conexão requer fechar socket manualmente — pode ser frágil

---

### Task S2.15: Idempotency Store PostgreSQL

**Componente:** api
**Agent:** api-engineer
**Spec:** §4.3 — Idempotency
**Depende de:** nenhuma
**Estimativa:** P

#### O que implementar
- `api/internal/db/idempotency_store.go`:
  - Implementa `middleware.IdempotencyStore` interface (já definida)
  - `Get(ctx, key)` → query na tabela `idempotency_keys`
  - `Store(ctx, rec)` → insert com `expires_at = NOW() + 24h`
  - `Cleanup(ctx)` → delete where `expires_at < NOW()`
- Substituir qualquer fake/in-memory store usado nos testes de Sprint 1

#### Critérios de aceite
- [ ] Get retorna nil quando key não existe
- [ ] Store persiste com TTL de 24h
- [ ] Cleanup remove expirados
- [ ] Implementa a interface existente `middleware.IdempotencyStore`

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/idempotency_store_pg_test.go`
- Testes: store + get, get inexistente, cleanup remove expirados

#### Riscos conhecidos
- Nenhum — tabela já existe

---

## Fase 3 — SSE Streaming e Observabilidade do Build

> Objetivo: CLI exibe progresso real do build em tempo real. Reconnect funciona.

### Task S2.16: CLI SSE Client com Reconnect

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §7.1 — SSE reconnection protocol
**Depende de:** S2.9, S2.12
**Estimativa:** M

#### O que implementar
- `cli/src/api/sse-client.ts`:
  - Conecta a `GET /builds/:id/stream`
  - Parseia eventos SSE (id, event, data)
  - Reconnect automático após 1s com `Last-Event-ID`
  - Fallback para polling `GET /builds/:id` a cada 5s se `JOURNAL_EXPIRED`
  - Dedup: ignora eventos com ID ≤ último visto
  - Emite eventos tipados para o comando deploy consumir
- Integrar com `deploy.ts`:
  - Atualiza progress display em cada evento
  - Mostra step-by-step: `✓ Building api... [45s]`
  - Mostra erro detalhado em falha

#### Critérios de aceite
- [ ] Eventos renderizados em tempo real
- [ ] Reconnect transparente (usuário não percebe)
- [ ] Fallback para polling funciona
- [ ] Dedup impede eventos duplicados
- [ ] Cleanup: close connection no exit/ctrl+c

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/api/sse-client.test.ts`
- Testes: parse de eventos, reconnect com Last-Event-ID, fallback polling, dedup

#### Riscos conhecidos
- `EventSource` nativo do Node.js pode não existir — pode precisar lib `eventsource`

---

### Task S2.17: Deploy Progress Rendering

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §0.1 Workflow 1 — output format, §0.3 — CI mode, DA-5
**Depende de:** S2.16
**Estimativa:** P

#### O que implementar
- `cli/src/output/deploy-renderer.ts`:
  - Modo interativo (TTY):
    - Spinner por app: `⠋ Building api...`
    - Completo: `✓ Building api... [45s]`
    - Falha: `✗ Building api... [error message]`
    - URLs finais: `🚀 Live:\n  api → https://...`
  - Modo CI (`CI=true`):
    - Linhas simples: `[build] api: building...`
    - Sem spinners, sem cores, sem emojis
    - Uma linha por evento
  - Modo JSON (`--json`):
    - Output final único match spec §0.3 (DA-5)
    - Capturado do evento `deploy.completed` do SSE stream

#### Critérios de aceite
- [ ] Modo TTY: spinners animados, cores, emojis
- [ ] Modo CI: plain text, line-by-line
- [ ] Modo JSON: schema §0.3 exata (DA-5)
- [ ] Nenhum termo interno (Rule 3)
- [ ] Worker apps mostram "(no URL — background worker)"

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/output/deploy-renderer.test.ts`
- Testes: render TTY, render CI, render JSON (schema match), no internal terms, worker sem URL

#### Riscos conhecidos
- Nenhum — output formatting é isolado e testável

---

## Fase 4 — CLI Polish e CI Mode

> Objetivo: todos os flags e edge cases do deploy.

### Task S2.18: `loopzero deploy --env` e `--app`

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25 — CLI Reference deploy flags, DA-1, DA-3, DA-10
**Depende de:** S2.12
**Estimativa:** P

#### O que implementar
- Flag `--env <name>`:
  - Default: `production` (DA-1 — stability contract)
  - Valida: non-empty string
  - Envia como campo no POST /builds
- Flag `--app <name>` **(DA-19):**
  - Pode ser usado múltiplas vezes: `--app api --app worker`
  - Filtra apps do loopzero.yaml
  - Valida: **cada** app especificada existe no loopzero.yaml. Se qualquer uma não existe → exit 1 antes de qualquer upload (fail fast)
  - Envia como array no POST /builds
  - Se omitido: todas as apps
- Combinação: `loopzero deploy --env staging --app api --app worker`

#### Critérios de aceite
- [ ] `--env staging` envia environment correto
- [ ] `--env UPPERCASE` → exit 1 com mensagem (DA-10)
- [ ] `--app api` filtra apenas uma app
- [ ] `--app api --app worker` filtra múltiplas apps (DA-19)
- [ ] `--app inexistente` → exit code 1 com mensagem (fail fast)
- [ ] `--app api --app inexistente` → exit code 1 (fail fast, não deploy parcial — DA-19)
- [ ] Sem flags: env=production, todas as apps
- [ ] `--json` inclui environment e apps filtradas

#### Teste primeiro (TDD)
- Arquivo: extensão de `cli/tests/unit/commands/deploy.test.ts`
- Testes: env flag, app flag single, app flag multi, app inexistente fail fast, app mix existente+inexistente → fail fast, env uppercase → exit 1, combinação, defaults

#### Riscos conhecidos
- Nenhum

---

### Task S2.19a: API — GET /secrets (names only)

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2 — GET /secrets, §10.1 step 0a
**Depende de:** nenhuma (pode rodar em Paralelo 2 junto com S2.3/S2.4)
**Estimativa:** P

#### O que implementar
- `api/internal/routes/secrets.go`:
  - `GET /api/v1/secrets?env={env}&project={project}`
  - Retorna lista de nomes de secrets configurados (nunca valores)
  - Response: `{ secrets: [{ name, environment, app }] }`
  - Auth obrigatória

#### Critérios de aceite
- [ ] Retorna nomes, NUNCA valores
- [ ] Auth ausente → 401
- [ ] Tenant isolation: não vê secrets de outro tenant

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/secrets_route_test.go`
- Testes: happy path, auth, tenant isolation, empty list

#### Riscos conhecidos
- Nenhum

---

### Task S2.19b: CLI — Secrets Validation Pre-Deploy

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §10.1 step 0a — Pre-deploy validation, DA-8
**Depende de:** S2.12, S2.19a
**Estimativa:** P

#### O que implementar
- Em `cli/src/commands/deploy.ts`, antes do upload/build:
  1. Lê `loopzero.yaml`, extrai lista de `secrets` de cada app
  2. Chama `GET /secrets` (S2.19a)
  3. Compara declarados vs existentes
  4. Se faltam: abort com lista e mensagem (DA-8 — sem sugerir comando inexistente):
     ```
     Missing secrets for env "production": DATABASE_URL, JWT_SECRET
     Set secrets before deploying. See: https://docs.loopzero.example/secrets
     ```
  5. Se todos presentes: continua

#### Critérios de aceite
- [ ] CLI aborta com lista de secrets faltando antes de qualquer upload
- [ ] Mensagem NÃO sugere `loopzero secret set` (comando não existe na Sprint 2 — DA-8)
- [ ] Mensagem direciona para documentação
- [ ] Deploy sem secrets declarados → prossegue normalmente

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/deploy.test.ts` (extensão)
- Testes: secrets ok → continua, secrets faltando → abort com lista, sem secrets declarados → ok

#### Riscos conhecidos
- Nenhum

---

## Fase 5 — Testes de Convergência e Hardening

> Objetivo: prova end-to-end de que `loopzero deploy` gera um app vivo. O exit gate.

### Task S2.20: ArgoCD Health Gate (Circuit Breaker)

**Componente:** api
**Agent:** api-engineer
**Spec:** §10.1 step 14 — ArgoCD health gate, DA-7
**Depende de:** S2.7
**Estimativa:** M

#### O que implementar
- `api/internal/services/argocd_client.go`:
  - Interface `SyncClient`:
    - `TriggerSync(ctx, appName) error`
    - `WaitForSync(ctx, appName, timeout) (SyncResult, error)`
    - `HealthCheck(ctx) error`
  - Circuit breaker:
    - 3 falhas consecutivas → circuit OPEN
    - OPEN: rejeita com `503 PLATFORM_DEGRADED`
    - Retry health check a cada 30s
    - 2 sucessos consecutivos → circuit CLOSED
    - Release fica em READY quando circuit OPEN
  - Metric: `loopzero_argocd_circuit_state` gauge (0=closed, 1=open)
- **Known limitation (DA-7):** Circuit breaker é in-process (cada réplica tem seu estado). Para v1 (single replica) é aceitável. Multi-replica: comportamento inconsistente entre réplicas possível. Upgrade para PG-backed state é tech debt explícito.

#### Critérios de aceite
- [ ] Sync success → release ACTIVE
- [ ] Sync timeout (2min) → release SYNC_FAILED
- [ ] 3 falhas → circuit OPEN
- [ ] Circuit OPEN → 503 PLATFORM_DEGRADED
- [ ] Recovery: 2 successes → circuit CLOSED
- [ ] Metric exposta
- [ ] Teste valida que circuit OPEN retorna 503 (independent de outras réplicas)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/argocd_client_test.go`
- Testes: sync success, timeout, circuit open, circuit recovery, metric value

#### Riscos conhecidos
- In-process circuit breaker inconsistente entre réplicas (DA-7) — aceitável para v1

---

### Task S2.21a: E2E Seed Helper

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §20 — Test Contracts
**Depende de:** S2.0 (Tiltfile — API e TenantController precisam estar rodando)
**Estimativa:** P

#### O que implementar
- `infra/scripts/seed_e2e.go` + Taskfile target `task dev:seed:e2e`:
  - Cria via API: tenant `e2e-tenant`, user `e2e-user` com token fixo `e2e-token-dev`, projeto `e2e-project`
  - Idempotente via upsert: se já existem, reutiliza
  - Espera TenantController reconciliar (namespaces, Gitea repo ready) com timeout 60s
  - Retorna 0 quando pronto, 1 se timeout
- Usado pelo `BeforeEach` do E2E (S2.21) e como step manual de setup

#### Critérios de aceite
- [ ] `task dev:seed:e2e` cria tenant + user + project
- [ ] Idempotente: rodar 2× não duplica nem falha
- [ ] Espera reconciliação: namespaces existem após seed
- [ ] Timeout 60s se reconciliação não completar

#### Teste primeiro (TDD)
- Arquivo: validação manual — rodar seed, verificar namespaces via `kubectl`
- Integração: S2.21 `BeforeEach` chama seed e falha se timeout

#### Riscos conhecidos
- Requer API e TenantController rodando — não pode rodar em CI sem cluster

---

### Task S2.21: E2E — Deploy Boilerplate → App Running at URL

**Componente:** cross
**Agent:** orchestrator
**Spec:** §10.1 — Deploy complete flow, Sprint 2 exit gate
**Depende de:** S2.0, S2.0b, S2.21a (seed), e todas as tasks de Fase 1-4 (nota: S2.14 depende de S2.7, não de S2.8 — pode estar completo antes de S2.8)
**Estimativa:** G

#### O que implementar
- **Pré-check:** verificar saúde da infra antes de rodar E2E
  ```bash
  curl -s localhost:<port>/api/v1/health/deep | jq .  # tudo healthy
  ```
- **Seed helper:** `task dev:seed:e2e` ou `BeforeEach` helper que cria tenant + projeto + user via API (não manualmente). Idempotente — se já existe, reutiliza. Sem seed automatizado, cada run de E2E requer setup manual → testes quebram em CI.
- `infra/tests/e2e/deploy_happy_path_test.go` (Ginkgo):
  1. **Test 1 — Single app:** `loopzero.yaml` com 1 app server (boilerplate Node.js)
     - `loopzero deploy` via CLI
     - Verifica: build SUCCEEDED, release ACTIVE
     - Verifica: app acessível via URL (HTTP 200) — health probe já garante isso, E2E confirma
     - Verifica: `loopzero deploy --json` output match schema §0.3 (DA-5), `deploy_id` format (DA-11)
  2. **Test 2 — Deploy com cache:**
     - Segundo `loopzero deploy` (mesmo código)
     - Verifica: build usa cache (mais rápido ou log indica cache hit)
  3. **Test 3 — Multi-app monorepo:** `loopzero.yaml` com 2+ apps (server + worker)
     - Verifica: ambos deployados
     - Verifica: worker sem URL, server com URL
     - Verifica: `--json` output contém ambos os apps
- **Teardown (`AfterEach`):** delete do Tenant CR → TenantController cascade cleanup (namespaces, Gitea repo, ArgoCD Application). Verificar que nenhum Build ativo permanece (partial unique index bloquearia próximo run). Timeout de teardown: 60s.

#### Critérios de aceite
- [ ] **Seed helper funciona:** `task dev:seed:e2e` cria tenant + user + project via API, idempotente
- [ ] Infra health check passa antes do E2E
- [ ] App rodando com URL real após deploy (Test 1)
- [ ] HTTP 200 na URL retornada (Test 1)
- [ ] Build utilizou cache no segundo deploy (Test 2)
- [ ] Multi-app: server com URL, worker sem URL (Test 3)
- [ ] JSON output match spec §0.3 (DA-5), `deploy_id` format `d-{4hex}` (DA-11), `build_log` format `b-{4hex}` (DA-11)
- [ ] Exit code 0
- [ ] Teardown limpa todos os recursos sem leak

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/e2e/deploy_happy_path_test.go`
- Requer: clusters k3d rodando, Argo Workflows, ArgoCD, Gitea, MinIO, TenantController (S2.0)

#### Riscos conhecidos
- E2E requer toda a infra levantada — pode ser lento (5-10 min por teste)
- Flakiness por dependência de rede/cluster — retry strategy no Ginkgo

---

### Task S2.22: E2E — Concurrent Deploys Rejection

**Componente:** cross
**Agent:** test-engineer
**Spec:** §4.4 — Conflict Matrix, §20 — Test contracts
**Depende de:** S2.21
**Estimativa:** M

#### O que implementar
- `infra/tests/e2e/concurrent_deploy_test.go` (Ginkgo):
  1. Dois `loopzero deploy` simultâneos (goroutines ou parallel CLI invocations)
  2. Exatamente um recebe exit 0, outro recebe exit com error
  3. **Verificar mensagem do segundo (DA-18):** `"A deploy is already in progress for this environment."` — sem termos internos
  4. **Verificar exit code do segundo:** 3 (deploy failure, não 1)
  5. Verificar que apenas um build existe com status não-terminal

#### Critérios de aceite
- [ ] Exatamente um deploy sucede
- [ ] Segundo deploy: exit code 3 com mensagem DA-18 (sem termos internos)
- [ ] Sem race conditions ou data corruption
- [ ] Determinístico (não flaky)

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/e2e/concurrent_deploy_test.go`

#### Riscos conhecidos
- Timing-dependent — usar synchronization barriers

---

## Grafo de Dependências

```
═══ FASE 0: Infra prerequisites ═══

S2.0 (Tiltfile + TenantController deploy)
S2.0b (ClusterCredentialsOperator) ◄── S2.0

═══ FASE 1: Vertical Slice ═══

S2.1 (migration)
 ├──> S2.3 (BuildStore, ReleaseStore)
 └──> S2.4 (EnvironmentTargetStore) ──┐
                                       │
S2.2 (withEnvironmentLock) ───────────┤
S2.3 (stores) ────────────────────────┤
S2.5 (SSE store) ────────────────────┤
S2.6 (source upload) ────────────────┤
                                      ▼
                               S2.7 (BuildOrchestrator) ──> S2.8 (POST /builds route)
                                                                 │
                                       ┌─────────────────────────┤
                                       ▼                         ▼
                                S2.9 (SSE stream)         S2.12 (CLI deploy)
                                       │                    │    │    │
                                       ▼                    │    │    │
                                S2.16 (CLI SSE client) ◄───┘    │    │
                                       │                         │    │
                                       ▼                         │    │
                                S2.17 (deploy renderer)         │    │
                                                                 │    │
                                S2.18 (--env, --app) ◄──────────┘    │
                                S2.19b (CLI secrets validation) ◄────┘

S2.10 (Argo WorkflowTemplate) ─────── independente (infra, parametrizado — contrato DA-15 já definido)
S2.11 (ArgoCD config) ◄── S2.0b
S2.15 (idempotency PG store) ──────── independente
S2.19a (GET /secrets API) ─────────── independente (Paralelo 2)

═══ FASE 2: Serialização ═══

S2.13 (concurrent test) ◄── S2.7, S2.8, S2.15  ← usa IdempotencyStore real
S2.14 (crash recovery test) ◄── S2.2, S2.7  ← NÃO depende de S2.8
S2.20 (ArgoCD circuit breaker) ◄── S2.7

═══ FASE 4 ═══

S2.19b (CLI secrets validation) ◄── S2.12, S2.19a

═══ CONVERGÊNCIA ═══

S2.21a (E2E seed helper) ◄── S2.0  (pode rodar no Paralelo 1)
S2.21 (E2E happy path) ◄── S2.21a, S2.0b, todas as tasks de Fase 1-4
S2.22 (E2E concurrent) ◄── S2.21
```

### Ordem de execução recomendada

```
Paralelo 0:  S2.0                                  (infra — bloqueante para E2E)
Paralelo 1:  S2.0b + S2.1 + S2.2 + S2.5 + S2.6 + S2.10 + S2.15 + S2.19a + S2.21a
                                                    (S2.0b/S2.21a após S2.0; demais independentes)
Paralelo 2:  S2.3 + S2.4 + S2.11                   (S2.3/S2.4 após S2.1; S2.11 após S2.0b)
Sequencial:  S2.7                                   (após S2.2, S2.3, S2.4, S2.5, S2.6)
Paralelo 3:  S2.8 + S2.14                           (S2.8 após S2.7; S2.14 após S2.2+S2.7)
Paralelo 4:  S2.9 + S2.12 + S2.13 + S2.20          (após S2.8; S2.13 também requer S2.15 do Paralelo 1)
Paralelo 5:  S2.16 + S2.18 + S2.19b                (S2.16/S2.18 após S2.12; S2.19b após S2.12+S2.19a)
Sequencial:  S2.17                                  (após S2.16)
Sequencial:  S2.21                                  (após tudo)
Sequencial:  S2.22                                  (após S2.21)
```

**Notas de scheduling:**
- S2.14 depende de S2.2 e S2.7, não de S2.8 — pode rodar em paralelo com S2.8.
- S2.19a (API route) é independente — roda no Paralelo 1. S2.19b (CLI integration) depende de S2.12 + S2.19a — roda no Paralelo 5.
- `task dev:up` levanta clusters. `tilt up` é separado (foreground). S2.21 requer ambos.

---

## Exit Gate

```bash
# 0. Verificação de saúde da infra
tilt status                       # todos os recursos up
curl -s localhost:<port>/api/v1/health/deep | jq .  # tudo healthy

# 1. Todos os testes passam
task test:unit                    # unitários CLI + API + operators
task test                         # todos os testes

# 2. Manifests válidos
task validate:manifests           # kubeconform --strict

# 3. Lint passa
task lint

# 4. E2E prove convergência
task test:e2e                     # deploy_happy_path + concurrent_deploy

# 5. Prova manual (the "5-minute promise")
loopzero login
loopzero init                         # em um projeto Node.js de exemplo
loopzero deploy                       # app rodando com URL real
curl -s <URL_RETORNADA>           # HTTP 200

# 6. Verificações de contrato
loopzero deploy --json | jq .         # schema match §0.3 (DA-5)
CI=true loopzero deploy               # sem spinners, sem cores

# 7. Zero termos internos
grep -rn "ArgoCD\|argocd\|GitOps\|gitops-push\|CRD\|tenant_id\|advisory lock\|pg_advisory\|Gitea\|MinIO\|kubebuilder\|envtest\|Kaniko" cli/src/ --include="*.ts" | grep -v "test" | grep -v ".d.ts"
# Resultado esperado: 0 matches
```

---

## Known Limitations

Limitações explícitas aceitas para Sprint 2, documentadas para não serem redescobiertas.

| # | Limitação | Impacto | Resolução planejada |
|---|-----------|---------|---------------------|
| KL-1 | Circuit breaker in-process (DA-7) | Réplicas podem ter estados diferentes | Multi-replica: PG-backed state (Sprint 8) |
| KL-2 | `loopzero secret set` não existe (DA-8) | Mensagem de secrets faltando não sugere comando | Sprint 3 atualiza mensagem |
| KL-3 | `git bundle --depth=1` pode não ser suportado em Git < 2.29 | Upload falha em versões antigas | Documentar requisito mínimo de Git |
| KL-4 | Cache de build depende de MinIO | Primeiro build sem cache é mais lento | Cache é warm após primeiro deploy |
| KL-5 | E2E lento (~5-10min) | Feedback loop lento | Otimizar em Sprint 8 (hardening) |
| KL-6 | Idempotency-Key TTL 24h (DA-13) | Key expirada é request novo, não replay | CLI gera UUID novo a cada invocação — risco só para scripts manuais |
| KL-7 | Runtime logs sem replay (Sprint 3) | Gap possível no reconnect de `--follow` | By-design: runtime logs são efêmeros (Sprint 3 DA-3) |
| KL-8 | `--force` não existe em `loopzero deploy` (DA-9) | Não pode cancelar build ativo via deploy | Se necessário, será adicionado com semântica explícita em sprint futura |
| KL-9 | `git rev-parse --show-toplevel` em worktrees/submodules (EC-S) | Busca de `loopzero.yaml` pode não achar a raiz correta | Documentar: worktrees/submodules podem requerer `loopzero.yaml` no worktree root |

---

## Resumo

| Fase | Tasks | Foco |
|------|-------|------|
| 0 — Pré-requisitos | S2.0, S2.0b | Tiltfile, TenantController, ClusterCredentials |
| 1 — Vertical Slice | S2.1–S2.12 | CLI → API → Build → Release → URL |
| 2 — Serialização | S2.13–S2.15 | Concorrência segura, idempotency PG |
| 3 — SSE Streaming | S2.16–S2.17 | Progresso real-time, reconnect |
| 4 — CLI Polish | S2.18–S2.19b | Flags, secrets validation, CI mode |
| 5 — Convergência | S2.20–S2.22 (+ S2.21a seed) | Circuit breaker, E2E seed, E2E, exit gate |

**Total: 26 tasks** · **9 paralelizáveis no início** · **3 tasks G, 10 M, 13 P**

### Review History

Todos os itens de todas as reviews foram aplicados diretamente no corpo das tasks. Esta tabela é referência histórica — não há itens pendentes.

<details>
<summary>Review 1 — 18 itens (todos aplicados)</summary>

| Item | Resolução | Onde aplicado |
|---|---|---|
| Gap 1: TenantController | Fase 0 + S2.0 | S2.0, S2.21 |
| Gap 2: JSON schema | DA-5 com schema explícito | S2.8, S2.12, S2.17 |
| Gap 3: Build timeout | 300s default em S2.7 | S2.7 |
| Gap 4: Health check URL | DA-6 + probe em S2.7 | S2.7 |
| Gap 5: --no-wait exit code | DA-2 | S2.12 |
| Gap 6: ClusterCredentialsOp | S2.0b | S2.0b, S2.11 |
| Gap 7: git bundle tamanho | --depth=1 + 500MB | S2.6, S2.12 |
| EC-A thru EC-F | Validações e testes | S2.2, S2.6, S2.9, S2.12 |
| Inc-1 thru Inc-5 | DAs + grafo corrigido | DA-1, S2.18, Grafo |

</details>

<details>
<summary>Review 2 — 15 itens (todos aplicados)</summary>

| Item | Resolução | Onde aplicado |
|---|---|---|
| Gap A: Tiltfile startup | resource_deps | S2.0 |
| Gap B: bundle contract | DA-12 | S2.6 |
| Gap C: E2E teardown | AfterEach explícito | S2.21 |
| Gap D: env case | DA-10 regex | S2.8, S2.12, S2.18 |
| Gap E: probe URL | ingress URL, não MinIO | S2.7 |
| Gap F: missing Secret | condition + requeue | S2.0b |
| EC-G thru EC-J | Validações e DAs | S2.6, S2.9, S2.12 |
| Inc-A thru Inc-D | DA-5/DA-11, split S2.19 | S2.8, S2.19a/b, Grafo |
| DA-14: probe SSE | release.health_check event | S2.7, S2.9 |

</details>

<details>
<summary>Review 4 — 16 itens (todos aplicados)</summary>

| Item | Resolução | Onde aplicado |
|---|---|---|
| Pendente 1: callback Argo→API | DA-15 + endpoint em S2.8 + finalize step em S2.10 | S2.7, S2.8, S2.10 |
| Pendente 2: Release PENDING | DA-16 + transições explícitas | S2.7, S2.9 |
| Pendente 3: frontend probe URL | Corrigido: ingress URL, não MinIO | S2.7 |
| Pendente 4: S2.15→S2.13 dep | Adicionado no grafo e ordem | S2.13, Grafo |
| Gap: callback endpoint | POST /builds/:id/callback em S2.8 | S2.8 |
| EC-N: race condition QUEUED | DA-15 guard + teste #12 em S2.7 | S2.7 |
| Inc-I: SUPERSEDED sem SSE | `release.superseded` na lista S2.9 | S2.9 |
| Changelog v3 não aplicado (13 items) | Seed em S2.21, Ctrl+C em S2.12, loopzero.yaml busca ascendente em S2.12, finalize callback em S2.10, multi-app E2E em S2.21, S2.18 ref DA-10, release.health_check em S2.9, release.pending em S2.9, ACTIVATION_TIMEOUT em S2.7, grafo S2.19→S2.19b | Todos aplicados no corpo |

</details>

<details>
<summary>Review 5 — 11 itens (todos aplicados)</summary>

| Item | Resolução | Onde aplicado |
|---|---|---|
| Gap 1: seed sem task | Critério explícito em S2.21 | S2.21 |
| Gap 2: deploy_id unicidade | DA-11 reescrito com escopo e collision handling | DA-11 |
| Gap 3: S2.10 contrato callback | Nota no grafo: parametrizado, DA-15 já definido | Grafo |
| Gap 4: fronteira do lock OnBuildCompleted | DA-17: dois locks separados, Lock 2 cobre passos 3-5h | DA-17, S2.7 |
| EC-O: callback apps vazio | DA-15 validação + teste #15 em S2.7, critério em S2.8 | DA-15, S2.7, S2.8 |
| EC-P: 409 CLI message | DA-18 + exit code 3, critério em S2.22 | DA-18, S2.12, S2.22 |
| EC-Q: --app multi-value | DA-19 + critérios e testes em S2.18 | DA-19, S2.18 |
| Inc-J: step 4 duplicado em S2.7 | Renumerado: passos 3-6 | S2.7 |
| Inc-K: S2.18 sem DA-10 | Critério `--env UPPERCASE` adicionado | S2.18 |
| Inc-L: release.health_check sem critério | 3 critérios + testes adicionados em S2.9 | S2.9 |
| Inc-M: build_log formato | DA-11 reescrito com tabela de aliases, DA-5 referencia DA-11 | DA-5, DA-11 |

</details>

<details>
<summary>Review 6 — 6 itens (todos aplicados)</summary>

| Item | Resolução | Onde aplicado |
|---|---|---|
| Gap A: seed sem task | S2.21a criado (P, infra-engineer) | S2.21a, S2.21, Grafo, Ordem |
| Gap B: LOOPZERO_ARGO_CALLBACK_SECRET | Critério em S2.0: gerado pelo Tiltfile | S2.0 |
| EC-R: callback bloqueia Lock 2 | DA-15: callback retorna 202 async, OnBuildCompleted em goroutine | DA-15, S2.8 |
| EC-S: git worktree/submodule | KL-9 documentado | KL-9 |
| Inc-N: sub-letras b→h sem a | Renumerado a→g em S2.7 passo 5 | S2.7 |
| Inc-O: PENDING→CANCELLED scope | DA-16: somente via ACTIVATION_TIMEOUT na Sprint 2 | DA-16 |

</details>
