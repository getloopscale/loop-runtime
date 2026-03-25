# Scan Completo do Projeto LoopZero

**Data:** 2026-03-24
**Objetivo:** Mapeamento completo de domínios, fluxos, gaps e estado de implementação.

---

## Visão Geral

LoopZero é um **PaaS CLI-first para Kubernetes** — o objetivo é entregar DX nível Vercel para backends: `loopzero login` → `loopzero init` → `loopzero deploy` → URL live com TLS em menos de 5 minutos, sem o usuário tocar em YAML ou kubectl.

---

## Domínios Macro (4 Planos Arquiteturais)

```
┌─────────────────────────────────────────────────────────────────┐
│  DEVELOPER PLANE                                                 │
│  CLI (TypeScript) ←→ API (Go)                                   │
│  O que o usuário vê e interage                                   │
├─────────────────────────────────────────────────────────────────┤
│  CONTROL PLANE  (mgmt cluster)                                   │
│  Operators · Gitea · PostgreSQL · MinIO · ArgoCD · Traefik       │
│  O cérebro — orquestra tudo                                      │
├─────────────────────────────────────────────────────────────────┤
│  BUILD PLANE  (build cluster)                                    │
│  Argo Workflows + Kaniko                                         │
│  Constrói imagens em isolamento                                  │
├─────────────────────────────────────────────────────────────────┤
│  WORKLOAD PLANE  (runtime cluster)                               │
│  Apps do usuário gerenciadas por ArgoCD                          │
│  Onde o código roda de verdade                                   │
└─────────────────────────────────────────────────────────────────┘
```

**Separação de blast radius:** build cluster fora do ar não afeta apps rodando; mgmt cluster fora do ar não derruba workloads ativos. O Control Plane é o "cérebro" — se PostgreSQL cai, nenhum deploy novo funciona, mas apps existentes continuam servindo.

---

## Domínios Médios (por Componente)

### 1. CLI (`cli/`) — TypeScript + Commander

| Subdomínio | O que faz | Status |
|---|---|---|
| **Auth** | login/logout/whoami via GitHub token | ✅ Completo |
| **Project Init** | Detecção de monorepo + framework, gera `loopzero.yaml` | ✅ Completo |
| **Deploy** | Bundle git + upload + SSE streaming + progress | ✅ Completo |
| **Project Delete** | Remove projeto de ambientes | ✅ Completo |
| **API Client** | HTTP com retry, idempotency-key, exit code mapping | ✅ Completo |
| **SSE Client** | Stream com reconnect, Last-Event-ID, fallback polling | ✅ Completo |
| **Output** | Formatação TTY/CI/JSON, deploy renderer | ✅ Completo |
| **Detection** | Monorepo (Turbo/Nx/Lerna/pnpm) + Framework (10 tipos) | ✅ Completo |
| **logs/status/secret** | Comandos operacionais | ❌ Sprint 3 |
| **rollback/promote** | Comandos de recovery | ❌ Sprint 4 |
| **preview** | Preview environments | ❌ Sprint 5 |

**Cobertura de testes:** 405 testes, 20 suites.

**Comandos implementados:**

| Comando | Flags | Exit Codes | JSON |
|---|---|---|---|
| `loopzero login` | `--token`, `--team`, `--json` | 0, 1, 4 | ✅ |
| `loopzero logout` | `--json` | 0 | ✅ |
| `loopzero whoami` | `--json` | 0, 4 | ✅ |
| `loopzero init` | `--yes`, `--force`, `--name`, `--json` | 0, 1, 4 | ✅ |
| `loopzero deploy` | `--env`, `--app`, `--wait`, `--no-wait`, `--json` | 0, 1, 2, 3, 4, 5 | ✅ |
| `loopzero project delete` | `--env`, `--yes`, `--json` | 0, 1, 3, 4 | ✅ |

---

### 2. API (`api/`) — Go + chi + pgx

| Subdomínio | O que faz | Status |
|---|---|---|
| **Routes** | 8 endpoints (auth, health, builds, projects, source, secrets, bootstrap, SSE stream) | ✅ Completo |
| **Middleware** | Auth, Idempotency, Correlation-ID, Rate Limit, Audit, Logging | ✅ Completo |
| **Environment Lock** | `pg_advisory_xact_lock` com 5s timeout | ✅ Completo |
| **Build Orchestrator** | Submit → Argo Workflows, concurrent-build check dentro do lock | ✅ Completo |
| **Build Reconcile Loop** | Polling de CRDs Argo (substitui callbacks HTTP) | ✅ Completo |
| **Build/Release Reapers** | Timeout enforcement (QUEUED>5m, RUNNING>30m) | ✅ Completo |
| **Circuit Breaker** | In-memory, per-replica, protege ArgoCD sync | ✅ Completo |
| **SSE Event Store** | NOTIFY/LISTEN PostgreSQL, replay com Last-Event-ID | ✅ Completo |
| **DB Stores** | Build, Release, Project, EnvTarget, Membership, Secret, Idempotency, Bootstrap | ✅ Completo |
| **External Integrations** | Argo Workflows, ArgoCD, Gitea, MinIO, GitHub, K8s | ✅ Completo |

**Endpoints:**

| Method | Path | Middleware | Descrição |
|---|---|---|---|
| GET | `/api/v1/auth/whoami` | Auth | Info do usuário autenticado |
| GET | `/api/v1/health` | — | Liveness probe |
| GET | `/api/v1/health/deep` | — | Readiness probe (DB check) |
| POST | `/api/v1/builds` | Auth, Idempotency, RateLimit | Submit build |
| GET | `/api/v1/builds/{id}` | Auth | Get build status |
| POST | `/api/v1/builds/{id}/cancel` | Auth | Cancel build |
| GET | `/api/v1/builds/{id}/stream` | Auth | SSE stream |
| POST | `/api/v1/projects` | Auth | Create project |
| DELETE | `/api/v1/projects/{name}` | Auth | Delete project |
| POST | `/api/v1/source/upload` | Auth | Upload source bundle |
| GET | `/api/v1/secrets` | Auth | List secret names |
| POST | `/internal/bootstrap` | — | Platform bootstrap (internal) |

**Cobertura de testes:** 60+ arquivos de teste unitário + integração.

---

### 3. Operators (`operators/`) — Go + kubebuilder

| Controller | CRD | O que faz | Status |
|---|---|---|---|
| **TenantController** | Tenant | Provisiona Gitea repo, ArgoCD app, build namespace, RBAC, quotas | ✅ Wired |
| **ClusterCredentialController** | ClusterCredential | Connectivity check + kubeconfig distribution | ✅ Wired |
| **EnvironmentController** | Environment | Cria namespace + RBAC + NetworkPolicy + quota no runtime cluster | ✅ Implementado, ⚠️ **NÃO wired** |
| **ApplicationController** | LoopZeroApplication | Hostname, ExposedRoute, health tracking | ✅ Implementado, ⚠️ **NÃO wired** |
| **ExposureController** | ExposedRoute | TLS cert check, reachability probes | ✅ Implementado, ⚠️ **NÃO wired** |
| **PlatformBootstrapController** | PlatformBootstrap | Setup declarativo: Gitea → ArgoCD → API seed | ✅ Implementado, ⚠️ **NÃO wired** |
| **GitRepositoryBindingController** | GitRepositoryBinding | Credential sync para ArgoCD com rotation detection | ✅ Implementado, ⚠️ **NÃO wired** |
| **PreviewEnvironmentController** | PreviewEnvironment | Ambientes efêmeros por PR | ❌ Sprint 5 |
| **AppSecretController** | AppSecret | Sync de secrets mgmt → workload | ❌ Não implementado |
| **ReconciliationReportController** | ReconciliationReport | Drift detection | ❌ Não implementado |

**CRDs definidos (9 total):**

| CRD | Phases | Conditions |
|---|---|---|
| Tenant | Provisioning → Active → Suspended → Deleting | GitRepoReady, NamespacesReady, RBACReady, QuotaEnforced, NetworkPolicyReady, BuildNamespaceReady, ArgoCDAppReady |
| Environment | Pending → Active → Deleting → Failed | NamespaceReady, RBACReady, NetworkPolicyReady, QuotaReady |
| LoopZeroApplication | Progressing → Serving → Reachable → Degraded → Down | ServiceResolved, IngressReady, CertificateIssued, RouteReachable |
| ExposedRoute | Pending → CertIssued → Reachable → Failed | CertificateReady, ProbeSuccess |
| PlatformBootstrap | Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → BootstrapComplete/Failed | GiteaReady, ArgoCDReady, PlatformSecretsReady, DatabaseSeeded |
| GitRepositoryBinding | — | CredentialsResolved, RepositoryRegistered, ConsumerHealthy |
| ClusterCredential | — | Connected |
| AppSecret | — | Synced |
| PreviewEnvironment | Creating → Active → Expiring/Deleting → Deleted/Failed | — |
| ReconciliationReport | — | DriftChecked |

---

### 4. Shared Libraries (`pkg/`) — Go puro, zero deps externas

| Pacote | O que faz | Status |
|---|---|---|
| **errors** | `LoopZeroError` tipado com 24+ códigos (LOCK_TIMEOUT, DEPLOY_IN_PROGRESS, etc.) | ✅ Completo |
| **logging** | Structured JSON via `slog`, WithCorrelationID | ✅ Completo |
| **id** | Build/release IDs com crypto/rand (`build-{16hex}`, `rel-{16hex}`) | ✅ Completo |
| **naming** | RFC 1123 naming determinístico (namespace, hostname, repo, ArgoCD app) | ✅ Completo |
| **health** | Agregação temporal com threshold anti-flapping | ✅ Completo |
| **statemachine** | 7 state machines (build, release, tenant, preview, promotion, rollback, bootstrap) | ✅ Completo |

---

### 5. Build System (`loopzero-forge/` + `loopzero-packs/`)

| Componente | O que faz | Status |
|---|---|---|
| **loopzero-forge** | SDK Go para construir Argo Workflows programaticamente (198 exemplos round-trip) | ✅ Completo |
| **loopzero-packs** | Detecção de linguagem/framework + geração de Dockerfile | ✅ Framework + Rails pack |

---

### 6. Infraestrutura (`infra/`)

| Componente | O que faz | Status |
|---|---|---|
| **Pulumi IaC** | 3-phase deploy: infra → bootstrap → expose. DigitalOcean completo, AWS stub | ✅ Completo |
| **Helm Charts** | 13 componentes (cert-manager, Traefik, ArgoCD, Gitea, MinIO, PostgreSQL, Argo Workflows, etc.) | ✅ Completo |
| **k3d configs** | 3 clusters locais (mgmt, build, workload) | ✅ Completo |
| **Tiltfile** | Dev loop com hot reload, migration, seed | ✅ Completo |
| **Taskfile** | 1159 linhas de automação (setup, test, lint, deploy, db, tokens) | ✅ Completo |
| **CWT templates** | Clone, cache-restore, cache-save, summary, resource-quota | ✅ Completo |
| **CI/CD Workflows** | 4 workflows (CI, Build Images, Infra Deploy, Infra Preview) | ✅ Completo |

---

## Fluxo Principal: Deploy Happy Path

```
loopzero deploy
    │
    ▼
[1] CLI valida auth + loopzero.yaml + git repo
    │
    ▼
[2] CLI cria git bundle + upload para API
    │  POST /api/v1/source/upload (multipart: bundle + loopzero.yaml)
    ▼
[3] API grava bundle em Gitea (source repo) + MinIO (build access)
    │
    ▼
[4] API adquire pg_advisory_xact_lock(tenant:project:env)
    │  Check concurrent build DENTRO do lock
    │  Cria Build record (QUEUED)
    │  Libera lock
    ▼
[5] API submete Workflow ao Argo Workflows (build cluster)
    │  CWT: clone → cache-restore → install-deps → build → cache-save
    ▼
[6] Build Reconcile Loop (polling) detecta conclusão
    │  Gera manifests K8s
    │  Push para Gitea (gitops repo)
    │  Cria Release record (READY → PENDING)
    ▼
[7] API triggera ArgoCD sync (circuit breaker protegido)
    │  ArgoCD aplica manifests no runtime cluster
    ▼
[8] Release → ACTIVE quando ArgoCD confirma sync
    │  SSE event: deploy.completed
    ▼
[9] CLI mostra URL final
    ✓ Deploy completed in 97s
    api → https://api.dev.loopzero.example
```

---

## State Machines

### Build (spec §5.1)
```
QUEUED ──BuildStarted──→ RUNNING ──BuildSucceeded──→ SUCCEEDED
  │                        │
  │ BuildCancelled         │ BuildFailed / BuildCancelled
  ▼                        ▼
CANCELLED               FAILED / CANCELLED / CANCELLING
```
**Timeouts:** QUEUED > 5min → FAILED | RUNNING > 30min → FAILED | CANCELLING > 60s → FAILED

### Release (spec §5.2)
```
READY ──GitMirrorCompleted──→ PENDING ──SyncCompleted──→ ACTIVE
  │                             │                          │
  │ GitMirrorFailed             │ SyncFailed               │ NewReleaseActivated
  ▼                             ▼                          ▼
GIT_MIRROR_FAILED          SYNC_FAILED               SUPERSEDED
                             │
                             │ SyncExhausted
                             ▼
                         DRIFT_ALERT
```
**Timeouts:** READY > 5min → GIT_MIRROR_FAILED | PENDING > 2min → SYNC_FAILED

### Tenant
```
"" ──Create──→ Provisioning ──AllConditionsReady──→ Active
                    │                                 │
                    │ Delete                           │ Delete / Suspend
                    ▼                                 ▼
                 Deleting                        Suspended / Deleting
```

### PlatformBootstrap
```
"" → Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → BootstrapComplete
                     │                    │                  │
                     ▼                    ▼                  ▼
                   Failed              Failed              Failed
```

### Preview Environment
```
CREATING → ACTIVE → EXPIRING/DELETING → DELETED
    │                    │
    ▼                    ▼
  FAILED           CLEANUP_FAILED → DELETED (GarbageCollected)
```

---

## Estado Atual por Sprint

| Sprint | Foco | Status | Testes |
|---|---|---|---|
| **0** Foundation | Clusters, CRDs, monitoring | ✅ **COMPLETO** | Validado 2026-03-13 |
| **1** Auth + Init | login/init/TenantController | ✅ **COMPLETO** | 247 testes, 19 integração |
| **2** Deploy | Build + Release + SSE | ⚠️ **Feature-complete** | 200+ CLI, 60+ API |
| **3** Logs/Status/Secrets | — | ❌ Não iniciado | — |
| **4** Rollback/Promote | — | ❌ Não iniciado | — |
| **5** Preview Environments | — | ❌ Não iniciado | — |
| **6** Security/Supply Chain | — | ❌ Não iniciado | — |
| **7** Observability | — | ❌ Não iniciado | — |
| **8** Hardening/Chaos | — | ❌ Não iniciado | — |
| **9** Polish/Launch | — | ❌ Não iniciado | — |

---

## Gaps Identificados

### Gaps Críticos (Bloqueadores de Produção)

| # | Gap | Impacto | Localização |
|---|---|---|---|
| **G1** | **5 controllers não wired no main.go** | Environment, Application, Exposure, PlatformBootstrap, GitRepositoryBinding existem mas não reconciliam | `operators/cmd/main.go` |
| **G2** | **TLS: 3/4 certificados falhando** no cluster DO | HTTPS quebrado para ArgoCD, Gitea, Grafana | Infra DigitalOcean |
| **G3** | **GitHub Actions billing** bloqueado | Nenhum workflow roda | Org `getloopscale` (resolvido: Pro ativado) |
| **G4** | **`docs/technical.md` não existe** | Spec autoritativa referenciada em todos os sprints nunca foi escrita como arquivo | `docs/` |

### Gaps de Padrão Sistêmico

| # | Padrão | Descrição | Ocorrências |
|---|---|---|---|
| **P1** | "Implemented but not wired" | Código correto existe mas não é integrado ao runtime | Controllers (G1), Circuit breaker (Sprint 2 B4), Build reaper (Sprint 2 C4) |
| **P2** | "Correct code in wrong place" | Duas implementações do mesmo conceito com behavior diferente | EnvironmentLocker (lock.go vs adapters.go) |
| **P3** | "Terminal state gap" | Controllers para CRDs AppSecret e ReconciliationReport nunca foram implementados | `operators/` |

### Gaps de Feature (Sprints 3-9)

| Feature | Sprint | CLI Command | Dependência |
|---|---|---|---|
| `loopzero logs` | 3 | `logs` | API endpoint + SSE |
| `loopzero status` | 3 | `status` | API endpoint |
| `loopzero inspect` | 3 | `inspect` | API endpoint |
| `loopzero secret set/delete` | 3 | `secret` | AppSecretController |
| `loopzero rollback` | 4 | `rollback` | Release state machine ROLLING_BACK |
| `loopzero promote` | 4 | `promote` | Promotion state machine |
| `loopzero build list/cancel` | 4 | `build` | API endpoint |
| `loopzero env list` | 4 | `env` | API endpoint |
| `loopzero team` | 4 | `team` | API endpoint |
| Preview environments | 5 | `preview` | PreviewEnvironmentController |
| Image signing (cosign) | 6 | — | Build pipeline CWT |
| KMS encryption | 6 | — | Secret encryption at rest |
| RBAC role-based | 6 | — | Auth middleware |
| Dashboards Grafana | 7 | — | 6 dashboards definidos no spec |
| Alerting rules | 7 | — | Prometheus alertmanager |
| Chaos tests | 8 | — | Litmus framework |
| Load test 50 concurrent | 8 | — | Load generator |
| Onboarding < 10min | 9 | — | CLI polish + docs |

---

## Infraestrutura de Produção (DigitalOcean)

| Recurso | Status | Detalhe |
|---|---|---|
| 3 clusters DOKS | ✅ Running | v1.32.13, 2 nodes cada, SFO3 |
| PostgreSQL managed | ✅ Online | v16, single node, db-s-1vcpu-2gb |
| Container Registry | ✅ Ativo | 4 repos (loopzero-api, tenant-controller, cache, user app) |
| DNS | ✅ Configurado | api/argocd/git/grafana.dev.loopzero.example + *.dev wildcard |
| loopzero-api | ✅ Running | 2 réplicas, health 200 |
| tenant-controller | ✅ Running | 1 réplica |
| ArgoCD | ✅ Synced | 1 app healthy (tn-loopzero-production) |
| Gitea | ✅ Running | 1 réplica |
| MinIO | ✅ Running | 1 réplica, standalone |
| Prometheus + Grafana | ✅ Running | kube-prometheus-stack |
| Argo Workflows | ✅ Running | Build cluster |
| TLS Certs | ⚠️ 1/4 | Apenas api.dev.loopzero.example funcionando |
| User app | ✅ Running | node-npm-json-test no runtime |

---

## Stack Tecnológica

| Componente | Tecnologia |
|---|---|
| CLI | TypeScript + Commander |
| API | Go + chi + pgx/sqlx |
| Operators | Go + kubebuilder + controller-runtime |
| Database | PostgreSQL (advisory locks para serialização) |
| Build engine | Argo Workflows + Kaniko |
| GitOps store | Gitea (desired state mirror) |
| GitOps sync | ArgoCD (cluster convergence) |
| Ingress | Traefik + cert-manager |
| Artifact store | MinIO (S3-compatible) / DO Spaces |
| Observability | Prometheus + OpenTelemetry + Grafana |
| IaC | Pulumi (TypeScript) |
| Local clusters | k3d × 3 (mgmt, build, workload) |
| Dev loop | Tilt |
| Task runner | Taskfile |
| Tests (Go) | Go testing + Ginkgo + Gomega + envtest |
| Tests (TS) | Jest |
| Migrations | golang-migrate (forward-only) |
| CI/CD | GitHub Actions (4 workflows) |

---

## Resumo Executivo

**O projeto está maduro na fundação (Sprints 0-2)** — o fluxo core `login → init → deploy → URL` funciona end-to-end com implementações reais (sem mocks/fakes), lock de concorrência, idempotency, SSE streaming, circuit breaker, e state machines estritas.

**Os gaps principais são:**

1. **Wiring** — código existe mas não está conectado (5 controllers, particularmente)
2. **Operacional** — comandos do dia-a-dia (logs, status, secrets, rollback) ainda não existem (Sprints 3-4)
3. **Infra** — TLS parcial no cluster DigitalOcean
4. **Spec** — `docs/technical.md` referenciado em todo lugar mas nunca materializado como arquivo

**Pontos fortes:**

- Arquitetura sólida com separação clara em 4 planos
- State machines rigorosas (7 máquinas, transições inválidas = panic)
- 650+ testes (CLI 405, API 60+, Operators 65+, pkg, Pulumi)
- DIP nas fronteiras (interfaces para Argo, ArgoCD, Gitea, MinIO)
- Infra como código (Pulumi 3-phase + 13 Helm charts)
- CI/CD completo (4 workflows: CI, Build, Deploy, Preview)
- Concurrency model robusto (pg_advisory_xact_lock + idempotency)
- SSE streaming com reconnect e fallback polling
