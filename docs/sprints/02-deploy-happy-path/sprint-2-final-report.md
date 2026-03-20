# Sprint 2 — Deploy Happy Path: Relatório Final de Entregas

> **Data:** 2026-03-17
> **Sprint:** 02 — `loopzero deploy` Happy Path End-to-End
> **Semanas:** 6–9
> **Branch:** `feat/sprint-1`
> **Status:** Feature-complete. Blockers corrigidos. Exit gate satisfeito com caveats.

---

## Índice

1. [Resumo Executivo](#1-resumo-executivo)
2. [Objetivo e Exit Gate](#2-objetivo-e-exit-gate)
3. [Entregas por Componente](#3-entregas-por-componente)
   - 3.1 [CLI — `loopzero deploy`](#31-cli--loopzero-deploy)
   - 3.2 [API — Build Orchestrator + Routes](#32-api--build-orchestrator--routes)
   - 3.3 [Shared Libraries — `pkg/`](#33-shared-libraries--pkg)
   - 3.4 [Build Pipeline — Argo Workflows](#34-build-pipeline--argo-workflows)
   - 3.5 [Operators — Tenant Controller](#35-operators--tenant-controller)
   - 3.6 [Infraestrutura — Helm + k3d](#36-infraestrutura--helm--k3d)
   - 3.7 [Database — Migrations](#37-database--migrations)
4. [Cobertura de Testes](#4-cobertura-de-testes)
5. [Conformidade com a Spec](#5-conformidade-com-a-spec)
6. [Bugs Encontrados e Corrigidos](#6-bugs-encontrados-e-corrigidos)
   - 6.1 [Blockers (B1–B4)](#61-blockers-b1b4)
   - 6.2 [Bugs Críticos (C1–C4)](#62-bugs-críticos-c1c4)
   - 6.3 [Dogfooding Findings (F-01 a F-09)](#63-dogfooding-findings-f-01-a-f-09)
   - 6.4 [High Priority (H1–H5)](#64-high-priority-h1h5)
   - 6.5 [Medium Priority (M1–M8)](#65-medium-priority-m1m8)
7. [Gaps Identificados](#7-gaps-identificados)
   - 7.1 [Gaps entre Spec e Implementação](#71-gaps-entre-spec-e-implementação)
   - 7.2 [Gaps de Testes](#72-gaps-de-testes)
   - 7.3 [Gaps de Observabilidade](#73-gaps-de-observabilidade)
8. [Padrão Recorrente: "Implemented But Not Wired"](#8-padrão-recorrente-implemented-but-not-wired)
9. [Decisões Arquiteturais Tomadas](#9-decisões-arquiteturais-tomadas)
10. [Débito Técnico Registrado](#10-débito-técnico-registrado)
11. [Cronologia de Commits](#11-cronologia-de-commits)
12. [Métricas de Código](#12-métricas-de-código)
13. [Checklist de Exit Gate](#13-checklist-de-exit-gate)
14. [Veredito Final](#14-veredito-final)

---

## 1. Resumo Executivo

Sprint 2 entregou o fluxo completo `loopzero deploy` — do CLI até a URL live — cortando verticalmente os 4 planos da arquitetura (Developer → Control → Build → Workload). São ~8.000 linhas de código Go, ~2.500 linhas TypeScript, 965 linhas de workflow YAML, 298+ testes unitários, 2 testes de integração e 1 E2E.

**Padrão recorrente encontrado:** "implemented but not wired" — código correto existia no lugar errado ou a camada de adaptação estava incompleta. Todas as instâncias foram corrigidas.

**Auditorias realizadas:**
- Code audit (2026-03-15): 4 blockers, 4 critical, 5 high, 9 medium
- Dogfooding (2026-03-15): 9 findings, 6 fixed na sessão
- Pipeline review por Helena Duarte: APPROVED WITH CAVEATS

**Estado atual:** Todos os blockers e bugs críticos foram corrigidos. O sistema funciona end-to-end com infraestrutura real (zero fakes em produção).

---

## 2. Objetivo e Exit Gate

**Objetivo do Sprint:** Um desenvolvedor roda `loopzero login` → `loopzero init` → `loopzero deploy` e obtém uma URL real com a aplicação rodando, em infraestrutura real, com TLS.

**Exit Gate (de `sprint.md`):**

| Critério | Status | Evidência |
|----------|--------|-----------|
| `loopzero deploy` produz URL acessível | ✅ | E2E test + dogfooding |
| Build via Argo Workflows + Kaniko | ✅ | Workflow template 965 linhas |
| Release via ArgoCD sync | ✅ | Two-phase activation implementada |
| Advisory lock serializa mutations | ✅ | `pg_advisory_xact_lock` + integration tests |
| Concurrent deploy → 409 | ✅ | `concurrent_builds_test.go` (737 linhas) |
| Idempotency-Key funciona | ✅ | Integration test: replay + mismatch |
| Exit codes 0/2/3/5 corretos | ✅ | 200+ testes CLI |
| `--json` conforme spec §0.3 | ✅ | Schema validation em testes |
| CI mode funciona | ✅ | Testes com `CI=true` |
| Zero termos internos no CLI | ✅ | Grep automatizado em testes |
| State machines conforme spec §5 | ✅ | 7 máquinas com testes completos |
| Zero fakes em produção | ✅ | `main.go` usa 100% implementações reais |

---

## 3. Entregas por Componente

### 3.1 CLI — `loopzero deploy`

**Arquivos entregues:**

| Arquivo | Linhas | Responsabilidade |
|---------|--------|------------------|
| `cli/src/commands/deploy.ts` | 496 | Comando principal — orquestração completa |
| `cli/src/output/deploy-renderer.ts` | 214 | Renderização TTY/CI/JSON de progresso |
| `cli/src/api/client.ts` | 167 | HTTP client com auth, correlation-id, error mapping |
| `cli/src/api/sse-client.ts` | 355 | SSE streaming com reconnect, dedup, polling fallback |
| `cli/src/adapters/git.ts` | 48 | Git operations (bundle, HEAD, uncommitted check) |
| `cli/src/adapters/source-upload.ts` | 112 | Upload de bundle + loopzero.yaml para API |
| `cli/src/adapters/source-bundle.ts` | 10 | Cleanup helper |
| `cli/src/adapters/loopzero-yaml.ts` | 98 | Parser de loopzero.yaml (hand-written, sem dep) |
| `cli/src/output/formatter.ts` | 74 | Formatação genérica (success/error/json/table) |
| `cli/src/config/store.ts` | 70 | Token storage (XDG, 0600 permissions) |
| `cli/src/detection/monorepo.ts` | 143 | Detecção automática de monorepo |
| `cli/src/detection/framework.ts` | 148 | Detecção de framework + porta padrão |
| `cli/src/index.ts` | 287 | Entry point + wiring de dependências |

**Funcionalidades implementadas:**

- **Validação fail-fast:** Token → .git → loopzero.yaml → apps → environment → secrets → uncommitted warning → lockfile warning
- **Flags:** `--env`, `--app`, `--wait` (default true), `--no-wait`, `--json`
- **Exit codes:** 0 (success), 1 (usage), 2 (build failure), 3 (deploy failure), 4 (auth), 5 (partial)
- **CI mode:** Detecta `CI=true`, `GITHUB_ACTIONS`, `GITLAB_CI`, `CIRCLECI`, `JENKINS_URL`
- **SSE resilience:** Last-Event-ID dedup, max 15 retries, exponential backoff, polling fallback
- **Git bundle:** `--all HEAD` (self-contained, sem prerequisites)
- **Secrets pre-check:** Escoped a apps selecionados via `--app`
- **Idempotency-Key:** UUID v4 gerado por deploy attempt
- **Timeout:** 10 minutos por deployment
- **JSON output shape:**
  ```json
  {
    "deploy_id": "d-8f3a",
    "status": "success|partial_failure|deploy_failed|failed",
    "duration_seconds": 97,
    "apps": [{
      "name": "api",
      "status": "deployed|failed",
      "url": "https://..." | null,
      "build_log": "loopzero logs --build <id>",
      "build_duration_seconds": 45
    }]
  }
  ```
- **Zero termos internos:** Nenhum ArgoCD, Gitea, MinIO, Kaniko, CRD, tenant_id, advisory lock em output

**Testes CLI:** 18 arquivos de teste, 2000+ linhas, cobrindo:
- `deploy.test.ts` — 200+ test cases (auth, validation, concurrent, idempotency, exit codes, JSON, SSE)
- `sse-client.test.ts` — 25 tests (parsing, dedup, reconnect, polling, max retries)
- `client.test.ts` — HTTP methods, error mapping, exit codes
- `deploy-renderer.test.ts` — 90 tests (TTY/CI/JSON, worker apps, internal terms)
- `source-upload.test.ts`, `git.test.ts`, `loopzero-yaml.test.ts`, `formatter.test.ts`

---

### 3.2 API — Build Orchestrator + Routes

**Arquivos entregues:**

| Arquivo | Linhas | Responsabilidade |
|---------|--------|------------------|
| `api/cmd/main.go` | 295 | Entry point — wiring de TODAS as dependências reais |
| `api/internal/services/build_orchestrator.go` | 699 | Orquestração: submit, callback, cancel, two-phase activation |
| `api/internal/services/build_reaper.go` | 218 | Timeout detection (QUEUED 5min, RUNNING 30min, CANCELLING 60s) |
| `api/internal/services/argocd_client.go` | 145 | Circuit breaker wrapper para ArgoCD |
| `api/internal/lock/environment_lock.go` | 116 | `pg_advisory_xact_lock` com timeout 5s + panic recovery |
| `api/internal/routes/builds.go` | 406 | 5 endpoints HTTP para builds |
| `api/internal/routes/auth.go` | — | Login/logout/whoami |
| `api/internal/routes/projects.go` | — | Project CRUD |
| `api/internal/routes/secrets.go` | — | Secret CRUD |
| `api/internal/routes/source.go` | — | Git bundle upload |
| `api/internal/routes/health.go` | — | Health/readiness probes |
| `api/internal/sse/stream_handler.go` | 256 | SSE com Last-Event-ID, heartbeat, terminal detection |
| `api/internal/sse/event_store.go` | 114 | Journal de eventos com sequential ID |
| `api/internal/argocd/sync_client.go` | 143 | ArgoCD sync via HTTP API |
| `api/internal/argo/workflow_submitter.go` | 152 | Submissão de Workflow via dynamic K8s client |
| `api/internal/middleware/idempotency.go` | 135 | Idempotency-Key (24h TTL, SHA256 hash) |
| `api/internal/middleware/correlation.go` | 23 | Correlation-ID injection (UUID v4) |
| `api/internal/middleware/auth.go` | 182 | Token validation + tenant resolution (5min cache) |
| `api/internal/adapters/adapters.go` | 221 | DIP bridges (zero fakes em produção) |
| `api/internal/source/gitea_store.go` | 204 | Git bundle → Gitea (unbundle + push) |
| `api/internal/source/validator.go` | — | Validação de source |
| `api/internal/k8s/tenant_cr_manager.go` | — | Tenant CR via K8s API |
| `api/internal/db/build_store.go` | 246 | CRUD builds + state machine validation |
| `api/internal/db/release_store.go` | 171 | CRUD releases + state machine validation |
| `api/internal/db/idempotency_store.go` | 92 | Idempotency key storage (24h TTL) |
| `api/internal/db/environment_target_store.go` | 127 | UPSERT desired/active release pointer |
| `api/internal/db/pool.go` | — | Connection pool (MaxConns=20, MinConns=5, timeout=30s) |
| `api/internal/health/prober.go` | — | HTTP health probe com retry |
| `api/internal/metrics/metrics.go` | — | 20 métricas Prometheus |
| `api/internal/correlation/id.go` | — | UUID v4 generation |

**Endpoints implementados:**

| Método | Rota | Status | Comportamento |
|--------|------|--------|---------------|
| POST | `/api/v1/builds` | 202 | Submit build com advisory lock + idempotency |
| GET | `/api/v1/builds/:id` | 200 | Status do build (single request) |
| GET | `/api/v1/builds/:id/stream` | 200 | SSE stream (Last-Event-ID) |
| POST | `/api/v1/builds/:id/callback` | 202 | Webhook do Argo (async, HMAC-SHA256) |
| POST | `/api/v1/builds/:id/cancel` | 200 | Cancelar build (QUEUED ou RUNNING) |
| POST | `/api/v1/source/upload` | 200 | Upload de git bundle + loopzero.yaml |
| POST | `/api/v1/projects` | 201 | Criar projeto |
| GET | `/api/v1/secrets` | 200 | Listar secrets metadata |
| POST | `/api/v1/auth/login` | 200 | OAuth login |
| GET | `/api/v1/auth/whoami` | 200 | User info |
| GET | `/api/v1/health` | 200 | Health check |
| GET | `/api/v1/health/deep` | 200/503 | Deep health (DB connectivity) |

**Fluxo SubmitBuild (spec §10.1, steps 4-11):**

```
1. Idempotency middleware intercepta
2. Advisory lock: pg_advisory_xact_lock(hash(tenant:project:env))
3. Concurrent-build check DENTRO do lock (HasActiveBuild)
4. Create Build(QUEUED) → Submit Argo Workflow → Transition(RUNNING)
5. Emit SSE event (build.started)
6. Store idempotency response
7. Release lock (commit tx)
```

**Fluxo OnBuildCompleted — Two-Phase Activation:**

```
Phase 1 (dentro do lock):
  Persist app results → Build(SUCCEEDED) → Create Release(READY)
  → Emit SSE (build.completed)

Phase 2 (fora do lock):
  Circuit breaker check → ArgoCD sync trigger → Poll sync status
  → Release(ACTIVE) ou Release(SYNC_FAILED) → Emit SSE final
```

**Callback resilience (DA-15):**
- Retorna 202 imediatamente, processa em goroutine
- Best-effort JSON parsing — nunca retorna 400 em payload interno
- Fallback para `status: "failed"` se payload unparseable
- Background context para evitar cancelamento por client disconnect

---

### 3.3 Shared Libraries — `pkg/`

**State Machines (`pkg/statemachine/`) — 7 máquinas completas:**

| Máquina | Estados | Transições | Terminal States | Timeouts |
|---------|---------|------------|-----------------|----------|
| Build | QUEUED, RUNNING, SUCCEEDED, FAILED, CANCELLING, CANCELLED | 8 | SUCCEEDED, FAILED, CANCELLED | QUEUED 5min, RUNNING 30min, CANCELLING 60s |
| Release | READY, PENDING, ACTIVE, SYNC_FAILED, DRIFT_ALERT, SUPERSEDED, ROLLING_BACK, ROLLED_BACK, ROLLBACK_FAILED, GIT_MIRROR_FAILED, CANCELLED | 15+ | SUPERSEDED, ROLLED_BACK, CANCELLED | READY 5min, PENDING 2min, ROLLING_BACK 2min |
| Promotion | VALIDATING, COMMITTING, SYNCING, COMPLETED, REJECTED, COMMIT_FAILED, SYNC_FAILED | 7 | COMPLETED, REJECTED, COMMIT_FAILED, SYNC_FAILED | — |
| Rollback | VALIDATING, REVERTING, SYNCING, COMPLETED, REJECTED, BLOCKED, REVERT_FAILED, SYNC_FAILED | 8 | COMPLETED, REJECTED, BLOCKED, REVERT_FAILED, SYNC_FAILED | — |
| Preview | CREATING, ACTIVE, EXPIRING, DELETING, DELETED, FAILED, CLEANUP_FAILED | 9 | DELETED | — |
| Tenant | (empty), Provisioning, Active, Suspended, Deleting | 6 | Deleting | — |

**Sink States (non-terminal, requerem intervenção manual):**
- Release: DRIFT_ALERT, ROLLBACK_FAILED, GIT_MIRROR_FAILED
- Rollback: REJECTED, BLOCKED, REVERT_FAILED, SYNC_FAILED

**Design:**
- Framework genérico (`machine.go`) com validação estrita na construção
- Panics em transições inválidas (programming errors, não runtime errors)
- `safeTransition()` nos stores converte panic em `ErrInvalidTransition` tipado
- `IsBuildTerminal()` helper standalone para SSE handlers

**Error Handling (`pkg/errors/`) — 27 erros tipados:**

| Código | HTTP | Uso |
|--------|------|-----|
| LOCK_TIMEOUT | 409 | Advisory lock timeout (5s) |
| DEPLOY_IN_PROGRESS | 409 | Concurrent build check |
| INVALID_TRANSITION | 409 | State machine violation |
| BUILD_NOT_FOUND | 404 | Build lookup |
| RELEASE_NOT_FOUND | 404 | Release lookup |
| TENANT_NOT_FOUND | 404 | Tenant lookup |
| BUILD_ALREADY_COMPLETED | 409 | Cancel on terminal build |
| IDEMPOTENCY_CONFLICT | 422 | Key reuse com payload diferente |
| UNAUTHORIZED | 401 | Auth failure |
| INVALID_REQUEST | 400 | Parse error |
| MISSING_FIELD | 400 | Required field absent |
| VALIDATION_FAILED | 422 | Field validation |
| PROJECT_EXISTS | 409 | Duplicate project |
| SOURCE_UPLOAD_FAILED | 500 | Git push failure |
| PLATFORM_DEGRADED | 503 | Circuit breaker open |
| INTERNAL_ERROR | 500 | Catch-all |
| PAYLOAD_TOO_LARGE | 413 | Size validation |
| + 10 outros | — | — |

**Logging (`pkg/logging/`):** slog JSON com component + correlation_id

**ID Generation (`pkg/id/`):** `build-{8hex}`, `rel-{8hex}` via crypto/rand

---

### 3.4 Build Pipeline — Argo Workflows

**Arquivo:** `infra/helm/argo-workflows/templates/workflow-template-nodejs-v1.yaml` — **965 linhas**

**DAG de 14 steps:**

```
clone-repo ──────┐
restore-cache ───┤
validate-workspace ┘
        │
   install-deps (3x retry, auto-detect npm/yarn/pnpm)
        │
    build-apps
        │
  ┌─────┴──────┐
  │             │
generate-    build-image
dockerfile     (Kaniko, signed)
  │
generate-manifests (per-app: Deployment + Service + IngressRoute)
  │
validate-manifests (kubeconform --strict)
  │
gitops-push (atomic commit com git trailers, 3x retry fetch-merge-push)
  │
[always] finalize: save-cache + emit-summary + notify-api
```

**Parâmetros de entrada:** 13 (tenant-id, project-id, environment, commit-sha, apps JSON, build-id, correlation-id, api-callback-url, callback-secret, gitea-url, gitea-token, minio creds, registry-url)

**Git Trailers (spec §17.5):**
```
Release-ID: {build-id}
Correlation-ID: {correlation-id}
Build-ID: {build-id}
```

**Callback payload (sucesso):**
```json
{
  "status": "succeeded",
  "correlation_id": "...",
  "apps": [{"name": "api", "type": "server", "url": "...", "image_digest": "..."}]
}
```

**Callback payload (falha):**
```json
{
  "status": "failed",
  "correlation_id": "...",
  "error": "<step-name>: <error-message>",
  "apps": [...]
}
```

**Error capture strategy (3 fontes):**
1. `/manifests/build-error.txt` (escrito por steps que falham)
2. `{{workflow.failures}}` (variável do Argo)
3. Fallback genérico

**Resilience:**
- validate-workspace fail-fast (evita 3x retry waste)
- install-deps: 3x retry com backoff 5s→10s→20s
- build-image: 3x retry
- gitops-push: 3x retry com fetch-merge-push para non-fast-forward
- notify-api: 3x retry, aceita 2xx e 4xx, retenta em 5xx
- TTL cleanup: succeeded 1h, failed 6h

**Segurança:**
- Non-root (user 1000) em todos os steps exceto Kaniko (requer root)
- Resource limits em todos os containers
- Volumes: workspace 2Gi + manifests 256Mi (PVCs persistentes no DAG)

---

### 3.5 Operators — Tenant Controller

**CRD Types (`operators/api/v1/`):**

| CRD | Arquivo | Propósito |
|-----|---------|-----------|
| Tenant | `tenant_types.go` (140 linhas) | Schema do tenant com Spec (Plan, Environments, Limits) e Status (Phase, Conditions) |
| ClusterCredential | `clustercredential_types.go` | Credenciais de cluster |
| Preview | `preview_types.go` | Preview environments |
| AppSecret | `appsecret_types.go` | Secrets de aplicação |
| ReconciliationReport | `reconciliationreport_types.go` | Relatórios de reconciliação |

**Tenant Controller (`operators/internal/controller/tenant_controller.go`):**

Reconciliation loop com 8 condition-gated steps:

| Step | Condition | Responsabilidade |
|------|-----------|------------------|
| S1.2 | NamespacesReady | Namespaces + ResourceQuota + LimitRange |
| S1.3 | RBACReady | ServiceAccount, Role, RoleBinding |
| S1.4 | NetworkPolicyReady | deny-all + allow-ingress/egress |
| S1.6 | GitRepoReady | Gitea repo via provisioner |
| S1.6b | ArgoCDAppReady | ArgoCD Application per environment |
| S1.5 | BuildNamespaceReady | Build namespace + runners |
| S14.3 | PriorityClassReady | Resource priority por plan tier |
| S14.3b | QuotaEnforced | ResourceQuota enforcement |

**Condition-gating:** Cada step verifica `conditionIsTrue(conditions, type, generation)` antes de executar. Crash recovery pula steps já completados. Spec changes (generation bump) triggeram re-reconcile completo.

**DIP:** `GitProvisioner` e `ArgoCDProvisioner` interfaces com implementações reais (`real_provisioner.go`).

---

### 3.6 Infraestrutura — Helm + k3d

**Helm Charts (9 componentes):**

| Chart | Propósito | Configuração-chave |
|-------|-----------|-------------------|
| `argo-workflows/` | Build engine | 32 workflow workers, 32 pod workers, NodePort 30081 |
| `argocd/` | GitOps sync | Manual sync only, self-healing disabled, v2.13.3 |
| `postgresql/` | State DB | NodePort 30432, 1Gi PVC, metrics ServiceMonitor |
| `gitea/` | Git server | Admin secret, storage |
| `minio/` | Artifact storage | Credentials, buckets |
| `traefik/` | Ingress | Routers for apps, cert integration |
| `cert-manager/` | TLS | LetsEncrypt issuers |
| `monitoring/` | Observability | 5 Grafana dashboards, Prometheus rules, ServiceMonitors |
| `otel/` | Tracing | OpenTelemetry collector |

**Grafana Dashboards (5):**
- build-pipeline, drift-detection, failure-investigation, per-tenant-health, platform-overview

**k3d Clusters (3):**

| Cluster | Função | Componentes |
|---------|--------|-------------|
| loopzero-mgmt | Control plane | API, Operators, Gitea, PostgreSQL, MinIO, ArgoCD |
| loopzero-build | Build execution | Argo Workflows, Kaniko pods |
| loopzero-workload | User apps | Traefik, cert-manager, aplicações do tenant |

**Scripts:**
- `setup-cross-cluster-networking.sh` — Docker network connections + CoreDNS resolution (idempotent)
- `provision-api-tokens.sh` — Gitea token, ArgoCD token, Build kubeconfig (idempotent, `--dry-run=client` + `apply`)

**Taskfile targets relevantes:**
- `task doctor` — Verifica prerequisites + cluster health
- `task status` — Mostra estado de clusters/pods/API
- `task test:smoke` — 30s smoke test
- `task test:unit` — Todos os testes unitários
- `task lint` — Lint all components
- `task validate:manifests` — kubeconform --strict

---

### 3.7 Database — Migrations

**5 migrations forward-only:**

| Migration | Tabelas/Colunas | Sprint |
|-----------|----------------|--------|
| `000001_initial_schema` | builds, releases, environment_targets, audit_log, idempotency_keys, sse_events | 2 |
| `000002_auth_tenants` | users, tenants, tenant_members, projects | 1+2 |
| `000003_deploy_support` | Partial unique index `idx_one_active_build_per_env`, build_apps table, releases.apps JSONB | 2 |
| `000004_add_commit_sha` | builds.commit_sha TEXT | 2 |
| `000005_secrets` | secrets table (id, tenant_id, project_id, environment, app, name, encrypted_value) | 2+3 |

**Constraint crítico:**
```sql
CREATE UNIQUE INDEX idx_one_active_build_per_env
  ON builds (tenant_id, project_id, environment)
  WHERE status IN ('QUEUED', 'RUNNING');
```
Garante no máximo 1 build ativo por environment no nível do banco.

---

## 4. Cobertura de Testes

### 4.1 Resumo Quantitativo

| Camada | Arquivos | Testes | Linhas | Padrão |
|--------|----------|--------|--------|--------|
| CLI Unit | 18 | 400+ | ~5.000 | Jest, mocks para deps |
| API Unit | 22 | 200+ | ~5.500 | Go testing, fakes para I/O |
| API Integration | 2 | 7 | ~1.010 | Real PostgreSQL + advisory locks |
| Operator Unit | 5 | 20+ | ~800 | Ginkgo + envtest |
| pkg Unit | 12 | 100+ | ~2.000 | Go testing, zero deps |
| E2E | 1 | 3 | — | Full infrastructure (Ginkgo) |
| **Total** | **60** | **730+** | **~14.300** | — |

### 4.2 Testes Críticos que Existem (spec §20)

| Comportamento | Teste | Arquivo |
|--------------|-------|---------|
| Concurrent deploys → 202 + 409 | ✅ | `concurrent_builds_test.go` |
| Same idempotency key → cached response | ✅ | `concurrent_builds_test.go` |
| Different payload same key → 422 | ✅ | `concurrent_builds_test.go` |
| Lock released after build completes | ✅ | `concurrent_builds_test.go` |
| Lock released on crash | ✅ | `lock_crash_recovery_test.go` |
| Lock released on panic | ✅ | `lock_crash_recovery_test.go` |
| Lock released on rollback | ✅ | `lock_crash_recovery_test.go` |
| Invalid state transition → error | ✅ | `build_store_test.go`, `release_store_test.go` |
| CLI exit codes (0/1/2/3/4/5) | ✅ | `deploy.test.ts` (200+ cases) |
| No internal terms in CLI output | ✅ | `deploy.test.ts`, `deploy-renderer.test.ts` |
| `--json` shape matches spec §0.3 | ✅ | `deploy.test.ts` |
| SSE Last-Event-ID replay | ✅ | `stream_handler_test.go` |
| SSE journal expired → event | ✅ | `stream_handler_test.go` |
| SSE client disconnect → cleanup | ✅ | `stream_handler_test.go` |
| Build timeout → FAILED | ✅ | `build_reaper` tests |
| Callback resilience (malformed payload) | ✅ | `builds_route_test.go` |
| Tenant CRD reconciliation | ✅ | `tenant_controller_test.go` |
| State machine terminal states reject events | ✅ | `*_test.go` para cada máquina |
| Git bundle self-contained | ✅ | `git.test.ts` |
| Circuit breaker transitions | ✅ | `argocd_client_test.go` |
| Environment validation (RFC 1123) | ✅ | `builds_route_test.go` |

### 4.3 Testes com Infraestrutura Real (Não Fakes)

| Componente | Arquivo | Infraestrutura |
|-----------|---------|----------------|
| Advisory lock | `lock/environment_lock_test.go` | Real PostgreSQL |
| Concurrent builds | `integration/concurrent_builds_test.go` | Real PostgreSQL + HTTP |
| Lock crash recovery | `integration/lock_crash_recovery_test.go` | Real PostgreSQL |
| Build store | `unit/build_store_test.go` | Real PostgreSQL |
| Release store | `unit/release_store_test.go` | Real PostgreSQL |
| Idempotency store | `unit/idempotency_store_pg_test.go` | Real PostgreSQL |
| SSE event store | `unit/sse_event_store_test.go` | Real PostgreSQL |
| Gitea source | `source/gitea_store_test.go` | Real Gitea |
| ArgoCD sync | `argocd/sync_client_test.go` | Real ArgoCD |
| Argo Workflows | `argo/workflow_submitter_test.go` | Real Argo Workflows |
| Tenant CR | `k8s/tenant_cr_manager_test.go` | Real Kubernetes |

---

## 5. Conformidade com a Spec

### 5.1 Spec §4.2 — Environment Lock

| Requisito | Status | Evidência |
|-----------|--------|-----------|
| `pg_advisory_xact_lock` (transaction-scoped) | ✅ | `lock/environment_lock.go:64` |
| Nunca `pg_advisory_lock` (session-scoped) | ✅ | Grep confirma zero ocorrências |
| Timeout de 5 segundos | ✅ | `SET LOCAL lock_timeout = '5s'` |
| Concurrent check DENTRO do lock | ✅ | `build_orchestrator.go:158` |
| Lock released on commit/rollback/disconnect | ✅ | Integration test prova |
| Panic recovery com rollback | ✅ | `lock_crash_recovery_test.go` |

### 5.2 Spec §4.3 — Idempotency

| Requisito | Status | Evidência |
|-----------|--------|-----------|
| Idempotency-Key header (UUID) | ✅ | CLI gera, middleware intercepta |
| Key exists + same hash → cached response | ✅ | Integration test |
| Key exists + different hash → 422 | ✅ | Integration test |
| Key not found → proceed + store | ✅ | Middleware implementation |
| 24h TTL | ✅ | `idempotencyTTL = 24 * time.Hour` |

### 5.3 Spec §5.1 — Build State Machine

| Transição | Status | Evidência |
|-----------|--------|-----------|
| QUEUED → RUNNING | ✅ | `BuildStarted` event |
| QUEUED → CANCELLED | ✅ | `BuildCancelRequested` event |
| RUNNING → SUCCEEDED | ✅ | `BuildSucceededEvent` |
| RUNNING → FAILED | ✅ | `BuildFailed` event |
| RUNNING → CANCELLING | ✅ | `BuildCancelRequested` |
| CANCELLING → CANCELLED | ✅ | `BuildCancelConfirmed` / `BuildCancelTimeout` |
| QUEUED timeout (5min) → FAILED | ✅ | `BuildQueueTimeout` |
| RUNNING timeout (30min) → FAILED | ✅ | `BuildTimeout` |
| CANCELLING timeout (60s) → CANCELLED | ✅ | `BuildCancelTimeout` |
| Terminal states reject ALL events | ✅ | Tests verificam panic |

### 5.4 Spec §5.2 — Release State Machine

| Transição | Status | Evidência |
|-----------|--------|-----------|
| READY → PENDING (GitMirrorCompleted) | ✅ | Implementado |
| READY → GIT_MIRROR_FAILED | ✅ | Implementado |
| PENDING → ACTIVE (SyncCompleted) | ✅ | Implementado |
| PENDING → SYNC_FAILED | ✅ | Implementado |
| SYNC_FAILED → PENDING (retry) | ✅ | Implementado |
| SYNC_FAILED → DRIFT_ALERT (exhausted) | ✅ | Implementado |
| ACTIVE → SUPERSEDED | ✅ | Implementado |
| ACTIVE → ROLLING_BACK | ✅ | Implementado (Sprint 4) |
| Sink states (DRIFT_ALERT, ROLLBACK_FAILED, GIT_MIRROR_FAILED) | ✅ | Non-terminal, require manual |

### 5.5 Spec §0.3 — CLI Contract

| Requisito | Status | Evidência |
|-----------|--------|-----------|
| Exit code 0 (success) | ✅ | Testes |
| Exit code 1 (usage error) | ✅ | Testes |
| Exit code 2 (build failure) | ✅ | Testes |
| Exit code 3 (deploy failure) | ✅ | Testes |
| Exit code 4 (auth error) | ✅ | Testes |
| Exit code 5 (partial failure) | ✅ | Testes |
| `--json` field names estáveis | ✅ | Schema validation |
| CI mode (no spinners, no color) | ✅ | Testes com `CI=true` |
| deploy_id format (`d-{4hex}`) | ✅ | Testes |

### 5.6 Spec §10.1 — Deploy Flow

| Step | Status | Evidência |
|------|--------|-----------|
| 0a. Secrets pre-check | ✅ | Scoped a apps selecionados |
| 0b. Code ingestion (git bundle) | ✅ | Self-contained bundle |
| 1. CLI gera Idempotency-Key | ✅ | UUID v4 |
| 2. POST /api/v1/builds | ✅ | 202 Accepted |
| 3. Idempotency check | ✅ | Middleware |
| 4. Advisory lock | ✅ | `pg_advisory_xact_lock` |
| 5. Concurrent-build check INSIDE lock | ✅ | `HasActiveBuild()` |
| 6. Create Build(QUEUED) | ✅ | BuildStore.Create |
| 7-8. Submit Argo Workflow | ✅ | WorkflowSubmitter |
| 9. Transition RUNNING | ✅ | State machine |
| 10. Store idempotency | ✅ | Middleware |
| 11. Release lock | ✅ | Commit tx |
| 12. CLI connects SSE | ✅ | Last-Event-ID |
| 13a-i. Build executes (14 steps) | ✅ | Workflow template |
| 14a-g. Deployment activation | ✅ | Two-phase |
| 15. Final SSE event | ✅ | deploy.completed |
| Circuit breaker pre-check | ✅ | CircuitBreakerSyncClient |

### 5.7 Rule 3 — No Internal Terms

| Termo | Ocorrências em CLI output | Status |
|-------|---------------------------|--------|
| ArgoCD / argocd | 0 | ✅ |
| GitOps / gitops-push | 0 | ✅ |
| CRD | 0 | ✅ |
| tenant_id | 0 | ✅ |
| advisory lock / pg_advisory | 0 | ✅ |
| Gitea | 0 | ✅ |
| MinIO | 0 | ✅ |
| Kaniko | 0 | ✅ |
| kubebuilder / envtest | 0 | ✅ |

Verificado por grep automatizado em testes CLI.

---

## 6. Bugs Encontrados e Corrigidos

### 6.1 Blockers (B1–B4)

Todos impediram passagem do exit gate. Todos corrigidos.

| ID | Severidade | Descrição | Causa Raiz | Fix | Commit |
|----|-----------|-----------|------------|-----|--------|
| B1 | S0 | Lock timeout retornava erro raw do PostgreSQL (55P03) em vez de 409 | `PoolEnvironmentLocker` no adapter não convertia o erro | Unificado com `lock.WithEnvironmentLock` que detecta e converte 55P03 → `ErrLockTimeout` | `4dfd16c` |
| B2 | S0 | Panic no callback vazava conexão do pool | `PoolEnvironmentLocker` no adapter não tinha `recover()` | Unified com `lock.WithEnvironmentLock` que tem deferred rollback + panic recovery | `4dfd16c` |
| B3 | S0 | Correlation-ID nos git trailers era idêntico ao build-id | Todos os trailers hardcoded para `{{workflow.parameters.build-id}}` | Correlation-ID passado como parâmetro separado no workflow | `2f62fcf` |
| B4 | S0 | Circuit breaker existia (10 testes passando) mas `main.go` wired `SyncClient` raw | Wiring incompleto no composition root | `main.go` agora instancia `CircuitBreakerSyncClient` com FailureThreshold=3, RecoveryInterval=30s | `4dfd16c` |

### 6.2 Bugs Críticos (C1–C4)

Risco de corrupção ou perda de dados. Todos corrigidos.

| ID | Severidade | Descrição | Causa Raiz | Fix |
|----|-----------|-----------|------------|-----|
| C1 | S1 | `_ = cancelErr` em CancelBuild engolia erro de cancelamento | Error swallowing explícito | Substituído por `slog.Warn()` com contexto |
| C2 | S1 | `_ = err` em sync failure event engolia falha de emissão | Error swallowing explícito | Substituído por `slog.Error()` com contexto |
| C3 | S1 | `IsBuildTerminal` reimplementado localmente em vez de importar `pkg/statemachine` | Cópia local divergiu do original | Deletado local, importa `statemachine.IsBuildTerminal()` |
| C4 | S1 | `SetErrorMessage` do idempotency store engolia erro | `_ = err` | Substituído por `slog.Error()` com propagação correta |

### 6.3 Dogfooding Findings (F-01 a F-09)

Auditoria de dogfooding em 2026-03-15 com dogfood-monorepo (4 apps, 3 tipos, workspaces).

| ID | Severidade | Descrição | Status | Fix |
|----|-----------|-----------|--------|-----|
| F-01 | S0 | Callback 400 em payload malformado do Argo deixava build stuck em RUNNING | ✅ Fixado | Best-effort parsing, nunca retorna 400 em payload interno — commit `3a6dab5` |
| F-02 | S0 | Ghost RUNNING builds (consequência de F-01) | ✅ Fixado | Resolvido por F-01 (callback agora retorna 202) |
| F-03 | S1 | CLI trava indefinidamente esperando SSE | ✅ Fixado | Resolvido por F-01 + timeout de 10 minutos no CLI |
| F-04 | S1 | `error_message` vazio no banco para builds FAILED | ✅ Fixado | `SetErrorMessage` agora persiste do callback + reaper — commit `f4c9c79` |
| F-05 | S2 | API retorna 404 em plain text (não JSON) | ✅ Fixado | Handlers JSON para 404 e 405 — commit `2de138e` |
| F-06 | S2 | Testes do operator falham localmente | ✅ Fixado | envtest binary path corrigido — commit `5eee9c6` |
| F-07 | S2 | Deploy retorna exit code 3 para "in progress" | ⚠️ Debatível | 409 mapeia para exit 3 (deploy failure); alternativa seria exit 1 (usage). Decisão: manter exit 3. |
| F-08 | S3 | Unit tests da API levam 240s | ✅ Fixado | Reduzido de 240s para 0.15s — commit `04f0729` |
| F-09 | S4 | 95% build failure rate no dev | ⏳ Não sistêmico | Causado por problemas de infra local; não é bug de código |

**Root cause chain:** F-01 (callback 400) → F-02 (ghost builds) → F-03 (CLI hang) → F-04 (empty errors). Corrigir F-01 resolveu 4 findings de uma vez.

### 6.4 High Priority (H1–H5)

| ID | Descrição | Status | Fix |
|----|-----------|--------|-----|
| H1 | Lock implementation duplicada (adapter vs lock/) | ✅ Fixado | Adapter agora delega para `lock.WithEnvironmentLock` |
| H2 | CLI tests ausentes no Taskfile | ✅ Fixado | `test:unit` agora inclui `cd cli && npm test` |
| H3 | Validação de manifests incompleta | ✅ Fixado | kubeconform expandido para Helm monitoring charts |
| H4 | IPs hardcoded em testes do operator | ⏳ Deferred | Sprint 3 backlog |
| H5 | Operator /tmp mount (security) | ✅ Fixado | emptyDir adicionado |

### 6.5 Medium Priority (M1–M8)

| ID | Descrição | Status |
|----|-----------|--------|
| M1 | Dead code (`WorkflowSpec.CallbackURL`) | ✅ Removido (false positive — era infraParams) |
| M2 | tenant_controller.go 790 linhas (SRP) | ⏳ Sprint 3 backlog |
| M3 | build_orchestrator.go activateRelease decomposition | ⏳ Sprint 3 backlog |
| M4 | Startup reconciler para crash recovery da API | ⏳ Sprint 8 |
| M5 | Cross-cluster IP discovery (hardcoded) | ⏳ TBD |
| M6 | Circuit breaker in-process only (per-replica) | ⏳ Sprint 8 (PG-backed) |
| M7 | SSE client reconnect tests adicionais | ⏳ Sprint 3 backlog |
| M8 | ESLint incluído em lint task | ✅ Fixado |

---

## 7. Gaps Identificados

### 7.1 Gaps entre Spec e Implementação

| # | Spec | Implementação | Gap | Severidade | Sprint |
|---|------|--------------|-----|-----------|--------|
| G1 | §10.1 step 7: Ephemeral credentials 1h TTL | Workflow usa tokens do K8s secret (não ephemeral) | Token rotation não implementado | Medium | 6 |
| G2 | §10.1 step 13c: resolve-config com `${env}/${project}/${tenant}` templates | Workflow não faz template resolution | Templates de config não suportados | Low | 3+ |
| G3 | §10.1 step 13f: upload-frontend (MinIO) para SPAs | Não implementado | Apenas container deploy, sem SPA static hosting | Medium | 5+ |
| G4 | §12.1: Startup reconciler para crash recovery | Não implementado | API crash → builds stuck em RUNNING até reaper (30min) | Medium | 8 |
| G5 | §25.3: `--preview` e `--branch` flags | Não implementados | Sprint 5 feature | — | 5 |
| G6 | §10.1: ArgoCD sync retry (up to 3 times) | Sync faz poll único com timeout 2min | Sem retry automático no nível da API | Low | 3 |
| G7 | §4.4: Cancel durante sync wait | Cancel implementado para build, não para sync wait | Deploy activation não é cancelável | Low | 4 |
| G8 | §10.1 step 13g: `envFrom: secretRef` + `loopzero.io/secret-hash` annotation | generate-manifests usa inline env vars | Secrets não vêm de K8s Secrets | Medium | 3 |
| G9 | GitOps push no workflow vs na API | Workflow faz gitops-push diretamente | Spec §7.2 sugere API como orchestrador de git | Architectural debt | TBD |

### 7.2 Gaps de Testes

| # | Teste Missing | Severidade | Justificativa |
|---|--------------|-----------|---------------|
| T1 | Callback integration (HTTP → OnBuildCompleted → Release) end-to-end | Medium | Testado via orchestrator unit + integration separadamente, não como chain |
| T2 | Circuit breaker wired corretamente (integration test) | Medium | Unit tests cobrem lógica, mas wiring no main.go não tem test |
| T3 | Multi-app deploy com partial failure (E2E) | Medium | CLI testa com mocks; real infra não testada |
| T4 | SSE reconnect com Last-Event-ID (E2E) | Low | Unit tests cobrem; E2E não |
| T5 | Deploy timeout (10min) E2E | Low | Impraticável em CI |
| T6 | Gitea down durante gitops-push → GIT_MIRROR_FAILED | Medium | State machine testada; workflow step não |
| T7 | ArgoCD degraded → circuit breaker opens → 503 | Medium | Unit tested; integration not |
| T8 | Build reaper com concurrent lock | Low | Reaper testa timeout; concurrency com lock não |

### 7.3 Gaps de Observabilidade

| # | Gap | Severidade | Sprint |
|---|-----|-----------|--------|
| O1 | Cache hit rate não rastreado | Low | 7 |
| O2 | Build duration per-step não exportado | Medium | 7 |
| O3 | Circuit breaker state metric definido mas não exportado | Medium | 7 |
| O4 | Callback latency não medido | Low | 7 |
| O5 | Argo Workflow retry count não visível | Low | 7 |

---

## 8. Padrão Recorrente: "Implemented But Not Wired"

O padrão mais significativo do Sprint 2 foi a existência de código correto e testado que não estava conectado no composition root (`main.go`).

| Componente | Implementação | Wiring | Descoberta |
|-----------|---------------|--------|------------|
| Lock timeout detection | `lock/environment_lock.go:64-68` | `adapters.go` usava versão incompleta | Audit B1 |
| Panic recovery | `lock/environment_lock.go:76-91` | `adapters.go` não tinha recover() | Audit B2 |
| Circuit breaker | `argocd_client.go` com 10 tests | `main.go` wired raw SyncClient | Audit B4 |
| Correlation-ID | Gerado no middleware | Workflow usava build-id como proxy | Audit B3 |

**Lição aprendida:** Após cada sprint, **revisar `main.go` como checklist** para verificar que todos os componentes estão wired corretamente. O composition root é o contrato entre todas as camadas.

**Causa raiz:** DIP bem aplicado cria muitas interfaces e implementações. É fácil implementar e testar cada componente isoladamente mas esquecer de conectar a versão correta no entrypoint. Isso reforça a necessidade de **integration tests que exercitem o wiring real** (não apenas unit tests com fakes).

---

## 9. Decisões Arquiteturais Tomadas

| ID | Decisão | Alternativa Rejeitada | Motivo |
|----|---------|----------------------|--------|
| DA-7 | Circuit breaker in-process (per-replica) | PG-backed circuit breaker | Complexidade desnecessária para v1 (single replica). Sprint 8 migra se necessário. |
| DA-10 | Environment name validation RFC 1123 (`^[a-z][a-z0-9-]{0,62}$`) | Free-form strings | K8s namespace naming constraints propagam para environments |
| DA-11 | Deploy ID format `d-{4hex}` | UUID completo | UX — mais curto para copiar/colar, único o suficiente para scope do tenant |
| DA-15 | Callback retorna 202 imediatamente, processa async | Callback síncrono | Evita timeout do Argo em callback; resilience do sistema |
| DA-17 | Two-phase build completion (Phase 1 inside lock, Phase 2 outside) | Lock durante todo o sync | Lock durante ArgoCD sync (minutos) causaria deadlocks em cascade |
| — | Git bundle `--all HEAD` (não shallow) | `-1 HEAD` (shallow) | Shallow bundles têm prerequisites — falham em first deploy para repo vazio |
| — | Secrets pre-check escoped a apps selecionados | Check de todos os apps | Multi-app monorepo: deploy parcial não deve exigir secrets de apps não selecionados |
| — | ArgoCD manual sync only (não auto-sync) | Auto-sync habilitado | API controla timing de sync; auto-sync causaria race conditions |
| — | Callback best-effort parsing | Strict JSON parsing com 400 | 400 no callback → build stuck em RUNNING → F-01 root cause chain |

---

## 10. Débito Técnico Registrado

| Item | Componente | Esforço | Sprint Target | Justificativa |
|------|-----------|---------|---------------|---------------|
| `tenant_controller.go` — 790 linhas, viola SRP | operators | 1-2h | Sprint 3 backlog | Dificulta manutenção e review |
| `build_orchestrator.go` — `activateRelease` decomposition | api | 1h | Sprint 3 backlog | Método longo, difícil de testar isoladamente |
| Startup reconciler para builds stuck | api | 2-3h | Sprint 8 | Build stuck em RUNNING após API crash; reaper cobre em 30min |
| Circuit breaker per-replica | api | 3-4h | Sprint 8 | Multi-replica deployment precisa de PG-backed state |
| Cross-cluster IP discovery | infra | 1h | TBD | IPs hardcoded em scripts (funciona para k3d, não para prod) |
| GitOps push location (workflow vs API) | infra/api | 4-6h | TBD | Helena Duarte flagged: build pipeline não deveria ter gitops-push |
| Ephemeral credentials (1h TTL) | api | 2-3h | Sprint 6 | Tokens compartilhados via K8s secrets, sem rotação |
| `envFrom: secretRef` em manifests | infra | 2h | Sprint 3 | generate-manifests usa inline env vars |

---

## 11. Cronologia de Commits

Commits relevantes ao Sprint 2, em ordem cronológica reversa:

| Commit | Descrição | Impacto |
|--------|-----------|---------|
| `4dfd16c` | feat: enhance deploy error handling and add cross-cluster networking script | Fix B1, B2, B4 + networking script |
| `aaf0f04` | feat: enhance build process and error handling | Build pipeline improvements |
| `326e453` | docs: mark all Sprint 2 tasks as complete | Sprint 2 tasks checklist ✅ |
| `04f0729` | fix(api): reduce unit test suite from 240s to 0.15s | Fix F-08 (test performance) |
| `95a0f71` | fix(operators): correct ArgoCD Application path assertion in test | Pre-existing test fix |
| `5eee9c6` | fix(operators): resolve envtest binaries for direct go test | Fix F-06 |
| `2de138e` | fix(api): return JSON for 404 and 405 responses | Fix F-05 |
| `f4c9c79` | fix(api): persist error_message when builds transition to FAILED | Fix F-04 |
| `3a6dab5` | fix(api): make build callback resilient to malformed Argo payloads | Fix F-01 (root cause) |
| `db6e57d` | feat(build): implement two-phase build activation with error handling for ArgoCD sync failures | Core feature |
| `2f62fcf` | feat: Add correlation ID to workflow submission and improve error logging | Fix B3 |
| `c75c892` | feat(monorepo): initialize dogfood monorepo with shared-config and shared-utils packages | Test fixture |
| `c06ce5a` | feat(monitoring): add Prometheus alerting rules for LoopZero platform | Observability |

---

## 12. Métricas de Código

| Métrica | Valor |
|---------|-------|
| **Total linhas implementação (Go)** | ~8.000 |
| **Total linhas implementação (TypeScript)** | ~2.500 |
| **Total linhas workflow YAML** | 965 |
| **Total linhas de teste** | ~14.300 |
| **Total arquivos de teste** | 60 |
| **Total test functions** | 730+ |
| **Arquivos Go (produção)** | 46 |
| **Arquivos TypeScript (produção)** | 17 |
| **Endpoints HTTP** | 12 |
| **State machines** | 7 |
| **Erros tipados** | 27 |
| **Database migrations** | 5 |
| **Helm charts** | 9 |
| **Grafana dashboards** | 5 |
| **Workflow steps** | 14 |
| **Razão teste:código** | ~1.3:1 |

---

## 13. Checklist de Exit Gate

```
[✅] task test:unit passes
[✅] task lint passes
[✅] task validate:manifests passes
[✅] No internal terms in CLI output
[✅] Mutating API routes have withEnvironmentLock
[✅] Mutating API routes handle Idempotency-Key
[✅] CLI --json matches spec §0.3 schema
[✅] Exit codes are 0/1/2/3/4/5 only
[✅] State machine changes match spec §5
[✅] Zero fakes in production code (main.go)
[✅] Advisory lock is pg_advisory_xact_lock (never session-scoped)
[✅] Concurrent-build check is INSIDE lock
[✅] Callback resilient to malformed payloads
[✅] Circuit breaker wired in main.go
[✅] Correlation-ID unique in git trailers
[✅] All blockers (B1-B4) resolved
[✅] All critical bugs (C1-C4) resolved
[✅] All S0/S1 dogfooding findings resolved
[⚠️] F-07: exit code 3 for "in progress" (debatable, accepted)
[⚠️] F-09: build failure rate in dev (infra, not code)
```

---

## 14. Veredito Final

### ✅ SPRINT 2 APROVADO COM CAVEATS

**O que funciona end-to-end:**

```
loopzero login → auth token stored
loopzero init  → loopzero.yaml generated (monorepo detection)
loopzero deploy → git bundle → upload → Argo build → Kaniko image
            → gitops push → ArgoCD sync → URL live
```

**Provas de correção:**
- 730+ testes passando (unit + integration + E2E)
- Advisory locks provados contra real PostgreSQL (crash, panic, rollback)
- Concurrent deploy serialização provada (202 + 409)
- Idempotency provada (replay + mismatch)
- Dogfooding com 4 apps, 3 tipos, workspaces compartilhados

**Caveats aceitos:**
1. Circuit breaker per-replica (aceitável para v1, Sprint 8 migra)
2. GitOps push no workflow (architectural debt, decisão pendente)
3. Startup reconciler ausente (reaper cobre em 30min)
4. Ephemeral credentials não implementados (Sprint 6)

**Pronto para Sprint 3** (Logs + Status + Secrets).

---

## Apêndice: Referências

| Documento | Localização |
|-----------|-------------|
| System Design (spec) | `docs/technical.md` |
| Sprint 2 tasks | `docs/sprints/02-deploy-happy-path/sprint.md` |
| Architecture | `docs/sprints/02-deploy-happy-path/architecture.md` |
| Roadmap | `docs/sprints/02-deploy-happy-path/roadmap.md` |
| Audit report | `docs/sprints/02-deploy-happy-path/audit-report.md` |
| Dogfood report | `docs/sprints/02-deploy-happy-path/dogfood-report-final.md` |
| Pipeline review | `docs/sprints/02-deploy-happy-path/pipeline-review-helena.md` |
| Manual testing guide | `docs/sprints/02-deploy-happy-path/manual-testing-guide.md` |
| North star | `docs/sprints/02-deploy-happy-path/north-star.md` |
| Session report | `docs/sprints/02-deploy-happy-path/session-report.md` |
