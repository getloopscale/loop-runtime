# Pulumi Local Deploy — Provisioning DO Resources from Your Machine

> How to run `pulumi up` locally to create/update DigitalOcean infrastructure.
> This is the same operation the CI pipeline does, but from your terminal.

---

## Prerequisites

### Tools Required

```bash
# Pulumi CLI
curl -fsSL https://get.pulumi.com | sh
pulumi version  # >= 3.227.0

# Node.js 20+
node --version  # v20.x

# kubectl (for validation)
kubectl version --client

# doctl (for registry operations)
doctl version
```

### Credentials Required

All credentials are in the `.env` file at the repo root. **Never commit this file.**

| Variable | Purpose | How to get |
|---|---|---|
| `DIGITALOCEAN_TOKEN` | DO API access (clusters, DB, DNS) | DO Dashboard → API → Generate Token |
| `SPACES_ACCESS_KEY_ID` | S3-compatible storage access | DO Dashboard → API → Spaces Keys |
| `SPACES_SECRET_ACCESS_KEY` | S3-compatible storage secret | Same as above |
| `PULUMI_CONFIG_PASSPHRASE` | Encrypts secrets in Pulumi state | Team shared secret |
| `CLOUDFLARE_API_TOKEN` | CF Tunnel, Worker, WAF, DNS | CF Dashboard → API Tokens |

---

## Architecture: 3 Projects

```
loopzero-infra          → Cloud primitives (clusters, DB, buckets, VPC)
  ↓ StackReference
loopzero-platform       → K8s services (Helm: ArgoCD, Traefik, Gitea, Prometheus, Grafana)
  ↓ StackReference
loopzero-control-plane  → Application services (API, Controller, Ory)
```

Each project has its own Pulumi stack. Deploy order matters: **infra → platform → control-plane**.

---

## Environments

| Environment | Branch | Stack name | Backend (Pulumi state) | Region |
|---|---|---|---|---|
| **dev** | `develop` | `dev` | `s3://loopzero-infra-pulumi-state` (nyc3) | sfo3 |
| **homolog** | `homolog` | `homolog` | `s3://loopzero-homolog-pulumi-state` (sfo3) | nyc3 |
| **prod** | `main` | `prod` | `s3://loopzero-infra-pulumi-state` (nyc3) | nyc3 |

---

## Quick Start: Deploy Homolog

### 1. Load credentials

```bash
# From the repo root
cd ~/Projetos/loopzero/loopzero

# Load env vars (credentials)
export $(grep -E '^(SPACES_ACCESS_KEY_ID|SPACES_SECRET_ACCESS_KEY|PULUMI_CONFIG_PASSPHRASE|DIGITALOCEAN_TOKEN|CLOUDFLARE_API_TOKEN)=' .env | xargs)

# Pulumi S3 backend uses AWS SDK — needs these vars
export AWS_ACCESS_KEY_ID="$SPACES_ACCESS_KEY_ID"
export AWS_SECRET_ACCESS_KEY="$SPACES_SECRET_ACCESS_KEY"
```

**Important:** These exports only last for your current terminal session. Run them again if you open a new terminal.

### 2. Login to the Pulumi backend

```bash
# Homolog backend (separate from dev/prod)
pulumi login "s3://loopzero-homolog-pulumi-state?endpoint=sfo3.digitaloceanspaces.com&region=us-east-1"

# Dev/prod backend
# pulumi login "s3://loopzero-infra-pulumi-state?endpoint=nyc3.digitaloceanspaces.com&region=us-east-1"
```

### 3. Deploy loopzero-infra

```bash
cd infra/pulumi-infra
npm ci                        # Install dependencies (first time only)
pulumi stack select homolog   # Select the stack

# Preview what will change (safe — no mutations)
pulumi preview -s homolog

# Apply changes (creates/updates real DO resources)
pulumi up --yes -s homolog
```

Expected resources: VPC, 2 K8s clusters, PostgreSQL, 4 Spaces buckets, CDN, Container Registry.
Takes ~6-10 minutes on first deploy (DOKS provisioning).

### 4. Deploy loopzero-platform

```bash
cd ../pulumi-platform
npm ci
pulumi stack select homolog

pulumi preview -s homolog
pulumi up --yes -s homolog
```

Expected resources: cert-manager ×2, Traefik ×2, ArgoCD, Gitea, Prometheus, Grafana (with 13 dashboards), Tempo, OTEL Collector, Loki, Promtail.
Takes ~5-8 minutes.

### 5. Deploy loopzero-control-plane

```bash
cd ../pulumi-control-plane
npm ci
pulumi stack select homolog

pulumi preview -s homolog
pulumi up --yes -s homolog
```

Expected resources: LoopZero API, Tenant Controller, Ory Kratos, Ory Hydra, IngressRoutes.

---

## Common Operations

### Preview changes (no mutations)

```bash
pulumi preview -s homolog
```

Always preview before `up`. The diff shows exactly what will be created/updated/deleted.

### Check stack outputs

```bash
# All outputs
pulumi stack output -s homolog

# Specific output
pulumi stack output mgmtClusterName -s homolog

# Secret outputs (kubeconfigs, passwords)
pulumi stack output mgmtKubeconfig --show-secrets -s homolog

# JSON format (for scripting)
pulumi stack output clusterRegistry --show-secrets --json -s homolog
```

### Connect to clusters via kubectl

```bash
# Save kubeconfig to temp file and use it
pulumi stack output mgmtKubeconfig --show-secrets -s homolog > /tmp/mgmt.kubeconfig
kubectl --kubeconfig /tmp/mgmt.kubeconfig get pods -n platform-system

# Or inline (one-liner)
pulumi stack output mgmtKubeconfig --show-secrets -s homolog | kubectl --kubeconfig /dev/stdin get nodes
```

### Set secrets

```bash
# Secrets are encrypted in Pulumi state (safe to commit in Pulumi.homolog.yaml)
pulumi config set --secret spacesAccessKeyId "$SPACES_ACCESS_KEY_ID" -s homolog
pulumi config set --secret digitaloceanToken "$DIGITALOCEAN_TOKEN" -s homolog
```

### Destroy environment

```bash
# Reverse order: control-plane → platform → infra
cd infra/pulumi-control-plane && pulumi destroy --yes -s homolog
cd ../pulumi-platform && pulumi destroy --yes -s homolog
cd ../pulumi-infra && pulumi destroy --yes -s homolog
```

**Warning:** `pulumi destroy` on infra deletes clusters and databases. Data loss is permanent.

### Cancel a stuck operation

```bash
pulumi cancel -s homolog
```

Use when a `pulumi up` was interrupted (Ctrl+C, network loss) and the stack is locked.

### Refresh state (sync with reality)

```bash
pulumi refresh --yes -s homolog
```

Use when resources were changed outside Pulumi (DO dashboard, kubectl) and state is stale.

---

## Troubleshooting

### `InvalidAccessKeyId` error

```
error: read ".pulumi/meta.yaml": blob (key ".pulumi/meta.yaml") (code=Unknown): InvalidAccessKeyId
```

**Cause:** `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY` env vars not set.

**Fix:** Re-export credentials:
```bash
export $(grep -E '^(SPACES_ACCESS_KEY_ID|SPACES_SECRET_ACCESS_KEY|PULUMI_CONFIG_PASSPHRASE)=' .env | xargs)
export AWS_ACCESS_KEY_ID="$SPACES_ACCESS_KEY_ID"
export AWS_SECRET_ACCESS_KEY="$SPACES_SECRET_ACCESS_KEY"
```

### `organization name must be 'organization'`

```
error: Preview failed: organization name must be 'organization'
```

**Cause:** StackReference uses wrong format. S3 backend requires `organization/project/stack`.

**Fix:** Check `Pulumi.homolog.yaml` — StackReference must be:
```yaml
loopzero-platform:infraStackRef: organization/loopzero-infra/homolog
```
Not `loopzero-infra/homolog` (missing `organization/` prefix).

### `cluster limit exceeded`

```
error: creating a cluster would exceed cluster limit
```

**Cause:** DO account limit on DOKS clusters (default varies by account).

**Fix:** Check current clusters: `doctl kubernetes cluster list`. Delete unused clusters or contact DO support to increase limit.

### `VPC overlaps`

```
error: This range/size overlaps with another VPC network
```

**Cause:** Two VPCs in the same region with the same CIDR.

**Fix:** Set a different CIDR in the stack config:
```yaml
loopzero-infra:vpcCidr: "10.30.0.0/16"  # Different from dev's 10.20.0.0/16
```

### Helm timeout (Gitea, Prometheus)

```
error: Helm release failed to initialize completely: context deadline exceeded
```

**Cause:** PVC provisioning or pod startup took longer than Helm timeout (5min default).

**Fix:** Usually resolves on retry — the resources are already partially created:
```bash
pulumi up --yes -s homolog  # Retry
```

### `permission denied for schema public` (Gitea)

```
Failed to initialize ORM engine: pq: permission denied for schema public
```

**Cause:** DO managed PostgreSQL 15+ restricts schema permissions for custom users.

**Fix:** Gitea must use `doadmin` user (has full privileges). This is configured in the Gitea component.

---

## Helper Script

For quick provisioning without remembering all steps:

```bash
./infra/scripts/provision-homolog.sh setup    # Initialize stacks + set secrets
./infra/scripts/provision-homolog.sh preview  # Preview all 3 projects
./infra/scripts/provision-homolog.sh up       # Deploy all 3 projects
./infra/scripts/provision-homolog.sh destroy  # Destroy all (reverse order)
```

---

## Cost Awareness

| Resource | Homolog (minimal) | Monthly cost |
|---|---|---|
| 2 DOKS clusters (2 nodes each, small) | 4 nodes × $24 | ~$96 |
| PostgreSQL managed (1 node, small) | 1 node | ~$15 |
| 4 Spaces buckets | Storage only | ~$5 |
| 2 Load Balancers (Traefik) | Always-on | ~$24 |
| CDN | Bandwidth only | ~$0 |
| **Total** | | **~$140/month** |

**Destroy when not in use** to avoid unnecessary costs:
```bash
./infra/scripts/provision-homolog.sh destroy
```
