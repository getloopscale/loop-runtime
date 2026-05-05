# CLAUDE.md

Contract between Claude and the LoopZero codebase. Read before touching any file.

## What This Project Is

LoopZero is a **CLI-first Kubernetes PaaS** for monorepo deployments. Vercel-quality DX for backends — `loopzero login`, `loopzero init`, `loopzero deploy`, and in under 5 minutes every service is live with TLS, preview environments on every PR, and rollback with one command. No Kubernetes. No infrastructure YAML.

The authoritative specification is **`docs/cli-reference.md`**. If this file and the system design conflict, the system design wins.

**Status:** Pre-release, Starter + Pro tiers launchable (98% confidence). **Maturity 3.9 / 5.0** (was 3.8; +0.1 from cell-2-activation-plan + fix-all-platform-issues-plan, 2026-05-03). 25/30 E2E tests pass. **Local cleanroom dogfood 12/12 GREEN end-to-end** (CHG-2026-05-05-DOGFOOD-CLEANROOM, tag `dogfood-cleanroom-converged-20260505`, 2026-05-05): `bash infra/scripts/dogfood-cleanroom-loop.sh` destroys all 4 k3d clusters → `task setup` + `task k3d:heal` + tilt CI → preflight 16/16 + 11 dogfood journeys (solo-deploy, multi-server, secrets, scale, promote, rollback, custom-domain, github-autodeploy, preview, failures, admin) all PASS in ~33min total without manual intervention. 39 systemic bugs B1-B39 identified + canonically fixed in 29 iterations. Sprints 0-8 (partial), 10, 11 complete. Sprint 6 (security + supply chain) — **fully shipped** (CHG-2026-04-26-SPRINT6-IMPL): cosign + syft SBOM + policy-controller deny-by-default + ClusterImagePolicy `loopzero-images-warn` shipped as Helm chart. Deny-flip + cosign keyless verification pending Sprint 9. Sprint 5 (preview envs) — **fully shipped** (CHG-2026-04-26-PREVIEW-IMPL). Sprint 8 Phase -1 + Phase 0 + Phase 2-subset GREEN (CHG-2026-04-27); Phase 1+3+4+5+6 deferred to CHG-002b/c. Sprint 9 not started. Hardening H1-H7 complete. KEDA scale-to-zero done. **Operator multi-binary split shipped** (CHG-2026-04-30-INFRA-PARITY): 4 independent binaries (tenant-operator, app-operator, preview-operator, platform-operator), each with own Helm chart, RBAC, Dockerfile, leader election. **Multi-cell infra-normalize shipped** (CHG-2026-04-30-INFRA-NORMALIZE): `cells` table (migration 000006), 4 cell metrics + 7 alerts, `loopzero admin tenants pin`, ClusterCredential bootstrap, validation matrix (10 offline gates), 2 new CI workflows (cli-commands, multi-cell). **Cell-2 ACTIVATED + fully validated LIVE** (cell-2-activation-plan + fix-all-platform-issues-plan, 2026-05-03): migration 000007 active, 12/12 ADR gates GREEN persistente, T4.3 E2E `loopzero deploy` → Pod Running on loopzero-runtime-2 + /health 200 + cell-1 NotFound (isolation), Stage 1 1h ADR-compliant 120 probes 0 5xx, EnvironmentReconciler cell-aware via `CellAwareRuntimeClientFactory` (covers monolithic + split-binary), heal H14+H15 (cell-2 ClusterCredential refresh+bootstrap + ArgoCD bearer token sync, idempotent), application reaper for orphan CRs (state-machine-driven via `ApplicationOrphaned` event → terminal `Cleaned`), migration 000008 ARCHIVED status (64 stale records archived), CC controller reorder eliminating token-propagation gap, mgmt-argocd duplicate env var fix, release reaper observability via `AppendSideEffect`. CI cron weekly armed (cell2-soaks.yml + platform-health-7day-watch.yml). **Layered bootstrap shipped** (CHG-2026-04-28-LAYERED-BOOTSTRAP): Pulumi L1-only + Helmfile L2 + ArgoCD L3 App-of-Apps. **Full dogfood validated** (CHG-2026-04-29): 13 iterations, 102+ bugs found/fixed, `loopzero deploy` end-to-end CLI→API→Argo→BuildKit→Gitea→ArgoCD→workload confirmed. 1171+ CLI tests + 41 Go packages + 200+ envtest specs + 269+ pulumi vitest specs passing. Production on DigitalOcean (4 DOKS + Loki + ESO + CF Tunnel + DO Spaces CDN + Valkey managed + Harbor). **Regression matrix: 166 entries, 0 critical miss, audit GREEN.**

**Pricing:** Starter (Free, 1 project), Pro ($29/mo, 3 projects), Team ($79/mo, 10 projects), Enterprise (custom).

**Completed:** deploy happy path (full E2E dogfood validated), scaling+HPA (Starter=2/Pro=5/Team=10), promote staging→prod, rollback, secrets via ESO, custom domains v2 (CF Zone Mgmt, model Vercel), GitHub App auto-deploy (~4min), monorepo validator (30 frameworks), cosign image signing + syft SBOM (CycloneDX), supply-chain admission (policy-controller deny-by-default + warn-mode ClusterImagePolicy + digest enforcement in manifests), plan enforcement, audit log, RSA-only JWT auth, two-host gateway, concurrent deploy rejection (1×202+1×409), JSON contracts (8/8), observability (43+ loopzero_* metrics + 4 cell metrics + Grafana + SLO alerts + 7 multi-cell alerts), KEDA scale-to-zero, cell architecture (Phase 0+1+1.5 + cells table + admin pin), **cell-2 ACTIVATED + LIVE-validated** (3/3 cells connected; namespace cell-aware reconcile; rollback SLO P50=648ms / P95=705ms <<300s budget; 12 ADR gates persistente), operator multi-binary split (4 binaries), layered bootstrap (Pulumi L1 + Helmfile L2 + ArgoCD L3), `loopzero admin tenants pin/evict`, **application reaper** for orphan CRs (`ApplicationCleaned` terminal state via `ApplicationOrphaned` event), **release reaper observability** (`ReleaseStore.AppendSideEffect` writes reaper context to `release.side_effects`), **release ARCHIVED status** (migration 000008, partial index excludes archived from active queries).

**Not built:** SSO/SAML, Loki cross-cluster streaming (infra gap, not code), cosign keyless verification with Fulcio (pending Sprint 9), Sprint 8 Phase 1+3+4+5+6 (require new T3 meeting), production canary stages 4-5 for cell-2 (1 customer 7-day → 10/50/100% — pending Sprint 9 readiness review per ADR D6 of cell-2-activation-plan).

**`loopzero scale` — partially functional** (B36 partial, 2026-05-05): handler persists to `app_scaling_config` table via `PostgresScalingStore` (`api/cmd/setup_orchestrator.go:144`) and returns `applied`/`no_change`. **GAP:** the manifest renderer (`api/internal/services/build_orchestrator_submit.go:114` → `manifests/renderer_deployment.go:114`) reads `Replicas` from `BuildAppSpec` populated from the request body (i.e., `loopzero.yaml`), NOT from `app_scaling_config`. Result: `loopzero scale --replicas N` writes the DB row but the next `loopzero deploy` ignores it and renders `Deployment.spec.replicas` from `loopzero.yaml`. The `app_scaling_config` table is currently orphan from the deploy path. Workaround: edit `loopzero.yaml` and `loopzero deploy`. Plan T2.3 (renderer integration with scaling store) was not shipped despite being declared so. Reference: B40 candidate in cleanroom retrospect.

**Path to maturity 4.0** (currently 3.9): (a) first CI Sunday cron GREEN producing real `cell2-soaks.yml` workflow run evidence, AND (b) 1 prod customer day-trial with no incidents (Sprint 9 prod canary). Local 7-day GREEN streak DROPPED as a gate (D7 reformulated 2026-05-03 — tautological in static dev env without diurnal traffic; CI cron + real prod usage are the legitimate signals).

**Known issues:** AppSecret controller not implemented (ESO works directly, no lifecycle automation); Pulumi L1 refactored (CHG-2026-04-28-LAYERED-BOOTSTRAP removed `@pulumi/kubernetes`, 33 L2 files deleted) but consolidation of remaining env vars pending (GitHub App envFrom, CF_ZONE_MANAGEMENT, PG keepalive); local k3d cert-manager letsencrypt-prod cannot issue for `*.loopzero.local` (no public DNS for ACME challenge) — production uses CF-managed TLS so no impact, but local Stage 1 canary HTTP probes report HTTP 000 in `--smoke` mode (probes informational only).

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

cd cli && npm test       # CLI (Jest, 1171+ tests)
cd api && go test ./...  # API (41 packages)
cd operators && go test ./...  # Operators (Ginkgo + envtest)
cd pkg && go test ./...  # Shared libs

task db:migrate          # Forward-only migrations
task db:reset            # Drop + recreate + migrate + seed

cd infra/pulumi && npx pulumi up --stack dev      # Deploy to DO
cd infra/pulumi && npx pulumi preview --stack dev # Preview
```

**Prerequisites:** Go 1.25+, Node.js 20+, Docker, k3d, Tilt, Task, Helm v3.14+, **helm-diff plugin** (`helm plugin install https://github.com/databus23/helm-diff`), **Helmfile v1.0+** (CHG-2026-04-28-LAYERED-BOOTSTRAP T0.3 — orchestrator L2), kubectl, kubeconform (`go install github.com/yannh/kubeconform/cmd/kubeconform@latest`), golangci-lint v2.11+, cosign (optional, image signing)

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
| IaC | Pulumi (TS) — L1-only (DO + CF providers, zero K8s resources); Helmfile L2 (bootstrap); ArgoCD L3 (Day-2 GitOps) |
| Local clusters | k3d × 3 (mgmt, build, workload) |
| Dev loop | Tilt + Taskfile (2296 lines) |
| Helm | 33 bootstrap charts (`infra/helm/`) + 15 published charts (`charts/`) |
| CI/CD | GitHub Actions (19 workflows) |
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
| 6 | Security + Supply chain | ✅ cosign + syft SBOM + policy-controller installed (CHG-2026-04-26-SPRINT6-IMPL). policy-controller is **deny-by-default**; warn-mode `loopzero-images-warn` ClusterImagePolicy is required (B16). As of CHG-2026-04-28-LAYERED-BOOTSTRAP T0.1 the policy is shipped as a Helm chart in `infra/helm/admission-policies/` (templates/cluster-image-policy.yaml). Deny-flip + cosign keyless verification pending Sprint 9 with Fulcio. |
| 7 | Observability | ✅ (H4) |
| 8 | Hardening + Chaos | 🟡 Phase -1 + Phase 0 + Phase 2 GREEN (CHG-001 + CHG-002a, MEET-060 + MEET-061, ADR-0015 + ADR-0016). Phase 1+3+4+5+6 → CHG-002b/c new meeting required |
| 9 | Polish + Launch | ❌ |
| 10 | Static Frontend Delivery | ✅ |
| 11 | Auth + Billing | ✅ |
| H1-H7 | Hardening (runtime, TLS, PG HA, observability, pipeline, ops, workload security) | ✅ all complete |
| Cleanroom Dogfood Loop | Real PaaS test: destroy infra + setup + 12 journeys end-to-end | ✅ 12/12 GREEN (2026-05-05, tag `dogfood-cleanroom-converged-20260505`). 39 bugs B1-B39 fixed canonically. See `docs/learnings/cleanroom-iter28-29-learnings-20260505.md` + `docs/reports/dogfood-cleanroom-20260505-progress.md`. |

**E2E Validation (2026-04-23):** 25/30 pass (83%). Confidence: deploy 99%, scaling 95%, promote 95%, rollback 99%, concurrent rejection 99%, JSON contracts 99%, internal terms (Rule 3) 99%. Log streaming 60% (infra gap, not code).

**Maturity:** 3.7/5 (was 3.6 pre-Sprint-8; +0.1 from Sprint 8 v3.2 Phase -1 + Phase 0 + Phase 2-subset GREEN: bootstrap reproducibility via ADR-0015 adopt-or-create + 4 runbooks + 42 tests + CI gates; PG max_connections + mgmt memory + Chaos Mesh + Gitea HA + Harbor HA + Valkey externo all delivered with runtime evidence; mgmt memory 36-39% met ADR-0010 #6 target). Path to 4.0: CHG-002b L3 stabilization + CHG-002c Gate 2a Exit + Cell-2 activation + 1 external early adopter. Full: `docs/maturity-assessment.md`.

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

### Helm Chart Dual System

Two chart systems coexist with distinct purposes (ADR D1, bootstrap-integrity-fixes-plan):

| System | Path | Purpose | Used by |
|--------|------|---------|---------|
| **Bootstrap charts** | `infra/helm/` | Lightweight, inline values, direct install | Helmfile L2 (`infra/helmfile/`) |
| **Published charts** | `charts/` | OCI registry, loopzero-common library, full RBAC/monitoring | ArgoCD L3 (Day-2 GitOps), CI (`helm-charts.yml`) |

- `infra/helm/` is canonical for `helmfile sync` (bootstrap). Charts are simple, standalone, with hardcoded defaults.
- `charts/` is canonical for OCI publishing and ArgoCD. Charts use `loopzero-common` library for consistent labels and security contexts.
- Changes to shared templates must be propagated to both systems.
- The operators are deployed as **4 split binaries** (ADR-0023, plan `operators-sota-faang-grade-plan.md` Phase 1, 2026-05-04 LOCAL): `loopzero-tenant-operator`, `loopzero-app-operator`, `loopzero-preview-operator`, `loopzero-platform-operator`. Each has its own `operators/cmd/<name>/main.go`, Dockerfile, Helm chart in `charts/`, ServiceAccount + ClusterRole + leader-election Lease, and only mounts the env vars its controllers need. The legacy monolith chart `infra/helm/tenant-controller` and the redundant `infra/helm/loopzero-controller` were deleted in T1.5; production rollout is governed by separate `CHG-NNNN-OPERATOR-SPLIT-PROD` per ADR-0023's 4-stage cutover.

### Production Infrastructure (DigitalOcean)

3 DOKS clusters (v1.32.13, 2 nodes, SFO3) · PostgreSQL 16 managed · DO Container Registry (`registry.digitalocean.com/loopzero-infra-registry`) · CF Tunnel (zero public IPs, all runtime via CF edge) · CF Worker `loopzero-static-delivery` · CF KV `loopzero-routing` · CF Zone Mgmt for custom domains · GitHub App `LoopZero Platform` (ID 3319955) · DO Spaces `loopzero-static-assets` + CDN · State backend `s3://loopzero-infra-pulumi-state`.

### Multi-Cell Routing

Tenant workloads run on independent K8s clusters called **cells** (`cell-default`, `cell-1`, `cell-2`, ...). Routing is **placement once, route many** — not load-balanced. Local k3d state validated 2026-05-01: 3/3 ClusterCredentials connected; DynamicRouter `count=3`.

**Lifecycle of a deploy:**

1. **`loopzero bootstrap` (NEW tenant) — placement decision (one-time).** `RoundRobinEngine.AssignAndExecute` (`api/internal/placement/engine.go:179`) runs inside a SERIALIZABLE transaction with `pg_advisory_xact_lock`. Algorithm: bin-packing — pick the cell with FEWEST tenants AMONG those not at `maxPerCell` capacity (default 500). Writes `tenants.cell_id` (canonical SoT) + Tenant CR `spec.cellID`. Sticky thereafter.
2. **`loopzero deploy` / `promote` / `rollback` / `logs` / `status` — routing (every call).** Reads `tenants.cell_id` from PG. `DynamicRouter.Resolve(cellID)` (`api/internal/placement/dynamic_router.go:133`) is pure hash lookup — zero decision. Returns `CellConfig {kubeconfig, ArgoCD URL/token, base domain}`. Used by `cellmgr/client_manager.go:85` (kubectl) and `argocd/sync_client_factory.go:72` (sync).
3. **`loopzero admin tenants pin --tenant X --cell Y` — operator override (only path to move tenant).** `CellPinner.Pin` (`api/internal/services/cell_pinner.go`): DB UPDATE with capacity gate + Tenant CR MergePatch atomically. Operator reconcile re-emits ArgoCD App+AppProject with `destination.name=Y`. ArgoCD resolves via cluster Secret label.

**Key SoT separation:**
- `tenants.cell_id` PG column — canonical placement decision
- `cells` SQL table — operational metadata (capacity, status active/draining/maintenance/retired, region, audit)
- `ClusterCredential` CRD on mgmt cluster — connectivity (kubeconfig Secret + spec.cellID) — `K8sCellStore.LoadCells()` polls every 60s and **looks up by `spec.cellID`, NOT CR name** (per MEET-053 fix B9)

**Self-healing (`task k3d:heal` codifies):**
- **H12** (matrix entry 153) — cell-1 ClusterCredential missing despite cells SQL active: bootstraps via `clustercredential-create.sh cell-1 workload <scoped-kc>`. Cluster-name=`workload` matches `k3d-loopzero-workload-server-0` docker container (NOT `runtime` — that container doesn't exist locally; B82 fallback would render unreachable).
- **H13** (matrix entry 154) — cell-default SA token rotated/invalidated (401 even with exp claim valid): `kubectl create token loopzero-system/default --duration=24h` + patches Secret `data.kubeconfig` preserving CA + in-cluster server URL.
- Both pinned via vitest in `infra/pulumi/src/__tests__/heal-script-H12.test.ts` (19 invariants).

**Why no automatic rebalancing** (ADR-0008 + ADR-0021 D3):
- DNS continuity (endpoints `*.{tenant}.loopzero.local` cravado em cluster origem)
- State affinity (Secrets/ESO + S3 bundles + ArgoCD App+AppProj)
- Hot-spot signal = upgrade tier signal, NOT migration signal
- Auditability (`loopzero admin tenants pin` writes audit_log; auto-rebalance would be opaque)

Cell drain (`cells.status='draining'`) is operator-driven — `loopzero admin tenants pin --to cell-novo` per tenant. See `docs/runbooks/multi-cell-canary.md` (dev) + `multi-cell-canary-prod.md` (prod stages 4/5a/5b/5c).

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
| Multi-cell ClusterCredential CR name = cellID assumption | Wrong — `K8sCellStore` lookup is by `spec.cellID`, not metadata.name. CR named `workload` with `spec.cellID=cell-1` works fine (per H12). |
| `wire-clusters.sh` with `cluster-name=runtime` for k3d | Wrong — local cluster is `loopzero-workload`. Use `cluster-name=workload` so `scoped-kubeconfig.sh` resolves `k3d-loopzero-workload-server-0` docker container (B82 + H12). |
| Cached SA token in ClusterCredential Secret stops working before exp claim | SA UID changed (recreated during rollout). Mint fresh: `kubectl create token loopzero-system/default --duration=24h` + patch Secret data.kubeconfig (per H13). |
| ArgoCD `InvalidSpecError: project X does not exist` (but project EXISTS) | Repo-server cached `app path does not exist` for ~5min. `SyncClient.TriggerSync` calls `?refresh=hard` BEFORE POST /sync (B32). |
| VAP `failurePolicy: Fail` deadlocks namespace deletion | Add `matchConditions` excluding `system:serviceaccount:kube-system:namespace-controller`, `system:kube-controller-manager`, `system:masters` group (B33). |
| `secret K8s manager not available — ESO sync disabled` in api logs | `RUNTIME_KUBECONFIG` not mounted. Mount `loopzero-cluster-kubeconfigs` Secret (key=`runtime.kubeconfig`) + export `RUNTIME_KUBE_CONTEXT=in-cluster` (B34). |
| `seed-local-user.sh failed during regen` mid-journey | `HYDRA_NAMESPACE=loopzero-auth` (prod default) ≠ `loopzero-system` (k3d standalone). Set `SEED_HYDRA_NS=loopzero-system` in `_common.sh` (B35). |
| `releases_status_check` constraint violation on rollback | DB CHECK missing `ROLLING_BACK`/`ROLLED_BACK`. Migration `000010_release_rollback_states.up.sql` adds them. State machine declares it but DB CHECK was never updated (B37). |
| Admin endpoints return 404 `Endpoint not available` despite valid X-Service-Auth | `LOOPZERO_ADMIN_API=true` env var missing (defense-in-depth flag). Production sets via Pulumi; standalone Tilt YAML must include it (B38). |
| `kubectl rollout restart` is non-blocking — race with downstream operations | Always pair with `kubectl rollout status` if subsequent commands trigger reconciles in old/new pod. Old pod (env stale) reconciles BEFORE termination, sobrescreve writes (B39). |
