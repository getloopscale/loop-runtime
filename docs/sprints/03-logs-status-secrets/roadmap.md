# Roadmap Executável — Sprint 3: `loopzero logs` + `loopzero status` + `loopzero secret`

> **Spec:** §23 Sprint 3 · **Semanas:** 10-12
> **Princípio:** Convergência primeiro. Cada fase fecha uma capacidade inteira e testável.
> Cada comando precisa de fonte de verdade definida, comportamento de degradação explícito e contrato de falha.

---

## Índice

- [Decisões de Arquitetura (respostas às perguntas abertas)](#decisões-de-arquitetura)
- [Fase 1 — `loopzero status` + `loopzero inspect` com fontes de verdade](#fase-1--loopzero-status--loopzero-inspect)
- [Fase 2 — `loopzero logs` com semântica de ordering e reconnect](#fase-2--loopzero-logs)
- [Fase 3 — `loopzero secret set/list/delete/pull` com modelo de consistência](#fase-3--loopzero-secret)
- [Fase 4 — `secret-hash-changed` + rollout/reconcile](#fase-4--secret-hash-changed--rollout)
- [Fase 5 — Hardening de Gitea: retry, fetch-merge-push, backup, alerts](#fase-5--gitea-hardening)
- [Grafo de Dependências](#grafo-de-dependências)
- [Exit Gate](#exit-gate)

---

## Decisões de Arquitetura

Respostas explícitas para as perguntas que a sprint precisa responder antes de qualquer linha de código.

### DA-1: Qual dado alimenta cada campo de `loopzero status`?

Spec §25.4, §3, §0.3 definem o contrato. A fonte de verdade por campo:

| Campo | Fonte autoritativa | Fallback se fonte indisponível |
|-------|-------------------|-------------------------------|
| `project` | `loopzero.yaml` (local) + API `projects` table | — (sempre disponível localmente) |
| `team` | Auth context (JWT → tenant_id) | — |
| `environment` | Flag `--env` ou default `production` | — |
| `apps[].name` | `loopzero.yaml` (app names) | — |
| `apps[].status` | **ArgoCD Application sync status** + **Pod readiness** via API | `"unknown"` + mensagem `"Platform health check failed"` |
| `apps[].deploy_id` | **PostgreSQL** `environment_targets.active_release_id` | `"unknown"` se PG down (503) |
| `apps[].url` | Computado: `{app}-{env}.{project}.{tenant}.loopzero.example` (spec §26) | Sempre computável |
| `apps[].deployed_at` | **PostgreSQL** `releases.created_at` (join via `active_release_id`) | `null` se PG down |
| `apps[].deployed_by` | **PostgreSQL** `releases.created_by` | `null` se PG down |

**Regra de degradação:**
- PostgreSQL down → `503 Service Unavailable`. CLI exit code 3 com mensagem `"Could not retrieve status. Try again in a moment."`
- ArgoCD unreachable → campos de status retornam `"unknown"`, demais campos (deploy_id, url, deployed_at) vêm do PostgreSQL normalmente
- Kubernetes API unreachable → pod readiness unknown, sync status from ArgoCD cache

### DA-2: O que significa "secret set succeeded"?

**Definição de sucesso para `loopzero secret set`:**

1. API persiste o valor no mgmt cluster (AppSecret CR criado/atualizado)
2. API retorna 200/201 ao CLI
3. CLI mostra `✓ Secret set: KEY (production)`

**O que "succeeded" NÃO significa:**
- NÃO significa reconciliado no workload cluster
- NÃO significa que pods foram reiniciados
- NÃO significa que o hash foi atualizado no Git

**Semântica explícita:** "Secret salvo" = persistido na API. Convergência para o workload é assíncrona via AppSecretOperator → `POST /internal/secret-hash-changed` → gitops-push → ArgoCD sync → rolling update. O CLI pode opcionalmente informar: `Secret saved. Active deployments will update within ~2 minutes.`

**Visibilidade do progresso de convergência:**
- `loopzero status` mostra se o secret hash do workload diverge do esperado (quando implementado via `status.hashMatch` no AppSecret CRD)
- Alert `LoopZeroSecretSyncFailed` dispara se reconciliação falha 3+ vezes em 15min

### DA-3: `loopzero logs` lê de onde? Semântica de ordering/reconnect?

**Build logs** (`loopzero logs --build <id>`):
- Fonte: SSE event journal (PostgreSQL `sse_events` table, já existe de Sprint 2)
- Protocolo: SSE com `Last-Event-ID` replay (já implementado Sprint 2)
- Ordering: por sequence number do journal (garantido, não best-effort)
- Reconnect: `Last-Event-ID` → replay. Journal expirado → `JOURNAL_EXPIRED` → polling fallback

**Runtime logs** (`loopzero logs --app api`):
- Fonte: **Kubernetes API direta** (`/api/v1/namespaces/{ns}/pods/{pod}/log`)
- Proxy via API: `GET /api/v1/apps/:app/logs` → API faz fan-in dos pods
- Protocolo: SSE do API para CLI
- **Multi-pod:** API descobre pods via label selector (`app={name}, env={env}, tenant={tenant}`). Faz fan-in de todos os pods, prefixando cada linha com pod name quando há >1 pod
- **Ordering:** best-effort por timestamp do Kubernetes. Não é garantido cross-pod (limitação fundamental de logs distribuídos). Dentro de um pod, ordering é garantido
- **Rotação de pod:** API monitora pod list via watch. Pod novo → conecta stream. Pod terminando → stream fecha gracefully, mensagem `[pod-xyz] terminated`
- **`--since 2h`:** traduzido para `sinceSeconds` na Kubernetes API. Lê backlog real dos logs retidos pelo kubelet (configuração default: 100MB ou rotação)
- **`--follow`:** API mantém SSE aberta, re-watches pods. CLI reconecta via SSE reconnect protocol. Não há `Last-Event-ID` para runtime logs (stateless — logs são efêmeros)
- **Build logs vs runtime logs:** são dois protocolos distintos:
  - Build: journal-based, replay via ID, garantia de ordering
  - Runtime: stream-based, sem replay, best-effort ordering

### DA-4: Comportamento quando Gitea/PostgreSQL/K8s API estão degradados?

| Comando | Gitea down | PostgreSQL down | K8s API down |
|---------|-----------|----------------|--------------|
| `loopzero status` | Status de sync = `"unknown"` | **503** — não consegue ler releases/targets | Pod readiness = `"unknown"`, demais campos ok |
| `loopzero inspect` | Git commit info = `null` | **503** — dado primário vive em PG | N/A |
| `loopzero logs --build` | N/A (logs vêm de PG) | **503** — journal em PG | N/A |
| `loopzero logs --app` | N/A | N/A (runtime logs não usam PG) | **503** — não consegue acessar pods |
| `loopzero logs --follow` | N/A | N/A | Reconnect automático. Se K8s API down > 30s: `"Connection lost. Retrying..."` |
| `loopzero secret set` | N/A (persiste em PG + AppSecret CR) | **503** | **503** se não consegue criar AppSecret CR |
| `loopzero secret list` | N/A | **503** — lista vem de PG | N/A |
| `loopzero secret delete` | N/A | **503** | **503** se não consegue deletar AppSecret CR |
| `loopzero secret pull` | N/A | **503** — valores vêm de PG/mgmt cluster | N/A |
| `secret-hash-changed` | gitops-push falha → retry (DA-5) | **503** — lock vive em PG | N/A |

**Princípio:** Nenhum comando retorna sucesso quando a operação não pode ser completada. Comandos de leitura degradam gracefully (campos `null`/`"unknown"`). Comandos de mutação falham com 503 e mensagem clara.

### DA-5: Limites por tenant no streaming de logs?

**Runtime logs:**
- **Max conexões SSE por tenant:** 10 conexões simultâneas (configurable via env var `LOOPZERO_MAX_LOG_STREAMS_PER_TENANT`)
- **Excedeu:** 429 `LOG_STREAM_LIMIT_REACHED` com mensagem `"Maximum concurrent log streams reached (10). Close an existing stream first."`
- **Backpressure:** Se o consumer (CLI) é lento, API descarta linhas mais antigas (buffer circular de 1000 linhas por stream). Insere marcador `[... N lines dropped ...]`
- **Truncamento por linha:** Linhas > 16KB são truncadas com `[truncated]`
- **Rate per-pod:** Kubernetes kubelet já limita log throughput. API não faz rate limiting adicional por pod
- **Fan-out:** API mantém uma conexão ao kubelet por pod. Com N pods × M tenants, o número máximo de conexões kubelet = N × min(M, 10)

**Build logs (SSE journal):**
- Sem rate limit adicional — journal é finito (1h retention, read-only após build terminal)
- Pruning do journal a cada 15min (já definido Sprint 2)

### DA-6: `secret pull` — merge, replace ou fail-on-drift?

**Comportamento: replace (overwrite).** Spec §25.8 confirma: "Downloaded to `.env.local`" sem menção de merge.

- `loopzero secret pull` sobrescreve `.env.local` completamente
- Formato: `KEY=value` (uma por linha, sem aspas, sem export prefix)
- Permissão do arquivo: `0600` (rw owner only — contém secrets)
- Comentários do `.env.local` existente: **não preservados** (overwrite completo)
- Variáveis locais extras: **perdidas** (overwrite completo)
- Mitigação: CLI mostra warning se `.env.local` existe com conteúdo extra: `"Warning: .env.local will be overwritten. Existing local-only variables will be lost."`
- `--app api`: puxa secrets apenas da app especificada
- Sem `--app`: puxa secrets de todas as apps do projeto, prefixados por app se houver conflito de nomes (e.g., `API_DATABASE_URL`, `WORKER_DATABASE_URL`)
- Conflito de nomes entre apps: prefixo `{APP}_` automaticamente, com warning ao usuário

### DA-7: O hash de secret participa do rollout de qual recurso?

**Recurso afetado:** Deployment (ou equivalente para workers).

**Mecanismo (spec §16.2 Option C):**
1. AppSecretOperator detecta hash change no Secret
2. Chama `POST /internal/secret-hash-changed { tenantId, projectId, env, app, newHash }`
3. API adquire environment lock
4. API atualiza annotation `loopzero.io/secret-hash: {newHash}` no Deployment manifest em Git
5. ArgoCD detecta diff → rolling update do Deployment
6. Kubernetes faz rolling update porque annotation mudou → novos pods lêem novo Secret

**Nota:** Frontends (`type: frontend`) não são afetados por secret rotation — secrets de frontend são compilados em build time. Secret change para frontend requer rebuild (`loopzero deploy`).

### DA-8: Qual evento dispara restart/redeploy após mudança de secret?

Não é um "restart" direto. É uma **rolling update via annotation change**.

Fluxo completo:
```
Secret value changes
  → AppSecretOperator detects hash(source) != hash(target)
  → AppSecretOperator syncs new Secret to workload namespace
  → AppSecretOperator calls POST /internal/secret-hash-changed
  → API updates loopzero.io/secret-hash annotation in Git manifest
  → ArgoCD detects annotation diff
  → ArgoCD applies updated Deployment
  → Kubernetes triggers rolling update (annotation change = spec change)
  → New pods start with new Secret value (envFrom: secretRef)
  → Old pods terminate gracefully
```

**Tempo de convergência esperado:** ~2 minutos (operator reconcile ~30s + API gitops-push ~10s + ArgoCD sync ~30s + rolling update ~30s)

---

## Fase 1 — `loopzero status` + `loopzero inspect`

> Objetivo: comando de leitura confiável que diz exatamente o que está rodando, de onde vem cada dado, e degrada gracefully quando um backend está indisponível. Se `loopzero status` mente, todo o resto é inútil.

### Task S3.1: API — Status Service com fontes de verdade explícitas

**Componente:** api
**Agent:** api-engineer
**Spec:** §25.4, §0.3 (JSON schema status), §3 (source of truth)
**Depende de:** nenhuma (Sprint 2 já tem releases, environment_targets, auth)
**Estimativa:** M

#### O que implementar
- `api/internal/services/status_service.go`:
  - `GetEnvironmentStatus(ctx, tenantID, projectID, env) (*EnvironmentStatus, error)`
    1. Query `environment_targets` para `active_release_id` e `desired_release_id` (PostgreSQL)
    2. Query `releases` para metadata do release ativo (created_at, created_by, build_id) (PostgreSQL)
    3. Query ArgoCD API para sync status de cada app (ArgoCD)
    4. Query Kubernetes API para pod readiness de cada app (K8s API)
    5. Computa URLs a partir de convenção de naming (determinístico)
    6. Merge sources: se ArgoCD unreachable → `status: "unknown"`. Se K8s unreachable → pod info null
  - `GetAppStatus(ctx, tenantID, projectID, env, app) (*AppStatus, error)` — mesma lógica, filtrada
- `api/internal/services/argocd_reader.go`:
  - Interface `SyncStatusReader`:
    - `GetAppSyncStatus(ctx, appName) (*SyncStatus, error)` — sync state, health
  - Implementação real: query ArgoCD REST API `/api/v1/applications/{name}`
  - Implementação fake: retorna status configurável
- `api/internal/services/pod_reader.go`:
  - Interface `PodStatusReader`:
    - `GetPodStatus(ctx, namespace, labelSelector) ([]PodStatus, error)` — ready, restarts, age
  - Implementação real: Kubernetes client-go
  - Implementação fake: retorna pods configuráveis

#### Critérios de aceite
- [ ] Cada campo tem fonte de verdade documentada no código (comentário)
- [ ] ArgoCD down → `status: "unknown"`, demais campos preenchidos
- [ ] K8s API down → pod info null, demais campos preenchidos
- [ ] PostgreSQL down → erro 503 (não há fallback — dados primários vivem lá)
- [ ] `desired_release_id != active_release_id` → status inclui `"deploying"` indicator
- [ ] URL para workers retorna `null` (sem ingress)
- [ ] Testes com fakes validam cada cenário de degradação

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/status_service_test.go`
- Testes:
  1. Happy path: todas as fontes disponíveis → response completa
  2. ArgoCD down: sync status = unknown, demais campos ok
  3. K8s API down: pod info null, demais ok
  4. Release ativo sem release desejado diferente → `"running"`
  5. Release desejado != ativo → `"deploying"`
  6. Sem release ativo (primeiro deploy pendente) → `"pending"`
  7. Release com `GIT_MIRROR_FAILED` → status indica degradação
  8. Release com `SYNC_FAILED` → status indica falha de sync

#### Riscos conhecidos
- ArgoCD API pode ser lento → timeout de 5s no client, não bloquear outros campos
- Múltiplos apps = múltiplas queries paralelas ao ArgoCD + K8s → usar `errgroup` com concurrency limit

---

### Task S3.2: API — Routes para status e inspect

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2 — GET /apps/:app/status, §0.3 JSON schema
**Depende de:** S3.1
**Estimativa:** P

#### O que implementar
- `api/internal/routes/status.go`:
  - `GET /api/v1/status` — status de todas as apps no environment
    - Query params: `env` (default: production)
    - Response: JSON schema §0.3 (status)
  - `GET /api/v1/status/:app` — status de uma app específica
    - Response: single app object
  - `GET /api/v1/inspect/:deploy_id` — detalhes de um deploy específico
    - Response: build metadata, release metadata, apps, artifacts, timing, git commit
- Auth middleware (já existe)
- Correlation-ID middleware (já existe)

#### Critérios de aceite
- [ ] JSON response match schema §0.3 exatamente
- [ ] Auth ausente → 401
- [ ] Tenant não autorizado → 403
- [ ] Deploy ID inexistente → 404
- [ ] Nenhum termo interno na response (Rule 3)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/status_route_test.go`
- Testes: happy path, auth, 404, JSON shape, no internal terms

#### Riscos conhecidos
- Nenhum — padrão dos routes existentes

---

### Task S3.3: CLI — `loopzero status`

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.4, §0.3 CI mode
**Depende de:** S3.2
**Estimativa:** M

#### O que implementar
- `cli/src/commands/status.ts`:
  - `loopzero status` — status de todas as apps (env default: production)
  - `loopzero status --env staging`
  - `loopzero status --app api`
  - `--json` — JSON output match schema §0.3
  - Output formatado (TTY):
    ```
    my-saas (production) — team: acme

    APP       STATUS    DEPLOY     URL                                              DEPLOYED
    api       running   d-8f3a     https://api-production.my-saas.acme.loopzero.example  3 min ago by lucas@acme.dev
    web       running   d-8f3a     https://production.my-saas.acme.loopzero.example      3 min ago by lucas@acme.dev
    worker    running   d-8f3a     (background worker)                              3 min ago by lucas@acme.dev
    ```
  - CI mode: plain text, sem cores
  - Status `"unknown"` quando backend indisponível, com footnote: `"⚠ Some status data unavailable. Platform health check in progress."`
  - Exit code 0 (success) mesmo com campos unknown (leitura parcial é sucesso)
  - Exit code 3 quando API retorna 503 (platform failure)

#### Critérios de aceite
- [ ] Output formatado em tabela alinhada
- [ ] Workers mostram "(background worker)" em vez de URL
- [ ] Status unknown com footnote explicativo
- [ ] `--json` match schema §0.3
- [ ] CI mode: sem cores, sem formatação rica
- [ ] Exit code 0 para sucesso parcial, 3 para 503
- [ ] Nenhum termo interno (Rule 3)

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/status.test.ts`
- Testes:
  1. Happy path com múltiplas apps
  2. Worker sem URL
  3. Status unknown com footnote
  4. `--json` shape
  5. CI mode
  6. 503 → exit code 3
  7. No internal terms

#### Riscos conhecidos
- Nenhum

---

### Task S3.4: CLI — `loopzero inspect`

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.4 — inspect
**Depende de:** S3.2
**Estimativa:** P

#### O que implementar
- `cli/src/commands/inspect.ts`:
  - `loopzero inspect <deploy-id>` — detalhes de um deploy
  - Output: build timing, app statuses, artifacts, git commit, who deployed, when
  - `--json` flag
  - Exit code 1 se deploy-id inválido, 4 se não autenticado

#### Critérios de aceite
- [ ] Mostra todos os campos do deploy
- [ ] Deploy inexistente → exit 1 com mensagem
- [ ] `--json` match expected schema
- [ ] Nenhum termo interno

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/inspect.test.ts`
- Testes: happy path, deploy inexistente, `--json`, no internal terms

#### Riscos conhecidos
- Nenhum

---

### Task S3.5: Integration test — `loopzero status` durante estados de falha

**Componente:** cross
**Agent:** test-engineer
**Spec:** §3 source of truth, §5.2 release states, §12 failure modes
**Depende de:** S3.1, S3.2, S3.3
**Estimativa:** M

#### O que implementar
- `api/tests/integration/status_degradation_test.go`:
  1. Status durante `GIT_MIRROR_FAILED`: mostra deploy ativo anterior + warning
  2. Status durante `SYNC_FAILED`: mostra status de sync failure
  3. Status durante `PENDING` (deploy in progress): mostra `"deploying"`
  4. Status com ArgoCD fake down: campos sync = unknown, demais ok
  5. Status com K8s API fake down: pod readiness = unknown, demais ok

#### Critérios de aceite
- [ ] Cada estado do release state machine produz status consistente
- [ ] Degradação parcial nunca produz resposta incoerente
- [ ] Campos unknown são explicitamente marcados (não null silencioso)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/integration/status_degradation_test.go`

#### Riscos conhecidos
- Requer fakes configuráveis para ArgoCD e K8s API — validar que interfaces da S3.1 são suficientes

---

## Fase 2 — `loopzero logs`

> Objetivo: streaming de logs confiável com semântica de ordering clara. Build logs reutilizam infraestrutura de Sprint 2. Runtime logs são novo subsistema com limites por tenant.

### Task S3.6: API — Runtime Log Streaming via Kubernetes API

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2 — GET /apps/:app/logs, §25.3, DA-3
**Depende de:** nenhuma
**Estimativa:** G

#### O que implementar
- `api/internal/services/log_streamer.go`:
  - Interface `LogStreamer`:
    - `StreamRuntimeLogs(ctx, StreamRequest) (<-chan LogLine, error)`
  - `StreamRequest`:
    - `TenantID, ProjectID, Environment, App string`
    - `Follow bool`
    - `Since time.Duration` (traduzido para `sinceSeconds` na K8s API)
  - Implementação real (`KubernetesLogStreamer`):
    1. Descobrir pods via label selector: `app={name}, env={env}, tenant={tenant}`
    2. Para cada pod: abrir stream `GET /api/v1/namespaces/{ns}/pods/{pod}/log?follow={follow}&sinceSeconds={since}&timestamps=true`
    3. **Fan-in:** goroutine per pod, merge into single channel
    4. **Multi-pod prefixing:** se >1 pod, prefixar cada linha com `[pod-name]`
    5. **Pod rotation:** watch pod list. Novo pod → spawn stream goroutine. Pod terminating → fechar stream, emitir `[pod-xyz] terminated`
    6. **Backpressure:** buffer circular de 1000 linhas por stream. Se consumer lento, descarta linhas antigas com marcador `[... N lines dropped ...]`
    7. **Line truncation:** linhas > 16KB truncadas com `[truncated]`
  - Implementação fake: emite linhas configuráveis

- **Rate limiting por tenant** (`api/internal/middleware/log_rate_limit.go`):
  - Max 10 conexões SSE de logs simultâneas por tenant
  - Tracked via in-memory counter (atomic) por tenant_id
  - Excedeu → 429 `LOG_STREAM_LIMIT_REACHED`
  - Counter decrementado no connection close (defer)

#### Critérios de aceite
- [ ] Single pod: linhas sem prefixo
- [ ] Multi-pod: linhas prefixadas com `[pod-name]`
- [ ] Pod novo aparece: stream inclui novas linhas
- [ ] Pod terminando: mensagem de terminação
- [ ] `--since 2h`: backlog real do kubelet
- [ ] Buffer overflow: marcador de linhas descartadas
- [ ] Line truncation a 16KB
- [ ] Rate limit: 11ª conexão do mesmo tenant → 429
- [ ] Follow=false: stream encerra após linhas existentes
- [ ] Follow=true: stream mantém aberta até ctx cancel
- [ ] Consumer disconnect: goroutines cleanup (sem leak)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/log_streamer_test.go`
- Testes:
  1. Single pod streaming
  2. Multi-pod fan-in com prefixo
  3. Pod rotation (new pod joins, old pod terminates)
  4. Backpressure: slow consumer → lines dropped marker
  5. Line truncation
  6. Since duration
  7. Follow=false encerra
  8. Context cancellation → cleanup

- Arquivo: `api/tests/unit/log_rate_limit_test.go`
- Testes:
  1. 10 conexões ok
  2. 11ª → 429
  3. Conexão fechada → counter decrementa
  4. Tenants diferentes não interferem

#### Riscos conhecidos
- Goroutine lifecycle é o maior risco — cada pod gera uma goroutine. Precisa de cleanup rigoroso via context propagation
- Kubelet log retention é configurável — `--since 2h` pode retornar vazio se logs foram rotacionados
- Watch de pods pode ficar stale — use watch com ResourceVersion para evitar

---

### Task S3.7: API — Route `GET /api/v1/apps/:app/logs`

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2, DA-3
**Depende de:** S3.6
**Estimativa:** M

#### O que implementar
- `api/internal/routes/logs.go`:
  - `GET /api/v1/apps/:app/logs`:
    - Query params: `env`, `follow` (bool), `since` (duration string: "2h", "30m")
    - SSE format: `data: {"timestamp":"...","pod":"...","message":"..."}\n\n`
    - Headers: `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`
    - Heartbeat: `:heartbeat\n\n` a cada 15s
    - Auth + rate limit middleware
    - Stream encerra quando: build terminal state, follow=false esgotou, client disconnect
  - `GET /api/v1/builds/:id/logs`:
    - Alias para `GET /api/v1/builds/:id/stream` (já existe Sprint 2)
    - Reutiliza SSE event journal com `Last-Event-ID` replay

#### Critérios de aceite
- [ ] SSE format correto (data + newlines)
- [ ] Heartbeat a cada 15s
- [ ] Auth obrigatória → 401
- [ ] Rate limit aplicado → 429
- [ ] Tenant isolation: não pode ver logs de outro tenant → 403
- [ ] K8s API down → 503 com mensagem clara
- [ ] Client disconnect → cleanup sem goroutine leak

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/logs_route_test.go`
- Testes: happy path SSE, heartbeat, auth, rate limit 429, tenant isolation 403, K8s down 503

#### Riscos conhecidos
- SSE com Go e `http.Flusher` — já validado em Sprint 2 (build stream)

---

### Task S3.8: CLI — `loopzero logs`

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.3, §0.1 Workflow 2, DA-3
**Depende de:** S3.7
**Estimativa:** M

#### O que implementar
- `cli/src/commands/logs.ts`:
  - `loopzero logs` — logs de todas as apps (env default: production)
  - `loopzero logs --app api` — logs de uma app
  - `loopzero logs --env staging`
  - `loopzero logs --follow` — streaming contínuo
  - `loopzero logs --since 2h` — backlog
  - `loopzero logs --build <build-id>` — build logs (reutiliza SSE client de Sprint 2)
  - `--json` — cada linha como JSON object
  - Output formatado:
    ```
    2026-03-12T14:32:01Z [api] Server listening on port 3000
    2026-03-12T14:32:05Z [api] GET /health 200 2ms
    ```
  - CI mode: mesmo formato (logs já são plain text)
  - Ctrl+C: fecha conexão gracefully
  - Reconnect para `--follow`: reconecta SSE após 1s de disconnect. Sem `Last-Event-ID` para runtime logs (stateless — linhas podem ser perdidas entre reconnects, aceito por design)
  - Warning ao reconnectar: `[reconnecting...]`
  - Exit codes: 0 (success), 3 (platform failure/503), 4 (auth error)

#### Critérios de aceite
- [ ] Output match spec §0.1 Workflow 2 exatamente
- [ ] `--build` reutiliza SSE client existente
- [ ] `--follow` mantém stream aberta
- [ ] `--since` envia duration correta
- [ ] Reconnect com warning
- [ ] `--json` cada linha como JSON
- [ ] Ctrl+C exit limpo
- [ ] Nenhum termo interno (Rule 3)
- [ ] Exit codes corretos

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/logs.test.ts`
- Testes:
  1. Happy path com output formatado
  2. `--build` usa SSE client
  3. `--follow` flag
  4. `--since` parsing e envio
  5. `--json` output
  6. Reconnect com warning
  7. Auth error → exit 4
  8. 503 → exit 3
  9. No internal terms

#### Riscos conhecidos
- Runtime logs sem `Last-Event-ID` = gap possível no reconnect. Aceitável para v1 (spec não define replay para runtime logs)

---

### Task S3.9: Integration test — logs com multi-pod e rotação

**Componente:** cross
**Agent:** test-engineer
**Spec:** DA-3, §20 test contracts
**Depende de:** S3.6, S3.7
**Estimativa:** M

#### O que implementar
- `api/tests/integration/log_streaming_test.go`:
  1. **Multi-pod interleaving:** 3 pods emitindo logs simultaneamente → fan-in correto com prefixos
  2. **Pod rotation durante --follow:** pod antigo termina, pod novo aparece → stream continua sem interrupção, mensagem de terminação emitida
  3. **Rate limit por tenant:** 10 streams ok, 11ª → 429
  4. **Backpressure:** consumer lento → marcador de linhas descartadas
  5. **Consumer disconnect:** todas as goroutines cleanup (verificar com runtime.NumGoroutine antes/depois)

#### Critérios de aceite
- [ ] Multi-pod: ordering best-effort mas todas as linhas presentes
- [ ] Pod rotation: transição suave, sem crash
- [ ] Rate limit funciona corretamente
- [ ] Zero goroutine leaks após disconnect

#### Teste primeiro (TDD)
- Arquivo: `api/tests/integration/log_streaming_test.go`
- Requer: fake Kubernetes client com pods simulados (client-go fake ou envtest)

#### Riscos conhecidos
- Pod rotation test pode ser timing-dependent — usar channels para sincronização

---

## Fase 3 — `loopzero secret`

> Objetivo: CRUD de secrets com modelo de consistência explícito. "Secret salvo" = persistido na API. Convergência para workload é assíncrona e observável.

### Task S3.10: API — Secret Store e CRUD

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2 secrets endpoints, §16.2, §25.8
**Depende de:** nenhuma
**Estimativa:** M

#### O que implementar
- Migration `000004_secrets.up.sql`:
  - Tabela `secrets`:
    - `id TEXT PRIMARY KEY` (secret-{8hex})
    - `tenant_id TEXT NOT NULL REFERENCES tenants(id)`
    - `project_id TEXT NOT NULL REFERENCES projects(id)`
    - `environment TEXT NOT NULL`
    - `app TEXT NOT NULL`
    - `name TEXT NOT NULL` (e.g., DATABASE_URL)
    - `encrypted_value BYTEA NOT NULL` (encrypted at rest)
    - `created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
    - `updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()`
    - `updated_by TEXT NOT NULL`
    - `UNIQUE (tenant_id, project_id, environment, app, name)`
- `api/internal/db/secret_store.go`:
  - `Set(ctx, secret) error` — upsert
  - `Get(ctx, tenantID, projectID, env, app, name) (*Secret, error)` — para pull
  - `List(ctx, tenantID, projectID, env) ([]SecretMeta, error)` — nomes e metadata, NUNCA valores
  - `Delete(ctx, tenantID, projectID, env, app, name) error`
  - `ListByApp(ctx, tenantID, projectID, env, app) ([]Secret, error)` — para pull com valores
- Interface `SecretEncryptor` (DIP):
  - `Encrypt(plaintext []byte) ([]byte, error)`
  - `Decrypt(ciphertext []byte) ([]byte, error)`
  - Implementação real: AES-256-GCM com key de env var
  - Implementação fake: passthrough para testes

#### Critérios de aceite
- [ ] Set cria ou atualiza (upsert)
- [ ] List NUNCA retorna valores — apenas nomes, env, app, updated_at
- [ ] Get retorna valor descriptografado
- [ ] Delete remove secret
- [ ] Unique constraint impede duplicatas
- [ ] Valores encriptados at rest
- [ ] Testes com banco real

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/secret_store_test.go`
- Testes: set, get, list (sem valores), delete, upsert update, unique constraint, encryption round-trip

#### Riscos conhecidos
- Encryption key management — para v1, key de env var é aceitável. Produção futura: vault/KMS

---

### Task S3.11: API — Secret Routes

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2, §25.8
**Depende de:** S3.10
**Estimativa:** M

#### O que implementar
- `api/internal/routes/secrets.go`:
  - `POST /api/v1/secrets`:
    - Body: `{ name, value, environment, app }`
    - Persiste via SecretStore
    - Cria/atualiza AppSecret CR no mgmt cluster (interface `SecretReconciler`)
    - Retorna 201 (created) ou 200 (updated)
    - Mensagem de resposta: `{ name, environment, app, status: "saved" }`
  - `GET /api/v1/secrets`:
    - Query params: `env`, `project`
    - Retorna lista de nomes (NUNCA valores): `{ secrets: [{ name, environment, app, updated_at }] }`
  - `GET /api/v1/secrets/values`:
    - Query params: `env`, `project`, `app`
    - Retorna nomes + valores descriptografados (para `secret pull`)
    - Requer auth + tenant-admin role
    - Rate limited
  - `DELETE /api/v1/secrets/:name`:
    - Query params: `env`, `app`
    - Remove secret + deleta AppSecret CR
    - Retorna 200 com `{ name, status: "deleted" }`
    - **Validação:** se secret está referenciado por release ativa → 409 com warning: `"Secret KEY is referenced by active deployment d-8f3a. Delete will take effect on next deploy. Use --force to proceed."`
- Interface `SecretReconciler` (DIP):
  - `CreateOrUpdateAppSecret(ctx, tenantID, projectID, env, app, secretData) error`
  - `DeleteAppSecret(ctx, tenantID, projectID, env, app) error`
  - Implementação real: Kubernetes client-go → cria AppSecret CRD
  - Implementação fake: in-memory

#### Critérios de aceite
- [ ] POST set → 201/200
- [ ] GET list → nomes apenas, NUNCA valores
- [ ] GET values → requer tenant-admin
- [ ] DELETE → 200, validação de referência ativa
- [ ] DELETE de secret referenciado por release ativa → 409 com warning
- [ ] Auth em todas as rotas
- [ ] Nenhum valor de secret em logs (jamais logar valores)

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/secrets_route_test.go`
- Testes:
  1. Set happy path
  2. Set update (upsert)
  3. List sem valores
  4. Values com admin role
  5. Values sem admin → 403
  6. Delete happy path
  7. Delete de secret referenciado → 409
  8. Auth missing → 401
  9. Verify: nenhum valor em response de list
  10. Verify: nenhum valor em logs

#### Riscos conhecidos
- AppSecret CR creation requer acesso ao mgmt cluster — testes usam fake

---

### Task S3.12: CLI — `loopzero secret` (set, list, delete, pull)

**Componente:** cli
**Agent:** cli-engineer
**Spec:** §25.8, §0.1 Workflow 4, DA-6
**Depende de:** S3.11
**Estimativa:** M

#### O que implementar
- `cli/src/commands/secret.ts`:
  - `loopzero secret set KEY=value` — default env: production, all apps
  - `loopzero secret set KEY=value --env staging --app api`
  - `loopzero secret list` — tabela: NAME, ENVIRONMENT, APP, LAST UPDATED
  - `loopzero secret list --env staging`
  - `loopzero secret delete KEY` — com confirmação (a menos que `--force`)
  - `loopzero secret delete KEY --env staging`
  - `loopzero secret pull` — download para `.env.local`
  - `loopzero secret pull --env staging --app api`
  - `--json` em todos os subcomandos
  - **Pull semantics (DA-6):**
    - Overwrite completo de `.env.local`
    - Formato: `KEY=value` (uma por linha, sem aspas, sem `export`)
    - Permissão: `0600`
    - Warning se `.env.local` existe com conteúdo: `"Warning: .env.local will be overwritten. Existing local-only variables will be lost."`
    - Sem `--app`: puxa de todas as apps. Conflito de nomes → prefixo `{APP}_`
    - Mensagem de sucesso: `✓ Pulled 4 secrets to .env.local (staging)`
  - **Set semantics:**
    - Sucesso: `✓ Secret set: DATABASE_URL (production)`
    - Info: `Secret saved. Active deployments will update within ~2 minutes.`
  - Exit codes: 0 (success), 1 (usage error), 4 (auth)

#### Critérios de aceite
- [ ] Set → mensagem de sucesso + info sobre convergência
- [ ] List → tabela formatada, NUNCA valores
- [ ] Delete → confirmação interativa (skip com `--force`)
- [ ] Pull → `.env.local` com permissão 0600
- [ ] Pull overwrite warning
- [ ] Pull multi-app com prefixo para conflitos
- [ ] `--json` em todos os subcomandos
- [ ] CI mode: sem confirmação interativa (requer `--force`)
- [ ] Nenhum termo interno
- [ ] Exit codes corretos

#### Teste primeiro (TDD)
- Arquivo: `cli/tests/unit/commands/secret.test.ts`
- Testes:
  1. Set happy path + mensagem
  2. List tabela sem valores
  3. Delete com confirmação
  4. Delete `--force` sem confirmação
  5. Pull overwrite warning
  6. Pull permissão 0600
  7. Pull formato KEY=value
  8. Pull multi-app prefixo conflito
  9. `--json` shape
  10. CI mode delete sem confirmação → exit 1
  11. No internal terms

#### Riscos conhecidos
- `secret pull` com multi-app e conflito de nomes pode confundir — warning explícito mitigar

---

### Task S3.13: Integration test — secret lifecycle completo

**Componente:** cross
**Agent:** test-engineer
**Spec:** §16.2, DA-2
**Depende de:** S3.10, S3.11
**Estimativa:** M

#### O que implementar
- `api/tests/integration/secret_lifecycle_test.go`:
  1. Set → List (aparece) → Get values → Delete → List (desapareceu)
  2. Set two concurrent secrets for same app/env → ambos persistem (nomes diferentes)
  3. Set mesma key twice → upsert, valor atualizado
  4. Delete secret referenciado por release ativa → 409
  5. Pull retorna valores descriptografados corretos
  6. **Isolamento:** tenant A não vê secrets de tenant B

#### Critérios de aceite
- [ ] Lifecycle completo funciona
- [ ] Upsert funciona
- [ ] Isolamento por tenant
- [ ] Delete com validação de referência

#### Teste primeiro (TDD)
- Arquivo: `api/tests/integration/secret_lifecycle_test.go`

#### Riscos conhecidos
- Nenhum

---

## Fase 4 — `secret-hash-changed` + Rollout

> Objetivo: mudança de secret propaga até rolling update de workload. O mecanismo: AppSecretOperator → API → gitops-push → ArgoCD → rolling update.

### Task S3.14: AppSecretOperator — Reconcile Loop

**Componente:** operators
**Agent:** operator-engineer
**Spec:** §7.3.2 — AppSecretOperator
**Depende de:** nenhuma
**Estimativa:** G

#### O que implementar
- `operators/api/v1/appsecret_types.go` (já existe — verificar se campos de status estão completos):
  - `status.syncedAt`
  - `status.targetNamespace`
  - `status.hash` — hash(source secret)
  - `status.workloadHash` — hash(target secret no workload)
  - `status.hashMatch` — bool
  - Condition: `Synced`
- `operators/internal/controller/appsecret_controller.go`:
  - Reconcile loop (spec §7.3.2):
    1. Read source secret from mgmt cluster → compute `hash(source)`
    2. Read target Secret from workload namespace → compute `hash(target)`
    3. If `hash(source) == hash(target)`: set `hashMatch=true`, done
    4. If `hash(source) != hash(target)`: copy source → target namespace, update hashes
    5. If target doesn't exist: create it
    6. After sync: call `POST /internal/secret-hash-changed` com `{ tenantId, projectId, env, app, newHash }`
    7. On failure: increment `loopzero_secret_sync_hash_mismatch_total`, retry with backoff
    8. After 3 consecutive failures: condition `Synced=False`, alert fires via metric
  - Idempotent: reconcile pode rodar N vezes sem efeito colateral
  - Crash recovery: se crash entre write e status update → próximo reconcile detecta hash mismatch e re-syncs
- Metric: `loopzero_secret_sync_hash_mismatch_total{tenant, env}` counter

#### Critérios de aceite
- [ ] Source → target sync funciona
- [ ] Hash match detection correta
- [ ] Target deletado externamente → recriado
- [ ] `POST /internal/secret-hash-changed` chamado após sync
- [ ] Idempotent: N reconciles = mesmo resultado
- [ ] Crash entre write e status update → recovery no próximo reconcile
- [ ] Metric incrementada em falha
- [ ] 3 falhas consecutivas → condition Synced=False

#### Teste primeiro (TDD)
- Arquivo: `operators/internal/controller/appsecret_controller_test.go` (envtest)
- Testes:
  1. Happy path: source → target sync
  2. Hash match: no-op
  3. Hash mismatch: re-sync
  4. Target deleted: recreated
  5. API call failure: retry com backoff
  6. 3 failures: condition Synced=False
  7. Idempotency: multiple reconciles same result
  8. Crash recovery: interrupted reconcile recovers

#### Riscos conhecidos
- Operator precisa de acesso cross-cluster (mgmt → workload). Em envtest, usar fake clients
- `POST /internal/secret-hash-changed` chamada HTTP do operator → API. Em testes, usar httptest server

---

### Task S3.15: API — `POST /internal/secret-hash-changed`

**Componente:** api
**Agent:** api-engineer
**Spec:** §16.2 Option C, DA-7, DA-8
**Depende de:** S3.14 (contrato), S2.2 (withEnvironmentLock de Sprint 2)
**Estimativa:** M

#### O que implementar
- `api/internal/routes/internal_hooks.go`:
  - `POST /internal/secret-hash-changed`:
    - Body: `{ tenant_id, project_id, environment, app, new_hash }`
    - **Internal-only** — não exposto externamente (validação por source IP ou shared secret)
    - **Idempotency check first:** Read Git HEAD manifest. Se `loopzero.io/secret-hash` já == `new_hash` → return 200 (no-op). Sem lock, sem commit
    - Se hash diferente:
      1. Acquire environment lock via `withEnvironmentLock`
      2. Update `loopzero.io/secret-hash` annotation no Deployment manifest
      3. gitops-push (single file update commit)
      4. Notify ArgoCD sync
      5. Release environment lock
    - Retorna 200 (success), 503 (PG/Gitea down), 409 (lock conflict)

#### Critérios de aceite
- [ ] Hash já match → 200 no-op (sem lock, sem commit)
- [ ] Hash diferente → lock + gitops-push + ArgoCD sync
- [ ] Lock conflict → 409 (deploy em progresso)
- [ ] Internal-only validation
- [ ] Gitops-push failure → retry (5x backoff)
- [ ] Commit tem trailer: `Secret-Hash-Update: {app}`

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/secret_hash_changed_test.go`
- Testes:
  1. Hash match → 200 no-op (verify: no lock acquired, no commit)
  2. Hash diff → lock + commit
  3. Lock conflict → 409
  4. Gitops-push failure → retry
  5. Concurrent with deploy → one waits for lock

#### Riscos conhecidos
- gitops-push pode conflitar com deploy em progresso → lock serializa corretamente
- Gitea down → retry policy (handled in Fase 5)

---

### Task S3.16: Integration test — secret update → workload rolling update

**Componente:** cross
**Agent:** test-engineer
**Spec:** §16.2, DA-7, DA-8
**Depende de:** S3.14, S3.15
**Estimativa:** G

#### O que implementar
- `api/tests/integration/secret_rotation_test.go`:
  1. **Happy path:** set secret → operator syncs → hash changed → gitops-push → annotation updated in Git
  2. **Concurrent with deploy:** secret-hash-changed chega durante deploy ativo → waits for lock → processes after deploy completes
  3. **Idempotency:** same hash twice → second is no-op
  4. **Operator crash recovery:** simulate operator crash between sync and API call → next reconcile retries
- `operators/internal/controller/appsecret_integration_test.go`:
  5. **External deletion:** target Secret deleted → operator recreates
  6. **Delayed reconciliation:** operator slow (simulated) → secret exists in API but not yet in workload → `hashMatch=false`

#### Critérios de aceite
- [ ] End-to-end: secret value change → Git annotation updated
- [ ] Serialization: no conflict with concurrent deploy
- [ ] Idempotency: no duplicate commits
- [ ] Recovery: crash doesn't leave inconsistent state

#### Teste primeiro (TDD)
- Arquivos: `api/tests/integration/secret_rotation_test.go`, `operators/internal/controller/appsecret_integration_test.go`

#### Riscos conhecidos
- Cross-component test (operator + API) — requer ambos rodando. Pode usar httptest para API mock

---

## Fase 5 — Gitea Hardening

> Objetivo: Gitea como dependência crítica tem retry, backup e alertas. Fetch-merge-push resolve conflitos entre deploy e secret-hash-changed.

### Task S3.17: gitops-push retry policy

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.4 — gitops-push conflict resolution, §12 Gitea failure scenarios
**Depende de:** nenhuma (refina interface GitOpsPusher de Sprint 2)
**Estimativa:** M

#### O que implementar
- `api/internal/services/gitops_pusher.go` (refinar implementação de Sprint 2):
  - **Immediate retry:** 5 retries, exponential backoff (5s, 10s, 20s, 40s, 80s)
  - **Non-fast-forward handling (fetch-merge-push):**
    1. Fetch latest Git HEAD
    2. Read runtime-mutable fields from HEAD: `loopzero.io/secret-hash`
    3. Merge: generated manifests + HEAD values for runtime-mutable fields only
    4. Create new commit on top of HEAD (NOT force-push)
    5. Retry push (up to 3 merge attempts)
    6. If still failing → `gitMirrorStatus = FAILED`
  - **Runtime-mutable field registry:**
    - `api/internal/services/runtime_mutable_fields.go`
    - Explicit list: `[]string{"loopzero.io/secret-hash"}`
    - Intentionally small and enumerated
  - **Async mirror retry:**
    - Background job: exponential backoff (5min, 10min, 20min, 40min). Max 6 retries
    - If retry succeeds: `gitMirrorStatus → SYNCED`
    - If exhausted: stays `GIT_MIRROR_FAILED`, alert `LoopZeroMirrorSyncStale`
    - New deploy cancels old mirror retry

#### Critérios de aceite
- [ ] 5 immediate retries com backoff correto
- [ ] Non-fast-forward → fetch-merge-push (não force-push)
- [ ] Merge preserva `loopzero.io/secret-hash` do HEAD
- [ ] Merge usa generated manifests para tudo exceto runtime-mutable
- [ ] 3 merge attempts exhausted → FAILED
- [ ] Async retry up to 6 attempts
- [ ] New deploy cancels stale retry
- [ ] Runtime-mutable fields explicitamente listados

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/gitops_pusher_test.go`
- Testes:
  1. Push success on first try
  2. Push fails 3x then succeeds
  3. Non-fast-forward → fetch-merge-push → success
  4. Merge preserves secret-hash from HEAD
  5. 3 merge attempts exhausted → FAILED
  6. Async retry succeeds → SYNCED
  7. New deploy cancels old retry

#### Riscos conhecidos
- Merge logic é delicada — precisa de testes com Git real (não fake)

---

### Task S3.18: Gitea health check no deep health

**Componente:** api
**Agent:** api-engineer
**Spec:** §7.2 — GET /health/deep, §12 Gitea failures
**Depende de:** nenhuma
**Estimativa:** P

#### O que implementar
- Adicionar Gitea check no endpoint `GET /api/v1/health/deep` (já existe):
  - Chamar Gitea API: `GET /api/v1/version`
  - Timeout: 5s
  - Se falhar: health check retorna `{ gitea: { status: "unhealthy", error: "..." } }`
  - Se ok: `{ gitea: { status: "healthy", version: "..." } }`
- Metric: `loopzero_gitea_health_status` gauge (0=down, 1=up)

#### Critérios de aceite
- [ ] Deep health inclui Gitea status
- [ ] Timeout de 5s (não bloqueia indefinidamente)
- [ ] Metric exposta

#### Teste primeiro (TDD)
- Arquivo: `api/tests/unit/health_deep_test.go` (extensão)
- Testes: Gitea up, Gitea down, Gitea timeout

#### Riscos conhecidos
- Nenhum

---

### Task S3.19: Gitea backup — git bundle to MinIO

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §12 — Gitea data loss scenario, §15 alerts
**Depende de:** nenhuma
**Estimativa:** M

#### O que implementar
- CronJob Kubernetes: `gitea-backup`
  - Schedule: every 6 hours (`0 */6 * * *`)
  - Para cada repo no Gitea: `git bundle create` → upload para MinIO
  - Gitea dump diário (full backup): `gitea dump` → MinIO
  - Atualiza metric: `loopzero_gitea_last_backup_timestamp` gauge
- Alert: `LoopZeroGiteaBackupStale`:
  - Expression: `time() - loopzero_gitea_last_backup_timestamp > 25200` (7 hours)
  - Severity: critical
- Manifest: non-root, resource limits, no `latest` tag

#### Critérios de aceite
- [ ] CronJob executa a cada 6h
- [ ] Bundle de cada repo salvo no MinIO
- [ ] Metric atualizada após backup
- [ ] Alert dispara se backup > 7h
- [ ] `kubeconform --strict` passa
- [ ] Non-root, resource limits

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/unit/gitea_backup_test.go`
- Testes: CronJob template rendering, schedule correto, resource limits, security context

#### Riscos conhecidos
- Gitea com muitos repos = backup lento. Para v1, aceitável (poucos tenants)

---

### Task S3.20: Alerts — LoopZeroGiteaDown, LoopZeroMirrorSyncStale, LoopZeroSecretSyncFailed

**Componente:** infra
**Agent:** infra-engineer
**Spec:** §15 — Observability alerts
**Depende de:** S3.17 (métricas), S3.18 (métricas), S3.14 (métricas)
**Estimativa:** P

#### O que implementar
- PrometheusRule manifest com alerts:
  ```yaml
  LoopZeroGiteaDown:
    expr: up{job="loopzero-gitea"} == 0
    for: 2m
    severity: critical

  LoopZeroGiteaBackupStale:
    expr: time() - loopzero_gitea_last_backup_timestamp > 25200
    severity: critical

  LoopZeroSecretSyncFailed:
    expr: increase(loopzero_secret_sync_hash_mismatch_total[15m]) > 3
    severity: warning

  LoopZeroMirrorSyncStale:
    expr: loopzero_git_mirror_status{status="FAILED"} == 1
    for: 1h
    severity: warning
  ```
- `kubeconform --strict` validation

#### Critérios de aceite
- [ ] Todos os 4 alerts definidos
- [ ] Expressions match spec §15
- [ ] Severities corretas
- [ ] Manifests passam kubeconform

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/unit/alerts_test.go`
- Testes: manifest rendering, expression syntax válida

#### Riscos conhecidos
- Nenhum

---

### Task S3.21: E2E — Secret rotation end-to-end

**Componente:** cross
**Agent:** orchestrator
**Spec:** §16.2, DA-7, DA-8, Sprint 3 exit gate
**Depende de:** S3.10-S3.16
**Estimativa:** G

#### O que implementar
- `infra/tests/e2e/secret_rotation_e2e_test.go` (Ginkgo):
  1. Deploy app com secret declarado no loopzero.yaml
  2. `loopzero secret set DATABASE_URL=postgres://...` → sucesso
  3. Wait: AppSecretOperator reconcile (observe metric)
  4. Verify: Deployment annotation `loopzero.io/secret-hash` updated in Git
  5. Verify: ArgoCD sync → rolling update → new pods
  6. Verify: new pod reads new secret value
  7. `loopzero secret list` → mostra DATABASE_URL
  8. `loopzero secret pull` → `.env.local` com valor correto

#### Critérios de aceite
- [ ] End-to-end: secret set → workload running with new value
- [ ] Convergence time < 3 minutes
- [ ] `.env.local` match secret value
- [ ] Git has correct annotation

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/e2e/secret_rotation_e2e_test.go`
- Requer: full cluster setup

#### Riscos conhecidos
- E2E lento (~3-5 min). Aceitável para convergence proof

---

### Task S3.22: E2E — Daily workflows convergence

**Componente:** cross
**Agent:** orchestrator
**Spec:** §0.1 Workflows 1-4 (exceto rollback/preview), Sprint 3 exit gate
**Depende de:** TODAS as tasks anteriores
**Estimativa:** G

#### O que implementar
- `infra/tests/e2e/daily_workflows_test.go` (Ginkgo):
  1. `loopzero login` → `loopzero init` → `loopzero deploy` (Sprint 2 prerequisite)
  2. `loopzero status` → all apps running
  3. `loopzero inspect <deploy-id>` → deploy details correct
  4. `loopzero logs --app api --since 1m` → logs visible
  5. `loopzero logs --build <build-id>` → build logs with replay
  6. `loopzero secret set KEY=value` → success
  7. `loopzero secret list` → KEY appears
  8. `loopzero secret pull` → `.env.local` created
  9. `loopzero status --json` → schema match
  10. All commands with `--json` → valid JSON output

#### Critérios de aceite
- [ ] All 4 spec workflows functional (except rollback/preview)
- [ ] All commands produce valid JSON with `--json`
- [ ] No internal terms in any output
- [ ] Exit codes correct throughout

#### Teste primeiro (TDD)
- Arquivo: `infra/tests/e2e/daily_workflows_test.go`

#### Riscos conhecidos
- Teste longo (~10 min). Aceitável para exit gate proof

---

## Grafo de Dependências

```
═══ FASE 1: status + inspect ═══

S3.1 (status service) ──> S3.2 (status routes) ──> S3.3 (CLI status)
                                                 ──> S3.4 (CLI inspect)
                                                 ──> S3.5 (status degradation test)

═══ FASE 2: logs ═══

S3.6 (log streamer) ──> S3.7 (logs route) ──> S3.8 (CLI logs)
                                            ──> S3.9 (multi-pod test)

═══ FASE 3: secrets CRUD ═══

S3.10 (secret store) ──> S3.11 (secret routes) ──> S3.12 (CLI secret)
                                                 ──> S3.13 (secret lifecycle test)

═══ FASE 4: secret-hash-changed + rollout ═══

S3.14 (AppSecretOperator) ──┐
S3.15 (secret-hash-changed) ←┘──> S3.16 (rotation integration test)

═══ FASE 5: Gitea hardening ═══

S3.17 (gitops-push retry)  ── independente
S3.18 (Gitea health check) ── independente
S3.19 (Gitea backup)       ── independente
S3.20 (alerts)              ←── S3.17, S3.18, S3.14 (métricas)

═══ CONVERGENCE ═══

S3.21 (E2E secret rotation) ←── Fase 3 + Fase 4
S3.22 (E2E daily workflows) ←── TUDO
```

### Ordem de execução recomendada

```
Paralelo 1:   S3.1 + S3.6 + S3.10 + S3.14 + S3.17 + S3.18 + S3.19
              (5 fases começam simultaneamente — sem dependências cruzadas)

Paralelo 2:   S3.2  (após S3.1)
              S3.7  (após S3.6)
              S3.11 (após S3.10)
              S3.15 (após S3.14, usa lock de Sprint 2)

Paralelo 3:   S3.3 + S3.4 + S3.5  (após S3.2)
              S3.8 + S3.9          (após S3.7)
              S3.12 + S3.13        (após S3.11)
              S3.16                (após S3.14 + S3.15)
              S3.20                (após S3.17 + S3.18 + S3.14)

Sequencial:   S3.21 (após Fase 3 + Fase 4)
Sequencial:   S3.22 (após TUDO)
```

---

## Exit Gate

```bash
# 1. Testes passam
task test:unit                    # unitários CLI + API + operators
task test                         # todos os testes

# 2. Manifests válidos
task validate:manifests           # kubeconform --strict

# 3. Lint passa
task lint

# 4. E2E prova convergência
task test:e2e                     # daily_workflows + secret_rotation

# 5. Prova manual — Workflows 1-4
loopzero login
loopzero init
loopzero deploy                       # Workflow 1 (Sprint 2)
loopzero status                       # Workflow 2 (status)
loopzero logs --app api --since 1m    # Workflow 3 (logs)
loopzero logs --follow                # logs streaming
loopzero secret set DB_URL=test       # Workflow 4 (secrets)
loopzero secret list
loopzero secret pull
cat .env.local                    # valor correto

# 6. Degradação
# Simular: kubectl scale deployment gitea --replicas=0
loopzero status                       # campos sync = "unknown", demais ok, exit 0
loopzero logs --app api               # funciona (não depende de Gitea)
loopzero secret set KEY=val           # funciona (persiste em PG + CR)
# Restaurar: kubectl scale deployment gitea --replicas=1

# 7. JSON contracts
loopzero status --json | jq .
loopzero logs --app api --json | head -5
loopzero secret list --json | jq .

# 8. Zero termos internos
grep -rn "ArgoCD\|argocd\|GitOps\|gitops-push\|CRD\|tenant_id\|advisory lock\|pg_advisory\|Gitea\|MinIO\|kubebuilder\|envtest\|Kaniko" \
  cli/src/ --include="*.ts" | grep -v "test" | grep -v ".d.ts"
# Resultado esperado: 0 matches

# 9. Rate limiting
# Abrir 11 conexões simultâneas de logs → 11ª recebe 429
```

---

## Resumo

| Fase | Tasks | O que prova |
|------|-------|-------------|
| **1 — Status** | S3.1–S3.5 | `loopzero status` com fonte de verdade por campo e degradação graceful |
| **2 — Logs** | S3.6–S3.9 | Streaming de logs com multi-pod, rate limit, reconnect |
| **3 — Secrets CRUD** | S3.10–S3.13 | Set/list/delete/pull com encriptação e isolamento |
| **4 — Secret Rollout** | S3.14–S3.16 | Operator → API → Git → ArgoCD → rolling update |
| **5 — Gitea Hardening** | S3.17–S3.20 | Retry, fetch-merge-push, backup, alerts |
| **E2E** | S3.21–S3.22 | Convergência end-to-end: daily workflows + secret rotation |

**Total: 22 tasks** · **7 paralelizáveis no início** · **4 tasks G, 10 M, 8 P**
