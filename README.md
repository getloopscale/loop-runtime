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

> **Phase: Pre-release.** Production infrastructure running on DigitalOcean (3 DOKS clusters, managed PG, CF Tunnel). Core deploy flow E2E validated. Not yet open to external users.
>
> **Maturity: 3.6/5** -- strong technical foundation, commercially unvalidated. (was 3.2 pre-Sprint-5; +0.4 from Preview Environments fully shipped + E2E live validated against k3d 2026-04-26 / CHG-2026-04-26-PREVIEW-IMPL.) See [full assessment](docs/maturity-assessment.md).

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
| `loopzero scale` | Scaling configuration (stub -- 202 acknowledged, apply via loopzero.yaml) |
| Plan enforcement (Starter/Pro/Team) | Limits on projects/builds/environments/members/replicas |
| Concurrent deploy protection | `pg_advisory_xact_lock` -> exactly one 202, others get 409 |
| Idempotency | Retry-safe via `Idempotency-Key` header |
| Image signing (cosign) | Every container image signed post-build |
| Scale-to-zero (KEDA) | Manifest generation + Helm charts + Pulumi bootstrap (cluster KEDA pending) |
| Cell architecture (Phase 0+1) | PlacementEngine, DynamicRouter, cell-scoped queries, leader fencing, build admission gate, PG work queue, capacity tests. Pulumi cell.ts provisions full cells. Phase 2b activation pending (config only). |
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
| SBOM generation (syft) | Medium | cosign signing done, syft not started. Sprint 6 remainder. |
| Admission policy (reject unsigned images) | Medium | Sprint 6 remainder. |
| `loopzero scale` persistence (DB) | Low | Endpoint returns 202 acknowledged as stub. Use loopzero.yaml + loopzero deploy. |
| Cell-2 activation (multi-cell prod) | Low | Code 100% complete. Missing: Pulumi.dev.yaml config to invoke createCell(). |
| Chaos engineering suite | Medium | Sprint 8 -- not started. |
| Polish + onboarding (< 10 min) | High | Sprint 9 -- not started. |
| SSO/SAML | Low | Phase 3. |

### Pricing Tiers

| Tier | Price | Projects | Builds/mo | Max Replicas |
|------|-------|----------|-----------|-------------|
| Starter | Free | 1 | 100 | 2 |
| Pro | $29/mo | 3 | 500 | 5 |
| Team | $79/mo | 10 | 2000 | 10 |
| Enterprise | Custom | Unlimited | Unlimited | Unlimited |

### Test Suite

| Component | Count | Status |
|-----------|------:|--------|
| CLI (Jest) | 1,079 | Passing (59 suites; 7 skipped) |
| API + Shared libs (Go) | 41 packages | Passing (preview store/service/handler/webhook/comment +60 unit tests added 2026-04-26) |
| Operators (Ginkgo + envtest) | 188+ specs | Passing (+2 preview unit tests) |
| CI pipeline | 14 workflows | Green |

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
| Shared libs | Go -- errors, logging, naming, health, statemachine, jwt, enforcement, circuitbreaker |
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
| CI/CD | GitHub Actions (14 workflows) |
| Helm charts | 22 charts (all pinned versions) |

### Production Infrastructure (DigitalOcean)

| Resource | Configuration |
|----------|---------------|
| 3 DOKS clusters | v1.32, 2 nodes each, SFO3 |
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
task setup       # Create 3 k3d clusters + install all infrastructure (idempotent)
tilt up          # Start dev loop with hot reload
task doctor      # Verify everything is healthy
```

### Tests

```bash
task test                     # All tests
cd cli && npm test            # CLI tests (Jest, 1079 tests)
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
- [Maturity Assessment](docs/maturity-assessment.md) -- honest 3.6/5 self-assessment (post Sprint 5 preview-envs full shipment + live E2E) with path to 4.0 and 5.0
- [Changelog](CHANGELOG.md)
