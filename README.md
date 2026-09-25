<p align="center">
  <h1 align="center">LoopZero</h1>
  <p align="center">
    <strong>The managed runtime for AI agents and the apps around them.</strong>
  </p>
  <p align="center">
    <em>The apex of the <a href="https://loopzero.example">loopzero</a> ecosystem — where LoopZeroCode, LoopZeroCreate and LoopZeroKit land in production.</em>
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

> **Running in production for loopzero customers — not released to external users.** That is the
> phase [`GOVERNANCE.md`](GOVERNANCE.md) declares, and it is what the word means here: backward
> compatibility is not required, and legacy code is deleted rather than carried.
>
> On DigitalOcean (3 DOKS clusters, managed PostgreSQL, Cloudflare Tunnel), multi-cell live across
> **2 cells** (`cell-0`, `cell-1`), progressive delivery shipped. Self-host via commercial licence.

### What Works (E2E Validated on DigitalOcean)

| Capability | Evidence |
|------------|----------|
| `loopzero login` / `logout` / `whoami` | Device Code Flow + Ory Hydra OIDC |
| `loopzero init` (LoopZeroKit project) | Fixed LoopZeroKit stack: Python + Node (server/worker) |
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
| Scale-to-zero (KEDA) | Manifest generation + Helm charts + Helmfile L2 bootstrap (cluster KEDA pending). KEDA HPA targets `Kind=Rollout` post-canary plan (T3.1) with `transfer-hpa-ownership` annotation so Argo Rollouts owns replica scaling during canary, KEDA after promotion. |
| Cell architecture (2 cells LIVE) | PlacementEngine, DynamicRouter,  cell-scoped queries, leader fencing, build admission gate, PG work queue, capacity tests. **Second cell activated 2026-05-03** (named `cell-2` then, `cell-1` since the B-001 rename): tenant pin via `loopzero admin tenants pin --cell cell-1` → CR MergePatch + ArgoCD destination flip → workload Pod Running on loopzero-cell-1. 12/12 ADR gates LIVE GREEN persistente. Rollback SLO P50=648ms / P95=705ms (<<300s budget). EnvironmentReconciler cell-aware via `CellAwareRuntimeClientFactory` covers monolithic + split-binary builds. Heal H14+H15 codify cell-1 ClusterCredential refresh+bootstrap + ArgoCD bearer token sync. Production stages 4-5 (1 customer 7-day → 10/50/100%) pending Sprint 9 readiness review. |
| SecurityContext (non-root pods) | `runAsNonRoot`, `drop: ALL`, `readOnlyRootFilesystem` |
| PodSecurity Admission | `restricted` profile on all tenant namespaces |
| NetworkPolicy | Mandatory deny-all + explicit allow rules |
| Observability | 126 distinct `loopzero_*` Prometheus metrics + 28 Grafana dashboard JSONs + 7 Sloth SLO files (`infra/sloth/`) |
| Auth hardened | UUID tenant IDs, RSA-only JWT, audience validation, secret rotation |
| JSON contracts | 8/8 commands validated, 0 internal term leaks |

### What's Not Built Yet

| Feature | Impact | Notes |
|---------|--------|-------|
| ~~Preview environments~~ | ~~High~~ | ✅ Shipped 2026-04-26 (Sprint 5 / CHG-2026-04-26-PREVIEW-IMPL). All 14 plan tasks done, E2E validated against live k3d (12 specs pass: PR opened→preview, synchronize→UPSERT, concurrent PRs, PR closed→cleanup + 8 CLI specs). |
| ~~Cell-2 activation (multi-cell dev)~~ | ~~Low~~ | ✅ Shipped 2026-05-03 (cell-1-activation-plan + fix-all-platform-issues-plan). 12/12 ADR gates LIVE GREEN persistente. T4.3 LIVE: `loopzero deploy` → Pod Running on loopzero-cell-1, `/health` 200, cell-0 NotFound (isolation). |
| Cell-2 production canary (stages 4-5) | Medium | Stage 4: 1 customer 7-day. Stage 5: 10/50/100% with 7-day holds. Pending Sprint 9 readiness review per ADR D6. The former `drill-prod-identical-soak.yml` Sunday cron went away with the Actions retirement — the drill itself survives and is operator-run via `task drill:l2-scale`. |
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
<td><h3>$29/mo</h3><em>For professional developers</em></td>
<td><h3>$99/mo</h3><em>For growing teams</em></td>
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

Every plan includes: GitHub auto-deploy on push, the fixed LoopZeroKit stack (Python + Node), encrypted secrets, `--json` output for CI/CD, and the same CLI experience.

> **Implementation note:** Plan limits enforced by `api/internal/db/migrations/000002_seed_data.up.sql` (plan_limits seed) + 4 enforcement middlewares (HTTP 402 on exceed).

### Test Suite

Counts below are `Test*` function counts per Go module, measured from the tree — run `task test` for
the live verdict. The **CLI (Jest) suite is no longer in this repo**; it moved to `../loopzero-cli` with
the CLI itself.

| Module | Test files | `Test*` funcs | Notes |
|--------|-----------:|--------------:|-------|
| `api/` | 505 | 3,167 | Routes, services, placement, gitops, manifests, metering |
| `cmd/loopzero-ops/` | 260 | 2,277 | The 126 wired quality gates + seed/drill/pulumi/chaos subcommands |
| `pkg/` | 68 | 706 | Shared libs — state machines, errors, naming, enforcement |
| `operators/` | 74 | 350 | Ginkgo + envtest reconcile suites |
| `infra/tests/` | 206 | 563 | E2E (Ginkgo), chaos, load (k6), hermetic infra checks |

Ginkgo `It(`/`Entry(` blocks across `operators/` + `infra/tests/`: **484**.

E2E and cleanroom journeys are **operator-run, not CI-run**. The chain is Cleanroom TOTAL → UP → seeds → dogfood → LIVE test; warm-infra results are not LIVE-valid. The rule file that carried this (`.claude/rules/cleanroom-up-live-test-mandate.md`) **no longer exists**, and `.claude/` is gitignored, so a fresh clone never had it.

---

## Why LoopZero

You have a monorepo with an API, a frontend, and workers. Vercel handles the frontend -- but the rest requires Dockerfiles, Helm charts, and CI pipelines that only one person understands.

LoopZero gives you Vercel-quality DX for everything Vercel can't deploy.

| | Vercel | LoopZero |
|---|:---:|:---:|
| Auto-deploy on `git push` | Yes | Yes (~4min) |
| Static frontends (React, Vite) | Yes | Yes (CDN via CF Worker) |
| API servers | Serverless only | Fixed LoopZeroKit stack: Python + Node (`server`/`worker`) |
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
Developer Plane     CLI (TypeScript, ../loopzero-cli) <-> API (Go, this repo)
Control Plane       Operators, Gitea, PostgreSQL, ArgoCD, Ory Kratos/Hydra
Build Plane         Argo Workflows + BuildKit (rootless) + cosign signing
Workload Plane      User applications managed by ArgoCD + KEDA autoscaling
Secrets             OpenBao (Transit) + ESO — runs in its own `loopzero-secrets` cluster
```

> **This repository is the engine.** The CLI (`../loopzero-cli`), the control-plane/dashboard
> (`../loopzero-cloud`) and the shared contracts (`../loopzero-contracts`) were extracted at M2-M4 of the
> monorepo decomposition. Look for their source there, not here.

### Multi-Cell Routing

Tenant workloads run on independent K8s clusters called **cells** (`cell-0`, `cell-1`, ...). Routing is **placement once, route many** — not load-balanced.

```
loopzero bootstrap (NEW tenant)
  -> RoundRobinEngine.AssignAndExecute (PG advisory lock + SERIALIZABLE)
     -> picks least-loaded cell with capacity (default cap=5000 tenants)
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
| Operator pin | `api/internal/services/placement/cell_pinner.go` | DB UPDATE + Tenant CR MergePatch |
| Self-healing | `infra/scripts/local/k3d-cross-cluster-heal.sh` (H12+H13) | Auto-recovers cell-0 missing CC + cell-0 SA token rotation |

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
| CLI | TypeScript + Commander — **source lives in `../loopzero-cli`** (extracted at M2) |
| API | Go 1.26 + chi + pgx (connection pool) |
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
| IaC — L1 cloud primitives | **Being re-platformed DigitalOcean/Pulumi → AWS/Terraform** per the 2026-06-17 CTO mandate (`../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`). Target: `../loopzero-infra-modules` (versioned blueprints) + `../loopzero-infra-live` (per-env Terragrunt units). The 5 Pulumi projects under `infra/` are **AS-IS legacy** — do not add new Pulumi for target infra. |
| IaC — L2 platform workloads | Helmfile v1.0+ (`infra/helmfile/`) — the source of truth for platform releases, **not** Pulumi |
| IaC — L3 tenant Day-2 | ArgoCD (manual sync, from Gitea) |
| CI/CD | 5 workflows in `.github/workflows/` (`build-publish`, `ci`, `delivery-gate-armed`, `release-artifacts`, `workflow-hygiene`). Actions was retired as the pipeline 2026-05-23; gates run via `task quality:*` |
| Helm charts | 43 bootstrap (`infra/helm/`) + 12 published (`charts/`) |

**The L1 schedule.** The Pulumi projects stay live until the AM9 cutover and are retired at
AM10, per the ADR cited in the row above. The prohibition on adding to them is separate and
unconditional: it carries no date and has been in force since the mandate.

### Edge Proxy vs Cluster Ingress (Caddy ↔ Traefik)

**LoopZero runs BOTH proxies — neither replaced the other. They serve different layers of the request
flow** (the canonical separation of concerns: AWS ALB + NGINX Ingress, Cloudflare + Envoy/Istio).

| Layer | Caddy (edge) | Traefik (cluster ingress) |
|-------|--------------|---------------------------|
| Where it runs | Host machine (local VPS or dev workstation) | Inside the workload cluster (K8s Deployment) |
| Request layer | Public L7 — first stop after DNS | Internal L7 — first stop after Caddy |
| Public TLS (Let's Encrypt) | ✅ Automatic + grace renewal | ❌ Possible, but complex to set up |
| Binds host ports 80/443 | ✅ Naturally | ❌ K8s needs NodePort/LoadBalancer + port-forward |
| Single binary, file config | ✅ Reload via `caddy reload` | ❌ Runs as a Deployment + IngressRoute CRDs |
| Native K8s CRD | ❌ Knows nothing about K8s | ✅ Native IngressRoute operator |
| Argo Rollouts canary | ❌ | ✅ TrafficRouter `traefik.io/v1alpha1` (weighted services) |
| Multi-tenant routing by Host header | Possible, but verbose | ✅ Natural via the IngressRoute CRD |
| Internal cluster cert (`.loopzero.local`) | ❌ | ✅ via cert-manager + ClusterIssuer |
| HTTP-01 ACME challenge | ✅ Auto | ❌ Not its focus |

#### Live request flow (J-23 preview example)

```
Browser
  ↓ DNS (Cloudflare: pr1-teste.preview-dev.loopzero.example → host IP)
  ↓
Host machine, port 443
  ↓
Caddy
  ├─ TLS termination (Let's Encrypt, automatic)
  ├─ Block match: pr1-teste.preview-dev.loopzero.example
  └─ reverse_proxy https://172.18.0.10:443
       header_up Host tn-...-teste-api-preview-pr-1.loopzero.local
  ↓
Workload cluster Traefik LoadBalancer (172.18.0.10:443)
  ↓
Traefik IngressRoute (in the preview namespace)
  └─ match: Host(`tn-...-teste-api-preview-pr-1.loopzero.local`)
  ↓
K8s Service teste-api-stable / teste-api-canary
  ↓
Rollout Pod
  ↓
App responds
```

#### Local k3d ↔ Prod DOKS

In production (DOKS), Caddy is replaced by **Cloudflare Tunnel** (zero public IPs, all runtime
traffic through the CF edge). Traefik stays inside the cluster.

```
Local dev/test:                       Prod (DOKS):
Internet → Caddy (host) → Traefik    Internet → CF Tunnel → Traefik
                          (k8s)                              (k8s)
```

Caddy locally is a pragmatic call: one binary on a VPS instead of provisioning a CF Tunnel plus
cluster reachability. The same trade-off as `task setup` on k3d rather than DOKS.

### Helm Charts Inventory (`infra/helm/`, 43 dirs)

Bootstrap charts installed via Helmfile L2. `ls infra/helm/` is the authoritative list — this table
groups it. **Classifying a tool as adopted or missing from memory is forbidden**; grep first
(`grep -ril "<tool>" infra/ charts/ taskfiles/`), because this table is a curated summary, not an inventory.

| Domain | Charts |
|--------|--------|
| Identity / Auth | `ory-kratos`, `ory-hydra`, `redis` |
| Secrets | `openbao-cert`, `openbao-init`, `openbao-snapshot-agent`, `external-secrets`, `loopzero-external-secrets` |
| Build pipeline | `argo-workflows`, `argo-events`, `argo-events-bus`, `argo-ci-namespace`, `loopzero-ci-workflows`, `harbor` (auxiliary registry; prod uses DOCR) |
| GitOps | `argocd`, `gitea` |
| Progressive delivery | `argo-rollouts`, `keda`, `keda-http-addon` |
| Ingress / TLS | `traefik`, `cert-manager`, `cluster-issuer`, `cloudflared` |
| Storage | `minio`, `nfs-server-provisioner`, `postgresql` |
| Admission control | `admission-policies` (custom VAPs), `policy-controller` + `sigstore-extras` (sigstore) |
| Network | `network-policies` (deny-all baseline) |
| Observability | `monitoring` (Prom+Grafana), `loki`, `promtail`, `tempo`, `otel`, `blackbox-exporter`, `opencost` (cost attribution) |
| Resilience | `chaos-mesh`, `velero` |
| Load + smoke testing | `k6-operator`, `smoke-test` |
| LoopZero platform | `loopzero-api`, `loopzero-cloud`, `static-assets` |

### Published OCI Charts (`charts/`, 12 dirs)

For ArgoCD L3 + downstream consumers. Use `loopzero-common` library for consistent labels and security contexts.

`loopzero-admission-policies` · `loopzero-api` · `loopzero-app-operator` · `loopzero-build-cwts` · `loopzero-cloud` · `loopzero-common` · `loopzero-crds` · `loopzero-dashboard` · `loopzero-monitoring` · `loopzero-platform-operator` · `loopzero-preview-operator` · `loopzero-tenant-operator`

This list named a thirteenth, `loopzero-platform`, until 2026-09-11; `charts/loopzero-platform` does not exist.

### Supply Chain Security

> **Where these run.** GitHub Actions was retired as the pipeline in 2026-05-23. **Five** workflows
> remain (`build-publish.yml`, `ci.yml`, `delivery-gate-armed.yml`, `release-artifacts.yml`,
> `workflow-hygiene.yml` — measured 2026-09-11; this said three); everything else is
> run by `task` locally or by whichever CI a deployment wires up. The tool configs below are live —
> their former workflow homes (`build-images.yml`, `ci.yml`, `helm-charts.yml`) are **gone**, so
> treat the run column as "how to invoke", not "enforced on every PR".

| Tool | How it runs | What it does |
|------|-------------|-----------|
| **cosign** | `.github/workflows/build-publish.yml`; rotation + cutover-deadline drills under `infra/scripts/security/` | Image signing (keyed today; keyless v1.1 with Fulcio committed for cutover) |
| **syft** | `.github/workflows/build-publish.yml` | SBOM generation (CycloneDX) + per-build download endpoint (`GET /builds/{id}/sbom`) |
| **Trivy** | `loopzero-ops quality trivy-scan`, wired into `task quality:gates`; `.trivyignore` at repo root | CVE scanning, `CRITICAL,HIGH` severity |
| **TruffleHog** | `loopzero-ops quality trufflehog-scan`, wired into `task quality:gates` | Verified-secret scanning (`--only-verified`) |
| **govulncheck** | `task vuln-scan` (report) · `task vuln-scan:strict` (fails on MEDIUM+) | Go stdlib + module CVEs across `api`, `operators`, `pkg` |
| **sigstore policy-controller** | `infra/helm/policy-controller/` | Admission deny-by-default; `loopzero-images-warn` warn-mode ClusterImagePolicy |

> **Reusable image workflow.** `.github/workflows/build-publish.yml` is **reusable** and consumed cross-repo by sibling
> projects (e.g. `loopzero-cloud` publishes its `loopzero-cloud` + `loopzero-dashboard` GHCR images through it —
> keyless cosign + SBOM + SLSA). It exposes a `runner` input (default `ubuntu-latest`) so callers on
> a billing-blocked org can build on a self-hosted runner: `runner: '["self-hosted","linux","x64"]'`.

### Quality & Validation Gates

The gate layer is **126 wired Go checks** in `cmd/loopzero-ops` (145 registered, 126 wired — measured 2026-09-11) — the substitute for the retired CI.
Build it first with `task quality:build`, then `/tmp/loopzero-ops quality --list` is the authoritative inventory (name / wired-into-which-task / summary). A stray `./loopzero-ops` at the repo root is a gitignored local artifact and can be badly stale.

| Tool | How it runs | What it does |
|------|-------------|-----------|
| **`loopzero-ops` quality gates (126 wired / 145 registered)** | `task quality:gates` (full scan) · `task quality:diff` (PR-scoped) · `task quality:all` | Pure-logic gates: `unhomed-logic`, `no-new-bash`, `advisory-lock-scope`, `cli-exit-codes`, `prod-image-tags`, `no-migrate-force`, … |
| **kubeconform** | `task validate:manifests` | K8s manifest schema validation (`--strict`) |
| **golangci-lint v2.11+** | `task lint` (all 9 workspace modules, with a per-module ratchet) | Go static analysis |
| **helmfile template + kubeconform** | `task validate:helmfile-templates` · `task validate:matrix` | Renders every release and schema-checks the output |
| **helm-diff** | `loopzero-ops quality helm-diff-structural` — **registered but not wired** into any task (`--list` shows `-`) | Structural pre-sync diff |
| **chart-testing (ct) / helm-chartsnap** | `ct.yaml` + `.chart-testing/` config retained; **no runner wired** | Chart lint / install test / output-drift snapshots |
| **Polaris (Fairwinds)** | `.polaris.yaml` retained; **no runner wired** (`run-polaris.sh` no longer exists) | K8s best-practices linter |
| **Renovate** | `.github/renovate.json` | Dependency mgmt with `pinDigests: true` (INF-G18 SHA pinning), weekly grouped PRs |

Adding a new `.sh` file is refused by the `no-new-bash` gate — new logic belongs in Go
(`cmd/loopzero-ops`), declarative Helmfile, or Ginkgo. The written allowlist contract
(`.claude/rules/shell-scripts-allowlist.md`) **no longer exists**; the gate still runs.

### Production Infrastructure (DigitalOcean)

| Resource | Configuration |
|----------|---------------|
| 3 DOKS clusters | v1.32, 2 nodes each, SFO3. Named `core`, `build`, `cell-N` since the B-001 rename — the old `mgmt` / `workload` / `runtime` appear in no filename |
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

Go 1.26+, Node.js 20+, Docker, [k3d](https://k3d.io), [Tilt](https://tilt.dev), [Task](https://taskfile.dev), [Helm](https://helm.sh) v3.14+ with the [helm-diff](https://github.com/databus23/helm-diff) plugin, [Helmfile](https://helmfile.readthedocs.io) v1.0+, kubectl, [kubeconform](https://github.com/yannh/kubeconform), [golangci-lint](https://golangci-lint.run) v2.11+

**This repo is a multi-module Go workspace.** `go.work` unions 9 modules and one of them is the
sibling repo `../loopzero-contracts` — clone it alongside this one or the workspace will not build.
There is no `go.mod` at the repo root, so `go build ./...` / `go test ./...` from the root do **not**
cover the repo; run them per module or use `task`.

### Setup

```bash
task setup       # ONE-SHOT: 4 k3d clusters + Helmfile-orchestrated infra + CRDs + tokens (15-25min)
tilt up          # DEV LOOP: hot reload of API + operators on file save
task doctor      # HEALTH CHECK: 38+ invariants across clusters/pods/tokens
```

**Distinction (canonical per plan 2026-05-22 Phase 3):**
- `task setup` is for **cold start** / fresh cleanroom / recovery. One-shot.
- `tilt up` is the **day-to-day dev loop**. Hot reload, no manual rebuilds.
- See [`docs/operations.md`](docs/operations.md) for the full workflow guide.

### Tests

```bash
task test                        # unit + lint + manifest validation
task quality:gates               # the 126 wired gates (full-repo scan)
task test:e2e                    # Ginkgo E2E (needs running clusters + tilt up)

cd api           && go test ./...   # API
cd pkg           && go test ./...   # Shared libraries
cd operators     && go test ./...   # Operators (Ginkgo + envtest)
cd cmd/loopzero-ops  && go test ./...   # Ops + quality binary

# One package / one test
cd api && go test ./internal/services/... -run TestScalingResolver -v

# One Ginkgo spec. `go test -timeout` alone does NOT raise Ginkgo's own 60min
# suite timeout — pass -ginkgo.timeout too, or deploy-heavy suites hit TIMEDOUT:
cd infra/tests && go test ./e2e/... -v -timeout 120m -args -ginkgo.timeout=2h
```

CLI tests moved to `../loopzero-cli` (`npm test` there) when the CLI was extracted at M2.

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

- [Documentation index](docs/README.md) — six living documents: architecture, the LoopZeroKit seam, API contracts, operations, runbooks, decisions
- [Changelog](CHANGELOG.md)
- [Governance](GOVERNANCE.md) · [Security policy](SECURITY.md)

## Community

- Discord: https://discord.loopzero.example/
- X: https://x.com/getloopscale
- LinkedIn: https://linkedin.com/company/getloopscale
