<p align="center">
  <h1 align="center">LoopZero</h1>
  <p align="center">
    <strong>The managed runtime for AI agents and the apps around them.</strong>
  </p>
  <p align="center">
    <em>The apex of the <a href="https://loopzero.example">loopzero</a> ecosystem — where LoopZeroKit and LoopZeroCloud land in production.</em>
  </p>
  <p align="center">
    <a href="#install">Install</a> &bull;
    <a href="#quickstart">Quickstart</a> &bull;
    <a href="#self-hosted">Self-Hosted</a> &bull;
    <a href="#cli-reference">CLI</a> &bull;
    <a href="#architecture">Architecture</a> &bull;
    <a href="#development">Development</a>
  </p>
</p>

---

## Install

```bash
curl -fsSL https://get.loopzero.example | sh
```

Four commands to ship. Then just `git push`.

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

No infrastructure to manage. No build config required. Just push.

---

## Project Status

> **Production.** Running on DigitalOcean (3 DOKS clusters, managed PostgreSQL, Cloudflare Tunnel). Multi-cell architecture live (3 cells). Progressive delivery (canary deploys) shipped. The commercial managed runtime for loopzero customers; self-host via commercial license.

### What Works (E2E Validated on DigitalOcean)

| Capability | Evidence |
|------------|----------|
| `loopzero login` / `logout` / `whoami` | Device Code Flow + Ory Hydra OIDC |
| `loopzero init` (monorepo detection) | Turbo/Nx/Lerna/pnpm + 30 frameworks |
| `loopzero deploy` (full pipeline) | CLI -> upload -> BuildKit -> cosign sign -> ArgoCD sync -> Argo Rollouts canary -> live URL (~4min) |
| `loopzero deploy` (progressive delivery) | **Default-on** under `LOOPZERO_PROGRESSIVE_DELIVERY=true`. Per-plan canary: Starter (50%/100%), Pro (20%/Analysis/50%/100%), Team & Enterprise (10%/Analysis/30%/60%/100%). TraefikService weighted routing between `<app>-stable` and `<app>-canary` Services. AnalysisTemplate hooks Prometheus success-rate when configured (pro+ tiers). Auto-abort on Degraded/Aborted via `progressDeadlineSeconds=300` + `rollout.aborted` SSE (CLI exit 3). 80/80 GREEN warm dogfood, 71/72 GREEN fresh cleanroom. |
| `loopzero deploy` (static frontends) | S3 upload + CF Worker KV routing + DO Spaces CDN |
| `loopzero rollback` | Instant revert to previous release (~3s) |
| `loopzero promote --from staging --to production` | Cross-environment promotion (HTTP 202, no rebuild) |
| `loopzero logs` | Local log streaming works; cross-cluster streaming not yet built (Loki infra gap) |
| `loopzero status` | Project status with active release, URL, scaling info, **progressive deploy phase** (canary step + setWeight%) |
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
| `loopzero scale` | Fully functional E2E — API persists scaling config, deploy enforces per-plan replica caps, validated end-to-end |
| Plan enforcement (Starter/Pro/Team) | Limits on projects/builds/environments/members/replicas |
| Concurrent deploy protection | Exactly one deploy per environment — first gets 202, others get 409 |
| Idempotency | Retry-safe via `Idempotency-Key` header |
| Image signing (cosign) | Every container image signed post-build |
| Scale-to-zero (KEDA) | Manifest generation + Helm charts + Pulumi bootstrap (cluster KEDA pending). KEDA HPA targets `Kind=Rollout` post-canary plan (T3.1) with `transfer-hpa-ownership` annotation so Argo Rollouts owns replica scaling during canary, KEDA after promotion. |
| Cell architecture (3 cells LIVE) | PlacementEngine, DynamicRouter (`loopzero_cell_discovery_count=3`), cell-scoped queries, leader fencing, build admission gate, PG work queue, capacity tests. **Cell-2 ACTIVATED 2026-05-03**: tenant pin via `loopzero admin tenants pin --cell cell-2` → CR MergePatch + ArgoCD destination flip → workload Pod Running on loopzero-runtime-2. 12/12 ADR gates LIVE GREEN persistente. Rollback SLO P50=648ms / P95=705ms (<<300s budget). EnvironmentReconciler cell-aware via `CellAwareRuntimeClientFactory` covers monolithic + split-binary builds. Heal H14+H15 codify cell-2 ClusterCredential refresh+bootstrap + ArgoCD bearer token sync. Production stages 4-5 (1 customer 7-day → 10/50/100%) pending Sprint 9 readiness review. |
| SecurityContext (non-root pods) | `runAsNonRoot`, `drop: ALL`, `readOnlyRootFilesystem` |
| PodSecurity Admission | `restricted` profile on all tenant namespaces |
| NetworkPolicy | Mandatory deny-all + explicit allow rules |
| Observability | 105 Prometheus metrics (incl. 6 cell metrics) + 21 Grafana dashboards + 7 SLO files (Sloth) |
| Auth hardened | UUID tenant IDs, RSA-only JWT, audience validation, secret rotation |
| JSON contracts | 8/8 commands validated, 0 internal term leaks |

### What's Not Built Yet

| Feature | Impact | Notes |
|---------|--------|-------|
| ~~Preview environments~~ | ~~High~~ | ✅ Shipped 2026-04-26 (Sprint 5 / CHG-2026-04-26-PREVIEW-IMPL). All 14 plan tasks done, E2E validated against live k3d (12 specs pass: PR opened→preview, synchronize→UPSERT, concurrent PRs, PR closed→cleanup + 8 CLI specs). |
| ~~Cell-2 activation (multi-cell dev)~~ | ~~Low~~ | ✅ Shipped 2026-05-03 (cell-2-activation-plan + fix-all-platform-issues-plan). 12/12 ADR gates LIVE GREEN persistente. T4.3 LIVE: `loopzero deploy` → Pod Running on loopzero-runtime-2, `/health` 200, cell-1 NotFound (isolation). |
| Cell-2 production canary (stages 4-5) | Medium | Stage 4: 1 customer 7-day. Stage 5: 10/50/100% with 7-day holds. Pending Sprint 9 readiness review per ADR D6. CI cron `drill-prod-identical-soak.yml` armed for ADR-compliant 1h+24h Sunday runs. |
| ~~SBOM generation (syft)~~ | ~~Medium~~ | ✅ Shipped (Sprint 6). Per-release CycloneDX manifest persisted in S3 with presigned URL endpoint. |
| Admission policy DENY mode | Medium | warn-mode shipped (Sprint 6). Deny-flip pending Sprint 9 with cosign keyless verification + Fulcio. |
| ~~`loopzero scale` end-to-end persistence~~ | ~~Medium~~ | ✅ Shipped — scaling config persisted and enforced end-to-end |
| Chaos engineering suite | Medium | Sprint 8 -- not started. |
| Polish + onboarding (< 10 min) | High | Sprint 9 -- not started. |
| SSO/SAML | Low | Phase 3. |

### Plans

<table>
<tr>
<th width="25%">Starter</th>
<th width="25%">Pro</th>
<th width="25%">Team</th>
<th width="25%">Enterprise</th>
</tr>
<tr>
<td><h3>Free</h3><em>For side projects and learning</em></td>
<td><h3>$19/mo</h3><em>Starter — early production agents</em></td>
<td><h3>$79/mo</h3><em>Growth — sustained traffic</em></td>
<td><h3>Custom</h3><em>For organizations at scale</em></td>
</tr>
<tr><td colspan="4"><strong>Deploy & Operate</strong></td></tr>
<tr>
<td>3 projects</td>
<td>10 projects</td>
<td>Unlimited projects</td>
<td>Unlimited projects</td>
</tr>
<tr>
<td>1 environment</td>
<td>3 environments</td>
<td>Unlimited environments</td>
<td>Unlimited environments</td>
</tr>
<tr>
<td>50 builds/mo</td>
<td>500 builds/mo</td>
<td>5,000 builds/mo</td>
<td>Unlimited</td>
</tr>
<tr>
<td>2 replicas/service</td>
<td>5 replicas/service</td>
<td>10 replicas/service</td>
<td>Unlimited</td>
</tr>
<tr>
<td>3 team members</td>
<td>10 team members</td>
<td>25 team members</td>
<td>Unlimited</td>
</tr>
<tr><td colspan="4"><strong>Deployment Safety</strong></td></tr>
<tr>
<td>Zero-downtime deploys</td>
<td>Zero-downtime deploys</td>
<td>Zero-downtime deploys</td>
<td>Zero-downtime deploys</td>
</tr>
<tr>
<td>Instant rollback</td>
<td>Instant rollback</td>
<td>Instant rollback</td>
<td>Instant rollback</td>
</tr>
<tr>
<td>Canary deploys (automatic)</td>
<td>Canary deploys (visible steps)</td>
<td>Canary deploys (configurable)</td>
<td>Canary deploys (full control + analysis hooks)</td>
</tr>
<tr>
<td>—</td>
<td>Preview environments (per PR)</td>
<td>Preview environments (per PR)</td>
<td>Preview environments (per PR)</td>
</tr>
<tr>
<td>—</td>
<td>Custom domains (TLS, WAF)</td>
<td>Custom domains (TLS, WAF)</td>
<td>Custom domains (TLS, WAF)</td>
</tr>
<tr>
<td>—</td>
<td>Cross-environment promote</td>
<td>Cross-environment promote</td>
<td>Cross-environment promote</td>
</tr>
<tr><td colspan="4"><strong>Security & Compliance</strong></td></tr>
<tr>
<td>Signed container images</td>
<td>Signed container images</td>
<td>Signed container images</td>
<td>Signed container images</td>
</tr>
<tr>
<td>—</td>
<td>—</td>
<td>SBOM per build (CycloneDX)</td>
<td>SBOM per build (CycloneDX)</td>
</tr>
<tr>
<td>—</td>
<td>—</td>
<td>Build attestation</td>
<td>Build attestation</td>
</tr>
<tr>
<td>—</td>
<td>Audit log (basic)</td>
<td>Audit log (full filters + export)</td>
<td>Audit log (full filters + export)</td>
</tr>
<tr>
<td>—</td>
<td>—</td>
<td>—</td>
<td>SSO/SAML</td>
</tr>
<tr><td colspan="4"><strong>Infrastructure</strong></td></tr>
<tr>
<td>Managed (shared cell)</td>
<td>Managed (shared cell)</td>
<td>Managed (shared cell)</td>
<td>Dedicated cells (multi-cluster isolation)</td>
</tr>
<tr>
<td>—</td>
<td>—</td>
<td>—</td>
<td>Self-hosted option (your Kubernetes)</td>
</tr>
<tr>
<td>—</td>
<td>—</td>
<td>—</td>
<td>SLA (99.9%+ uptime)</td>
</tr>
<tr>
<td>—</td>
<td>—</td>
<td>—</td>
<td>Dedicated support</td>
</tr>
</table>

Every plan includes: GitHub auto-deploy on push, monorepo support (30+ frameworks), encrypted secrets, `--json` output for CI/CD, and the same CLI experience.

> **Implementation note:** Plan limits enforced by `api/internal/db/migrations/000002_seed_data.up.sql` (plan_limits seed) + 4 enforcement middlewares (HTTP 402 on exceed).

### Test Suite

| Component | Count | Status |
|-----------|------:|--------|
| CLI (Jest) | 1,260 | 85 suites — 1,244 passing, 9 failing (4 suites: platform-status-drift, admin-tenants-pin + 2 minor), 7 skipped. |
| API (35 subpacotes) + Shared libs (pkg/, 10 packages) | 2,283 + 388 test funcs | Passing (multi-cell: placement determinism with -race + admin_pin_cell C11 authz + migration_007/008 invariants + reaper observability + canary: `TestBuildRollout_*` × 7 + `TestBuildRollout_ProgressDeadlineSeconds` × 4 plan tiers + `TestBuildHPA_TargetsRollout_Progressive` + `TestBuildScaledObject_TargetsRollout_Progressive` + `TestGetAppStatus_RolloutCounted_AsWorkloadPrimitive` + `TestGetAppStatus_RolloutDegraded_BlocksHealthy`) |
| Operators (Ginkgo + envtest) | 200+ specs | Passing (+ application reaper 10 fake-client tests + cell-aware factory 7 tests, no envtest dependency) |
| Pulumi (vitest) | 310+ specs | Passing (heal-script H14 24 invariants + migrations-forward-only 11 invariants) |
| State machines | 10 application + 9 release + 7 build | Passing (`ApplicationCleaned` terminal state via `ApplicationOrphaned` event added 2026-05-03) |
| CI pipeline | 48 workflows | Green (incl. drill prod-identical canary soak weekly Sunday 02:00 UTC + chaos-suite weekly Sunday 03:00 UTC) |
| Regression matrix | 166 entries | 0 critical miss, audit GREEN |
| Cleanroom dogfood (E2E) | preflight + 11 journeys | **12/12 GREEN** — fresh destroy/setup cycle validates all commands end-to-end (~33min) |
| Ginkgo journey suite (Argo Rollouts) | 80 specs | **80/80 GREEN** — canary happy path, auto-abort on failure, promote/rollback over Rollouts, multi-server naming |

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
| Progressive delivery (canary) | -- | Yes (Argo Rollouts: per-plan steps, Traefik weighted routing, auto-abort) |
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

10-step automated installer: validates cluster, installs all dependencies (PostgreSQL, Gitea, ArgoCD, Argo Workflows, Ory Kratos/Hydra, MinIO, cert-manager, Traefik), bootstraps credentials, runs migrations, and verifies health.

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
| Self-healing | `infra/scripts/local/k3d-cross-cluster-heal.sh` (H12+H13) | Auto-recovers cell-1 missing CC + cell-default SA token rotation |

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
| CLI | TypeScript + Commander (35 commands) |
| API | Go 1.25 + chi + pgx (connection pool) |
| Operators | Go + kubebuilder + controller-runtime (8 CRDs, all wired; 4 split binaries: tenant/app/preview/platform) |
| Shared libs | Go -- errors, errors/http, logging, naming, conditions, statemachine, jwt, enforcement, circuitbreaker |
| Auth | Ory Kratos (identity) + Ory Hydra (OAuth2/OIDC) |
| Build engine | Argo Workflows + BuildKit (rootless) |
| GitOps | ArgoCD (manual sync) + Gitea |
| Progressive delivery | Argo Rollouts 1.7.2 (per-plan canary steps, Traefik weighted routing, auto-abort on failure) |
| Database | PostgreSQL 16 (Helm chart in self-hosted, managed DO in cloud) |
| Autoscaling | KEDA + HTTP Add-on (scale-to-zero) + HPA (min >= 1) |
| Secrets | External Secrets Operator |
| Edge | Cloudflare Tunnel + Worker + KV + Zone Management |
| Static assets | DigitalOcean Spaces + CDN |
| Monitoring | Prometheus + Grafana + Loki + Tempo + OpenTelemetry |
| IaC | 5 Pulumi projetos (TS): `pulumi/` (L1 DO+CF), `pulumi-infra/` (multi-cluster topology), `pulumi-platform/` (L2 com 32 Helm releases), `pulumi-drill/` (DR drill prod-identical stack), `pulumi-shared/` (cross-project type contracts). Nota: `pulumi-control-plane/` foi deletado em 2026-05-15 (close-all-gaps T2.1) — control-plane workloads agora vivem em L2 Helmfile, não Pulumi, per MEET-2026-05-12 D3. |
| CI/CD | GitHub Actions (48 workflows — verified 2026-05-21) |
| Helm charts | 34 bootstrap (`infra/helm/`) + 13 published (`charts/`) — verified 2026-05-22 |

### Helm Charts Inventory (`infra/helm/`, 34 dirs)

Bootstrap charts installed via Helmfile L2. Counts and names verified 2026-05-22 via `ls infra/helm/`.

| Domain | Charts |
|--------|--------|
| Identity / Auth | `ory-kratos`, `ory-hydra`, `redis` |
| Build pipeline | `argo-workflows`, `harbor` (auxiliary registry; prod uses DOCR) |
| GitOps | `argocd`, `gitea` |
| Progressive delivery | `argo-rollouts`, `keda`, `keda-http-addon` |
| Ingress / TLS | `traefik`, `cert-manager`, `cluster-issuer`, `cloudflared` |
| Storage | `minio`, `nfs-server-provisioner` |
| Admission control | `admission-policies` (custom VAPs), `policy-controller` (sigstore) |
| Network | `network-policies` (deny-all baseline) |
| Observability | `monitoring` (Prom+Grafana), `loki`, `promtail`, `tempo`, `otel`, `blackbox-exporter` |
| Resilience | `chaos-mesh`, `velero` |
| Load + smoke testing | `k6-operator`, `smoke-test` |
| LoopZero platform | `loopzero-api`, `loopzero-cloud`, `static-assets`, `external-secrets`, `postgresql` |

### Published OCI Charts (`charts/`, 13 dirs)

For ArgoCD L3 + downstream consumers. Use `loopzero-common` library for consistent labels and security contexts.

`loopzero-admission-policies` · `loopzero-api` · `loopzero-app-operator` · `loopzero-build-cwts` · `loopzero-cloud` · `loopzero-common` · `loopzero-crds` · `loopzero-dashboard` · `loopzero-monitoring` · `loopzero-platform` · `loopzero-platform-operator` · `loopzero-preview-operator` · `loopzero-tenant-operator`

### Supply Chain Security (CI-enforced)

| Tool | Where | What it does |
|------|-------|-----------|
| **cosign** | `build-images.yml` + rotation drill + cutover deadline checks | Image signing (keyed today; keyless v1.1 with Fulcio committed for cutover) |
| **syft** | `build-images.yml` | SBOM generation (CycloneDX) + per-build download endpoint (`GET /builds/{id}/sbom`) |
| **Trivy** | `ci.yml` (filesystem scan) + `build-images.yml` (image scan) | CVE scanning, `CRITICAL,HIGH` severity, `exit-code: 1` — **auto-deletes vulnerable images from DOCR** on post-push failure |
| **TruffleHog** | `ci.yml` | Secret scanner (`--only-verified`) |
| **sigstore policy-controller** | `infra/helm/policy-controller/` | Admission deny-by-default; `loopzero-images-warn` warn-mode ClusterImagePolicy |

### Quality & Validation Gates (CI-enforced)

| Tool | Where | What it does |
|------|-------|-----------|
| **kubeconform** | `task validate:manifests`, `helm-charts.yml` | K8s manifest schema validation (`--strict`) |
| **Polaris (Fairwinds)** | `scripts/run-polaris.sh --severity=danger` | K8s best-practices linter (DANGER blocks merge) |
| **chart-testing (ct)** | `helm-charts.yml` multi-K8s-version matrix | Helm chart lint + install test |
| **helm-chartsnap** | `helm-charts.yml` | Snapshot testing to detect chart output drift |
| **helm-diff** | `infra/scripts/helm-diff-structural.sh` + helmfile plugin | Pre-sync structural diff |
| **golangci-lint v2.11+** | `ci.yml`, `task lint` | Go static analysis |
| **18+ `pkg/quality/*` gates** | `cmd/loopzero-ops quality <gate>` wired in workflows | Pure-logic gates (prod-image-tags, no-migrate-force, dogfood-slo, etc.) |
| **Renovate** | `.github/renovate.json` | Dependency mgmt with `pinDigests: true` (INF-G18 SHA pinning), weekly grouped PRs |

### Production Infrastructure (DigitalOcean)

| Resource | Configuration |
|----------|---------------|
| 3 DOKS clusters | v1.32, 2 nodes each, SFO3 (mgmt, build, runtime) |
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
task setup       # ONE-SHOT: 4 k3d clusters + Helmfile-orchestrated infra + CRDs + tokens (15-25min)
tilt up          # DEV LOOP: hot reload of API/operators/CLI on file save
task doctor      # HEALTH CHECK: 38+ invariants across clusters/pods/tokens
```

**Distinction (canonical per plan 2026-05-22 Phase 3):**
- `task setup` is for **cold start** / fresh cleanroom / recovery. One-shot.
- `tilt up` is the **day-to-day dev loop**. Hot reload, no manual rebuilds.
- See `docs/operations/dev-loop.md` for full workflow guide.

### Tests

```bash
task test                     # All tests
cd cli && npm test            # CLI tests (Jest, 1226+ tests)
cd api && go test ./...       # API tests
cd operators && go test ./... # Operator tests (Ginkgo + envtest)
cd pkg && go test ./...       # Shared library tests
```

---

## Known Issues

- **Local k3d cert-manager** cannot issue for `*.loopzero.local` (no public DNS for ACME challenge). Self-signed CA issuer (`loopzero-local-ca-issuer`) fills local gap; production uses CF-managed TLS.
- **Local Stage 1 canary HTTP probes** report HTTP 000 in `--smoke` mode (probes informational only, no functional impact).

## What LoopZero Does Not Solve

- **Database migrations** -- LoopZero deploys code, not schema changes.
- **Custom domains without nameserver change** -- Users must change nameservers to Cloudflare.
- **Multi-region** -- Single-region in v1.
- **GPU/AI workloads** -- Not planned.

---

## Stability Contract

- Commands are permanent -- no removal without major version bump.
- `--json` output shape doesn't change without major version.
- Exit codes are permanent.
- Default behavior is frozen.

---

## License

**Proprietary / Commercial** — see [LICENSE](LICENSE).

LoopZeroCloud is a managed offering operated by loopzero.example. Self-hosted deployment is available under a commercial license — contact `getloopscale@gmail.com`.

## Contributing

Every PR starts with a failing test. See the [development commands](CLAUDE.md#development-commands) for setup instructions.

## Links

- [Changelog](CHANGELOG.md)

## Community

- Discord: https://discord.loopzero.example/
- X: https://x.com/getloopscale
- LinkedIn: https://linkedin.com/company/getloopscale
