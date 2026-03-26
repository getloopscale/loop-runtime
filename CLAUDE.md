# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> This file is the contract between Claude and the LoopZero codebase. Read it entirely before touching any file.

---

## What This Project Is

LoopZero is a **CLI-first Kubernetes PaaS** for monorepo deployments. The goal is Vercel-quality DX for backends — `loopzero login`, `loopzero init`, `loopzero deploy`, and in under 5 minutes every service is live with TLS, preview environments on every PR, and rollback with one command. No Kubernetes. No infrastructure YAML.

The authoritative specification is **`docs/technical.md`**. Every state machine, failure mode, concurrency decision, CLI output format, and exit code is documented there. If this file and the system design conflict, the system design wins.

Sprint details are in **`docs/sprints/`** — one folder per sprint with tasks, exit gates, and spec references.

**Current status:** Sprints 0-2 complete (foundation + auth + deploy happy path). Hardening sprints 1-5 complete (runtime, TLS, PG HA, observability, pipeline). Hardening sprint 6 (operation) 3/4 complete: `loopzero logs` (Loki), `loopzero rollback` (3s), `loopzero secret` (ESO sync <30s) all validated on DigitalOcean. `loopzero status` pending (handler exists, route not wired). Core flow `loopzero login` → `loopzero init` → `loopzero deploy` → live URL works end-to-end. 455+ CLI tests + 60+ API test files passing. Production infrastructure running on DigitalOcean (3 DOKS clusters + Loki + Promtail + ESO).

---

## Development Commands

```bash
# Local environment
task setup               # Bootstrap k3d clusters + Helm + CRDs + tokens (idempotent)
task dev:up              # Boot k3d clusters + dependencies
tilt up                  # Start dev loop with hot reload (API, operator, migrations, seed)
task doctor              # Environment health check (tools, clusters, services, tokens)

# Tests
task test                # Run all tests
task test:unit           # Run unit tests only
task test:e2e            # Run E2E tests (Ginkgo)
task lint                # Lint all components (golangci-lint + ESLint)
task validate:manifests  # kubeconform --strict on all manifests

# Per-component
cd cli  && npm test           # CLI unit tests (Jest, 405 tests)
cd api  && go test ./...      # API tests (Go testing, 60+ files)
cd operators && go test ./... # Operator tests (Ginkgo + envtest)
cd pkg && go test ./...       # Shared library tests
cd infra/pulumi && npm test   # Pulumi IaC tests (vitest)
cd infra/tests && go test ./unit/... # Infra unit tests

# Database
task db:migrate          # Run forward-only migrations
task db:seed             # Insert dev seed data
task db:shell            # PostgreSQL shell (psql)
task db:reset            # Reset database (drop + recreate + migrate + seed)

# Infrastructure (DigitalOcean)
# Managed via GitHub Actions (infra-deploy.yml) or manual:
cd infra/pulumi && npx pulumi up --stack dev    # Deploy to DigitalOcean
cd infra/pulumi && npx pulumi preview --stack dev  # Preview changes

# CLI development
task cli:build           # Build CLI
task cli:link            # Link CLI for local testing
```

**Prerequisites:** Go 1.25+, Node.js 20+, Docker, [k3d](https://k3d.io), [Tilt](https://tilt.dev), [Task](https://taskfile.dev), [Helm](https://helm.sh) v3.14+, [kubectl](https://kubernetes.io/docs/tasks/tools/), [kubeconform](https://github.com/yannh/kubeconform), [golangci-lint](https://golangci-lint.run) v2.11+

---

## The Rules (Non-Negotiable)

### Rule 1: Tests before code
No production code without a failing test. Write the test. Run it. Confirm it fails. Then implement. This is not a guideline — it is the development process.

### Rule 2: Read the spec first
Before implementing anything, find the relevant section in `docs/technical.md`. The spec is the design. Do not invent behavior the spec doesn't describe.

### Rule 3: No internal terms in user-facing output
CLI output, error messages, and `--json` responses must never contain:
`ArgoCD`, `argocd`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`

Use product language: deploy, team, account, service, revert, build.

### Rule 4: Exit codes are permanent
`0` success · `1` usage error · `2` build failure · `3` deploy failure · `4` auth error · `5` partial failure. These never change. No other codes.

### Rule 5: `--json` output is a stability contract
Field names cannot change without a major version bump. New fields can be added. See spec §0.3 for required schemas.

### Rule 6: Every mutation uses the environment lock
Any operation that mutates `(tenant, project, environment)` state MUST use `withEnvironmentLock`. Never bypass it. Never perform the concurrent-build check OUTSIDE the lock (race condition).

### Rule 7: `pg_advisory_xact_lock` only
Never use `pg_advisory_lock` (session-scoped). It breaks with connection pools. The transaction-scoped variant releases automatically on commit/rollback/disconnect. See spec §4.2.

### Rule 8: State machines are strict
Transitions go through the state machine functions. Direct status updates are forbidden. Invalid transitions panic — they are programming errors, not runtime errors.

### Rule 9: CLI is the primary interface
Every feature lives in the CLI before the dashboard. A feature that requires the dashboard but has no CLI equivalent is a product bug.

### Rule 10: Local equals production
Same Helm charts, same operators, same build pipeline. No Docker Compose shortcuts. No "development mode" with different behavior.

---

## Stack

### Application

| Component | Technology | Module |
|-----------|-----------|--------|
| CLI | TypeScript + Commander | `cli/` · `@loopzero/cli` |
| API | Go 1.25 + chi + pgx (connection pool) | `api/` · `loopzero/api` |
| Operators | Go 1.25 + kubebuilder + controller-runtime | `operators/` |
| Shared libraries | Go (errors, logging, id, naming, health, statemachine) | `pkg/` |
| Build SDK | Go — programmatic Argo Workflow builder | `loopzero-forge/` |
| Language packs | Go — framework detection + Dockerfile generation | `loopzero-packs/` |

### Infrastructure

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Database | PostgreSQL 16 (managed DO) | State, advisory locks for serialization |
| Build engine | Argo Workflows + BuildKit | Container image builds in isolation (rootless, sidecar pattern) |
| GitOps store | Gitea 1.22.6 | Desired state mirror (source + gitops repos) |
| GitOps sync | ArgoCD 2.13.3 | Cluster convergence (manual sync only) |
| Ingress | Traefik 28.3.0 + cert-manager 1.14.5 | TLS termination, HTTP→HTTPS redirect |
| Artifact store | MinIO (S3-compatible) / DO Spaces | Source bundles, build cache |
| Container registry | DigitalOcean Container Registry | Container image storage |
| Observability | Prometheus 25.27.0 + Grafana 8.5.2 + OpenTelemetry | Metrics, dashboards, tracing |
| Log aggregation | Grafana Loki 6.24.0 + Promtail 6.16.6 | Build log storage (build cluster), queried by API via LogQL |
| Secret sync | External Secrets Operator 0.14.3 | Cross-cluster secret sync mgmt → workload via ClusterSecretStore |
| DNS | DigitalOcean DNS | A records, wildcard for user apps |

### Development & Operations

| Component | Technology | Purpose |
|-----------|-----------|---------|
| IaC | Pulumi (TypeScript) + @pulumi/digitalocean + @pulumi/kubernetes | 3-phase infra provisioning (infra → bootstrap → expose) |
| Cloud provider | DigitalOcean (SFO3) — AWS stub ready | 3 DOKS clusters + managed PG + Spaces + DNS |
| Local clusters | k3d × 3 (mgmt, build, workload) | Local dev parity with production |
| Dev loop | Tilt | Hot reload, migration, CRD deploy, seed data |
| Task runner | Taskfile (1159 lines) | Idempotent automation (setup, test, lint, deploy) |
| Helm charts | 13 charts (pinned versions) | cert-manager, Traefik, ArgoCD, Gitea, MinIO, PostgreSQL, Argo Workflows, monitoring, network-policies, otel |
| CI/CD | GitHub Actions (4 workflows) | CI (lint+test+security), Build Images, Infra Deploy, Infra Preview |
| Linting (Go) | golangci-lint v2.11.3 (errcheck, govet, staticcheck, etc.) | Code quality |
| Linting (TS) | ESLint + TypeScript strict mode | Code quality |
| Security scanning | govulncheck + npm audit + Trivy (fs + IaC + Dockerfile) + TruffleHog | Vulnerability + secret detection |
| Manifest validation | kubeconform --strict | K8s YAML correctness |
| Tests (Go) | Go testing + Ginkgo v2 + Gomega + envtest (K8s 1.31.0) | Unit, integration, operator tests |
| Tests (TS) | Jest | CLI unit tests (405 tests) |
| Migrations | golang-migrate (forward-only, .up.sql only) | Database schema evolution |
| Image signing | cosign + syft (Sprint 6 — not yet implemented) | Supply chain security |
| CLI tools | doctl, gh (GitHub CLI) | Cloud + CI interaction |

---

## Repository Structure

```
loopzero/
├── CLAUDE.md                          ← you are here
├── go.work                            ← Go workspace (modules: api, operators, pkg, infra/scripts, infra/tests)
├── Taskfile.yml                       ← task runner (1159 lines of automation)
├── Tiltfile                           ← dev loop orchestrator
├── docs/
│   ├── sprints/                       ← sprint details (one folder per sprint)
│   │   ├── README.md                  ← sprint index + overview table
│   │   ├── 00-foundation/
│   │   ├── 01-login-init-tenant-controller/
│   │   ├── 02-deploy-happy-path/
│   │   ├── 03-logs-status-secrets/
│   │   ├── 04-rollback-promote-cancel/
│   │   ├── 05-preview-environments/
│   │   ├── 06-security-supply-chain/
│   │   ├── 07-observability/
│   │   ├── 08-hardening-chaos/
│   │   └── 09-polish-launch/
│   └── project-scan-2026-03-24.md     ← full project scan (domains, gaps, flows)
├── pkg/                               ← shared Go module (zero external deps)
│   ├── errors/                        ← LoopZeroError + 24 typed error codes
│   ├── logging/                       ← structured JSON logging (slog)
│   ├── id/                            ← build/release ID generation (crypto/rand)
│   ├── naming/                        ← RFC 1123 deterministic naming (namespace, hostname, repo)
│   ├── health/                        ← temporal health aggregation (anti-flapping)
│   └── statemachine/                  ← 7 state machines (build, release, tenant, preview, promotion, rollback, bootstrap)
├── cli/                               ← @loopzero/cli (TypeScript + Commander)
│   ├── src/
│   │   ├── commands/                  ← login, logout, whoami, init, deploy, project-delete
│   │   ├── api/                       ← API client (retry, idempotency-key) + SSE client (reconnect, fallback polling)
│   │   ├── output/                    ← formatters: human/json/CI + deploy renderer (TTY/CI modes)
│   │   ├── detection/                 ← monorepo detection (Turbo/Nx/Lerna/pnpm) + framework detection (10 frameworks)
│   │   ├── adapters/                  ← git operations, loopzero.yaml parsing, source upload, bundle cleanup
│   │   └── config/                    ← credential store (~/.loopzero/config.json), constants, env overrides
│   └── tests/unit/                    ← 405 tests, 20 suites (Jest)
├── api/                               ← loopzero/api (Go + chi + pgx)
│   ├── cmd/                           ← entrypoint (server bootstrap, background services)
│   ├── internal/
│   │   ├── routes/                    ← auth, health, builds, projects, source, secrets, bootstrap
│   │   ├── services/                  ← BuildOrchestrator, BuildReconcileLoop, BuildReaper, ReleaseReaper, ProjectDeleter, CircuitBreaker
│   │   ├── db/                        ← pgx pool + 9 stores (build, release, project, envtarget, membership, secret, idempotency, bootstrap, event)
│   │   ├── middleware/                ← auth (GitHub token + cache), idempotency, correlation-id, rate-limit, audit, logging
│   │   ├── lock/                      ← withEnvironmentLock (pg_advisory_xact_lock, 5s timeout)
│   │   ├── sse/                       ← SSE streaming + event journal (NOTIFY/LISTEN)
│   │   ├── correlation/               ← correlation ID generation (UUID v4)
│   │   ├── argo/                      ← Argo Workflows client (submit, cancel, get status)
│   │   ├── argocd/                    ← ArgoCD sync client (trigger, wait)
│   │   ├── gitops/                    ← Gitea gitops pusher (manifest commit with trailers)
│   │   ├── source/                    ← Gitea source store (bundle push)
│   │   ├── s3/                        ← MinIO/Spaces uploader (source bundles)
│   │   ├── k8s/                       ← Tenant CR manager (mgmt cluster)
│   │   ├── github/                    ← GitHub token validator
│   │   ├── manifests/                 ← K8s manifest generation
│   │   ├── adapters/                  ← DIP bridges (pgx↔db, services↔routes)
│   │   ├── validation/                ← path validation (SEC-3), environment name validation
│   │   ├── metrics/                   ← Prometheus metrics
│   │   ├── health/                    ← health check endpoints
│   │   ├── config/                    ← 50+ config fields with production validation
│   │   └── server/                    ← HTTP server setup + graceful shutdown
│   └── tests/                         ← unit/ (60+ files) + integration/ (concurrent builds, lock crash)
├── operators/                         ← Go operators (kubebuilder + controller-runtime)
│   ├── api/v1/                        ← 9 CRD types (Tenant, Environment, LoopZeroApplication, ExposedRoute, PlatformBootstrap, GitRepositoryBinding, ClusterCredential, AppSecret, PreviewEnvironment, ReconciliationReport)
│   ├── internal/
│   │   ├── controller/                ← 7 controllers (Tenant, Environment, Application, Exposure, PlatformBootstrap, GitRepositoryBinding, ClusterCredential)
│   │   ├── conditions/                ← typed condition constants (no freeform strings)
│   │   ├── naming/                    ← K8s namespace naming (delegates to pkg/naming)
│   │   ├── argocd/                    ← ArgoCD provisioner (AppProject, Application CRs)
│   │   ├── gitea/                     ← Gitea provisioner (repo creation, archival)
│   │   └── minio/                     ← MinIO provisioner (per-tenant credentials + policies)
│   ├── operatortest/                  ← shared test setup (envtest)
│   ├── config/crd/bases/              ← CRD YAML manifests
│   └── cmd/                           ← entrypoint (multi-cluster client setup, leader election)
├── loopzero-forge/                        ← Go SDK for building Argo Workflows programmatically
│   ├── client/                        ← REST client for Argo Workflows API
│   ├── model/                         ← serializable types matching Argo schema
│   ├── serialize/                     ← YAML/JSON serialization
│   ├── expr/                          ← expression DSL builder
│   ├── validate/                      ← resource unit validation
│   └── config/                        ← global configuration
├── loopzero-packs/                        ← language/framework detection + Dockerfile generation
│   ├── core/                          ← pack framework (dockerfile gen, manifest gen, resolver)
│   ├── railpack/                      ← Rails-specific pack
│   └── internal/                      ← internal utilities
├── infra/
│   ├── pulumi/                        ← IaC (TypeScript + Pulumi)
│   │   ├── src/
│   │   │   ├── index.ts               ← 3-phase orchestrator (infra → bootstrap → expose)
│   │   │   ├── config/                ← stack config loader (dev/staging/prod)
│   │   │   ├── core/                  ← naming, tags, errors, types, execution tracking
│   │   │   ├── contracts/             ← 6 ISP interfaces (network, kubernetes, storage, dns, compute, database)
│   │   │   ├── components/            ← infra.ts, bootstrap.ts, expose.ts
│   │   │   └── providers/
│   │   │       ├── digitalocean/      ← fully implemented (8 files)
│   │   │       └── aws/               ← stub with implementation guide
│   │   ├── Pulumi.dev.yaml           ← dev stack (SFO3, dev.loopzero.example)
│   │   ├── Pulumi.staging.yaml       ← staging stack (NYC3)
│   │   └── Pulumi.prod.yaml          ← prod stack (NYC3, loopzero.example)
│   ├── k3d/                           ← 3 cluster configs (mgmt, build, workload)
│   ├── helm/                          ← 13 Helm charts (all pinned versions)
│   │   ├── argo-workflows/            ← CWTs: clone, cache-restore, cache-save, summary, resource-quota
│   │   ├── argocd/                    ← v2.13.3, manual sync, loopzero-api RBAC
│   │   ├── cert-manager/              ← TLS certificate management
│   │   ├── gitea/                     ← 1.22.6, admin setup, metrics
│   │   ├── minio/                     ← standalone, loopzero-artifacts + loopzero-builds buckets
│   │   ├── monitoring/                ← kube-prometheus-stack (Prometheus + Grafana + cross-cluster scrape)
│   │   ├── network-policies/          ← K8s NetworkPolicy rules
│   │   ├── otel/                      ← OpenTelemetry collector
│   │   ├── postgresql/                ← PostgreSQL with init scripts
│   │   ├── postgresql-ha/             ← HA config (Patroni, for production)
│   │   └── traefik/                   ← 2 replicas, HA, JSON logging, correlation-id tracking
│   ├── tilt/                          ← Tilt deployment manifests (api, operator)
│   ├── scripts/                       ← infrastructure utility scripts (Go)
│   └── tests/
│       ├── e2e/                       ← Ginkgo E2E tests + fixtures (dogfood-monorepo, hello-server, multi-app)
│       ├── chaos/                     ← chaos engineering tests (Ginkgo, skeleton)
│       ├── unit/                      ← infra unit tests
│       └── load/                      ← load tests
└── .github/
    └── workflows/
        ├── ci.yml                     ← lint + tests + security + manifests (22+ jobs, matrix strategy)
        ├── build-images.yml           ← container build + push to DO Registry + CWT update
        ├── infra-deploy.yml           ← Pulumi 3-step deploy (infra → images → converge) + bootstrap
        └── infra-preview.yml          ← Pulumi preview on PRs with comment
```

---

## Agents

Claude operates as a team of specialists. When a task spans multiple components, sequence the work across agents.

### Orchestrator
Entry point for multi-component tasks. Reads the spec, breaks work into subtasks, delegates, validates.

**Activates when:** The task touches more than one component (CLI + API, API + operator, etc.)

**Process:**
1. Identify which components are touched
2. Find the authoritative spec section
3. Sequence subtasks: test spec → implementation → integration
4. Validate: tests pass, no internal terms, output matches spec

### CLI Engineer (`agent:cli`)
Owns `cli/`. TypeScript + Commander. DX quality bar is the Vercel CLI.

**Before any command is done:**
- `--json` flag with schema matching spec §0.3
- Correct exit code
- CI mode works (`CI=true`)
- No internal terms in any string
- Error messages suggest next action
- Unit tests cover success + all failure codes + JSON shape

### API Engineer (`agent:api`)
Owns `api/`. Go + net/http (or chi) + pgx/sqlx.

**Before any endpoint is done:**
- `withEnvironmentLock` for state mutations
- `Idempotency-Key` middleware active
- Typed errors (no `"Something went wrong"`)
- `Correlation-ID` forwarded
- Component test: success + 409 conflict + idempotency replay + concurrent rejection

### Operator Engineer (`agent:operator`)
Owns `operators/`. Go + kubebuilder.

**Before any controller is done:**
- Ginkgo spec written before implementation
- Runs against `envtest` (no real cluster)
- Reconcile is idempotent
- Crash mid-reconcile → resume correctly
- State transitions via state machine only
- Leader election enabled

### Infrastructure Engineer (`agent:infra`)
Owns `infra/`. k3d + Helm + Tilt + Taskfile.

**Standards:**
- All Taskfile tasks are idempotent
- All manifests pass `kubeconform --strict`
- All containers have resource limits, readiness/liveness probes, non-root security context
- No `latest` image tags

### Test Engineer (`agent:test`)
Owns test quality across all layers. Writes specs before implementation.

**Critical tests that must exist:**
- Concurrent deploys → exactly one 202, one 409
- Same idempotency key → same response
- Different payload same key → 422
- Lock released immediately on API crash
- Invalid state transition → panic
- CLI exit codes for all error paths
- No internal terms in any CLI output
- `--json` shape matches spec schema

### Pipeline Reviewer — Helena Duarte (`agent:pipeline-reviewer`)
Reviews Argo Workflows and CI/CD pipelines. Helena Duarte is a Principal CI/CD Pipeline Engineer persona.

**Five review criteria:**
- **Determinism:** same commit = same artifact (lockfiles, pinned images, no floating deps)
- **Build/Release separation:** build produces artifacts only — no gitops push, no deploy, no manifests
- **Scale:** 100 concurrent builds won't break it (parallelism limits, cache, CRD count)
- **Observability:** duration, cache hit rate, failure causes, artifact lineage
- **Recovery:** retry is safe, callbacks idempotent, no duplicate releases

**Activates when:** reviewing or designing Argo Workflow definitions, build pipeline changes, or workflow-API callback integration.

### Meeting Review Board

A separate team of 9 Staff/Principal-level review agents + 1 orchestrator for critical technical decisions. These agents **never write code** — they analyze, decide, and produce execution contracts.

**Full documentation:** `docs/meetings/README.md`

**Meeting Orchestrator** coordinates meetings: classifies decisions, convenes specialists, controls protocol, forces productive conflict, synthesizes decisions.

**9 Specialist Agents:** Principal Architect, K8s Controller Engineer, Distributed Systems Engineer, Platform Security Engineer, SRE/Production Engineer, GitOps/Delivery Engineer, API/Domain Architect, DX/Tooling Engineer, Verification Architect.

**Governance:**
- Security/SRE have absolute veto power
- Minimum 3 agents per meeting
- Every decision needs sponsor + challenger + owner
- Meeting records saved to `docs/meetings/records/`

**Activates when:** `/meeting <topic>` — for critical architecture, security, operability, GitOps, controller, or API boundary decisions.

---

## Slash Commands

Use these in Claude Code to trigger structured workflows:

| Command | Effect |
|---------|--------|
| `/sprint` | Show current sprint status, completed tasks, next task, exit gate |
| `/implement <feature>` | TDD workflow: spec → fail → implement → pass → validate |
| `/test <component>` | Write or run tests for a component |
| `/review <path>` | Compliance review: lock, idempotency, terms, exit codes |
| `/design-check <description>` | Validate approach against system design before coding |
| `/failure-check <scenario>` | Verify a failure mode from spec §12 has a passing test |
| `/schema-check <command>` | Validate CLI `--json` output against spec §0.3 schema |
| `/state-machine <name>` | Inspect implementation vs spec §5 |
| `/dogfood <scope>` | Dogfooding audit: test product as real user, generate actionable report |
| `/pipeline-review <scope>` | Helena Duarte reviews pipelines: determinism, build/release separation, scale, observability, recovery |
| `/meeting <topic>` | Conduct FAANG-level technical meeting with specialist agents for critical decisions |
| `/start-loop <prompt>` | Start an iterative self-referential loop (Ralph Wiggum technique) |
| `/cancel-loop` | Cancel the active loop |

---

## Loop (Ralph Wiggum Technique)

An iterative self-referential loop that feeds the SAME prompt back to Claude on every exit attempt. Claude sees its own previous work in files and git history, enabling autonomous multi-iteration tasks.

Based on Geoffrey Huntley's Ralph Wiggum technique: a Stop hook intercepts session exit, re-injects the original prompt, and Claude iterates until done.

### How it works

```
Claude receives prompt → works on task → tries to exit
    ↑                                          ↓
    └──── Stop hook blocks exit ←── feeds SAME prompt back
```

Each iteration: same prompt, but files reflect previous work. State tracked in `.claude/loop-state.local.md`.

### Usage

```bash
# Basic: loop with iteration limit
/start-loop "Fix all failing tests" --max-iterations 20

# With completion promise: loop until Claude declares done
/start-loop "Implement auth module" --completion-promise "AUTH COMPLETE" --max-iterations 30

# Dogfood in a loop: iterate until audit is done
/start-loop "/dogfood sprint-2" --completion-promise "AUDIT COMPLETE" --max-iterations 10

# Cancel a running loop
/cancel-loop
```

### Options

| Option | Default | Description |
|--------|---------|-------------|
| `--max-iterations <n>` | unlimited | Safety limit — stops after N iterations |
| `--completion-promise '<text>'` | none | Phrase that signals completion (must be inside `<promise>` tags) |

### Completion promises

To signal that the task is done, Claude must output the exact promise text inside `<promise>` tags:

```
<promise>AUDIT COMPLETE</promise>
```

The promise must be TRUE. Claude must not output false promises to escape the loop.

### Monitoring

```bash
# Check current iteration
head -10 .claude/loop-state.local.md
```

### When to use

- Well-defined tasks with clear success criteria (tests passing, audit complete)
- Tasks requiring iteration and self-correction
- Running `/dogfood` or `/review` in autonomous mode
- Greenfield implementation sessions where you can walk away

### When NOT to use

- Tasks requiring human judgment or design decisions
- One-shot operations
- Tasks with unclear success criteria

---

## Hooks

### Before writing a production file
Ask: does a failing test for this already exist? If not, write the test first.

### Before any bash command
Block: `kubectl delete` (without dry-run), `k3d cluster delete`, `DROP TABLE/COLUMN`, `git push --force` to main, `helm uninstall` on postgresql/gitea/minio.

### After writing any CLI file
Grep for internal terms in string literals. Report violations before proceeding.

### After writing any operator file
Remind: run the test, confirm it fails (if new), then implement.

### Before marking a task complete
Run the pre-completion checklist:
```
[ ] task test:unit passes
[ ] task lint passes
[ ] task validate:manifests passes
[ ] No internal terms in CLI output
[ ] Mutating API routes have withEnvironmentLock
[ ] Mutating API routes handle Idempotency-Key
[ ] CLI --json matches spec §0.3 schema
[ ] Exit codes are 0/1/2/3/4/5 only
[ ] State machine changes match spec §5
```

---

## Sprint Plan

> Full details: `docs/sprints/README.md` and each `docs/sprints/XX-name/sprint.md`

| Sprint | Weeks | Focus | Exit gate | CLI Commands | Status |
|--------|-------|-------|-----------|--------------|--------|
| **[0](docs/sprints/00-foundation/sprint.md)** | 1-2 | Foundation | `task setup` boots, Grafana running | — | ✅ Complete |
| **[1](docs/sprints/01-login-init-tenant-controller/sprint.md)** | 3-5 | Login + Init + TenantController | `loopzero login` → `loopzero init` → `loopzero.yaml` | `login`, `logout`, `whoami`, `init` | ✅ Complete |
| **[2](docs/sprints/02-deploy-happy-path/sprint.md)** | 6-9 | Deploy happy path | CLI → live URL | `deploy` | ⚠️ Feature-complete |
| **[3](docs/sprints/03-logs-status-secrets/sprint.md)** | 10-12 | Logs + Status + Secrets | All daily workflows functional | `logs`, `status`, `inspect`, `secret` | ⚠️ Replaced by hardening sprint 6 |
| **[4](docs/sprints/04-rollback-promote-cancel/sprint.md)** | 13-15 | Rollback + Promote + Cancel | Recovery in < 30s | `rollback`, `promote`, `build`, `env`, `team`, `project` | ⚠️ `rollback` done via hardening 6B |
| **[5](docs/sprints/05-preview-environments/sprint.md)** | 16-18 | Preview environments | PR → preview URL → cleanup | `preview` | ❌ Not started |
| **[6](docs/sprints/06-security-supply-chain/sprint.md)** | 19-21 | Security + Supply chain | Signed images, encrypted secrets, RBAC | — | ❌ Not started |
| **[7](docs/sprints/07-observability/sprint.md)** | 22-23 | Observability complete | Incident resolved via dashboards in < 15 min | — | ❌ Not started |
| **[8](docs/sprints/08-hardening-chaos/sprint.md)** | 24-25 | Hardening + Chaos | 50 concurrent deploys, all chaos passes | — | ❌ Not started |
| **[9](docs/sprints/09-polish-launch/sprint.md)** | 26-27 | Polish + Launch | Non-team-member onboards in < 10 min | — | ❌ Not started |

**Hardening sprints** (parallel track, production readiness):

| Sprint | Focus | Status |
|--------|-------|--------|
| H1-Runtime | Wire 7 controllers | ✅ Complete |
| H2-Estabilização | TLS fix | ✅ Complete |
| H3-Banco | PG HA + failover | ✅ Complete |
| H4-Observabilidade | Metrics + SLOs + Dashboards | ✅ Complete |
| H5-Pipeline | Build fix + cache | ✅ Complete |
| H6-Operação | logs (Loki) + rollback + secrets (ESO) + status | ⚠️ 3/4 (`status` pending) |

Full hardening details: [`docs/hardening-sprints/ROADMAP.md`](docs/hardening-sprints/ROADMAP.md)

Check the current sprint before starting any implementation. Only implement what belongs to the current sprint.

---

## Architecture Overview

### Four Planes

```
┌─────────────────────────────────────────────────────────────────┐
│  DEVELOPER PLANE                                                 │
│  CLI (TypeScript) ←→ API (Go)                                   │
├─────────────────────────────────────────────────────────────────┤
│  CONTROL PLANE  (mgmt cluster)                                   │
│  TenantController · EnvironmentController · ApplicationController│
│  ExposureController · PlatformBootstrapController                │
│  GitRepositoryBindingController · ClusterCredentialController    │
│  Gitea · PostgreSQL · MinIO · ArgoCD · Traefik · cert-manager    │
├─────────────────────────────────────────────────────────────────┤
│  BUILD PLANE  (build cluster)                                    │
│  Argo Workflows + BuildKit (CWTs: clone, cache + inline build)  │
├─────────────────────────────────────────────────────────────────┤
│  WORKLOAD PLANE  (runtime cluster)                               │
│  User applications managed by ArgoCD                             │
│  Traefik ingress + cert-manager TLS                              │
└─────────────────────────────────────────────────────────────────┘
```

### Deploy Flow (Happy Path)

```
loopzero deploy → CLI validates → upload bundle → API locks env → submit Argo Workflow
    → build (BuildKit) → push image → generate manifests → push to Gitea (gitops)
    → ArgoCD sync → app running → SSE event → CLI shows URL
```

### CRDs (9 total)

| CRD | Controller | Wired | Purpose |
|-----|-----------|-------|---------|
| Tenant | TenantController | ✅ | Gitea repo, ArgoCD app, build namespace, RBAC, quotas |
| Environment | EnvironmentController | ⚠️ No | Namespace + RBAC + NetworkPolicy + quota on runtime |
| LoopZeroApplication | ApplicationController | ⚠️ No | Hostname assignment, ExposedRoute, health tracking |
| ExposedRoute | ExposureController | ⚠️ No | TLS cert check, reachability probes |
| PlatformBootstrap | PlatformBootstrapController | ⚠️ No | Declarative setup: Gitea → ArgoCD → API seed |
| GitRepositoryBinding | GitRepositoryBindingController | ⚠️ No | Credential sync to ArgoCD with rotation |
| ClusterCredential | ClusterCredentialController | ✅ | Connectivity check + kubeconfig distribution |
| AppSecret | — | ❌ | Secret sync mgmt → workload (not implemented) |
| PreviewEnvironment | — | ❌ | Ephemeral PR environments (Sprint 5) |
| ReconciliationReport | — | ❌ | Drift detection (not implemented) |

### State Machines (7 total, defined in `pkg/statemachine/`)

| Machine | States | Terminal | Timeouts |
|---------|--------|----------|----------|
| Build | QUEUED → RUNNING → SUCCEEDED/FAILED/CANCELLED | SUCCEEDED, FAILED, CANCELLED | QUEUED>5m, RUNNING>30m, CANCELLING>60s |
| Release | READY → PENDING → ACTIVE → SUPERSEDED | SUPERSEDED, ROLLED_BACK, CANCELLED | READY>5m, PENDING>2m |
| Tenant | Provisioning → Active → Suspended → Deleting | Deleting | — |
| PlatformBootstrap | Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → Complete | Complete, Failed | — |
| Preview | Creating → Active → Expiring/Deleting → Deleted | Deleted | TTL-based |
| Promotion | Validating → Committing → Syncing → Completed | Completed, Failed | — |
| Rollback | Validating → Reverting → Syncing → Completed | Completed, Failed | — |

### Production Infrastructure (DigitalOcean)

| Resource | Config | Domain |
|----------|--------|--------|
| 3 DOKS clusters | v1.32.13, 2 nodes each, SFO3 | — |
| PostgreSQL | v16, managed, single node | — |
| Container Registry | DO Container Registry | registry.digitalocean.com/loopzero-infra-registry |
| DNS | A records + wildcard | *.dev.loopzero.example (runtime), api/argocd/git/grafana.dev.loopzero.example (mgmt) |
| State backend | DO Spaces (S3-compatible) | s3://loopzero-infra-pulumi-state |

### Known Systemic Patterns

| Pattern | Description | Mitigation |
|---------|-------------|------------|
| "Implemented but not wired" | Code exists in correct location but not integrated in main.go or runtime path | Always verify call site after implementation; integration test required |
| "Correct code in wrong place" | Duplicate implementations with different error handling | Single source of truth per behavior; grep for duplicates before merge |

---

## Common Mistakes — Read Before Coding

| Mistake | Consequence | Fix |
|---------|------------|-----|
| `pg_advisory_lock` instead of `pg_advisory_xact_lock` | Lock persists on connection pool reuse → deadlocks | Always use `xact` variant |
| Concurrent build check OUTSIDE advisory lock | Race condition between check and insert | Move check inside `withEnvironmentLock` |
| `errors.New("something failed")` | Untraceable, no error code | Use `LoopZeroError` with typed code |
| Direct state update (`release.Status.State = "active"`) | Bypasses validation, invalid transitions silently succeed | Use `release.Transition(event)` |
| `kubectl` in user-facing workflow | Breaks the "no Kubernetes" promise | Proxy through API, never expose kubectl |
| Marking release ACTIVE before ArgoCD confirms sync | False positive — app may not be running | Wait for ArgoCD sync confirmation |
| Writing prod code before failing test | Unverifiable correctness | Test first, always |
| Internal terms in CLI output | Developer experience failure | Use product language, see Rule 3 |
| Forward-only migration violations (`DROP`) | Irreversible data loss | Explicit sign-off required |

---

## Key References in System Design

| Topic | Section |
|-------|---------|
| Core CLI workflows (terminal output) | §0.1 |
| CLI stability contract | §0.2 |
| Exit codes + JSON schemas + CI mode | §0.3 |
| North Star + target persona | §1 |
| Design philosophy (8 principles) | §2 |
| Source of truth hierarchy | §3 |
| Serialization + idempotency | §4 |
| State machines (all 5) | §5 |
| Architecture (4 planes) | §6 |
| System components | §7 |
| Data model | §9 |
| Core flows (deploy, promote, rollback, preview) | §10 |
| Failure modes + recovery | §12 |
| Multi-tenancy + isolation | §14 |
| Observability (metrics, dashboards, alerts) | §15 |
| Security model | §16 |
| TDD/BDD strategy | §18 |
| Local infrastructure | §19 |
| Test contracts by layer | §20 |
| CLI reference (all commands) | §25 |
| `loopzero.yaml` complete spec | §26 |