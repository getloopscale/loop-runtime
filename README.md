<p align="center">
  <h1 align="center">LoopZero</h1>
  <p align="center">
    <strong>The deployment platform for teams who outgrew Vercel but don't want to manage Kubernetes.</strong>
  </p>
  <p align="center">
    <a href="#project-status">Status</a> &bull;
    <a href="#quickstart">Quickstart</a> &bull;
    <a href="#cli-reference">CLI Reference</a> &bull;
    <a href="#how-it-works">How It Works</a> &bull;
    <a href="#development">Development</a>
  </p>
</p>

---

Three commands. Your monorepo is live.

```bash
loopzero login
loopzero init
loopzero deploy
```

```
✓ Building api...    [24s]
✓ Building web...    [18s]
✓ Building worker... [12s]
✓ Deploying...       [15s]

Live:
  api    → https://acme-my-saas-api-production.loopzero.example
  web    → https://acme-my-saas-web-production.loopzero.example (CDN)
  worker → (background)
```

No Kubernetes. No Helm. No Dockerfile required.

---

## Project Status

> **Phase: Pre-release.** Infrastructure validated on production (DigitalOcean). Not yet available to external users.
>
> Last validation: 2026-03-29 — full destroy + recreate via GitHub Actions pipeline (10m33s).

### What Works (Validated on DigitalOcean)

| Capability | Status | Evidence |
|------------|:------:|----------|
| `loopzero login` / `logout` / `whoami` | **Working** | GitHub token auth + RBAC |
| `loopzero init` (monorepo detection) | **Working** | Turbo/Nx/Lerna/pnpm + 10 frameworks |
| `loopzero deploy` (full pipeline) | **Working** | 24s build, pod Running, health OK |
| `loopzero deploy` (static frontends) | **Working** | S3 upload + CF Worker KV routing + CDN delivery |
| `loopzero rollback` | **Working** | ~3s revert, version verified |
| `loopzero logs` | **Working** | Loki cross-cluster, real-time streaming |
| `loopzero status` | **Working** | Project status with active release + URL |
| `loopzero secret set/list/delete` | **Working** | ESO sync <30s, values never exposed |
| `loopzero cancel` | **Working** | Cancels running/queued builds |
| `loopzero project create/list/inspect/delete` | **Working** | Full CRUD with clean resource removal |
| `loopzero env create/list/inspect/delete` | **Working** | Production env protected from deletion |
| `loopzero team list/add/remove/role` | **Working** | RBAC: owner/admin/member roles |
| `loopzero build list/inspect` | **Working** | Build history with env filter |
| `loopzero validate` | **Working** | Local loopzero.yaml validation |
| TLS (HTTPS) | **Working** | Let's Encrypt DNS-01 via Cloudflare |
| Observability | **Working** | Prometheus + Grafana + Loki + 4 SLO alert rules |
| Concurrent deploy protection | **Working** | `pg_advisory_xact_lock` → 409 on conflict |
| Idempotency | **Working** | Retry-safe via `Idempotency-Key` header |
| SecurityContext (non-root pods) | **Working** | `runAsNonRoot: true`, `drop: ALL`, `readOnlyRootFilesystem` |
| PodSecurity Admission | **Working** | `restricted` profile enforced on all tenant namespaces |
| NetworkPolicy | **Working** | Mandatory deny-all + allow-ingress + allow-egress |
| IaC destroy + recreate | **Working** | Full pipeline in 10m33s via GitHub Actions |
| ArgoCD notifications | **Working** | Push-based sync callback activation |

### What's Not Built Yet

| Feature | Sprint | Notes |
|---------|--------|-------|
| Preview environments (PR → temp URL → cleanup) | Sprint 5 | CRD defined, controller not implemented |
| `loopzero promote` (staging → production) | Sprint 4 | Not started |
| Image signing (cosign + syft) | Sprint 6 | Supply chain security, not started |
| Chaos engineering tests | Sprint 8 | Not started |
| AppSecret controller | — | CRD defined, no reconciler. Secrets work via ESO directly but lack lifecycle automation |

### Known Issues

| Issue | Severity | Detail |
|-------|----------|--------|
| 1 CLI test failing | High | `--json` output for missing required argument returns text instead of JSON (`index.test.ts:209`). Stability contract §0.3 violation. |
| `feat/auth` branch not merged | High | 440 commits ahead of `main` with team management, security, and CI improvements. `main` is stale. |
| Hardening roadmap stale | Low | `docs/hardening-sprints/ROADMAP.md` marks Sprint 7 (security) as TODO, but SecurityContext + PSA + NetworkPolicy are already implemented in code. |

### Test Suite

| Component | Tests | Status |
|-----------|------:|--------|
| CLI (Jest) | 747 | 746 passing, 1 failing |
| API (Go) | 28 packages | All passing |
| Shared libraries (Go) | 7 packages | All passing |
| Operators (Ginkgo + envtest) | — | Passing (requires envtest binary) |
| Infra (Pulumi + vitest) | — | Passing |
| CI pipeline | 4 workflows | Green (lint, test, security, manifests) |

---

## Why LoopZero

You have a monorepo with an API, a frontend, and workers. Vercel handles the frontend beautifully — but the rest is a mess of Dockerfiles, Helm charts, and CI pipelines that only one person understands.

LoopZero gives you Vercel-quality DX for everything Vercel can't deploy.

| | Vercel | LoopZero |
|---|:---:|:---:|
| Static frontends (React, Vite, Next.js) | Yes | Yes (CDN) |
| API servers (Express, NestJS, Go, Python) | Serverless only | Any framework |
| Background workers | — | Yes |
| Multiple services per repo | — | Yes |
| Rollback in seconds | Yes | Yes (~3s) |
| Secrets per environment | Yes | Yes (encrypted, ESO sync) |
| Preview environments per PR | Yes | Not yet (Sprint 5) |
| CLI-first | Yes | Yes |
| Team management + RBAC | Yes | Yes |
| Real-time build logs | Yes | Yes (Loki) |

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

LoopZero detects your monorepo structure (Turborepo, Nx, pnpm workspaces) and frameworks (Node.js, Go, Python, Next.js, Vite, and 10+ others), then generates `loopzero.yaml`:

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

### 3. Deploy

```bash
loopzero deploy
```

That's it. TLS, health checks, ingress routing, and zero-downtime rolling updates are automatic.

---

## CLI Reference

### Core Workflow

| Command | Purpose |
|---------|---------|
| `loopzero login` | Authenticate via GitHub |
| `loopzero init` | Detect monorepo, generate `loopzero.yaml` |
| `loopzero deploy` | Build and deploy all apps |
| `loopzero deploy --env staging` | Deploy to a specific environment |
| `loopzero deploy --app api` | Deploy a single app |
| `loopzero deploy --no-wait` | Submit and return immediately |
| `loopzero status` | Show project status, active release, URL |
| `loopzero logs` | Show build logs (auto-detects last build) |
| `loopzero rollback` | Revert to previous release (~3s) |
| `loopzero cancel <build-id>` | Cancel a running build |
| `loopzero validate` | Validate `loopzero.yaml` locally |

### Project & Environment Management

| Command | Purpose |
|---------|---------|
| `loopzero project list` | List all projects |
| `loopzero project inspect <name>` | Show project details |
| `loopzero project create <name>` | Create a new project |
| `loopzero project delete <name>` | Delete project and all resources |
| `loopzero env list` | List environments |
| `loopzero env create <name>` | Create a new environment |
| `loopzero env inspect <name>` | Show environment details |
| `loopzero env delete <name>` | Delete environment (with `--confirm`) |

### Team Management

| Command | Purpose |
|---------|---------|
| `loopzero team list` | List team members and roles |
| `loopzero team add <user>` | Add a member (`--role admin` or `--role member`) |
| `loopzero team remove <user>` | Remove a member |
| `loopzero team role <user> <role>` | Change a member's role (owner only) |

### Secrets

| Command | Purpose |
|---------|---------|
| `loopzero secret set KEY=value` | Set a secret (encrypted at rest, synced via ESO) |
| `loopzero secret list` | List secret names (values never exposed) |
| `loopzero secret delete KEY` | Delete a secret |

### Build Management

| Command | Purpose |
|---------|---------|
| `loopzero build list` | List recent builds |
| `loopzero build inspect <id>` | Show build details |

### Session

| Command | Purpose |
|---------|---------|
| `loopzero whoami` | Show authenticated user and team |
| `loopzero logout` | Remove stored credentials |

Every command supports `--json` for CI/CD integration. JSON output is a **stability contract** — field names never change without a major version bump.

### Exit Codes

| Code | Meaning |
|:----:|---------|
| `0` | Success |
| `1` | Usage error |
| `2` | Build failure |
| `3` | Deploy/operation failure |
| `4` | Authentication error |
| `5` | Partial failure (some apps failed) |

---

## How It Works

From the developer's perspective: `loopzero deploy` → URL. Everything below is invisible.

```
                    ┌──────────────────────────────────────────┐
  loopzero deploy ───►  │  LoopZero API                                │
                    │  • Lock environment (advisory lock)       │
                    │  • Submit build to Argo Workflows         │
                    └────────────┬─────────────────────────────┘
                                 │
                    ┌────────────▼─────────────────────────────┐
                    │  Build Cluster                            │
                    │  • BuildKit (rootless, single-container)  │
                    │  • Push image to container registry       │
                    └────────────┬─────────────────────────────┘
                                 │
              ┌──────────────────┼──────────────────┐
              ▼                  ▼                   ▼
        Server apps        Static apps         Worker apps
        (API, Go, etc)     (Vite, Next.js)     (BullMQ, etc)
              │                  │                   │
     ArgoCD sync          Upload to S3 +       ArgoCD sync
     → K8s Deployment     CF Worker KV         → K8s Deployment
     → Health check       → CDN delivery       → (no ingress)
              │                  │                   │
              ▼                  ▼                   ▼
          Live URL           Live URL           Running
```

### Platform Guarantees

- **Serialization** — No two operations can mutate the same environment concurrently. Concurrent deploys get `409`.
- **Idempotency** — Retrying any operation produces the same result.
- **Isolation** — Tenants are isolated at compute, network, and build levels.
- **Consistency** — A release is marked active only after cluster confirmation.
- **Rollback** — Every deploy creates a rollback point. `loopzero rollback` reverts in ~3 seconds.
- **Clean delete** — `loopzero project delete` removes manifests, triggers prune, verifies pods are gone.
- **Security** — Pods run as non-root, drop all capabilities, use dedicated ServiceAccount, PSA restricted.

### Architecture

Four-plane separation with dedicated Kubernetes clusters:

| Plane | Cluster | Purpose |
|-------|---------|---------|
| Developer | — | CLI (TypeScript) + API (Go) |
| Control | mgmt | Operators, Gitea, PostgreSQL, ArgoCD, Traefik, cert-manager |
| Build | build | Argo Workflows + BuildKit (rootless) |
| Workload | workload | User applications managed by ArgoCD |

### Stack

| Layer | Technology |
|-------|-----------|
| CLI | TypeScript + Commander |
| API | Go + chi + pgx |
| Operators | Go + kubebuilder + controller-runtime (9 CRDs, 7 controllers) |
| Build engine | Argo Workflows + BuildKit (rootless, single-container) |
| GitOps | ArgoCD (manual sync) + Gitea |
| Database | PostgreSQL 16 (managed, advisory locks) |
| Secrets | External Secrets Operator |
| Edge | Cloudflare Tunnel + Worker + KV |
| Static assets | DigitalOcean Spaces + CDN |
| Monitoring | Prometheus + Grafana + Loki + Alertmanager |
| IaC | Pulumi (TypeScript) — DigitalOcean + Cloudflare |
| CI/CD | GitHub Actions (4 workflows: CI, Build Images, Infra Deploy, Infra Preview) |

### Production Infrastructure (DigitalOcean)

| Resource | Configuration |
|----------|---------------|
| 3 DOKS clusters | v1.32.13, SFO3 (dev) / NYC3 (prod) |
| PostgreSQL | v16, managed, HA (3 nodes in prod) |
| Container Registry | DigitalOcean Container Registry (Professional) |
| Edge proxy | Cloudflare Tunnel (DDoS/WAF, zero public IPs) |
| Static delivery | CF Worker + DO Spaces CDN |
| DNS | `*.dev.loopzero.example` (dev), `*.loopzero.example` (prod) |
| State backend | DO Spaces (S3-compatible) for Pulumi state |

---

## Configuration

The only file you write is `loopzero.yaml`. Generated by `loopzero init`.

```yaml
version: 1
project: my-saas

apps:
  api:
    path: apps/api
    framework: express
    type: server
    build: buildpack
    port: 3000

  web:
    path: apps/web
    framework: vite
    type: frontend
    build: buildpack

  worker:
    path: apps/worker
    type: worker
    build: buildpack
```

**Auto-detected frameworks:** Node.js, Express, NestJS, Next.js, Vite, Remix, Go, Python/FastAPI, Python/Django, Java/Spring, Ruby on Rails, and any Dockerfile.

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
task test              # All tests (unit + lint + manifests)
cd cli && npm test     # CLI tests (Jest)
go test ./api/...      # API tests (Go testing)
go test ./operators/...# Operator tests (Ginkgo + envtest)
go test ./pkg/...      # Shared library tests
```

### Local Observability

| Tool | URL |
|------|-----|
| Grafana | [localhost:3002](http://localhost:3002) |
| Prometheus | [localhost:9090](http://localhost:9090) |
| ArgoCD | [localhost:30082](http://localhost:30082) |

---

## Partial Failure Handling

Apps build and deploy independently. A failing app does not block siblings.

```
$ loopzero deploy
✓ api     → deployed
✗ worker  → build failed (run: loopzero logs --build b-4a2c)

Exit code: 5 (partial failure)
```

---

## What LoopZero Does Not Solve

- **Database migrations** — LoopZero deploys code, not schema changes.
- **Application data repair** — If a bug corrupted data, that's on you.
- **Custom domains** — All URLs are on `*.loopzero.example` for now.
- **Multi-region** — Single-region in v1.
- **Preview environments** — Planned for Sprint 5, not yet implemented.
- **Image signing** — Supply chain security (cosign + syft) planned for Sprint 6.

---

## Stability Contract

- Commands are permanent — no removal without 6-month deprecation + major version bump.
- Flags are permanent — semantics don't change.
- `--json` output is a contract — shape doesn't change without major version.
- Exit codes are permanent.
- Default behavior is frozen.

---

## License

[Apache License 2.0](LICENSE)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Every PR starts with a failing test.

## Links

- [System Design](docs/technical.md) — authoritative specification
- [Sprint Roadmap](docs/sprints/ROADMAP.md)
- [Hardening Roadmap](docs/hardening-sprints/ROADMAP.md)
- [Changelog](CHANGELOG.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security Policy](SECURITY.md)
