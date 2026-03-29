# Component Inventory — LoopZero PaaS

**Date:** 2026-03-29  
**Reviewer:** architecture-analyst  
**Phase:** 1 — Baseline  

---

## Overview

LoopZero is a CLI-first Kubernetes PaaS built across 4 planes: Developer (CLI+API), Control (operators+mgmt cluster), Build (Argo Workflows+BuildKit), and Workload (user apps via ArgoCD). The codebase spans ~75,600 lines across Go, TypeScript, and YAML, plus ~11,800 lines in an external module (loopzero-packs).

**Repository layout:**

```
loopzero/                    ← main repo (this review)
├── api/                 ← Go API server (35,542 lines total; 16,053 prod / 19,489 test)
├── cli/                 ← TypeScript CLI (13,496 lines)
├── operators/           ← Go K8s operators (14,616 lines total; 8,142 prod / 6,474 test)
├── pkg/                 ← shared Go libs (3,908 lines; 1,516 prod / 2,392 test)
├── infra/pulumi/        ← Pulumi IaC (4,651 lines TypeScript)
├── infra/helm/          ← 13 Helm charts
├── infra/cloudflare/    ← 84-line Cloudflare Worker
├── infra/tests/         ← E2E + chaos + unit infra tests
└── .github/workflows/   ← 4 GitHub Actions workflows

loopzero-packs/              ← separate git repo (~11,781 lines Go)
```

**Module graph (Go workspace `go.work`):**

```
go.work members: api, operators, pkg, infra/scripts, infra/tests
  api       → pkg (replace ../pkg), loopzero-forge (external v0.2.1)
  operators → pkg (replace ../pkg)
  pkg       → (zero external dependencies)
  loopzero-packs → separate repo (NOT in go.work; module: github.com/loopzero/loopzeropacks)
```

---

## Component Inventory Table

| ID | Name | Type | Language/Framework | Location | Size (prod lines) | Status |
|----|------|------|--------------------|----------|-------------------|--------|
| comp_cli | CLI (loopzero) | Application | TypeScript + Commander v12 | `cli/` | ~7,200 | Active |
| comp_api | API Server | Application | Go 1.25 + chi v5 + pgx v5 | `api/` | 16,053 | Active |
| comp_operators | K8s Operators | Application | Go 1.25 + kubebuilder + controller-runtime v0.23.3 | `operators/` | 8,142 | Active (3 CRDs not impl) |
| comp_pkg | Shared Go Libs | Library | Go 1.23 (stdlib only) | `pkg/` | 1,516 | Active |
| comp_forge | Build SDK | Library (external) | Go, Argo Workflows DSL | external: v0.2.1 | ~unknown | Active |
| comp_packs | Language Packs | Library (external repo) | Go 1.25 | `/home/paulo/loopzero-packs/` | 11,781 | Separate repo |
| comp_pulumi | IaC (Pulumi) | Infrastructure | TypeScript + Pulumi | `infra/pulumi/` | ~3,500 | Active |
| comp_postgres | PostgreSQL | Infrastructure | PostgreSQL 16 | `infra/helm/postgresql/` | n/a | Active |
| comp_gitea | Gitea | Infrastructure | Gitea 1.22.6 | `infra/helm/gitea/` | n/a | Active |
| comp_argocd | ArgoCD | Infrastructure | ArgoCD 2.13.3 | `infra/helm/argocd/` | n/a | Active |
| comp_argo_workflows | Argo Workflows | Infrastructure | Argo Workflows | `infra/helm/argo-workflows/` | n/a | Active |
| comp_buildkit | BuildKit | Infrastructure | BuildKit (rootless) | Build cluster (Argo pods) | n/a | Active (Kaniko removed) |
| comp_minio | MinIO / DO Spaces | Infrastructure | S3-compatible | `infra/helm/minio/` + DO | n/a | Active |
| comp_cf_worker | CF Worker | Infrastructure | JavaScript (ES module) | `infra/cloudflare/worker.mjs` | 84 lines | Active |
| comp_cf_kv | CF KV | Infrastructure | REST / Cloudflare Workers KV | `api/internal/cfkv/` | small | Active |
| comp_loki | Loki + Promtail | Infrastructure | Grafana Loki 6.24.0 | `infra/helm/loki/`, `infra/helm/promtail/` | n/a | Active |
| comp_eso | External Secrets Operator | Infrastructure | ESO 0.14.3 | `infra/helm/external-secrets/` | n/a | Active (AppSecret CRD controller missing) |
| comp_observability | Prometheus + Grafana + OTel | Infrastructure | kube-prometheus-stack 25.27.0 | `infra/helm/monitoring/`, `infra/helm/otel/` | n/a | Active |
| comp_helm_charts | Helm Charts | Infrastructure | Helm v3.14+ YAML | `infra/helm/` | n/a | 13 charts, all pinned |
| comp_ci_cd | CI/CD Pipelines | Infrastructure | GitHub Actions | `.github/workflows/` | n/a | 4 workflows |

---

## Component Detail: Application Layer

### comp_cli — CLI (loopzero)

**Entry points:** `cli/bin/loopzero`, `cli/src/commands/`  
**Commands (11):** deploy, init, login, logout, whoami, logs, rollback, secret, status, validate, project-delete  
**Key packages:**

| Package | Responsibility |
|---------|---------------|
| `src/commands/` | Command handlers (11 files, 2,184 lines total) |
| `src/api/` | HTTP + SSE client (retry, idempotency key, SSE reconnect) |
| `src/adapters/` | git operations, loopzero.yaml parsing, source upload, app-unit |
| `src/detection/` | Monorepo (Turbo/Nx/Lerna/pnpm) + framework detection |
| `src/output/` | Human/JSON/CI formatters, deploy renderer (TTY/CI modes) |
| `src/config/` | Credential store (~/.loopzero/config.json), constants |

**Dependencies:** comp_api (HTTP only — no direct DB, no K8s)  
**Test coverage:** 28 test files, 498 Jest tests  
**Largest files:** deploy.ts (613 lines), init.ts (439 lines), secret.ts (262 lines)

### comp_api — API Server

**Entry point:** `api/cmd/main.go` (481 lines — wires ALL dependencies inline)  
**HTTP router:** chi v5  
**Key packages:**

| Package | Responsibility |
|---------|---------------|
| `internal/routes/` | HTTP handlers (13 files, 2,006 lines) |
| `internal/services/` | Business logic + background loops (19 files, 4,739 lines) |
| `internal/db/` | pgx stores (12 files, 1,721 lines) |
| `internal/argo/` | Argo Workflow builder (10 files, 3,861 lines incl. tests) |
| `internal/middleware/` | auth, idempotency, correlation-id, rate-limit, audit, logging |
| `internal/lock/` | withEnvironmentLock (pg_advisory_xact_lock, 5s timeout) |
| `internal/sse/` | SSE streaming + event journal (NOTIFY/LISTEN) |
| `internal/argocd/` | ArgoCD sync client |
| `internal/gitops/` | Gitea gitops pusher |
| `internal/k8s/` | TenantCR manager, SecretK8sManager |
| `internal/adapters/` | DIP bridges (pgx↔db, services↔routes) |
| `internal/cfkv/` | Cloudflare KV client |
| `internal/loki/` | Loki query client |
| `internal/s3/` | MinIO/Spaces uploader |
| `internal/server/` | HTTP server setup + graceful shutdown (361 lines) |
| `internal/config/` | 50+ config fields with production validation |

**Background goroutines (6):** BuildReconcileLoop, BuildReaper, ReleaseReaper, ReleaseConvergenceLoop, IdempotencyCleaner, SSEEventPruner  
**Dependencies:** comp_postgres, comp_gitea, comp_argocd, comp_argo_workflows, comp_minio, comp_loki, comp_cf_kv, comp_pkg, comp_forge  
**Test coverage:** 42 Go test files (unit + integration)  
**Notable:** main.go at 481 lines is a significant wiring file; build_orchestrator.go at 736 lines is the largest service file.

### comp_operators — Kubernetes Operators

**Entry point:** `operators/cmd/`  
**CRDs defined (9):** Tenant, Environment, LoopZeroApplication, ExposedRoute, PlatformBootstrap, GitRepositoryBinding, ClusterCredential, AppSecret*, PreviewEnvironment*, ReconciliationReport*  
(*= defined but controller NOT implemented)

**Controllers wired (7):**

| Controller | File(s) | Lines |
|-----------|---------|-------|
| TenantController | tenant_controller.go + tenant_build.go + tenant_gitops.go + tenant_credentials.go | 369+385+~200+~100 |
| EnvironmentController | environment_controller.go | 532 |
| ApplicationController | application_controller.go | 251 |
| ExposureController | exposure_controller.go | ~200 |
| PlatformBootstrapController | platformbootstrap_controller.go + _gitea.go + _argocd.go + _api.go | ~400 |
| GitRepositoryBindingController | gitrepositorybinding_controller.go | 323 |
| ClusterCredentialController | clustercredential_controller.go | 301 |

**Internal packages:** controller/, conditions/, constants/, argocd/, gitea/, minio/  
**Dependencies:** comp_pkg (state machines), comp_gitea, comp_argocd, comp_minio  
**Test coverage:** Ginkgo + envtest, co-located tests. tenant_controller_test.go is 1,776 lines — largest test file.

### comp_pkg — Shared Go Libraries

**Location:** `pkg/`  
**Zero external dependencies** (stdlib only)  
**Sub-packages:**

| Package | Responsibility | Key types |
|---------|---------------|-----------|
| `errors/` | LoopZeroError + 24 typed error codes (304 lines) | LoopZeroError, ErrorCode |
| `statemachine/` | 7 state machines (147-line base + 6 domain machines) | Machine, State, Event, Transition |
| `logging/` | Structured JSON logging (slog-based) | Logger |
| `naming/` | RFC 1123 deterministic naming | Namespace, Hostname, Repo names |
| `id/` | Build/release ID generation (crypto/rand) | BuildID, ReleaseID |
| `health/` | Temporal health aggregation (anti-flapping) | HealthAggregator, Phase |

**Used by:** comp_api and comp_operators via `replace ../pkg` in go.work  
**Test ratio:** 2,392 test lines vs 1,516 prod lines (1.58x — excellent)

---

## Component Detail: Infrastructure Layer

### comp_forge — Build SDK (loopzero-forge)

**Module:** `github.com/getloopscale/loopzero-forge v0.2.1` (external, not in this repo)  
**Used in:** `api/internal/argo/` — all 6 workflow builder files import it  
**Purpose:** Programmatic Argo Workflow DSL builder — avoids raw YAML construction  
**Risk:** Pinned at v0.2.1 but external to this repo's go.work. Any breaking changes require manual go.mod update.

### comp_packs — Language Packs (loopzero-packs)

**Location:** `/home/paulo/loopzero-packs/` (separate git repo, NOT in this repo's go.work)  
**Module:** `github.com/loopzero/loopzeropacks`  
**Purpose:** Framework detection (10+ frameworks) + Dockerfile generation for all supported runtimes  
**Key finding:** loopzero-packs is NOT directly imported by the API server in the current go.work workspace. It is a separate binary (`cmd/loopzeropacks/`) that may be invoked as a subprocess or its logic is embedded differently. The examples/ directory in the main repo mirrors the examples/ in loopzero-packs, suggesting tight coupling via file copy or shared submodule.

### comp_pulumi — IaC (Pulumi)

**Entry point:** `infra/pulumi/src/index.ts`  
**3-phase orchestration:** infra (clusters, DB, DNS, storage) → bootstrap (Helm charts, Gitea setup) → expose (CF Tunnel, CF Worker, DNS records)  
**Providers:**
- DigitalOcean: fully implemented (8 files — network, kubernetes, database, dns, storage, compute, nodes, index)
- Cloudflare: tunnel.ts, static-delivery.ts, dns.ts
- AWS: stub only (index.ts with implementation guide)

**ISP contracts:** 6 interfaces in `contracts/` (network, kubernetes, storage, dns, compute, database)  
**Stacks:** dev (SFO3), staging (NYC3), prod (NYC3, loopzero.example)

### comp_postgres — PostgreSQL

**Stores (9):** build, release, project, envtarget, membership, secret, idempotency, bootstrap, circuit_breaker  
**Concurrency mechanism:** `pg_advisory_xact_lock` — transaction-scoped, releases on commit/rollback/disconnect  
**Event distribution:** `LISTEN/NOTIFY` for SSE streaming  
**Migrations:** golang-migrate, forward-only (.up.sql only), no .down.sql files

---

## CRD Status Matrix

| CRD | Controller | Wired | Sprint |
|-----|-----------|-------|--------|
| Tenant | TenantController | ✅ | H1 |
| Environment | EnvironmentController | ✅ | H1 |
| LoopZeroApplication | ApplicationController | ✅ | H1 |
| ExposedRoute | ExposureController | ✅ | H1 |
| PlatformBootstrap | PlatformBootstrapController | ✅ | H1 |
| GitRepositoryBinding | GitRepositoryBindingController | ✅ | H1 |
| ClusterCredential | ClusterCredentialController | ✅ | H1 |
| AppSecret | — | ❌ | Not started |
| PreviewEnvironment | — | ❌ | Sprint 5 |
| ReconciliationReport | — | ❌ | Not started |

---

## State Machine Registry (all in pkg/statemachine/)

| Machine | File | States | Terminal States |
|---------|------|--------|----------------|
| Build | build.go (101 lines) | QUEUED → RUNNING → RELEASING → SUCCEEDED/FAILED/CANCELLED | SUCCEEDED, FAILED, CANCELLED |
| Release | release.go (125 lines) | READY → PENDING → ACTIVE → SUPERSEDED | SUPERSEDED, ROLLED_BACK, CANCELLED |
| Tenant | tenant.go (56 lines) | Provisioning → Active → Suspended → Deleting | Deleting |
| PlatformBootstrap | platformbootstrap.go (56 lines) | Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → Complete | Complete, Failed |
| Preview | preview.go (61 lines) | Creating → Active → Expiring/Deleting → Deleted | Deleted |
| Promotion | promotion.go (~60 lines) | Validating → Committing → Syncing → Completed | Completed, Failed |
| Rollback | rollback.go (~60 lines) | Validating → Reverting → Syncing → Completed | Completed, Failed |

---

## API Endpoints Inventory

| Method | Path | Handler file | Auth |
|--------|------|-------------|------|
| POST | /api/v1/auth/login | auth.go | public |
| GET | /api/v1/auth/whoami | auth.go | required |
| POST | /api/v1/source/upload | source.go | required |
| POST | /api/v1/builds | builds.go | required + lock |
| GET | /api/v1/builds/{id} | builds.go | required |
| GET | /api/v1/builds/{id}/logs | logs.go | required |
| GET | /api/v1/builds/{id}/stream | builds.go (SSE) | required |
| GET | /api/v1/projects | projects.go | required |
| POST | /api/v1/projects | projects.go | required |
| DELETE | /api/v1/projects/{name} | projects.go | required |
| GET | /api/v1/projects/{name}/status | status.go | required |
| POST | /api/v1/projects/{name}/rollback | rollback.go | required + lock |
| POST | /api/v1/secrets | secrets.go | required + lock |
| GET | /api/v1/secrets | secrets.go | required |
| GET | /api/v1/health | health.go | public |
| GET | /api/v1/health/deep | health.go | public |
| POST | /internal/bootstrap | bootstrap.go | internal token |
| POST | /internal/argocd-callback | argocd_callback.go | callback secret |

---

## Dependency Graph Summary

```
comp_cli
  └──► comp_api (HTTP/SSE)

comp_api
  ├──► comp_postgres (pgx pool, advisory locks, LISTEN/NOTIFY)
  ├──► comp_gitea (source store, gitops push)
  ├──► comp_argocd (sync trigger)
  ├──► comp_argo_workflows (workflow submit, status poll)
  ├──► comp_minio (source bundle upload)
  ├──► comp_loki (LogQL queries)
  ├──► comp_cf_kv (static routing table write)
  ├──► comp_pkg (errors, logging, statemachine)
  └──► comp_forge (Argo Workflow builder DSL)

comp_operators
  ├──► comp_pkg (errors, logging, statemachine, naming)
  ├──► comp_gitea (repo create/archive)
  ├──► comp_argocd (AppProject, Application CRs)
  └──► comp_minio (per-tenant credentials + policies)

comp_pulumi
  ├──► (provisions) comp_postgres
  ├──► (provisions) comp_gitea
  ├──► (provisions) comp_argocd
  ├──► (provisions) comp_argo_workflows
  ├──► (provisions) comp_minio
  ├──► (provisions) comp_loki
  ├──► (provisions) comp_cf_worker
  └──► (provisions) comp_cf_kv

comp_argo_workflows
  └──► comp_buildkit (workflow steps run BuildKit)

comp_eso
  └──► (syncs secrets to) workload cluster namespaces

comp_cf_worker
  ├──► comp_cf_kv (routing table read)
  └──► comp_minio / DO Spaces (asset proxy)
```

---

## Notable Findings from Baseline Scan

### Finding 1: main.go at 481 lines — significant wiring complexity
`api/cmd/main.go` directly instantiates and wires 20+ dependencies inline. While this is a deliberate "composition root" pattern, the sheer size means any new service integration requires modifying this single file. The lack of a `wire.go` or DI container means dependency changes require careful manual threading.

### Finding 2: loopzero-packs not in go.work
The loopzero-packs module (`github.com/loopzero/loopzeropacks`) lives in a separate git repository at `/home/paulo/loopzero-packs` and is NOT included in the main repo's `go.work`. Given CLAUDE.md describes it as part of the system, this raises a question about how the API server performs framework detection and Dockerfile generation — the logic may be duplicated in `api/internal/argo/languages.go` (264 lines) rather than imported from loopzero-packs.

### Finding 3: AppSecret CRD defined but controller missing
The `AppSecret` CRD type is fully defined in `operators/api/v1/appsecret_types.go` but has no corresponding controller. This is a documented gap (Sprint status shows "not started"), but the ESO integration partially compensates via `api/internal/k8s/SecretK8sManager`.

### Finding 4: Three CRDs without controllers
PreviewEnvironment (Sprint 5), AppSecret, and ReconciliationReport are defined as types but have no reconcile loops. This creates dead schema that could mislead operators expecting functionality.

### Finding 5: Circuit breaker is in-memory, not persisted
`services/pg_circuit_breaker.go` is named with "pg" prefix but per the main.go comment, it is **in-memory per-replica** (not DB-backed). The name is misleading. This was an intentional fix (M-02) to avoid write amplification, but the file name is a relic of the previous design.

### Finding 6: build_orchestrator.go at 736 lines
The largest production service file. Orchestrates the full deploy pipeline: lock → build → manifest → gitops push → ArgoCD sync. Multiple distinct responsibilities within one file.

### Finding 7: 481-line main.go doubles as configuration validation
main.go performs production env validation, HTTP transport setup, all service instantiation, background goroutine starting, and server configuration. This is not a clean separation — it is a composition root that has grown beyond its intended scope.

---

## Summary Statistics

| Metric | Value |
|--------|-------|
| Total components mapped | 20 |
| Application components | 4 (CLI, API, Operators, pkg) |
| Infrastructure components | 16 |
| External modules (not in repo) | 2 (loopzero-forge, loopzero-packs) |
| Total production lines (Go+TS) | ~75,600 (main repo) + ~11,800 (loopzero-packs) |
| API production lines | 16,053 Go |
| Operators production lines | 8,142 Go |
| pkg production lines | 1,516 Go |
| CLI lines | ~13,500 TS |
| Test-to-prod ratio (pkg) | 1.58x (excellent) |
| Test-to-prod ratio (api) | 1.21x (good) |
| Test-to-prod ratio (operators) | 0.80x (below target) |
| CRDs defined | 10 |
| CRDs with controllers | 7 |
| CRDs without controllers | 3 |
| State machines | 7 |
| Background goroutines (API) | 6 |
| Helm charts | 13 |
| GitHub Actions workflows | 4 |
