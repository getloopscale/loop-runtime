# loopzero-infra

> **LEGACY — retired at AM10.** This Pulumi project is the AS-IS L1 layer,
> live until the AM9 cutover and retired at AM10 by the CTO mandate of
> 2026-06-17 (ADR `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`).
> The target infrastructure is Terraform in `../loopzero-infra-modules` and
> `../loopzero-infra-live`. Do not add resources, providers or cells here; keep
> changes to what the running stacks need until the cutover.

`infra/pulumi-infra` declares the same project name, `loopzero-infra`; select this
project by directory (`pulumi --cwd infra/pulumi ...`), never by a bare stack
name.

Infrastructure-as-Code for the LoopZero platform. Provisions a complete production topology on DigitalOcean using Pulumi.

## Topology

```
                    ┌──────────────────────────────┐
                    │   Management Cluster (DOKS)  │
                    │                              │
                    │  ArgoCD       ← GitOps       │
                    │  Traefik      ← ingress      │
                    │  cert-manager ← TLS          │
                    │  API (Go)     ← platform     │
                    └──────────────┬───────────────┘
                                   │
         ┌─────────────────────────┼──────────────────────────┐
         │                         │                          │
┌────────▼──────────┐   ┌─────────▼──────────┐   ┌───────────▼──────────┐
│   Gitea VM        │   │   Runner VMs       │   │   PostgreSQL (DO)    │
│   (git state)     │   │   (CI/CD builds)   │   │   Managed            │
└────────┬──────────┘   └─────────┬──────────┘   └───────────┬──────────┘
         │                        │                          │
         └────────────┬───────────┴────────────┬─────────────┘
                      │                        │
                ┌─────▼──────┐          ┌──────▼──────────┐
                │  Spaces    │          │ Runtime Cluster  │
                │  (S3-like) │          │ (DOKS)           │
                │  artifacts │          │ user apps only   │
                └────────────┘          └─────────────────┘
```

### Separation of responsibilities

| Component | Where it runs | Why |
|------------|-----------|---------|
| ArgoCD, API, Traefik | Management cluster | Platform tooling isolated from the product |
| User applications | Runtime cluster | Separate blast radius — mgmt down ≠ apps down |
| Gitea | Dedicated VM | Stateful + critical path — outside K8s |
| Runners | Separate VMs | Builds consume resources unpredictably |
| PostgreSQL | Managed (DO) | Automatic backups, failover, patching |
| Artifacts | Spaces (S3) | Cheap, scalable object storage |

## Three-phase pipeline

```
Phase 1: createInfra()        Phase 2: bootstrapCluster()     Phase 3: exposePlatform()
┌────────────────────┐        ┌───────────────────────┐       ┌──────────────────────┐
│ VPC                │        │ Mgmt: cert-manager    │       │ Mgmt DNS:            │
│ Mgmt DOKS          │        │ Mgmt: ArgoCD          │       │   api.{domain}       │
│ Runtime DOKS       │──────> │ Mgmt: Traefik         │─────> │   argocd.{domain}    │
│ Gitea VM           │        │ Runtime: Traefik      │       │   grafana.{domain}   │
│ Runner VMs         │        └───────────────────────┘       │ Runtime DNS:         │
│ PostgreSQL         │                                        │   *.{domain}         │
│ Spaces             │                                        └──────────────────────┘
└────────────────────┘
     InfraOutputs      ──>      BootstrapOutputs       ──>     ExposeOutputs
```

## Structure

```
src/
├── index.ts                    Orchestrator: 3 phases + execution state + wiring
├── config/
│   └── stack.ts                Per-environment config (fail-fast)
├── core/
│   ├── types.ts                CloudProvider, Environment, NodeClass
│   ├── naming.ts               Deterministic names
│   ├── tags.ts                 Standard tags
│   ├── errors.ts               Typed InfraError
│   └── execution.ts            State tracking per phase and component
├── contracts/
│   ├── provider.ts             Factory + 6 interfaces ISP
│   ├── network.ts              NetworkSpec / Output
│   ├── kubernetes.ts           KubernetesClusterSpec / Output
│   ├── storage.ts              ObjectStorageSpec / Output
│   ├── compute.ts              ComputeInstanceSpec / Output (VMs)
│   └── database.ts             DatabaseClusterSpec / Output
├── components/
│   ├── infra.ts                Phase 1: full topology
│   ├── bootstrap.ts            Phase 2: dual-cluster (mgmt + runtime)
│   └── expose.ts               Phase 3: DNS (mgmt + runtime wildcard)
├── __tests__/
│   ├── naming.test.ts          resourceName, bucketName
│   ├── tags.test.ts            standardTags, tagsToStringArray
│   ├── errors.test.ts          InfraError formatting + structured log
│   ├── nodes.test.ts           resolveNodeSize, resolveDbSize
│   └── resolve-provider.test.ts DigitalOcean only; anything else is INVALID_CONFIG
└── providers/
    ├── digitalocean/            Complete implementation (8 files)
    │   ├── index.ts             Factory
    │   ├── nodes.ts             NodeClass → droplet/DB sizes
    │   ├── network.ts           VPC
    │   ├── kubernetes.ts        DOKS cluster
    │   ├── storage.ts           Spaces bucket
        │   ├── compute.ts           Droplets (VMs)
    │   └── database.ts          Managed PostgreSQL
    └── cloudflare/              Tunnel, Worker + KV static delivery
```

## Environments

| Stack | Domain | Mgmt Nodes | Runtime Nodes | Runners | DB |
|-------|--------|------------|---------------|---------|-----|
| `dev` | dev.loopzero.example | small 2–3 | small 2–3 | 1× small | small ×1 |
| `staging` | staging.loopzero.example | small 2–3 | small 2–4 | 2× small | small ×1 |
| `prod` | loopzero.example | medium 3–5 | medium 3–6 | 2× medium | medium ×2 |

## State Backend

Pulumi state is stored in **DigitalOcean Spaces** (S3-compatible). No dependency on Pulumi Cloud.

```
┌──────────────────────────────────┐
│  DigitalOcean Spaces             │
│  s3://loopzero-infra-pulumi-state          │
│  ├── .pulumi/stacks/dev.json     │  ← dev environment state
│  ├── .pulumi/stacks/staging.json │  ← staging environment state
│  └── .pulumi/stacks/prod.json    │  ← prod environment state
└──────────────────────────────────┘
```

### Bootstrap (once, before the first deploy)

The state bucket is "day-zero infrastructure" — it must exist before Pulumi runs.

```bash
# 1. Create Spaces access keys in the DO panel:
#    https://cloud.digitalocean.com/account/api/spaces

# 2. Create the state bucket
doctl serverless functions init  # (or via the DO panel)
doctl compute cdn create --origin loopzero-infra-pulumi-state.nyc3.digitaloceanspaces.com 2>/dev/null || true

# Or with s3cmd:
export AWS_ACCESS_KEY_ID="<spaces-key>"
export AWS_SECRET_ACCESS_KEY="<spaces-secret>"
aws s3 mb s3://loopzero-infra-pulumi-state --endpoint-url https://nyc3.digitaloceanspaces.com

# 3. Configure local variables
export AWS_ACCESS_KEY_ID="<spaces-key>"
export AWS_SECRET_ACCESS_KEY="<spaces-secret>"
export PULUMI_CONFIG_PASSPHRASE="<strong-passphrase>"

# 4. Log in to the S3 backend
pulumi login 's3://loopzero-infra-pulumi-state?endpoint=nyc3.digitaloceanspaces.com&region=us-east-1'
```

## Usage

```bash
npm install
npm run typecheck
npm test

# Log in to the state backend (S3/Spaces)
export AWS_ACCESS_KEY_ID="<spaces-key>"
export AWS_SECRET_ACCESS_KEY="<spaces-secret>"
export PULUMI_CONFIG_PASSPHRASE="<passphrase>"
pulumi login 's3://loopzero-infra-pulumi-state?endpoint=nyc3.digitaloceanspaces.com&region=us-east-1'

# Deploy
pulumi up -s dev

# Outputs
pulumi stack output --json
pulumi stack output execution --json
pulumi stack output wiring --json
pulumi stack output mgmtKubeconfig --show-secrets > core.kubeconfig
pulumi stack output runtimeKubeconfig --show-secrets > runtime.kubeconfig
```

## Stack Outputs

### Phase 1 — Infra

| Output | Type | Description |
|--------|------|-----------|
| `networkId` | `string` | VPC ID |
| `mgmtClusterName` | `string` | Management cluster name |
| `mgmtClusterEndpoint` | `string` | Management cluster API endpoint |
| `mgmtKubeconfig` | `string` (secret) | Management cluster kubeconfig |
| `runtimeClusterName` | `string` | Runtime cluster name |
| `runtimeClusterEndpoint` | `string` | Runtime cluster API endpoint |
| `runtimeKubeconfig` | `string` (secret) | Runtime cluster kubeconfig |
| `giteaIp` | `string` | Gitea public IP |
| `giteaPrivateIp` | `string` | Gitea private IP (VPC) |
| `runnerIps` | `string[]` | Runner VM IPs |
| `dbHost` | `string` | PostgreSQL private host |
| `dbPort` | `number` | PostgreSQL port |
| `dbUri` | `string` (secret) | Full connection string |
| `dbPassword` | `string` (secret) | PostgreSQL password |
| `bucketName` | `string` | Bucket name |
| `bucketEndpoint` | `string` | Bucket endpoint |

### Phase 2 — Bootstrap

| Output | Type | Description |
|--------|------|-----------|
| `mgmtIngressReady` | `boolean` | Traefik deployed on mgmt |
| `mgmtIngressEndpoint` | `string` | Mgmt LB IP/hostname |
| `certManagerReady` | `boolean` | cert-manager deployed |
| `argoCdReady` | `boolean` | ArgoCD deployed |
| `runtimeIngressReady` | `boolean` | Traefik deployed on runtime |
| `runtimeIngressEndpoint` | `string` | Runtime LB IP/hostname |

### Wiring (how the components connect)

```bash
pulumi stack output wiring --json
```

```json
{
  "gitea": {
    "publicUrl": "http://1.2.3.4:3000",
    "privateUrl": "http://10.10.0.5:3000"
  },
  "database": {
    "privateHost": "db-host.db.ondigitalocean.com",
    "port": 25060,
    "database": "defaultdb",
    "user": "doadmin"
  },
  "storage": {
    "bucket": "loopzero-infra-dev-nyc3-artifacts",
    "endpoint": "nyc3.digitaloceanspaces.com"
  },
  "mgmt": {
    "ingressEndpoint": "1.2.3.5",
    "clusterEndpoint": "https://..."
  },
  "runtime": {
    "ingressEndpoint": "1.2.3.6",
    "clusterEndpoint": "https://..."
  }
}
```

### Execution State

```bash
pulumi stack output execution --json
```

```json
{
  "installationId": "dev-2026-03-21T10-00-00-000Z",
  "phases": [
    {
      "phase": "infra",
      "status": "completed",
      "components": [
        { "component": "network", "status": "completed" },
        { "component": "mgmtCluster", "status": "completed" },
        { "component": "runtimeCluster", "status": "completed" },
        { "component": "gitea", "status": "completed" },
        { "component": "runners", "status": "completed" },
        { "component": "database", "status": "completed" },
        { "component": "storage", "status": "completed" }
      ]
    },
    {
      "phase": "bootstrap",
      "status": "completed",
      "components": [
        { "component": "mgmtIngress", "status": "completed" },
        { "component": "mgmtCertManager", "status": "completed" },
        { "component": "mgmtArgocd", "status": "completed" },
        { "component": "runtimeIngress", "status": "completed" }
      ]
    },
    {
      "phase": "expose",
      "status": "pending",
      "components": [
        { "component": "mgmtDns", "status": "pending" },
        { "component": "runtimeDns", "status": "pending" }
      ]
    }
  ]
}
```

## Pinned Helm charts

| Chart | Version | Cluster | Repo |
|-------|--------|---------|------|
| cert-manager | v1.14.5 | mgmt | https://charts.jetstack.io |
| ArgoCD | 7.3.11 | mgmt | https://argoproj.github.io/argo-helm |
| Traefik | 28.3.0 | mgmt + runtime | https://traefik.github.io/charts |

## Provider contracts (ISP)

```
CloudProviderFactory
├── IProviderNetwork          VPC / network
├── IProviderKubernetes       cluster K8s
├── IProviderStorage          object storage (S3-compatible)
├── IProviderCompute          VMs (Droplet / EC2)
└── IProviderDatabase         managed database (PostgreSQL)
```

| Provider | Status |
|----------|--------|
| DigitalOcean | Implemented — the only provider this stack has |

No provider is added here. This stack is AS-IS legacy retired at AM10; the
AWS re-platform is Terraform in `../loopzero-infra-modules` + `../loopzero-infra-live`
(ADR `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`).
The AWS stub and the unused DNS-record strategy (DigitalOcean + Cloudflare
implementations nothing called) were removed on 2026-09-23; DNS for the
platform is owned by the Cloudflare tunnel resource.

## Design decisions

**Why 2 clusters?** The management cluster can go down without affecting apps in production. ArgoCD down means deploys stop while apps keep running — a contained blast radius.

**Why Gitea on a VM?** Stateful and on the critical path. A pod restart in K8s can corrupt data. A dedicated VM is simpler to operate, back up and restore.

**Why runners on VMs?** Builds consume CPU and memory unpredictably. Inside the cluster, one heavy build evicts control-plane pods.

**Why managed PostgreSQL?** Automatic backups, failover and patching with no operational overhead. The platform's state should not depend on a StatefulSet.

**Why ArgoCD only on the management cluster?** ArgoCD is Kubernetes-native and manages the runtime cluster through a kubeconfig. Putting it outside K8s makes no sense.

**AWS portability:** Gitea VM → EC2, Spaces → S3, DOKS → EKS, DB → RDS. The AWS build happens in `../loopzero-infra-modules` and `../loopzero-infra-live` (Terraform), per `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`.

## Dependencies

| Package | Version | Purpose |
|--------|--------|-----------|
| `@pulumi/pulumi` | ^3.153.1 | Core SDK |
| `@pulumi/digitalocean` | ^4.40.0 | DigitalOcean provider |
| `@pulumi/kubernetes` | ^4.18.0 | Kubernetes provider (Phase 2) |
| `vitest` | ^3.2.4 | Test runner (dev) |

## CI/CD Pipeline

> **This section describes workflows that do not exist.** Measured 2026-09-11: `.github/workflows/`
> holds `build-publish.yml`, `ci.yml`, `delivery-gate-armed.yml`, `release-artifacts.yml` and
> `workflow-hygiene.yml`. `infra-preview.yml` and `infra-deploy.yml` are in no commit, and GitHub
> Actions was retired as the pipeline on 2026-05-23. What follows is the intended shape, not what
> runs — and `infra/pulumi*` is AS-IS legacy: live until the AM9 cutover, and retired at AM10,
> per `../loopzero-workspace/docs/decisions/2026-06-17-infra-state-decomposition-aws.md`.
>
> Do not cite Pulumi as the forward path for infra construction, and do not add new Pulumi for target infra.
> This rule is unconditional and carries no date.

### Workflows

| Workflow | Trigger | Action |
|----------|---------|------|
| `ci.yml` (test-pulumi) | Push on any branch | Typecheck + unit tests |
| `infra-preview.yml` | PR to develop/main | `pulumi preview` → PR comment |
| `infra-deploy.yml` | Push to develop | `pulumi up` → dev stack (automatic) |
| `infra-deploy.yml` | Push to main | `pulumi up` → prod stack (requires approval) |
| `infra-deploy.yml` | Manual (workflow_dispatch) | Any command on any stack |

### Flow

```
PR → develop                    Push → develop              Push → main
┌─────────────────┐            ┌─────────────────┐         ┌─────────────────┐
│ typecheck        │            │ typecheck        │         │ typecheck        │
│ vitest           │            │ vitest           │         │ vitest           │
│ pulumi preview   │            │ pulumi up (dev)  │         │ approval gate    │
│ → PR comment     │            │ → execution state│         │ pulumi up (prod) │
└─────────────────┘            └─────────────────┘         └─────────────────┘
```

### Required secrets

Configure these in the GitHub Environments (`infra-dev`, `infra-prod`):

| Secret | Description |
|--------|-----------|
| `DIGITALOCEAN_TOKEN` | DigitalOcean API token (full access, for deploy) |
| `SPACES_ACCESS_KEY_ID` | Spaces access key (state backend S3) |
| `SPACES_SECRET_ACCESS_KEY` | Spaces secret key (state backend S3) |
| `PULUMI_CONFIG_PASSPHRASE` | Passphrase encrypting secrets in the state |

### GitHub Environments

| Environment | Protection |
|-------------|-----------|
| `infra-dev` | None — automatic deploy |
| `infra-staging` | Optional — manual deploy via workflow_dispatch |
| `infra-prod` | Required reviewers + deployment branch = main |

### Manual operations

```bash
# Via GitHub Actions UI → workflow_dispatch:
#   Stack: dev | staging | prod
#   Command: up | preview | refresh | destroy
#   Message: reason for the operation

# Exemplos:
# - refresh: reconcile state with real resources (drift detection)
# - destroy: full environment teardown (requires approval for prod)
# - preview: dry-run before a manual deploy
```

### Concurrency

- **Preview (PRs):** cancel-in-progress: true — safe, it is read-only
- **Deploy:** cancel-in-progress: false — NEVER cancel a `pulumi up` in flight
