# Sprint 2 — Arquitetura do Sistema

> Deploy Happy Path End-to-End
>
> Documento técnico: arquitetura, fluxogramas, técnicas e ferramentas.

---

## 1. Visão Geral

Sprint 2 implementa a promessa central do LoopZero: **`loopzero deploy` → URL ao vivo**. O fluxo atravessa 4 planos arquiteturais e 6 componentes, desde o terminal do desenvolvedor até o serviço rodando em Kubernetes.

```
┌─────────────────────────────────────────────────────────────────────┐
│                        DEVELOPER PLANE                              │
│  CLI (TypeScript)  ←──HTTP/SSE──→  API (Go)                        │
└─────────────────────────────────────────────────────────────────────┘
         │                              │
         │ git bundle upload            │ Advisory Lock + Idempotency
         │                              │
┌────────┴──────────────────────────────┴─────────────────────────────┐
│                        CONTROL PLANE (mgmt cluster)                 │
│  PostgreSQL  │  Gitea  │  MinIO  │  ArgoCD  │  TenantController    │
└─────────────────────────────────────────────────────────────────────┘
         │                              │
         │ Argo Workflow CR             │ ArgoCD Application sync
         │                              │
┌────────┴──────────────┐  ┌────────────┴─────────────────────────────┐
│     BUILD PLANE       │  │          WORKLOAD PLANE                   │
│  (build cluster)      │  │  (workload cluster)                      │
│  Argo Workflows       │  │  Traefik + Apps do usuário               │
│  Kaniko               │  │  cert-manager                            │
└───────────────────────┘  └──────────────────────────────────────────┘
```

### Stack Tecnológica do Sprint 2

| Componente | Tecnologia | Papel |
|------------|-----------|-------|
| CLI | TypeScript + Commander | Interface do desenvolvedor |
| API | Go + net/http + pgx/pgxpool | Orquestração de builds e releases |
| Estado | PostgreSQL + `pg_advisory_xact_lock` | Serialização e persistência |
| Builds | Argo Workflows + Kaniko | Pipeline de build containerizado |
| Artefatos | MinIO (S3) | Source bundles e cache |
| GitOps | Gitea + ArgoCD | Estado desejado → convergência |
| Ingress | Traefik + cert-manager | TLS e roteamento HTTP |
| Streaming | SSE (Server-Sent Events) | Progresso em tempo real |

---

## 2. Fluxo Principal: `loopzero deploy`

### 2.1 Fluxograma End-to-End

```
                          DEVELOPER
                             │
                             ▼
                    ┌─────────────────┐
                    │   loopzero deploy   │
                    │   (CLI - TS)    │
                    └────────┬────────┘
                             │
        ┌────────────────────┼────────────────────┐
        ▼                    ▼                    ▼
   Validações           Git Bundle           Idempotency-Key
   (.git, loopzero.yaml,    (source code)        (UUID v4)
    --env, --app)            │                    │
        │                    ▼                    │
        │           ┌────────────────┐            │
        │           │ POST /source   │            │
        │           │ (upload MinIO) │            │
        │           └───────┬────────┘            │
        │                   │ commit_sha          │
        └───────────────────┼─────────────────────┘
                            ▼
                   ┌─────────────────┐
                   │ POST /api/v1/   │
                   │ builds          │
                   │ (202 Accepted)  │
                   └────────┬────────┘
                            │
         ┌──────────────────┼──────────────────────┐
         │                  │                       │
         ▼                  ▼                       ▼
    Idempotency        Advisory Lock           SSE Stream
    Middleware         (5s timeout)             (/builds/:id/stream)
         │                  │                       │
         │            ┌─────┴─────────┐             │
         │            │ HasActiveBuild│             │
         │            │ (INSIDE lock) │             │
         │            └─────┬─────────┘             │
         │                  │ no conflict            │
         │                  ▼                       │
         │     ┌────────────────────┐               │
         │     │ Create Build       │               │
         │     │ (QUEUED → RUNNING) │               │
         │     └─────────┬──────────┘               │
         │               │                          │
         │               ▼                          │
         │    ┌──────────────────────┐   SSE        │
         │    │ Submit Argo Workflow │──────────────▶│ build.started
         │    │ (build cluster)     │               │
         │    └──────────┬──────────┘               │
         │               │                          │
         │    ┌──────────┴──────────────────────────┤
         │    │    ARGO WORKFLOW DAG                 │
         │    │                                     │
         │    │  clone → restore-cache → install    │
         │    │  → build → kaniko → generate-manifests
         │    │  → gitops-push                      │
         │    │                                     │
         │    └──────────┬──────────────────────────┤
         │               │                          │
         │               ▼                          │
         │   ┌───────────────────────┐              │
         │   │ POST /builds/:id/    │              │
         │   │ callback             │              │
         │   │ (from Argo Workflow) │              │
         │   └───────────┬──────────┘              │
         │               │                          │
         │      ┌────────┴────────┐                 │
         │      │  PHASE 1        │                 │
         │      │  (Lock 1)       │                 │
         │      │  Build →        │ SSE             │
         │      │  SUCCEEDED      │────────────────▶│ build.succeeded
         │      └────────┬────────┘                 │
         │               │                          │
         │      ┌────────┴────────┐                 │
         │      │  PHASE 2        │                 │
         │      │  (Lock 2)       │                 │
         │      │  Create Release │ SSE             │
         │      │  ArgoCD sync    │────────────────▶│ deploy.pending
         │      │  Health probes  │                 │
         │      │  Release →      │ SSE             │
         │      │  ACTIVE         │────────────────▶│ deploy.completed
         │      └─────────────────┘                 │
         │                                          │
         │                                          ▼
         │                               ┌──────────────────┐
         │                               │  CLI exibe URLs  │
         │                               │  exit code 0     │
         │                               └──────────────────┘
         │
         └──(se key duplicada)──▶ retorna resposta cacheada
```

### 2.2 Sequência Temporal (happy path, ~5 minutos)

```
T+0s     CLI: validações locais (loopzero.yaml, .git, auth)
T+1s     CLI → API: upload source bundle (git bundle → MinIO)
T+2s     CLI → API: POST /builds (202 Accepted)
T+2s     API: Lock 1 adquirido, Build QUEUED → RUNNING
T+2s     API → Argo: Workflow submetido no build cluster
T+3s     CLI: conecta ao SSE stream, recebe build.started
T+5s     Argo: clone-repo + restore-cache
T+15s    Argo: npm install (cache hit = mais rápido)
T+30s    Argo: npm run build
T+60s    Argo: Kaniko build + push para registry
T+90s    Argo: generate-manifests + gitops-push (commit no Gitea)
T+95s    Argo → API: callback (build succeeded)
T+95s    API: Lock 1 — Build → SUCCEEDED
T+96s    API: Lock 2 — Release criada (READY → PENDING)
T+96s    API → ArgoCD: trigger sync
T+120s   ArgoCD: sync completo, pods running
T+125s   API: health probe HTTP GET → 200 OK
T+125s   API: Release → ACTIVE, SSE deploy.completed
T+125s   CLI: exibe URLs, exit 0
```

---

## 3. Máquinas de Estado

### 3.1 Build State Machine (spec §5.1)

```
                    ┌──────────────────────────────────────────────────┐
                    │                                                  │
                    ▼                                                  │
              ┌──────────┐  BuildStarted   ┌──────────┐               │
              │  QUEUED   │───────────────▶│ RUNNING   │               │
              └──────────┘                 └──────────┘               │
                 │    │                      │  │  │                   │
   QueueTimeout  │    │ CancelRequested      │  │  │ CancelRequested  │
      (5min)     │    │                      │  │  │                  │
                 │    │                      │  │  ▼                  │
                 │    │    BuildSucceeded    │  │ ┌─────────────┐     │
                 │    │    ┌────────────────▶│  │ │ CANCELLING  │     │
                 │    │    │                 │  │ └──────┬──────┘     │
                 │    ▼    │                 │  │  CancelConfirmed    │
                 │  ┌──────┴───┐            │  │  │ / CancelTimeout  │
                 └─▶│  FAILED  │◀───────────┘  │  │     (60s)       │
                    └──────────┘  BuildFailed   │  │                  │
                     (terminal)   / Timeout     │  ▼                  │
                                  (30min)       │ ┌──────────┐       │
                                                │ │CANCELLED │◀──────┘
                    ┌──────────┐                │ └──────────┘
                    │SUCCEEDED │◀───────────────┘  (terminal)
                    └──────────┘  BuildSucceeded
                     (terminal)

  Timeouts:
    QUEUED   → 5 min  (QUEUE_TIMEOUT)
    RUNNING  → 30 min (BUILD_TIMEOUT)
    CANCELLING → 60s  (CANCEL_TIMEOUT)
```

**Arquivo:** `pkg/statemachine/build.go`

### 3.2 Release State Machine (spec §5.2)

```
  ┌─────────┐  GitMirrorCompleted  ┌──────────┐  SyncCompleted  ┌─────────┐
  │  READY   │────────────────────▶│ PENDING   │───────────────▶│ ACTIVE   │
  └─────────┘                      └──────────┘                 └─────────┘
       │                              │    ▲                      │    │
       │ GitMirrorFailed              │    │ SyncRetry            │    │
       │ / GitPushTimeout             │    │                      │    │
       ▼                              ▼    │                      │    │
  ┌────────────────┐          ┌────────────┴──┐                   │    │
  │GIT_MIRROR_FAILED│          │  SYNC_FAILED  │                   │    │
  └────────────────┘          └───────────────┘                   │    │
       │                              │                           │    │
       │ GitMirrorRetried             │ SyncExhausted             │    │
       │                              ▼                           │    │
       │                      ┌──────────────┐                    │    │
       └─────────────────────▶│ DRIFT_ALERT  │                    │    │
         (retries via PENDING)└──────────────┘                    │    │
                               (intervenção manual)               │    │
                                                                  │    │
                                       NewReleaseActivated ◀──────┘    │
                                                │             RollbackStarted
                                                ▼                      │
                                        ┌─────────────┐               ▼
                                        │ SUPERSEDED  │        ┌─────────────┐
                                        └─────────────┘        │ROLLING_BACK │
                                         (terminal)            └──────┬──────┘
                                                                      │
                                                        ┌─────────────┼──────────────┐
                                                        ▼             │              ▼
                                                 ┌────────────┐      │     ┌────────────────┐
                                                 │ROLLED_BACK │      │     │ROLLBACK_FAILED │
                                                 └────────────┘      │     └────────────────┘
                                                  (terminal)         │      (intervenção)
                                                                     ▼
                                                              ┌────────────┐
                                                              │ CANCELLED  │
                                                              └────────────┘
                                                               (terminal)
```

**Arquivo:** `pkg/statemachine/release.go`

---

## 4. Técnicas de Concorrência

### 4.1 Environment Lock (`pg_advisory_xact_lock`)

A técnica central de serialização do LoopZero. Todo estado mutável em `(tenant, project, environment)` é protegido por lock transacional do PostgreSQL.

```
┌─── Requisição A ──────────────────────────┐   ┌─── Requisição B ──────────────┐
│                                            │   │                                │
│  BEGIN                                     │   │  BEGIN                          │
│  SET LOCAL lock_timeout = '5s'             │   │  SET LOCAL lock_timeout = '5s'  │
│  SELECT pg_advisory_xact_lock(             │   │  SELECT pg_advisory_xact_lock(  │
│    hashtext('acme:saas:production')        │   │    hashtext('acme:saas:prod')   │
│  );                                        │   │  ); ◀── BLOQUEIA (até 5s)      │
│                                            │   │                                │
│  -- Executa dentro do lock:                │   │  -- Se timeout:                 │
│  --   HasActiveBuild? (INSIDE lock)        │   │  --   ERROR 55P03              │
│  --   Create Build                         │   │  --   → ErrLockTimeout          │
│  --   Submit Workflow                      │   │  --   → 409 "deploy in          │
│                                            │   │        progress"               │
│  COMMIT  ◀── lock liberado                 │   │                                │
│                                            │   │  -- Se A fez commit:            │
└────────────────────────────────────────────┘   │  --   Lock adquirido            │
                                                 │  --   HasActiveBuild? SIM       │
                                                 │  --   → 409 conflict            │
                                                 │  ROLLBACK                       │
                                                 └────────────────────────────────┘
```

**Por que `pg_advisory_xact_lock` e NUNCA `pg_advisory_lock`:**

| Aspecto | `pg_advisory_xact_lock` (usado) | `pg_advisory_lock` (proibido) |
|---------|-------------------------------|-------------------------------|
| Escopo | Transação | Sessão |
| Liberação | Automática no COMMIT/ROLLBACK | Manual (`pg_advisory_unlock`) |
| Connection Pool | Seguro (libera com a tx) | DEADLOCK (lock sobrevive retorno ao pool) |
| Crash recovery | Automática | Lock órfão bloqueia ambiente |

**Arquivo:** `api/internal/lock/environment_lock.go`

### 4.2 Idempotency Key

```
  Cliente                     Middleware                     Handler
    │                            │                              │
    │  POST /builds              │                              │
    │  Idempotency-Key: uuid-1   │                              │
    │  Body: {project, env, ...} │                              │
    │──────────────────────────▶│                              │
    │                            │  SHA256(body) → hash         │
    │                            │  Lookup(uuid-1)              │
    │                            │                              │
    │                            │──[key não existe]──────────▶│
    │                            │                              │ process()
    │                            │◀──────────────── response ──│
    │                            │  Store(uuid-1, hash, resp)  │
    │◀────────────── response ──│                              │
    │                            │                              │
    │  RETRY (mesmo body)        │                              │
    │  Idempotency-Key: uuid-1   │                              │
    │──────────────────────────▶│                              │
    │                            │  Lookup(uuid-1) → FOUND      │
    │                            │  hash matches? SIM           │
    │◀────────── cached resp ──│  (não chama handler)          │
    │                            │                              │
    │  RETRY (body diferente)    │                              │
    │  Idempotency-Key: uuid-1   │                              │
    │──────────────────────────▶│                              │
    │                            │  Lookup(uuid-1) → FOUND      │
    │                            │  hash matches? NÃO           │
    │◀────── 422 Unprocessable ─│  (payload mismatch)          │
```

**Arquivo:** `api/internal/middleware/idempotency.go`

### 4.3 Two-Phase Locking

A separação em Lock 1 + Lock 2 é uma decisão arquitetural crítica:

```
  ┌─────────── Lock 1 (Build) ──────────────┐
  │                                          │
  │  Callback chega do Argo Workflow         │
  │  ├── Re-fetch build (estado pode ter     │
  │  │   mudado por cancel concorrente)      │
  │  ├── Guard EC-N: QUEUED → RUNNING        │
  │  │   (se Argo terminou antes do submit)  │
  │  ├── Build → SUCCEEDED ou FAILED         │
  │  ├── Store error_message (se falhou)     │
  │  └── Emit SSE build.succeeded/failed     │
  │                                          │
  │  COMMIT ◀── Lock 1 liberado              │
  │  (Phase 1 independente — se Phase 2      │
  │   crashar, build já está correto)        │
  └──────────────────────────────────────────┘
                     │
                     ▼  (se SUCCEEDED)
  ┌─────────── Lock 2 (Release) ────────────┐
  │                                          │
  │  ├── Create Release (READY)              │
  │  ├── Release READY → PENDING             │
  │  ├── Update environment_targets          │
  │  │   (desired_release_id)                │
  │  ├── Trigger ArgoCD sync                 │
  │  ├── Health probes (HTTP GET apps)       │
  │  ├── Release PENDING → ACTIVE            │
  │  │   (ou → SYNC_FAILED)                 │
  │  ├── Update active_release_id            │
  │  ├── Supersede release anterior          │
  │  └── Emit SSE deploy.completed           │
  │                                          │
  │  COMMIT ◀── Lock 2 liberado              │
  └──────────────────────────────────────────┘
```

**Justificativa:** Se Phase 2 falha (crash, ArgoCD timeout), Phase 1 já commitou. O build está corretamente marcado como SUCCEEDED. Um novo deploy pode prosseguir sem ambiente travado.

---

## 5. Pipeline de Build (Argo Workflows)

### 5.1 DAG do Workflow

```
  clone-repo ────────────────────────────────────────────┐
       │                                                  │
       ▼                                                  ▼
  restore-cache                                    validate-workspace
       │                                                  │
       ▼                                                  │
  install-deps ◀──────────────────────────────────────────┘
       │
       ▼
  build-apps
       │
       ▼
  security-scan ────────────────────┐
       │                             │
       ▼                             │
  generate-dockerfile                │
       │                             │
       ▼                             │
  build-image (Kaniko) ◀─────────────┘
       │
       ├──────────────────────┐
       ▼                      ▼
  generate-manifests    validate-manifests
       │                (continueOnFailed)
       ▼
  gitops-push
       │
       ├──────────────────────┐
       ▼                      ▼
  save-cache            emit-summary
                              │
                              ▼
                        notify-api
                        (callback)
```

### 5.2 Detalhes por Step

| Step | Container | Função | Retry |
|------|-----------|--------|-------|
| clone-repo | alpine/git | Clona source do MinIO (git bundle) | — |
| restore-cache | minio/mc | Restaura node_modules do S3 cache | — |
| validate-workspace | node:20 | Valida loopzero.yaml, package.json | — |
| install-deps | node:20 | `npm ci --prefer-offline` | 3× |
| build-apps | node:20 | `npm run build` por app | — |
| security-scan | node:20 | `npm audit --audit-level=critical` | — |
| generate-dockerfile | alpine | Gera Dockerfile multi-stage | — |
| build-image | gcr.io/kaniko | Build + push para registry | 3× |
| generate-manifests | alpine | Cria Deployment + Service YAML | — |
| validate-manifests | kubeconform | Valida YAML contra schemas K8s | continueOnFailed |
| gitops-push | alpine/git | Commit manifests no Gitea repo | — |
| save-cache | minio/mc | Salva node_modules no S3 | — |
| emit-summary | alpine | JSON summary do build | — |
| notify-api | curlimages/curl | `POST /builds/:id/callback` | — |

**Arquivo:** `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml`

### 5.3 Git Commit Format (gitops-push)

```
Release: r-{4hex}
Build-ID: b-{4hex}
Correlation-ID: {uuid}

deploy {project} to {environment}

---
Generated by LoopZero build pipeline
```

Os trailers permitem rastreabilidade end-to-end: CLI request → API → Argo → Git → ArgoCD.

---

## 6. SSE (Server-Sent Events) — Streaming de Progresso

### 6.1 Arquitetura do Event Journal

```
  ┌──────────────┐                ┌───────────────────┐
  │ Orchestrator │──emit()──────▶│  sse_events (PG)   │
  │              │                │  ┌───────────────┐ │
  │  build.*     │                │  │ id (serial)   │ │
  │  deploy.*    │                │  │ build_id (FK) │ │
  └──────────────┘                │  │ event_type    │ │
                                  │  │ data (jsonb)  │ │
  ┌──────────────┐                │  │ created_at    │ │
  │ CLI (SSE)    │◀──poll(1s)───│  └───────────────┘ │
  │              │                └───────────────────┘
  │ Last-Event-ID│
  │ para replay  │    ┌──────────────────────┐
  │              │    │  Heartbeat (:ping)   │
  │              │◀──│  a cada 15s          │
  └──────────────┘    └──────────────────────┘
```

### 6.2 Eventos SSE

| Evento | Quando | Dados |
|--------|--------|-------|
| `build.started` | Workflow submetido | `{build_id, apps}` |
| `build.log` | Durante execução | `{step, message}` |
| `build.succeeded` | Argo callback success | `{build_id, duration}` |
| `build.failed` | Argo callback failure | `{build_id, error}` |
| `deploy.pending` | Release criada, ArgoCD syncing | `{release_id}` |
| `deploy.active` | Health probe OK | `{release_id, urls}` |
| `deploy.sync_failed` | ArgoCD/health failed | `{release_id, error}` |
| `deploy.completed` | Fluxo completo | `{status, apps[], urls[], duration}` |

### 6.3 Reconexão e Replay

```
  CLI conecta                  CLI desconecta (rede)       CLI reconecta
       │                              │                          │
       ▼                              │                          ▼
  GET /builds/:id/stream              │              GET /builds/:id/stream
  Last-Event-ID: (vazio)              │              Last-Event-ID: 42
       │                              │                          │
       ▼                              │                          ▼
  ← event: build.started              │              ← event: build.succeeded
     id: 40                           │                 id: 43
  ← event: build.log                  │              ← event: deploy.pending
     id: 41                           │                 id: 44
  ← event: build.log                  │              ← event: deploy.completed
     id: 42                           │                 id: 45
  ← :ping (heartbeat)                 │
       │                              │              (replay automático de id > 42)
       ✕ desconexão                   │
```

**Arquivos:** `api/internal/sse/event_store.go`, `api/internal/sse/stream_handler.go`, `cli/src/api/sse-client.ts`

---

## 7. Circuit Breaker (ArgoCD)

```
  API                    CircuitBreakerSyncClient              ArgoCD
   │                              │                              │
   │──TriggerSync()──────────────▶│                              │
   │                              │──[CLOSED]──TriggerSync()───▶│
   │                              │                              │── OK
   │                              │◀──────────── success ────────│
   │◀────────── success ─────────│  failures = 0                │
   │                              │                              │
   │──TriggerSync()──────────────▶│                              │
   │                              │──[CLOSED]──TriggerSync()───▶│
   │                              │                              │── FAIL
   │                              │◀──────────── error ──────────│
   │                              │  failures++ (1/3)            │
   │                              │  ... (mais 2 falhas)         │
   │                              │  failures = 3 → OPEN         │
   │                              │                              │
   │──TriggerSync()──────────────▶│                              │
   │                              │──[OPEN]── 503 imediato       │
   │◀── 503 PLATFORM_DEGRADED ──│  (não chama ArgoCD)           │
   │                              │                              │
   │  ... (após 2 successes)      │                              │
   │                              │  recoveries = 2 → CLOSED     │
```

**Parâmetros:** 3 falhas consecutivas → OPEN, 2 recuperações → CLOSED.

**Arquivo:** `api/internal/argocd/circuit_breaker.go`

---

## 8. Modelo de Dados

### 8.1 Tabelas Principais (Sprint 2)

```
  ┌─────────────┐     ┌─────────────────┐     ┌───────────────────┐
  │   tenants    │     │    projects     │     │      builds       │
  │─────────────│     │─────────────────│     │───────────────────│
  │ id (PK)     │◀───│ tenant_id (FK)  │     │ id (PK)           │
  │ display_name│     │ id (PK)         │◀───│ project_id (FK)   │
  │ plan        │     │ name            │     │ tenant_id          │
  │ owner_id    │     │ created_by      │     │ environment        │
  └─────────────┘     └─────────────────┘     │ commit_sha         │
                                              │ status (state)     │
                                              │ error_message      │
  ┌──────────────────────┐                    │ started_at         │
  │  environment_targets  │                    │ finished_at        │
  │──────────────────────│                    └───────────────────┘
  │ tenant_id (PK)       │                           │
  │ project_id (PK)      │                           │ 1:N
  │ environment (PK)     │                           ▼
  │ desired_release_id   │───▶┌──────────────────┐  ┌───────────────┐
  │ active_release_id    │───▶│    releases      │  │  sse_events   │
  └──────────────────────┘    │──────────────────│  │───────────────│
                              │ id (PK)          │  │ id (serial)   │
                              │ build_id (FK)    │  │ build_id (FK) │
                              │ environment      │  │ event_type    │
                              │ status (state)   │  │ data (jsonb)  │
                              │ commit_sha       │  │ created_at    │
                              │ activated_at     │  └───────────────┘
                              └──────────────────┘
                                                    ┌───────────────────┐
                                                    │ idempotency_keys  │
                                                    │───────────────────│
                                                    │ key (PK)          │
                                                    │ request_hash      │
                                                    │ response_status   │
                                                    │ response_body     │
                                                    │ created_at        │
                                                    │ expires_at        │
                                                    └───────────────────┘
```

### 8.2 Índice Parcial (Concurrent Build Guard)

```sql
CREATE UNIQUE INDEX idx_one_active_build_per_env
ON builds (tenant_id, project_id, environment)
WHERE status NOT IN ('SUCCEEDED', 'FAILED', 'CANCELLED');
```

Garante em nível de banco que no máximo UM build não-terminal existe por ambiente. Camada extra de proteção além do advisory lock.

---

## 9. Injeção de Dependências (DIP)

```
  ┌───────────────────────────────────────────────┐
  │            BuildOrchestrator                   │
  │  (orquestra todo o fluxo de deploy)           │
  │───────────────────────────────────────────────│
  │  EnvironmentLocker  (interface)               │
  │  ├── WithEnvironmentLock (produção: pgx)      │
  │  └── FakeLocker (testes: in-memory)           │
  │                                               │
  │  BuildStore  (interface)                      │
  │  ├── PostgresBuildStore (produção)            │
  │  └── FakeBuildStore (testes)                  │
  │                                               │
  │  ReleaseStore  (interface)                    │
  │  ├── PostgresReleaseStore (produção)          │
  │  └── FakeReleaseStore (testes)                │
  │                                               │
  │  WorkflowSubmitter  (interface)               │
  │  ├── ArgoWorkflowSubmitter (produção)         │
  │  └── FakeSubmitter (testes)                   │
  │                                               │
  │  SyncClient  (interface)                      │
  │  ├── CircuitBreakerSyncClient (produção)      │
  │  │   └── wraps ArgoCD real client             │
  │  └── FakeSyncClient (testes)                  │
  │                                               │
  │  HealthProber  (interface)                    │
  │  ├── HTTPHealthProber (produção)              │
  │  └── FakeProber (testes: always healthy)      │
  │                                               │
  │  EventEmitter  (interface)                    │
  │  ├── PostgresEventStore (produção)            │
  │  └── InMemoryEventStore (testes)              │
  └───────────────────────────────────────────────┘
```

---

## 10. Tratamento de Erros e Resiliência

### 10.1 Callback Resilience (lição aprendida)

O callback do Argo Workflow é o ponto mais crítico do sistema. Se retornar erro, o build fica permanentemente em RUNNING, travando o ambiente.

```
  Argo Workflow                  API /callback
       │                              │
       │── POST (JSON malformado) ──▶│
       │                              │── Lê body como bytes
       │                              │── Tenta JSON parse
       │                              │── Se falha: fallbackParse()
       │                              │     └── regex/string matching
       │                              │── Se nenhum status: default "failed"
       │                              │── Return 202 IMEDIATO
       │                              │── goroutine: OnBuildCompleted()
       │◀──────── 202 ───────────────│
       │                              │
       │   NUNCA retorna 400          │
       │   NUNCA bloqueia callback    │
```

### 10.2 Build Reaper (timeout safety net)

```
  ┌──────────────────────────────────────────────────┐
  │  Build Reaper (background goroutine, a cada 1m)  │
  │──────────────────────────────────────────────────│
  │                                                  │
  │  SELECT * FROM builds                            │
  │  WHERE status IN ('QUEUED', 'RUNNING')           │
  │  AND started_at < now() - timeout                │
  │                                                  │
  │  Para cada build expirado:                       │
  │  ├── Transition → FAILED                         │
  │  ├── SetErrorMessage("build timed out after Xm") │
  │  └── Emit SSE build.failed                       │
  │                                                  │
  │  Timeouts:                                       │
  │    QUEUED:  5 minutos                            │
  │    RUNNING: 30 minutos                           │
  └──────────────────────────────────────────────────┘
```

### 10.3 Exit Codes CLI

| Código | Significado | Quando |
|--------|------------|--------|
| `0` | Sucesso | Deploy completo, todos apps ativos |
| `1` | Erro de uso | Argumento inválido, loopzero.yaml ausente, --app inexistente |
| `2` | Falha no build | Kaniko falhou, npm build falhou |
| `3` | Falha no deploy | ArgoCD sync falhou, health probe falhou, timeout |
| `4` | Erro de auth | Token expirado, não autenticado |
| `5` | Falha parcial | Multi-app: alguns subiram, outros falharam |

---

## 11. Decisões Arquiteturais (DAs)

| ID | Decisão | Justificativa |
|----|---------|---------------|
| DA-1 | Source upload via git bundle (não Docker context) | Menor payload, preserva git history, cache de camadas |
| DA-2 | Advisory lock transaction-scoped | Seguro com connection pool, auto-release em crash |
| DA-3 | Concurrent check INSIDE lock | Evita race condition entre check e insert |
| DA-4 | Lock key inclui tenant_id | Isolamento multi-tenant |
| DA-5 | SSE para streaming (não WebSocket) | Unidirecional, HTTP nativo, reconexão automática |
| DA-6 | Two-phase locking (Lock 1 + Lock 2) | Phase 1 commita independente, evita ambiente travado |
| DA-7 | Callback NUNCA retorna 400 | Build travado em RUNNING = pior falha possível |
| DA-8 | Build Reaper como safety net | Garante que builds nunca ficam stuck infinitamente |
| DA-10 | Environment names RFC 1123 | Compatível com K8s namespace naming |
| DA-11 | IDs curtos (b-{4hex}, r-{4hex}, d-{4hex}) | DX: fácil de copiar/colar, comunicar verbalmente |
| DA-18 | Mensagem de rejeição user-friendly | "A deploy is already in progress" (sem termos internos) |
| DA-19 | Circuit breaker no ArgoCD | 3 falhas → OPEN → 503 PLATFORM_DEGRADED |

---

## 12. Testes

### 12.1 Pirâmide de Testes (Sprint 2)

```
          /  E2E (Ginkgo)  \         ← deploy → URL live
         /------------------\           concurrent rejection
        /   Integration      \       ← lock crash recovery
       /----------------------\         concurrent builds (pg real)
      /     Unit (Go + Jest)   \     ← 323 testes
     /--------------------------\       state machines, routes, CLI
```

### 12.2 Testes Críticos Implementados

| Teste | Tipo | Verifica |
|-------|------|----------|
| Concurrent deploys same env → 1×202 + 1×409 | E2E | Lock serializa corretamente |
| Concurrent deploys diff env → 2×202 | E2E | Lock não serializa cross-env |
| API crash mid-lock → próxima operação OK | Integration | Lock auto-release |
| Same idempotency key → cached response | Unit | Idempotency middleware |
| Different payload same key → 422 | Unit | Idempotency payload check |
| Malformed callback → 202 (não 400) | Unit | Callback resilience |
| Build timeout → FAILED + error_message | Unit | Reaper funciona |
| CLI forbidden terms check | E2E | Nenhum termo interno no output |
| CLI --json schema validation | Unit | Contrato de estabilidade |
| State machine invalid transition → panic | Unit | Transições estritas |

### 12.3 Arquivos de Teste

```
api/tests/unit/builds_route_test.go        ← rotas de build
api/tests/unit/build_orchestrator_test.go   ← orquestração
api/tests/unit/build_store_test.go          ← persistência
api/tests/unit/release_store_test.go        ← releases
api/tests/unit/idempotency_test.go          ← middleware
api/tests/unit/health_prober_test.go        ← health checks
api/tests/integration/lock_crash_recovery_test.go
api/tests/integration/concurrent_builds_test.go
infra/tests/e2e/deploy_happy_path_test.go
infra/tests/e2e/concurrent_deploy_test.go
cli/tests/unit/commands/deploy.test.ts
cli/tests/unit/api/sse-client.test.ts
cli/tests/unit/output/deploy-renderer.test.ts
pkg/statemachine/*_test.go
```

---

## 13. Ferramentas e Infraestrutura

### 13.1 Ferramentas de Desenvolvimento

| Ferramenta | Versão | Uso |
|------------|--------|-----|
| Go | 1.22+ | API, operators, state machines |
| TypeScript | 5.x | CLI (Commander.js) |
| PostgreSQL | 18 | Estado, locks, idempotency, SSE journal |
| k3d | 5.x | 3 clusters locais (mgmt, build, workload) |
| Tilt | — | Dev loop com hot reload |
| Task (Taskfile) | 3 | Automação de tarefas |
| Ginkgo/Gomega | v2 | Testes E2E (Go) |
| Jest | 29 | Testes unitários CLI |
| envtest | — | K8s API fake para testes de operator |
| kubeconform | — | Validação de manifests YAML |

### 13.2 Ferramentas de Runtime

| Ferramenta | Cluster | Uso |
|------------|---------|-----|
| Argo Workflows | build | Pipeline de build (DAG) |
| Kaniko | build | Build de imagens sem Docker daemon |
| Gitea | mgmt | Git server para estado desejado (GitOps) |
| ArgoCD | mgmt | Sincronização GitOps → workload cluster |
| MinIO | mgmt | Object storage (source bundles, cache) |
| Traefik | workload | Ingress controller + TLS |
| cert-manager | mgmt | Certificados TLS automáticos |
| PostgreSQL | mgmt | Banco de dados principal |

### 13.3 Clusters k3d

```
  ┌─────────────────────────────────────────────────────────────┐
  │  Docker Host                                                 │
  │                                                              │
  │  ┌──────────────────┐  ┌──────────────┐  ┌────────────────┐│
  │  │  loopzero-mgmt       │  │ loopzero-build   │  │ loopzero-workload  ││
  │  │  (1 server,      │  │ (1 server,   │  │ (1 server,     ││
  │  │   2 agents)      │  │  1 agent)    │  │  2 agents)     ││
  │  │                  │  │              │  │                ││
  │  │  PostgreSQL      │  │ Argo         │  │ Traefik        ││
  │  │  Gitea           │  │ Workflows    │  │ User Apps      ││
  │  │  MinIO           │  │ Kaniko       │  │ cert-manager   ││
  │  │  ArgoCD          │  │              │  │                ││
  │  │  LoopZero API        │  │              │  │                ││
  │  │  TenantController│  │              │  │                ││
  │  │  Prometheus      │  │              │  │                ││
  │  │  Grafana         │  │              │  │                ││
  │  └──────────────────┘  └──────────────┘  └────────────────┘│
  │                                                              │
  │  ┌──────────────────────────────────────────────────────┐   │
  │  │  k3d-loopzero-registry:5000 (shared container registry)  │   │
  │  └──────────────────────────────────────────────────────┘   │
  └─────────────────────────────────────────────────────────────┘
```

---

## 14. Lições Aprendidas

| # | Problema | Solução |
|---|---------|---------|
| 1 | Callback retornando 400 → build stuck forever | NUNCA retornar 400 em callback; fallback parse + default "failed" |
| 2 | Concurrent check fora do lock → race condition | Check SEMPRE dentro do advisory lock |
| 3 | `pg_advisory_lock` com pool → deadlock | APENAS `pg_advisory_xact_lock` (transaction-scoped) |
| 4 | Phase 2 crash → ambiente travado | Two-phase locking: Phase 1 commita independente |
| 5 | Termos internos no CLI output | Grep automático + E2E test com forbidden terms list |
| 6 | Testes de health prober com retry → 240s | Backoff de 1 tentativa nos testes |
| 7 | Build id em todos git trailers | Correlation-ID propagado end-to-end |
| 8 | Circuit breaker implementado mas não wired | Validação em `main.go` que wrapper está presente |
| 9 | Error silenciosamente engolidos (`_ = err`) | Logging obrigatório em todos os catch |

---

*Documento gerado em 2026-03-16. Referência autoritativa: `docs/technical.md`.*
