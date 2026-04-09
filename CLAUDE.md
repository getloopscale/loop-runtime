# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

> This file is the contract between Claude and the LoopZero codebase. Read it entirely before touching any file.

---

## What This Project Is

LoopZero is a **CLI-first Kubernetes PaaS** for monorepo deployments. The goal is Vercel-quality DX for backends — `loopzero login`, `loopzero init`, `loopzero deploy`, and in under 5 minutes every service is live with TLS, preview environments on every PR, and rollback with one command. No Kubernetes. No infrastructure YAML.

The authoritative specification is **`docs/technical.md`**. Every state machine, failure mode, concurrency decision, CLI output format, and exit code is documented there. If this file and the system design conflict, the system design wins.

Sprint details are in **`docs/sprints/`** — one folder per sprint with tasks, exit gates, and spec references.

**Current status: Pre-release, Starter + Pro tiers launchable.** Sprints 0-2 complete (foundation + auth + deploy happy path). Hardening sprints 1-6 complete. Sprint 10 (static frontend) complete. Sprint 11 (auth + billing) complete. **Phase 1 (enforcement + audit) complete (2026-04-08):** plan limits enforced on projects/builds/environments/members, audit log writing to DB, GET /api/v1/audit endpoint, `loopzero audit` CLI command. **Custom Domains v2 complete (2026-04-08):** CF Zone Management (model Vercel) — `loopzero domain add` creates CF zone per domain, user changes nameservers, full WAF/DDoS via CF edge, zero cert-manager dependency for custom domains. E2E validated with aquim.ai. **GitHub App auto-deploy complete (2026-04-08):** `git push` → automatic deploy (model Vercel). GitHub App "LoopZero Platform" (ID 3319955), webhook handler with HMAC-SHA256, source download via installation token, tarball→git bundle conversion, S3 upload, build submission. E2E validated: push to getloopscale/loopzero-repotest → build SUCCEEDED → app live at aquim.ai in ~4min. **Monorepo validator (2026-04-08):** frontend auto-infer static, framework validation (30 frameworks), max 2 pods, Dockerfile detection, path traversal protection. Runs before upload (instant feedback). 857 CLI tests. **Auth hardened (MEET-039):** UUID tenant IDs, engine RSA-only, audience validation, secret rotation with kid+dual-key, no "default" tenant fallback. **Two-host gateway:** cloud.{domain} for browser (dashboard+Kratos+Hydra), api.{domain} for CLI (engine). **Pricing:** Starter (Free, 1 project), Pro ($29/mo, 3 projects), Team ($79/mo, 10 projects), Enterprise (custom). **Production hardening (2026-04-09):** Failure mode analysis (MEET-043): 5/8 scenarios already handled, 3 gaps fixed. TCP keepalive on PG pool (detects dead connections in 90s vs 2h). Leader heartbeat timeout (10s context prevents zombie hangs). Non-leader periodic re-election (60s retry). Build/release GC (retain 10 per project, delete >30 days). Manifest validation gate (rejects Certificate CRs in gitops — prevents ArgoCD sync blockage). Gitea health gate. 30 language frameworks supported (Node, Go, Python, Ruby, Java, PHP, Rust). `loopzero connect`/`disconnect` CLI commands. GitHub commit status lifecycle (pending→success). CI pipeline green. 871 CLI tests + 33 Go packages passing. Production on DigitalOcean (3 DOKS + Loki + ESO + CF Tunnel + DO Spaces CDN). **Not yet built:** preview environments (Sprint 5, Phase 2), `loopzero promote` (Sprint 4), SSO/SAML (Phase 3), image signing (Sprint 6). **Known issues:** AppSecret controller not implemented (secrets work via ESO directly). Pulumi consolidation pending (GitHub App envFrom, CF_ZONE_MANAGEMENT, PG keepalive — code committed, `pulumi up` needed).

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
cd cli  && npm test           # CLI unit tests (Jest, 747 tests)
cd api  && go test ./...      # API tests (Go testing, 80+ test files, 22 packages)
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

### Infrastructure

| Component | Technology | Purpose |
|-----------|-----------|---------|
| Database | PostgreSQL 16 (managed DO) | State, advisory locks for serialization |
| Build engine | Argo Workflows + BuildKit | Container image builds in isolation (rootless, single-container) |
| GitOps store | Gitea 1.22.6 | Desired state mirror (source + gitops repos) |
| GitOps sync | ArgoCD 2.13.3 | Cluster convergence (manual sync only) |
| Ingress | Traefik 28.3.0 + cert-manager 1.14.5 | TLS termination, HTTP→HTTPS redirect |
| Artifact store | MinIO (S3-compatible) / DO Spaces | Source bundles, build cache |
| Container registry | DigitalOcean Container Registry | Container image storage |
| Observability | Prometheus 25.27.0 + Grafana 8.5.2 + OpenTelemetry | Metrics, dashboards, tracing |
| Log aggregation | Grafana Loki 6.24.0 + Promtail 6.16.6 | Build log storage (build cluster), queried by API via LogQL |
| Secret sync | External Secrets Operator 0.14.3 | Cross-cluster secret sync mgmt → workload via ClusterSecretStore |
| DNS | DigitalOcean DNS | A records, wildcard for user apps |
| Edge proxy | Cloudflare Tunnel + Worker | DDoS/WAF protection, static asset routing from DO Spaces |
| Static assets | DO Spaces + CDN | Frontend build output (JS/CSS/HTML), public-read ACL |
| Routing table | Cloudflare Workers KV | Static app routing: subdomain → build-id + S3 prefix |

### Development & Operations

| Component | Technology | Purpose |
|-----------|-----------|---------|
| IaC | Pulumi (TypeScript) + @pulumi/digitalocean + @pulumi/kubernetes + @pulumi/cloudflare | 3-phase infra provisioning (infra → bootstrap → expose) + CF Worker/KV |
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
| Tests (TS) | Jest | CLI unit tests (747 tests) |
| Migrations | golang-migrate (forward-only, .up.sql only) | Database schema evolution |
| Image signing | cosign + syft (Sprint 6 — not yet built) | Supply chain security |
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
│   │   ├── ROADMAP.md                 ← sprint roadmap
│   │   ├── EXECUTION-PLAN.md          ← execution plan
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
│   │   ├── commands/                  ← login, logout, whoami, init, deploy, validate, logs, rollback, secret, project-delete
│   │   ├── api/                       ← API client (retry, idempotency-key) + SSE client (reconnect, fallback polling)
│   │   ├── output/                    ← formatters: human/json/CI + deploy renderer (TTY/CI modes)
│   │   ├── detection/                 ← monorepo detection (Turbo/Nx/Lerna/pnpm) + framework detection (10 frameworks)
│   │   ├── adapters/                  ← git operations, loopzero.yaml parsing (v2: render, build, secrets), source upload, bundle cleanup, app-unit, validate-config
│   │   └── config/                    ← credential store (~/.loopzero/config.json), constants, env overrides
│   └── tests/unit/                    ← 747 tests, 40+ suites (Jest)
├── api/                               ← loopzero/api (Go + chi + pgx)
│   ├── cmd/                           ← entrypoint (server bootstrap, background services)
│   ├── internal/
│   │   ├── routes/                    ← auth, health, builds, projects, source, secrets, bootstrap
│   │   ├── services/                  ← BuildOrchestrator, BuildReconcileLoop, BuildReaper, ReleaseReaper, ProjectDeleter, CircuitBreaker, StaticDelivery
│   │   ├── db/                        ← pgx pool + 9 stores (build, release, project, envtarget, membership, secret, idempotency, bootstrap, event)
│   │   ├── middleware/                ← auth (GitHub token + cache), idempotency, correlation-id, rate-limit, audit, logging
│   │   ├── lock/                      ← withEnvironmentLock (pg_advisory_xact_lock, 5s timeout)
│   │   ├── sse/                       ← SSE streaming + event journal (NOTIFY/LISTEN)
│   │   ├── correlation/               ← correlation ID generation (UUID v4)
│   │   ├── argo/                      ← Argo Workflows client (submit, cancel, get status)
│   │   ├── argocd/                    ← ArgoCD sync client (trigger, wait)
│   │   ├── cfkv/                      ← Cloudflare Workers KV client (static frontend routing)
│   │   ├── cfzone/                    ← Cloudflare Zone Management client (custom domains v2)
│   │   ├── github/webhook/           ← GitHub App webhook handler (HMAC, push/PR/install events)
│   │   ├── github/appclient.go       ← GitHub App JWT auth + installation tokens + API calls
│   │   ├── github/autodeploy.go      ← Auto-deploy on push (download→bundle→S3→build)
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
│   │   │       ├── cloudflare/        ← CF Worker + KV for static frontend delivery
│   │   │       └── aws/               ← stub with implementation guide
│   │   ├── Pulumi.dev.yaml           ← dev stack (SFO3, dev.loopzero.example)
│   │   ├── Pulumi.staging.yaml       ← staging stack (NYC3)
│   │   └── Pulumi.prod.yaml          ← prod stack (NYC3, loopzero.example)
│   ├── k3d/                           ← 3 cluster configs (mgmt, build, workload)
│   ├── cloudflare/                    ← CF Worker source (worker.mjs for static delivery)
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
| **[2](docs/sprints/02-deploy-happy-path/sprint.md)** | 6-9 | Deploy happy path | CLI → live URL | `deploy` | ✅ Complete (validated on DO) |
| **[3](docs/sprints/03-logs-status-secrets/sprint.md)** | 10-12 | Logs + Status + Secrets | All daily workflows functional | `logs`, `status`, `inspect`, `secret` | ✅ Done via hardening H6 |
| **[4](docs/sprints/04-rollback-promote-cancel/sprint.md)** | 13-15 | Rollback + Promote + Cancel | Recovery in < 30s | `rollback`, `promote`, `build`, `env`, `team`, `project` | ⚠️ `rollback` done (H6B), `promote` not started |
| **[5](docs/sprints/05-preview-environments/sprint.md)** | 16-18 | Preview environments | PR → preview URL → cleanup | `preview` | ❌ Not started (CRD defined, no controller) |
| **[6](docs/sprints/06-security-supply-chain/sprint.md)** | 19-21 | Security + Supply chain | Signed images, encrypted secrets, RBAC | — | ❌ Not started (cosign+syft) |
| **[7](docs/sprints/07-observability/sprint.md)** | 22-23 | Observability complete | Incident resolved via dashboards in < 15 min | — | ✅ Done via hardening H4 |
| **[8](docs/sprints/08-hardening-chaos/sprint.md)** | 24-25 | Hardening + Chaos | 50 concurrent deploys, all chaos passes | — | ❌ Not started |
| **[9](docs/sprints/09-polish-launch/sprint.md)** | 26-27 | Polish + Launch | Non-team-member onboards in < 10 min | — | ❌ Not started |
| **[10](docs/sprints/10-static-frontend-delivery/sprint.md)** | — | Static Frontend + Security | Monorepo (API+Web+Worker) on DO, static via CF edge, zero pods for frontend | — | ✅ Complete |

**Hardening sprints** (parallel track, production readiness):

| Sprint | Focus | Status |
|--------|-------|--------|
| H1-Runtime | Wire 7 controllers | ✅ Complete |
| H2-Estabilização | TLS fix | ✅ Complete |
| H3-Banco | PG HA + failover | ✅ Complete |
| H4-Observabilidade | Metrics + SLOs + Dashboards | ✅ Complete |
| H5-Pipeline | Build fix + cache | ✅ Complete |
| H6-Operação | logs (Loki) + rollback + secrets (ESO) + status | ✅ Complete |
| H7-Workload Security | SecurityContext non-root + PSA restricted + NetworkPolicy mandatory | ✅ Complete (code implemented, roadmap doc stale) |

Full hardening details: [`docs/hardening-sprints/ROADMAP.md`](docs/hardening-sprints/ROADMAP.md)

Check the current sprint before starting any implementation. Only implement what belongs to the current sprint.

**Production Readiness** (validated 2026-03-29):

| Phase | Status | Description |
|-------|--------|-------------|
| Secure passwords | ✅ | Gitea + MinIO passwords configurable via `pulumi config set --secret` |
| Pulumi.prod.yaml | ✅ | Production sizing (medium nodes, 3 HA DB, nyc3) |
| Rollback | ✅ | `loopzero rollback` validated (3s, version verified) |
| ESO/Secrets | ⚠️ | ESO sync works (<30s). AppSecret controller not implemented — secrets work via ESO directly but lack lifecycle automation. E2E test not in CI. |
| SLOs + Alerts | ✅ | Alertmanager + 4 rules (API down, deploy failing, latency, crash loop) |
| `loopzero status` | ✅ | Handler wired in main.go + server.go, functional |
| Workload security | ✅ | SecurityContext (non-root, drop ALL, readOnlyRootFilesystem), PSA restricted, NetworkPolicy mandatory |
| Custom Domains v2 | ✅ | CF Zone Management (model Vercel). `loopzero domain add` creates CF zone, scans DNS, user changes NS. WAF/DDoS via CF edge. E2E validated with aquim.ai. Plan: `docs/plans/custom-domains-v2-cf-zones.md` |
| GitHub App auto-deploy | ✅ | `git push` → automatic deploy. GitHub App "LoopZero Platform", webhook HMAC-SHA256, source download via installation token, tarball→bundle→S3→build. E2E: push → SUCCEEDED → live in ~4min. Plan: `docs/plans/github-app-integration.md` |
| Monorepo validator | ✅ | Pre-upload validation: 30 frameworks, frontend=static auto-infer, max 2 pods, Dockerfile detection, path traversal (SEC-10). 857 CLI tests. Plan: `docs/plans/monorepo-validator.md` |
| Image signing | ❌ | cosign + syft not implemented (Sprint 6) |
| Preview environments | ❌ | CRD defined, controller not implemented (Sprint 5). GitHub App receives PR events (foundation ready). |

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
    (static apps: build → upload S3 → write CF KV → CF Worker serves from DO Spaces)
```

### CRDs (9 total)

| CRD | Controller | Wired | Purpose |
|-----|-----------|-------|---------|
| Tenant | TenantController | ✅ | Gitea repo, ArgoCD app, build namespace, RBAC, quotas |
| Environment | EnvironmentController | ✅ Wired | Namespace + RBAC + NetworkPolicy + quota on runtime |
| LoopZeroApplication | ApplicationController | ✅ Wired | Hostname assignment, ExposedRoute, health tracking |
| ExposedRoute | ExposureController | ✅ Wired | TLS cert check, reachability probes |
| PlatformBootstrap | PlatformBootstrapController | ✅ Wired | Declarative setup: Gitea → ArgoCD → API seed |
| GitRepositoryBinding | GitRepositoryBindingController | ✅ Wired | Credential sync to ArgoCD with rotation |
| ClusterCredential | ClusterCredentialController | ✅ | Connectivity check + kubeconfig distribution |
| AppSecret | — | ❌ | CRD defined, no controller. Secrets work via ESO directly. |
| PreviewEnvironment | — | ❌ | CRD defined, no controller (Sprint 5) |
| ReconciliationReport | — | ❌ | CRD placeholder, no controller (drift detection, deferred) |

### State Machines (7 total, defined in `pkg/statemachine/`)

| Machine | States | Terminal | Timeouts |
|---------|--------|----------|----------|
| Build | QUEUED → RUNNING → RELEASING → SUCCEEDED/FAILED/CANCELLED | SUCCEEDED, FAILED, CANCELLED | QUEUED>5m, RUNNING>30m, RELEASING>10m, CANCELLING>60s |
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
| Container Registry | DO Container Registry (Professional) | registry.digitalocean.com/loopzero-infra-registry |
| DNS | A records + wildcard CNAME via CF Tunnel | *.dev.loopzero.example (CF Tunnel → runtime), api/argocd/git/grafana.dev.loopzero.example (mgmt) |
| Cloudflare Tunnel | c35b9d63... | All runtime traffic via CF edge (DDoS/WAF), zero public IPs |
| CF Worker | loopzero-static-delivery | Routes static frontend requests to DO Spaces via KV lookup |
| CF KV | loopzero-routing (28992ed1...) | Routing table: subdomain → { buildId, s3Prefix } |
| CF Zone Management | Cloudflare for Platforms | Custom domains: per-domain zone creation, DNS scan+import, WAF/DDoS, TLS (model Vercel) |
| GitHub App | LoopZero Platform (ID 3319955) | Auto-deploy on push, webhook events, installation tokens, source download |
| DO Spaces | loopzero-static-assets + CDN | Static frontend assets (public-read, immutable per build-id) |
| State backend | DO Spaces (S3-compatible) | s3://loopzero-infra-pulumi-state |

### Known Systemic Patterns

| Pattern | Description | Mitigation |
|---------|-------------|------------|
| "Implemented but not wired" | Code exists in correct location but not integrated in main.go or runtime path | Always verify call site after implementation; integration test required |
| "Correct code in wrong place" | Duplicate implementations with different error handling | Single source of truth per behavior; grep for duplicates before merge |
| "Project name vs project ID" | CLI sends project name (from loopzero.yaml) as `project_id` in builds. DB `projects` table uses UUID. Status/rollback/delete must use the **name** for build/release queries, not the UUID. | All queries against builds/releases use the project name. Only the `projects` table uses UUID. |
| "ArgoCD notifications `notifiers`" | The argo-cd Helm chart v7.x uses `notifications.notifiers` for webhook services in the ConfigMap. `notifications.services` is silently ignored. | Always use `notifiers` key in Pulumi values and helm/argocd/values.yaml |
| "Single ArgoCD Application per tenant" | One Application manages all projects in an environment. If ANY resource is Degraded, ALL notifications are blocked. | Clean orphaned manifests immediately on delete; gitops.Remove failure is FATAL |
| "Implemented but not documented" | Code is implemented and tested but roadmap/docs still say "TODO" or "not started". Inverse of "implemented but not wired". | Before claiming something is missing, verify in the code — roadmaps can be stale. The code is the source of truth for what exists. |
| "Custom domains = CF Zone per domain" | Custom domains use Cloudflare Zone Management (v2). Each domain gets its own CF zone. User changes nameservers. CF handles TLS+WAF. No cert-manager Certificate for CF-managed domains. Apex domains work via CF CNAME flattening. | `cfzone` package handles CF API. DNS scan imports existing records (email preservation). State: `pending_ns → ns_active → provisioning → active`. Plan: `docs/plans/custom-domains-v2-cf-zones.md` |
| "CF scan imports conflicting apex records" | When CF scans DNS, it imports the existing A record for the apex domain. This conflicts with the CNAME LoopZero needs to add. | The v2 create flow deletes conflicting A/AAAA/CNAME at apex BEFORE creating the routing CNAME. |
| "GitHub tarball ≠ git bundle" | GitHub API returns .tar.gz source archives. The loopzero-clone CWT expects git bundles. Uploading a tarball directly to S3 causes `does not look like a v2 or v3 bundle file`. | autodeploy.go converts tarball to git bundle: extract → git init → git add → git commit → git bundle create → upload .bundle to S3 |
| "Auto-deploy source = commit SHA" | When auto-deploy downloads source from GitHub, the S3 key is `{tenant}/source/{commitSHA}.bundle`. This is the same key the CLI uses. The build pipeline (loopzero-clone CWT) downloads by this key. | CommitSHA from webhook push event must match exactly. The build uses `inputs.parameters.build-id` which maps to the commit SHA. |

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
| `notifications.services` in ArgoCD Helm values | Silently ignored — webhook never fires | Use `notifications.notifiers` (chart v7.x) |
| `project.ID` (UUID) for build/release queries | Returns 0 results — builds use project name | Use project name from `loopzero.yaml`, not internal UUID |
| `gitops.Remove` error treated as non-fatal | Orphaned manifests → Degraded ArgoCD → blocks all deploys | `gitops.Remove` failure is FATAL — delete must fail |
| `GetActiveByEnvTx` without ORDER BY | Arbitrary ACTIVE release returned — may skip supersede | ORDER BY created_at ASC LIMIT 1 for supersede target |
| cert-manager Certificate for custom domains | Stuck `Progressing` Certificate blocks ArgoCD sync → release never ACTIVE → circular dependency with domain provisioner | CF-managed domains (v2) skip Certificate CR entirely. CF handles TLS at edge. Use `CustomDomainInput.CFManaged` to control. |
| `react`/`vue`/`svelte` framework not mapped server-side | Falls to `custom` strategy → `scriptValidateCustom` requires Dockerfile at root → build fails in monorepos | Added to `resolveLanguageStrategy()` in `build_plan.go`. All CLI-recognized frontend frameworks map to `node` strategy. |

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