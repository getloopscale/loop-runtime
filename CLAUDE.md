# CLAUDE.md

Contract between Claude and the LoopZero codebase. Read before touching any file.

## What This Project Is

LoopZero is a **CLI-first Kubernetes PaaS** for monorepo deployments. Vercel-quality DX for backends — `loopzero login`, `loopzero init`, `loopzero deploy`, and in under 5 minutes every service is live with TLS, preview environments on every PR, and rollback with one command. No Kubernetes. No infrastructure YAML.

The authoritative specification is **`docs/cli-reference.md`**. If this file and the system design conflict, the system design wins.

**Status:** Pre-release, Starter + Pro tiers launchable (98% confidence — E2E validated 2026-04-23). 25/30 E2E tests pass. Sprints 0-7, 10, 11 complete. Sprint 6 (security + supply chain) — **fully shipped** 2026-04-26 (CHG-2026-04-26-SPRINT6-IMPL): cosign + syft SBOM (CycloneDX per release in S3 with presigned URL endpoint) + sigstore policy-controller activated on runtime cluster (Audit mode, key-based verification, Deny-flip pending 2-week observation). Sprint 5 (preview envs) — **ALL 14 plan tasks done** with E2E validated against live k3d (CHG-2026-04-26-PREVIEW-IMPL). Sprints 8-9 not started. Hardening H1-H7 complete. AUDIT series (21 fixes) hardened CLI↔API contracts. KEDA scale-to-zero done. Cell architecture Phase 0+1 complete. 1079 CLI tests + 41 Go packages + 200+ envtest specs passing. Production on DigitalOcean (3 DOKS + Loki + ESO + CF Tunnel + DO Spaces CDN).

**Pricing:** Starter (Free, 1 project), Pro ($29/mo, 3 projects), Team ($79/mo, 10 projects), Enterprise (custom).

**Completed:** deploy happy path, scaling+HPA (Starter=2/Pro=5/Team=10), promote staging→prod, rollback, secrets via ESO, custom domains v2 (CF Zone Mgmt, model Vercel), GitHub App auto-deploy (~4min), monorepo validator (30 frameworks), cosign image signing, plan enforcement, audit log, RSA-only JWT auth, two-host gateway, concurrent deploy rejection (1×202+1×409), JSON contracts (8/8), observability (43 loopzero_* metrics + Grafana + SLO alerts), KEDA scale-to-zero, cell architecture (Phase 0+1).

**Not built:** SSO/SAML, `loopzero scale` DB persistence (stub returns 202 acknowledged — use loopzero.yaml + loopzero deploy), Loki cross-cluster streaming (infra gap, not code), Cell Phase 2b activation (code 100%, Pulumi.dev.yaml multi-cell config missing). Sprint 5 / Preview Environments **fully shipped** (CHG-2026-04-26-PREVIEW-IMPL). Sprint 6 / Supply chain **fully shipped** (CHG-2026-04-26-SPRINT6-IMPL — Audit mode active; Deny-mode flip pending 2-week observation window).

**Known issues:** AppSecret controller not implemented (ESO works directly, no lifecycle automation); Pulumi consolidation pending (GitHub App envFrom, CF_ZONE_MANAGEMENT, PG keepalive); 10 failing tests in `api/internal/k8s` (TenantCRManager — contract drift from AUDIT series).

---

## Development Commands

```bash
task setup               # Bootstrap k3d clusters + Helm + CRDs (idempotent)
task dev:up              # Boot k3d clusters + dependencies
tilt up                  # Dev loop with hot reload
task doctor              # Environment health check

task test                # All tests
task test:unit           # Unit tests
task test:e2e            # E2E (Ginkgo)
task lint                # golangci-lint + ESLint
task validate:manifests  # kubeconform --strict

cd cli && npm test       # CLI (Jest, 1079 tests, 59 suites)
cd api && go test ./...  # API (41 packages)
cd operators && go test ./...  # Operators (Ginkgo + envtest)
cd pkg && go test ./...  # Shared libs

task db:migrate          # Forward-only migrations
task db:reset            # Drop + recreate + migrate + seed

cd infra/pulumi && npx pulumi up --stack dev      # Deploy to DO
cd infra/pulumi && npx pulumi preview --stack dev # Preview
```

**Prerequisites:** Go 1.25+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, kubectl, kubeconform, golangci-lint v2.11+

---

## The Rules (Non-Negotiable)

1. **Tests before code** — No production code without a failing test. Write the test, run it, confirm it fails, then implement.
2. **Read the spec first** — `docs/cli-reference.md` is authoritative. Don't invent behavior the spec doesn't describe.
3. **No internal terms in user-facing output** — Forbidden in CLI output, error messages, `--json`: `ArgoCD`, `argocd`, `GitOps`, `gitops-push`, `CRD`, `tenant_id`, `advisory lock`, `pg_advisory`, `Gitea`, `MinIO`, `kubebuilder`, `envtest`, `Kaniko`. Use product language: deploy, team, account, service, revert, build.
4. **Exit codes are permanent** — `0` success, `1` usage error, `2` build failure, `3` deploy failure, `4` auth error, `5` partial failure. No others.
5. **`--json` is a stability contract** — Field names cannot change without major version bump. New fields can be added.
6. **Every mutation uses the environment lock** — `withEnvironmentLock` for all `(tenant, project, environment)` mutations. Concurrent-build check MUST be inside the lock (race condition otherwise).
7. **`pg_advisory_xact_lock` only** — Never session-scoped (`pg_advisory_lock`) — breaks with connection pools.
8. **State machines are strict** — Use state machine functions, never direct status updates. Invalid transitions panic (programming error).
9. **CLI is the primary interface** — Every feature lives in CLI before dashboard. Dashboard-only features are product bugs.
10. **Local equals production** — Same Helm charts, same operators, same build pipeline. No Docker Compose shortcuts. No "dev mode" with different behavior.

---

## Stack

| Layer | Technology |
|-------|-----------|
| CLI | TypeScript + Commander (`cli/`) |
| API | Go 1.25 + chi + pgx (`api/`) |
| Operators | Go 1.25 + kubebuilder + controller-runtime (`operators/`) |
| Shared libs | Go (`pkg/`: errors, logging, id, naming, health, statemachine, jwt, enforcement, circuitbreaker) |
| Database | PostgreSQL 16 (managed DO) |
| Build | Argo Workflows + BuildKit (rootless) |
| Image signing | cosign (in-pipeline, key-based) |
| GitOps | Gitea 1.22.6 + ArgoCD 2.13.3 (manual sync) |
| Ingress | Traefik 28.3.0 + cert-manager 1.14.5 |
| Storage | MinIO / DO Spaces (S3) |
| Registry | DigitalOcean Container Registry |
| Observability | Prometheus 25.27.0 + Grafana 8.5.2 + OTel + Tempo + Loki 6.24.0 + Promtail 6.16.6 |
| Secrets | External Secrets Operator 0.14.3 |
| Auth | Ory Kratos + Ory Hydra + Redis |
| Edge | Cloudflare Tunnel + Worker + KV |
| Autoscaling | KEDA 2.16.1 + HTTP Add-on 0.8.0 |
| IaC | Pulumi (TS) — DO + CF + K8s providers, 3-phase (infra → bootstrap → expose) |
| Local clusters | k3d × 3 (mgmt, build, workload) |
| Dev loop | Tilt + Taskfile (1695 lines) |
| Helm | 22 charts (pinned versions) |
| CI/CD | GitHub Actions (14 workflows) |
| Migrations | golang-migrate (forward-only `.up.sql`) |

---

## Repository Structure (Brief)

```
loopzero/
├── docs/                  ← cli-reference.md (spec), maturity-assessment.md, meetings/, runbooks/, strategy/
├── pkg/                   ← shared Go: statemachine, errors, jwt, naming, health, id, logging, enforcement, circuitbreaker
├── cli/                   ← @loopzero/cli — TS + Commander, 36 commands, 1079 tests
├── api/internal/          ← routes (53), services (76), db, middleware (20), lock, sse, argo, argocd, cfkv, cfzone, github, gitops, source, s3, k8s, manifests, validation, metrics, health, config
├── operators/             ← 8 controllers (Tenant, Environment, Application, Exposure, PlatformBootstrap, GitRepositoryBinding, ClusterCredential, Preview), 10 CRDs
├── infra/
│   ├── pulumi/            ← IaC (TS), providers: digitalocean/, cloudflare/, aws/(stub)
│   ├── helm/              ← 22 charts pinned
│   ├── k3d/               ← 3 cluster configs
│   ├── cloudflare/        ← CF Worker source (worker.mjs)
│   └── tests/             ← e2e/ (Ginkgo), chaos/, unit/, load/
└── .github/workflows/     ← 14 workflows: ci, build-images, infra-*, release, load-test
```

Use `Glob` / `git ls-files` for current details.

---

## Agents

Claude operates as a team of specialists. Sequence work across agents for multi-component tasks.

- **orchestrator** — entry for multi-component tasks. Reads spec, breaks into subtasks, delegates, validates.
- **agent:cli** — owns `cli/`. Bar: Vercel CLI quality. Required: `--json` schema, exit codes, CI mode (`CI=true`), no internal terms, error messages suggest next action, unit tests cover all paths.
- **agent:api** — owns `api/`. Required: `withEnvironmentLock` on mutations, `Idempotency-Key` middleware, typed errors (`LoopZeroError`), `Correlation-ID` forwarded, component test for success + 409 + idempotency replay + concurrent rejection.
- **agent:operator** — owns `operators/`. Required: Ginkgo + envtest spec written first, idempotent reconcile, crash-mid-reconcile resumes correctly, state via state machine only, leader election enabled.
- **agent:infra** — owns `infra/`. Standards: idempotent Taskfile, `kubeconform --strict` passes, resource limits + readiness/liveness probes + non-root, no `latest` image tags.
- **agent:test** — writes specs before implementation. Critical tests that MUST exist: concurrent deploys (1×202+1×409), idempotency replay, lock release on crash, invalid transition panic, exit codes, JSON shape, no internal terms.
- **agent:pipeline-reviewer (Helena Duarte)** — Argo Workflows / CI/CD. Reviews: determinism (same commit = same artifact), build/release separation, scale (100 concurrent), observability, recovery (idempotent retries).
- **Meeting Review Board** — 9 Staff/Principal review agents + orchestrator for critical decisions. Never write code. Security/SRE have absolute veto. Min 3 agents per meeting. See `docs/meetings/README.md`.

Skills/slash commands (subset): `/sprint`, `/implement`, `/test`, `/review`, `/design-check`, `/failure-check`, `/schema-check`, `/state-machine`, `/dogfood`, `/sre-guardian`, `/pipeline-review`, `/meeting`, `/start-loop`, `/cancel-loop`. Full list injected by harness.

---

## Pre-Completion Checklist

```
[ ] task test:unit passes
[ ] task lint passes
[ ] task validate:manifests passes
[ ] No internal terms in CLI output (Rule 3)
[ ] Mutating API routes have withEnvironmentLock
[ ] Mutating API routes handle Idempotency-Key
[ ] CLI --json matches spec schema
[ ] Exit codes are 0/1/2/3/4/5 only
[ ] State machine changes match spec
```

**Bash blocked:** `kubectl delete` (without dry-run), `k3d cluster delete`, `DROP TABLE/COLUMN`, `git push --force` to main, `helm uninstall` on postgresql/gitea/minio.

---

## Sprint Plan

| Sprint | Focus | Status |
|--------|-------|--------|
| 0 | Foundation | ✅ |
| 1 | Login + Init + TenantController | ✅ |
| 2 | Deploy happy path | ✅ (E2E on DO) |
| 3 | Logs + Status + Secrets | ✅ (H6) |
| 4 | Rollback + Promote + Cancel | ✅ (E2E 2026-04-23) |
| 5 | Preview environments | ✅ ALL 14 tasks done. E2E validated live (12 specs pass against k3d). |
| 6 | Security + Supply chain | ✅ cosign + syft SBOM + policy-controller (Audit mode) shipped 2026-04-26 (CHG-2026-04-26-SPRINT6-IMPL); Deny-mode flip pending 2-week observation |
| 7 | Observability | ✅ (H4) |
| 8 | Hardening + Chaos | ❌ |
| 9 | Polish + Launch | ❌ |
| 10 | Static Frontend Delivery | ✅ |
| 11 | Auth + Billing | ✅ |
| H1-H7 | Hardening (runtime, TLS, PG HA, observability, pipeline, ops, workload security) | ✅ all complete |

**E2E Validation (2026-04-23):** 25/30 pass (83%). Confidence: deploy 99%, scaling 95%, promote 95%, rollback 99%, concurrent rejection 99%, JSON contracts 99%, internal terms (Rule 3) 99%. Log streaming 60% (infra gap, not code).

**Maturity:** 3.6/5 (was 3.2 pre-Sprint-5; +0.4 from Preview Environments fully shipped + E2E live validated — CHG-2026-04-26-PREVIEW-IMPL). Path to 4.0: Sprint 9 + Cell-2 activation + 1 external early adopter. Full: `docs/maturity-assessment.md`.

---

## Architecture Overview

### Four Planes

```
DEVELOPER PLANE   — CLI (TypeScript) ↔ API (Go)
CONTROL PLANE     — mgmt cluster: 8 operators · Gitea · PG · MinIO · ArgoCD · Traefik · cert-manager · Ory Kratos · Ory Hydra · Redis
BUILD PLANE       — build cluster: Argo Workflows + BuildKit + cosign signing
WORKLOAD PLANE    — runtime cluster: user apps via ArgoCD · Traefik + cert-manager + KEDA
```

### Deploy Flow (Happy Path)

```
loopzero deploy → CLI validates → upload bundle → API locks env → submit Argo Workflow
  → build (BuildKit) → cosign sign → push image → generate manifests → push to Gitea (gitops)
  → ArgoCD sync → app running → SSE event → CLI shows URL
  (static apps: build → upload S3 → write CF KV → CF Worker serves from DO Spaces)
```

### CRDs (10 total)

Wired (✅): Tenant, Environment, LoopZeroApplication, ExposedRoute, PlatformBootstrap, GitRepositoryBinding, ClusterCredential.
Wired (✅): PreviewEnvironment — namespace + NetworkPolicy (deny-all/allow-ingress/allow-egress) + ResourceQuota + LimitRange + TTL + condition-wait flow (BuildSubmitted→BuildCompleted→WorkloadHealthy with grace period) + Failed transition + cleanup retry/CLEANUP_FAILED + finalizer + canonical naming via `naming.PreviewNamespace`. 12 envtest specs covering full lifecycle. E2E live validated against k3d.
Defined but unwired (❌): AppSecret (CRD only, no controller — ESO works directly), ReconciliationReport (placeholder).

### State Machines (9 in `pkg/statemachine/`)

| Machine | Flow | Timeouts |
|---------|------|----------|
| Build | QUEUED → RUNNING → RELEASING → SUCCEEDED/FAILED/CANCELLED | QUEUED>5m, RUNNING>30m, RELEASING>10m, CANCELLING>60s |
| Release | READY → PENDING → ACTIVE → SUPERSEDED/ROLLED_BACK/CANCELLED | READY>5m, PENDING>2m |
| Tenant | Provisioning → Active → Suspended → Deleting | — |
| PlatformBootstrap | Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → Complete | — |
| Preview | Creating → Active → Expiring/Deleting → Deleted | TTL-based |
| Promotion | Validating → Committing → Syncing → Completed | — |
| Rollback | Validating → Reverting → Syncing → Completed | — |
| Domain | pending_ns → ns_active → provisioning → active | — |
| Environment | Creating → Active → Deleting → Deleted | — |

### Production Infrastructure (DigitalOcean)

3 DOKS clusters (v1.32.13, 2 nodes, SFO3) · PostgreSQL 16 managed · DO Container Registry (`registry.digitalocean.com/loopzero-infra-registry`) · CF Tunnel (zero public IPs, all runtime via CF edge) · CF Worker `loopzero-static-delivery` · CF KV `loopzero-routing` · CF Zone Mgmt for custom domains · GitHub App `LoopZero Platform` (ID 3319955) · DO Spaces `loopzero-static-assets` + CDN · State backend `s3://loopzero-infra-pulumi-state`.

---

## Known Systemic Patterns

| Pattern | Mitigation |
|---------|-----------|
| "Implemented but not wired" | Verify call site after implementation; integration test required |
| "Correct code in wrong place" | Single source of truth per behavior; grep duplicates before merge |
| "Project name vs project ID" | CLI sends project name as `project_id` in builds. Use name (not UUID) for build/release queries. Only `projects` table uses UUID. |
| "ArgoCD `notifiers` not `services`" | argo-cd Helm v7.x: use `notifications.notifiers`, `services` silently ignored |
| "Single ArgoCD App per tenant" | One App per environment. Any Degraded resource blocks ALL notifications. `gitops.Remove` failure is FATAL. |
| "Implemented but not documented" | Code is source of truth. Verify before claiming missing. |
| "Custom domains = CF Zone per domain" | v2: CF Zone Mgmt, NS change by user, no cert-manager Certificate (CF handles TLS). State: `pending_ns → ns_active → provisioning → active`. |
| "CF scan imports conflicting apex" | v2 create flow deletes conflicting A/AAAA/CNAME at apex BEFORE creating routing CNAME |
| "GitHub tarball ≠ git bundle" | autodeploy.go: tarball → extract → git init/add/commit → bundle → S3 |
| "Auto-deploy source = commit SHA" | Key `{tenant}/source/{commitSHA}.bundle`. CommitSHA from webhook must match exactly. |

---

## Common Mistakes — Read Before Coding

| Mistake | Fix |
|---------|-----|
| `pg_advisory_lock` (session-scoped) | Use `pg_advisory_xact_lock` |
| Concurrent build check OUTSIDE lock | Move inside `withEnvironmentLock` |
| `errors.New("...")` | Use `LoopZeroError` with typed code |
| `release.Status.State = "active"` | Use `release.Transition(event)` |
| `kubectl` in user-facing flow | Proxy through API |
| Mark release ACTIVE before ArgoCD confirms | Wait for sync confirmation |
| Internal terms in CLI output | Product language (Rule 3) |
| `notifications.services` in ArgoCD Helm | Use `notifications.notifiers` |
| `project.ID` (UUID) for build/release queries | Use project name from `loopzero.yaml` |
| `gitops.Remove` error treated non-fatal | FATAL — delete must fail |
| `GetActiveByEnvTx` without ORDER BY | Add `ORDER BY created_at ASC LIMIT 1` |
| cert-manager Certificate for CF-managed domains | Skip Certificate CR (use `CFManaged`) |
| `react`/`vue`/`svelte` not in `resolveLanguageStrategy()` | Map to `node` strategy in `build_plan.go` |
| Forward-only migration violations (`DROP`) | Explicit sign-off required |
| Writing prod code before failing test | Test first, always |
