<p align="center">
  <h1 align="center">LoopZero</h1>
  <p align="center">
    <strong>The deployment platform for teams who outgrew Vercel but don't want to manage Kubernetes.</strong>
  </p>
  <p align="center">
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

## Why LoopZero

You have a monorepo with an API, a frontend, and workers. Vercel handles the frontend beautifully — but the rest is a mess of Dockerfiles, Helm charts, and CI pipelines that only one person understands.

LoopZero gives you Vercel-quality DX for everything Vercel can't deploy.

| | Vercel | LoopZero |
|---|:---:|:---:|
| Static frontends (React, Vite, Next.js) | ✓ | ✓ via CDN |
| API servers (Express, NestJS, Go, Python) | Serverless only | ✓ Any framework |
| Background workers | - | ✓ |
| Multiple services per repo | - | ✓ |
| Rollback in seconds | ✓ | ✓ (~3s) |
| Secrets per environment | ✓ | ✓ (encrypted, ESO sync) |
| Preview environments per PR | ✓ | Roadmap |
| CLI-first | ✓ | ✓ |
| Team management + RBAC | ✓ | ✓ |
| Real-time build logs | ✓ | ✓ (Loki) |

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

### Stack

| Layer | Technology |
|-------|-----------|
| CLI | TypeScript + Commander |
| API | Go + chi + pgx |
| Build engine | Argo Workflows + BuildKit (rootless) |
| GitOps | ArgoCD (manual sync) + Gitea |
| Database | PostgreSQL (advisory locks, RLS) |
| Secrets | External Secrets Operator |
| Edge | Cloudflare Tunnel + Worker + KV |
| Static assets | DigitalOcean Spaces + CDN |
| Monitoring | Prometheus + Grafana + Loki + Alertmanager |
| IaC | Pulumi (TypeScript) |
| Operators | Go + kubebuilder + controller-runtime |

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

Go 1.25+, Node.js 20+, Docker, [k3d](https://k3d.io), [Tilt](https://tilt.dev), [Task](https://taskfile.dev), [Helm](https://helm.sh) v3.14+, kubectl

### Setup

```bash
task setup       # Create 3 k3d clusters + install all infrastructure
tilt up          # Start dev loop with hot reload
task doctor      # Verify everything is healthy
```

### Tests

```bash
task test              # All tests (unit + lint + manifests)
cd cli && npm test     # 747 CLI tests (Jest)
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

- [Roadmap](docs/sprints/ROADMAP.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)
- [Security Policy](SECURITY.md)
