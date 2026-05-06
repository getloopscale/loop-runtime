<p align="center">
  <h1 align="center">LoopZero</h1>
  <p align="center">
    <strong>CLI-first Kubernetes PaaS for monorepo deployments.</strong>
  </p>
  <p align="center">
    <a href="#project-status">Status</a> &bull;
    <a href="#quickstart">Quickstart</a> &bull;
    <a href="#self-hosted">Self-Hosted</a> &bull;
    <a href="#cli-reference">CLI</a> &bull;
    <a href="#architecture">Architecture</a> &bull;
    <a href="#development">Development</a>
  </p>
</p>

---

Three commands to start. Then just `git push`.

```bash
loopzero login
loopzero init        # detect monorepo, generate loopzero.yaml
loopzero connect     # link GitHub repo (one-time)
git push         # every push deploys automatically
```

```
$ git push origin main

# LoopZero deploys automatically:
  Building api...    [24s]
  Building web...    [18s]
  Deploying...       [15s]

Live:
  api    -> https://acme-my-saas-api-production.loopzero.example
  web    -> https://acme-my-saas-web-production.loopzero.example (CDN)
```

No Kubernetes knowledge required. No Helm. No Dockerfile required. Just push.

---

## Project Status

> **Phase: Pre-release.** Production infrastructure running on DigitalOcean (4 DOKS clusters, managed PG, CF Tunnel). Core deploy flow E2E validated. **Multi-cell ACTIVATED + LIVE-validated** (cell-default + cell-1 + cell-2). **Local cleanroom dogfood 12/12 GREEN end-to-end** (2026-05-05). Not yet open to external users.
>
> **Maturity: 3.9/5** -- strong technical foundation, commercially unvalidated. (was 3.6 pre-cell-2; +0.3 from cell-2-activation-plan + fix-all-platform-issues-plan, 2026-05-03.) See [full assessment](docs/maturity-assessment.md).
>
> **Local validation gate**: `bash infra/scripts/dogfood-cleanroom-loop.sh` destroys all 4 k3d clusters and re-runs `task setup` + `task k3d:heal` + tilt + 11 dogfood journeys + preflight, all PASS in ~33min without manual intervention. 39 systemic bugs (B1-B39) caught and canonically fixed across 29 iterations. Tag: `dogfood-cleanroom-converged-20260505`. Plan: [`docs/plans/real-paas-cleanroom-dogfood-loop-plan.md`](docs/plans/real-paas-cleanroom-dogfood-loop-plan.md). Final report: [`docs/reports/dogfood-cleanroom-20260505-progress.md`](docs/reports/dogfood-cleanroom-20260505-progress.md). Learnings: [`docs/learnings/cleanroom-iter28-29-learnings-20260505.md`](docs/learnings/cleanroom-iter28-29-learnings-20260505.md).
>
> **Path to 4.0**: (a) first CI Sunday cron GREEN producing real `cell2-soaks.yml` 1h+24h workflow run evidence + (b) 1 prod customer day-trial with no incidents (Sprint 9 prod canary stage 4). Local 7-day GREEN streak NOT a gate (tautological in static dev env without diurnal traffic; CI cron + real prod usage are the legitimate signals — D7 reformulated 2026-05-03).

### What Works (E2E Validated on DigitalOcean)

| Capability | Evidence |
|------------|----------|
| `loopzero login` / `logout` / `whoami` | Device Code Flow + Ory Hydra OIDC |
| `loopzero init` (monorepo detection) | Turbo/Nx/Lerna/pnpm + 30 frameworks |
| `loopzero deploy` (full pipeline) | CLI -> upload -> BuildKit -> cosign sign -> ArgoCD sync -> live URL (~4min) |
| `loopzero deploy` (static frontends) | S3 upload + CF Worker KV routing + DO Spaces CDN |
| `loopzero rollback` | Instant revert to previous release (~3s) |
| `loopzero promote --from staging --to production` | Cross-environment promotion (HTTP 202, no rebuild) |
| `loopzero logs` | Loki cross-cluster streaming |
| `loopzero status` | Project status with active release, URL, scaling info |
| `loopzero secret set/list/delete` | ESO sync <30s, values never exposed in CLI output |
| `loopzero cancel` | Cancel running/queued builds |
| `loopzero project create/list/inspect/delete` | Full CRUD with clean resource removal |
| `loopzero env create/list/inspect/delete` | Multi-environment with protected production |
| `loopzero team list/add/remove/role` | RBAC: owner/admin/member roles |
| `loopzero build list/inspect` | Build history with env filter |
| `loopzero domain add/status/dns/list/remove` | CF Zone Management -- apex + subdomain, WAF/DDoS, auto TLS |
| `loopzero connect` / `disconnect` | GitHub App auto-deploy on push (~4min E2E) |
| `loopzero audit` | Audit log with action/timestamp filters |
| `loopzero validate` | Local loopzero.yaml validation |
| `loopzero scale` | **Fully functional E2E** (B40 fixed 2026-05-06). API persists to `app_scaling_config` (project NAME), `ScalingResolver` reads it during deploy submit, `enforceScaling` validates plan caps on resolved config (closes MEET-2026-04-23-059 Constraint 6 hole), renderer emits `Deployment.spec.replicas` matching DB. Validated via journey-04 with kubectl assertion + cleanroom 12/12 GREEN. Tag: `scale-b40-converged-20260506`. |
| Plan enforcement (Starter/Pro/Team) | Limits on projects/builds/environments/members/replicas |
| Concurrent deploy protection | `pg_advisory_xact_lock` -> exactly one 202, others get 409 |
| Idempotency | Retry-safe via `Idempotency-Key` header |
| Image signing (cosign) | Every container image signed post-build |
| Scale-to-zero (KEDA) | Manifest generation + Helm charts + Pulumi bootstrap (cluster KEDA pending) |
| Cell architecture (3 cells LIVE) | PlacementEngine, DynamicRouter (`loopzero_cell_discovery_count=3`), cell-scoped queries, leader fencing, build admission gate, PG work queue, capacity tests. **Cell-2 ACTIVATED 2026-05-03**: tenant pin via `loopzero admin tenants pin --cell cell-2` → CR MergePatch + ArgoCD destination flip → workload Pod Running on loopzero-runtime-2. 12/12 ADR gates LIVE GREEN persistente. Rollback SLO P50=648ms / P95=705ms (<<300s budget). EnvironmentReconciler cell-aware via `CellAwareRuntimeClientFactory` covers monolithic + split-binary builds. Heal H14+H15 codify cell-2 ClusterCredential refresh+bootstrap + ArgoCD bearer token sync. Production stages 4-5 (1 customer 7-day → 10/50/100%) pending Sprint 9 readiness review. |
| SecurityContext (non-root pods) | `runAsNonRoot`, `drop: ALL`, `readOnlyRootFilesystem` |
| PodSecurity Admission | `restricted` profile on all tenant namespaces |
| NetworkPolicy | Mandatory deny-all + explicit allow rules |
| Observability | 43 Prometheus metrics + Grafana dashboards + 4 SLO alert rules |
| Auth hardened | UUID tenant IDs, RSA-only JWT, audience validation, secret rotation |
| JSON contracts | 8/8 commands validated, 0 internal term leaks |

### What's Not Built Yet

| Feature | Impact | Notes |
|---------|--------|-------|
| ~~Preview environments~~ | ~~High~~ | ✅ Shipped 2026-04-26 (Sprint 5 / CHG-2026-04-26-PREVIEW-IMPL). All 14 plan tasks done, E2E validated against live k3d (12 specs pass: PR opened→preview, synchronize→UPSERT, concurrent PRs, PR closed→cleanup + 8 CLI specs). |
| ~~Cell-2 activation (multi-cell dev)~~ | ~~Low~~ | ✅ Shipped 2026-05-03 (cell-2-activation-plan + fix-all-platform-issues-plan). 12/12 ADR gates LIVE GREEN persistente. T4.3 LIVE: `loopzero deploy` → Pod Running on loopzero-runtime-2, `/health` 200, cell-1 NotFound (isolation). |
| Cell-2 production canary (stages 4-5) | Medium | Stage 4: 1 customer 7-day. Stage 5: 10/50/100% with 7-day holds. Pending Sprint 9 readiness review per ADR D6. CI cron `cell2-soaks.yml` armed for ADR-compliant 1h+24h Sunday runs. |
| ~~SBOM generation (syft)~~ | ~~Medium~~ | ✅ Shipped (Sprint 6). Per-release CycloneDX manifest persisted in S3 with presigned URL endpoint. |
| Admission policy DENY mode | Medium | warn-mode shipped (Sprint 6). Deny-flip pending Sprint 9 with cosign keyless verification + Fulcio. |
| ~~`loopzero scale` end-to-end persistence~~ | ~~Medium~~ | ✅ **Shipped (B40 fix, 2026-05-06).** ScalingResolver in build_orchestrator_submit closes the chain. Plan-cap on resolved. Cleanroom 12/12 GREEN. Tag `scale-b40-converged-20260506`. |
| Chaos engineering suite | Medium | Sprint 8 -- not started. |
| Polish + onboarding (< 10 min) | High | Sprint 9 -- not started. |
| SSO/SAML | Low | Phase 3. |

### Pricing Tiers

| Tier | Price | Projects | Builds/mo | Max Replicas |
|------|-------|----------|-----------|-------------|
| Starter | Free | 1 | 50 | 1 |
| Pro | $29/mo | 2 | 300 | 4 |
| Team | $79/mo | 5 | 600 | 10 |
| Enterprise | Custom | Unlimited | Unlimited | Unlimited |

### Test Suite

| Component | Count | Status |
|-----------|------:|--------|
| CLI (Jest) | 1,210+ | Passing |
| API + Shared libs (Go) | 41 packages | Passing (multi-cell: placement determinism with -race + admin_pin_cell C11 authz + migration_007/008 invariants + reaper observability) |
| Operators (Ginkgo + envtest) | 200+ specs | Passing (+ application reaper 10 fake-client tests + cell-aware factory 7 tests, no envtest dependency) |
| Pulumi (vitest) | 310+ specs | Passing (heal-script H14 24 invariants + migrations-forward-only 11 invariants) |
| State machines | 10 application + 9 release + 7 build | Passing (`ApplicationCleaned` terminal state via `ApplicationOrphaned` event added 2026-05-03) |
| CI pipeline | 29 workflows | Green (incl. `cell2-soaks.yml` weekly Sunday 02:00 UTC + `platform-health-7day-watch.yml` hourly) |
| Regression matrix | 166 entries | 0 critical miss, audit GREEN |
| Cleanroom dogfood (E2E) | preflight + 11 journeys | **12/12 GREEN** end-to-end on fresh `k3d` destroy/setup cycle (2026-05-05, tag `dogfood-cleanroom-converged-20260505`). Validates: solo deploy, multi-server (api+worker), secrets via ESO, scale persistence, promote staging→production, rollback v2→v1 with state machine, custom domain register/list/status/remove, GitHub connect/disconnect, preview list/inspect/delete, failure modes (auth/cap/build), admin (evict dry-run + service-auth gating). Total: ~33min. Rule 3 audit: 0 internal-term matches in CLI output. |

---

## Why LoopZero

You have a monorepo with an API, a frontend, and workers. Vercel handles the frontend -- but the rest requires Dockerfiles, Helm charts, and CI pipelines that only one person understands.

LoopZero gives you Vercel-quality DX for everything Vercel can't deploy.

| | Vercel | LoopZero |
|---|:---:|:---:|
| Auto-deploy on `git push` | Yes | Yes (~4min) |
| Static frontends (React, Vite) | Yes | Yes (CDN via CF Worker) |
| API servers (Express, Go, Python) | Serverless only | Any framework (30 supported) |
| Background workers | -- | Yes |
| Multiple services per repo | -- | Yes (monorepo-native) |
| Rollback in seconds | Yes | Yes (~3s) |
| Promote across environments | -- | Yes (no rebuild) |
| Secrets per environment | Yes | Yes (encrypted, ESO sync) |
| Custom domains (apex + subdomain) | Yes | Yes (CF Zone, WAF/DDoS) |
| Preview environments per PR | Yes | Yes (Sprint 5 fully shipped — PR open/synchronize/close webhooks → preview lifecycle, 12 E2E specs pass live) |
| Scale-to-zero | Yes | Yes (KEDA, pending cluster setup) |
| Image signing | -- | Yes (cosign) |
| CLI-first | Yes | Yes |

---

## Quickstart

### 1. Authenticate

```bash
loopzero login
```

### 2. Initialize your project

```bash
cd my-monorepo
loopzero init
```

LoopZero detects your monorepo structure and frameworks, then generates `loopzero.yaml`:

```yaml
version: 1
project: my-saas

apps:
  api:
    path: apps/api
    framework: express
    type: server
    port: 3000

  web:
    path: apps/web
    framework: vite
    type: frontend

  worker:
    path: apps/worker
    type: worker
```

### 3. Connect your GitHub repo

```bash
loopzero connect
```

### 4. Deploy

```bash
loopzero deploy
```

TLS, health checks, ingress routing, and zero-downtime rolling updates are automatic. After connecting, every `git push` deploys automatically.

**Scale-to-zero** (optional):

```yaml
apps:
  api:
    path: apps/api
    framework: express
    port: 3000
    autoscaling:
      min: 0        # scale-to-zero when idle
      max: 5
      targetCPU: 80
      cooldown: 300  # seconds before sleeping
```

---

## Self-Hosted

Run LoopZero on your own Kubernetes cluster.

### Prerequisites

- Kubernetes cluster (1.28+) with default StorageClass
- `kubectl` + `helm` v3.14+ + `doctl`

### Install

```bash
loopzero platform install -f customer-values.yaml
```

10-step automated installer: validates cluster, installs all dependencies (CloudNativePG, Gitea, ArgoCD, Argo Workflows, Ory Kratos/Hydra, MinIO, cert-manager, Traefik), bootstraps credentials, runs migrations, and verifies health.

### Management

```bash
loopzero platform status     # Component health, versions, endpoints
loopzero platform upgrade    # Upgrade to new version
loopzero platform uninstall  # Remove platform (preserves data)
```

---

## CLI Reference

### Core Workflow

| Command | Purpose |
|---------|---------|
| `loopzero login` | Authenticate (Device Code Flow) |
| `loopzero init` | Detect monorepo, generate `loopzero.yaml` |
| `loopzero connect` / `disconnect` | Link/unlink GitHub repo for auto-deploy |
| `loopzero deploy` | Build and deploy all apps |
| `loopzero status` | Show project status, active release, scaling, URL |
| `loopzero logs` | Show build logs |
| `loopzero rollback` | Revert to previous release (~3s) |
| `loopzero promote --from staging --to production` | Promote release (no rebuild) |
| `loopzero cancel <build-id>` | Cancel a running build |
| `loopzero validate` | Validate `loopzero.yaml` locally |

### Resources

| Command | Purpose |
|---------|---------|
| `loopzero project list/inspect/create/delete` | Project management |
| `loopzero env list/inspect/create/delete` | Environment management |
| `loopzero team list/add/remove/role` | Team + RBAC management |
| `loopzero secret set/list/delete` | Encrypted secrets (ESO sync) |
| `loopzero build list/inspect` | Build history |
| `loopzero domain add/status/dns/list/remove` | Custom domains (CF Zone) |
| `loopzero scale --app api --min 0 --max 5` | Scaling configuration |
| `loopzero audit` | Audit log |
| `loopzero whoami` / `loopzero logout` | Session management |

Every command supports `--json` for CI/CD integration. JSON field names are a stability contract.

### Exit Codes

| Code | Meaning |
|:----:|---------|
| `0` | Success |
| `1` | Usage error |
| `2` | Build failure |
| `3` | Deploy/operation failure |
| `4` | Authentication error |
| `5` | Partial failure |

---

## Architecture

Four-plane separation:

```
Developer Plane     CLI (TypeScript) <-> API (Go)
Control Plane       Operators, Gitea, PostgreSQL, ArgoCD, Ory Kratos/Hydra
Build Plane         Argo Workflows + BuildKit (rootless) + cosign signing
Workload Plane      User applications managed by ArgoCD + KEDA autoscaling
```

### Multi-Cell Routing

Tenant workloads run on independent K8s clusters called **cells** (`cell-default`, `cell-1`, `cell-2`, ...). Routing is **placement once, route many** — not load-balanced.

```
loopzero bootstrap (NEW tenant)
  -> RoundRobinEngine.AssignAndExecute (PG advisory lock + SERIALIZABLE)
     -> picks least-loaded cell with capacity (default cap=500 tenants)
     -> writes tenants.cell_id (canonical SoT) + Tenant CR spec.cellID
     -> sticky: tenant stays on this cell forever unless operator pins

loopzero deploy / promote / rollback / logs / status
  -> reads tenants.cell_id from PG
  -> DynamicRouter.Resolve(cellID) -> hash lookup, ZERO decision
  -> returns CellConfig {kubeconfig, ArgoCD URL/token, base domain}
  -> ArgoCD client / kubectl client uses that config

loopzero admin tenants pin --tenant X --cell Y    (operator override only)
  -> CellPinner.Pin atomically:
     - DB UPDATE tenants.cell_id (with capacity gate)
     - Tenant CR MergePatch spec.cellID := Y
  -> operator reconcile re-emits ArgoCD App+AppProj with destination.name=Y
```

**Key components:**

| Layer | File | Purpose |
|---|---|---|
| Placement engine | `api/internal/placement/engine.go` | Bin-packing least-loaded; sticky reads |
| Router cache | `api/internal/placement/dynamic_router.go` | Hash lookup `cellID -> CellConfig` |
| Cell store | `api/internal/placement/k8s_cell_store.go` | Polls ClusterCredential CRs every 60s by `spec.cellID` |
| Operator pin | `api/internal/services/cell_pinner.go` | DB UPDATE + Tenant CR MergePatch |
| Self-healing | `infra/scripts/k3d-cross-cluster-heal.sh` (H12+H13) | Auto-recovers cell-1 missing CC + cell-default SA token rotation |

**No automatic rebalancing.** Tenant assignment is sticky to preserve DNS continuity, ESO/MinIO state affinity, and ArgoCD App/Project bindings. Cell drain (`cells.status='draining'`) is operator-driven via `loopzero admin tenants pin --to cell-novo`. See ADR-0008 + ADR-0021 D3.

### Deploy Flow

```
loopzero deploy (or git push)
  -> CLI validates loopzero.yaml + uploads bundle
  -> API locks environment (pg_advisory_xact_lock)
  -> Submit Argo Workflow (BuildKit build + cosign sign)
  -> Generate K8s manifests + push to Gitea (GitOps)
  -> ArgoCD sync -> app running
  -> SSE event -> CLI shows URL

  Static apps: build -> upload DO Spaces -> write CF KV -> CF Worker serves from CDN
```

### Stack

| Layer | Technology |
|-------|-----------|
| CLI | TypeScript + Commander (36 commands) |
| API | Go 1.25 + chi + pgx (connection pool) |
| Operators | Go + kubebuilder + controller-runtime (10 CRDs, 8 controllers) |
| Shared libs | Go -- errors, errors/http, logging, naming, conditions, statemachine, jwt, enforcement, circuitbreaker |
| Auth | Ory Kratos (identity) + Ory Hydra (OAuth2/OIDC) |
| Build engine | Argo Workflows + BuildKit (rootless) |
| GitOps | ArgoCD (manual sync) + Gitea |
| Database | PostgreSQL 16 (CloudNativePG in self-hosted, managed DO in cloud) |
| Autoscaling | KEDA + HTTP Add-on (scale-to-zero) + HPA (min >= 1) |
| Secrets | External Secrets Operator |
| Edge | Cloudflare Tunnel + Worker + KV + Zone Management |
| Static assets | DigitalOcean Spaces + CDN |
| Monitoring | Prometheus + Grafana + Loki + Tempo + OpenTelemetry |
| IaC | Pulumi (TypeScript) -- DigitalOcean + Cloudflare |
| CI/CD | GitHub Actions (29 workflows) |
| Helm charts | 32 bootstrap (`infra/helm/`) + 13 published (`charts/`) |

### Production Infrastructure (DigitalOcean)

| Resource | Configuration |
|----------|---------------|
| 4 DOKS clusters | v1.32, 2 nodes each, SFO3 (mgmt, build, runtime, runtime-2) |
| PostgreSQL | v16, managed |
| Container Registry | DigitalOcean (Professional) |
| Edge proxy | Cloudflare Tunnel (zero public IPs, WAF/DDoS) |
| Static delivery | CF Worker + DO Spaces CDN |
| DNS | `*.dev.loopzero.example` via CF Tunnel |

### Platform Guarantees

- **Serialization** -- No two operations mutate the same environment concurrently (409 on conflict)
- **Idempotency** -- Retrying any operation produces the same result
- **Isolation** -- Tenants isolated at compute, network, and build levels
- **Consistency** -- Release marked active only after cluster confirmation
- **Rollback** -- Every deploy creates a rollback point (~3s revert)
- **Clean delete** -- Project deletion removes manifests, triggers prune, verifies pods gone
- **Security** -- Pods run non-root, drop all capabilities, PSA restricted, images signed

---

## Development

### Prerequisites

Go 1.25+, Node.js 20+, Docker, [k3d](https://k3d.io), [Tilt](https://tilt.dev), [Task](https://taskfile.dev), [Helm](https://helm.sh) v3.14+, kubectl, [kubeconform](https://github.com/yannh/kubeconform), [golangci-lint](https://golangci-lint.run) v2.11+

### Setup

```bash
task setup       # Create 4 k3d clusters + install all infrastructure (idempotent)
tilt up          # Start dev loop with hot reload
task doctor      # Verify everything is healthy
```

### Tests

```bash
task test                     # All tests
cd cli && npm test            # CLI tests (Jest, 1210+ tests)
cd api && go test ./...       # API tests
cd operators && go test ./... # Operator tests (Ginkgo + envtest)
cd pkg && go test ./...       # Shared library tests
```

---

## What LoopZero Does Not Solve

- **Database migrations** -- LoopZero deploys code, not schema changes.
- **Custom domains without nameserver change** -- Users must change nameservers to Cloudflare.
- **Multi-region** -- Single-region in v1.
- *(item removido — preview environments shipped no Sprint 5)*
- **GPU/AI workloads** -- Not planned.

---

## Stability Contract

- Commands are permanent -- no removal without major version bump.
- `--json` output shape doesn't change without major version.
- Exit codes are permanent.
- Default behavior is frozen.

---

## License

[Apache License 2.0](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Every PR starts with a failing test.

## Links

- [CLI Reference](docs/cli-reference.md) -- authoritative specification (commands, exit codes, JSON schemas, contracts)
- [Maturity Assessment](docs/maturity-assessment.md) -- 3.9/5 self-assessment (post cell-2 activation + cleanroom dogfood 12/12 GREEN) with path to 4.0 and 5.0
- [Changelog](CHANGELOG.md)
