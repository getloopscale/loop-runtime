# ADR-001: Pulumi Project Boundaries

> Status: PROPOSED
> Date: 2026-04-10
> Decision: Split monolithic infra/pulumi into 3 projects
> Depends on: INFRA-SPLIT Baseline (Epic 0)

---

## Context

The monolithic `infra/pulumi/` project manages ~160 resources across 3 distinct lifecycles:
- Cloud primitives (44 commits, rarely changes)
- Platform services (128 commits, moderate changes)
- Application services (81 commits, frequent changes — mostly in mgmt-bootstrap.ts)

A single `pulumi up` takes all 160 resources through preview/diff/apply. A failed Ory Hydra config change blocks a cluster scaling operation. A Stripe key rotation requires previewing database cluster state.

## Decision

Split into 3 Pulumi projects connected via `StackReference`:

```
loopzero-infra ──outputs──▶ loopzero-platform ──outputs──▶ loopzero-control-plane
   (rarely)                (moderate)                 (frequent)
```

### loopzero-infra — Cloud Primitives

**Lifecycle:** Changes on cluster upgrades, scaling, new regions. Days/weeks between deploys.

**Resources (16):**

| Resource | Notes |
|---|---|
| VPC | Shared network for all 3 clusters |
| K8s Clusters ×3 | mgmt, build, runtime |
| PostgreSQL Cluster + 4 DBs | gitea, loopzero, kratos, hydra |
| Database Firewall | trusted sources |
| Spaces Buckets ×4 | artifacts, builds, logs, static-assets |
| CDN | Static assets edge cache |
| Container Registry | Shared across stacks, `protect: true` |

**Exports (stable contract):**
- `kubeconfigs` (3x, secret)
- `database` (host, port, uri, password — secret)
- `bucketNames` (artifacts, builds, logs, static-assets)
- `bucketEndpoints` (spacesEndpoint, s3Endpoint, staticBucketEndpoint)
- `registryEndpoint`, `registryName`
- `vpcId`, `networkId`
- `clusterNames`, `clusterEndpoints`

**Config:** `environment`, `provider`, `region`, `domain`, `dnsZone`, cluster sizing, DB sizing, `spacesAccessKeyId`, `spacesSecretAccessKey`, `digitaloceanToken`

**Shared code:** `core/` (naming, tags, errors, types, execution), `contracts/`, `providers/`

---

### loopzero-platform — Infrastructure Services

**Lifecycle:** Changes on Helm upgrades, new observability tooling, networking changes. Hours/days between deploys.

**Resources (~110):**

| Category | Resources |
|---|---|
| **Edge/Networking** | CF Tunnel + config, CF Worker + KV + route, DNS records (mgmt + runtime wildcard) |
| **Ingress** | cert-manager ×2, Traefik ×2, ClusterIssuers ×2 |
| **GitOps** | ArgoCD, Gitea (Helm + bootstrap Job), ArgoCD repo credentials |
| **Storage** | MinIO (in-cluster), MinIO credentials ×2 |
| **Build Engine** | Argo Workflows, CWTs, build ServiceAccount |
| **Logging** | Loki, Promtail ×2 (build + runtime), NodePorts |
| **Observability** | Prometheus, Grafana, Grafana dashboards ConfigMap, Tempo, OTEL Collector |
| **Secret Sync** | ESO + ClusterSecretStore, scoped SA tokens, RBAC |
| **Utilities** | metrics-server ×3, monitoring namespaces, platform namespaces |

**Consumes (from loopzero-infra via StackReference):**
- kubeconfigs (3x) — to create K8s providers
- spacesEndpoint, s3Endpoint — for Loki S3 backend, MinIO credentials
- logsBucketName — for Loki storage
- staticBucketEndpoint — for CF Worker S3 origin
- registryEndpoint — for image references

**Exports:**
- `mgmtIngressEndpoint`, `runtimeIngressEndpoint` — LB IPs for DNS
- `lokiEndpoint` — cross-cluster log access (build node IP + port)
- `argoMetricsEndpoint` — cross-cluster Prometheus scrape
- `giteaReady`, `argoCdReady`, `prometheusReady`, `grafanaReady` — readiness
- `cfTunnelId`, `cfKvNamespaceId`, `cfWorkerName` — CF resource IDs
- `mgmtDnsConfigured`, `runtimeDnsConfigured` — DNS readiness

**Config:** `cfApiToken`, `cfAccountId`, `cfZoneId`, `spacesAccessKeyId`, `spacesSecretAccessKey` (duplicated from infra — needed for Loki/MinIO), `giteaAdminPassword`, `minioRootPassword`, `lokiPushUrl`, `tempoStorageBucket`

---

### loopzero-control-plane — Application Services

**Lifecycle:** Changes on every feature, API update, auth change, billing integration. Minutes/hours between deploys.

**Resources (~35):**

| Category | Resources |
|---|---|
| **LoopZero API** | Deployment, Service, PDB, ConfigMap, Secrets (DB, CF, GitHub App), RBAC, scoped kubeconfigs (build + runtime), migrations ConfigMap |
| **Cloud Service** | Deployment, Service, ConfigMap, Secret (Stripe, OAuth, session) |
| **Cloud Dashboard** | Deployment, Service |
| **Tenant Controller** | Deployment, Service, PDB, CRDs (yaml.ConfigGroup), RBAC, scoped kubeconfig, credentials, random callback token |
| **Auth** | Ory Kratos (Helm + UI Deployment + Service), Ory Hydra (Helm) |
| **Networking** | IngressRoutes (api, argocd, cloud gateway), Certificates, NetworkPolicies |
| **Cross-cluster** | ArgoCD cluster secret (runtime registration), scoped kubeconfig creator |

**Consumes (from loopzero-infra via StackReference):**
- kubeconfigs (mgmt, build, runtime) — for K8s providers + scoped kubeconfig creation
- database (uri, host, port, password) — for API + migrations
- registryEndpoint — for image tags
- bucketEndpoints — for API S3 config

**Consumes (from loopzero-platform via StackReference):**
- lokiEndpoint — for API log query config
- argoMetricsEndpoint — not directly, but available
- cfKvNamespaceId — for API CF credentials secret (if CF enabled)

**Config:** `stripeApiKey`, `stripeWebhookSecret`, `stripePriceProId`, `stripePriceTeamId`, `sessionTokenSecret`, `serviceAuthSecret`, `resendApiKey`, `githubOAuthClientId/Secret`, `googleOAuthClientId/Secret`, `loopzeroApiDbPassword`

---

## Boundary Decisions (Contested Cases)

### 1. Observability → Platform (not Control-Plane)
**Rationale:** Prometheus, Grafana, Tempo, OTEL are infrastructure monitoring tools, not application services. They change on Helm version upgrades, not on API features. Grafana dashboards are provisioned via ConfigMap which is managed alongside Grafana's Helm release.

### 2. Ory Kratos/Hydra → Control-Plane (not Platform)
**Rationale:** Ory services are application-level auth. They change when auth features change (OAuth providers, session config, cookie secrets). They depend on control-plane secrets (OAuth client IDs) and their Helm values include application config (UI URLs, webhook endpoints). They are consumers of platform infrastructure (database, ingress), not providers of it.

### 3. ArgoCD → Platform (not Control-Plane)
**Rationale:** ArgoCD is a GitOps delivery mechanism, not an application service. It changes rarely after initial setup. The Tenant Controller creates ArgoCD Applications dynamically, but ArgoCD itself is infrastructure.

### 4. DNS Records → Platform (not Infra)
**Rationale:** DNS records depend on ingress endpoints (Phase 2 outputs). They change when subdomains are added. The Cloudflare Tunnel (platform) owns the runtime wildcard. Keeping DNS in platform avoids a circular dependency (platform needs tunnel, DNS needs platform ingress).

### 5. CF Tunnel/Worker/KV → Platform (not Infra)
**Rationale:** CF resources bridge cloud infrastructure and application routing. They depend on runtime cluster kubeconfig (infra output) and are consumed by control-plane (API uses KV namespace ID). Placing them in platform gives the right change cadence — they change with networking decisions, not cloud provisioning.

### 6. CRDs → Control-Plane (with Tenant Controller)
**Rationale:** CRDs define the API contract for operators. They change when controller features change. The yaml.ConfigGroup that installs CRDs should deploy alongside the controller that consumes them.

### 7. Scoped Kubeconfigs → Control-Plane
**Rationale:** `createScopedKubeconfig` creates ServiceAccounts + RBAC on target clusters. These are consumed by API and Tenant Controller. They change when RBAC requirements change (new API capabilities, new resource types).

---

## StackReference Contract

```
                    loopzero-infra
                   /          \
           StackRef            StackRef
              |                    |
       loopzero-platform      loopzero-control-plane
              \                    |
           StackRef ───────────────┘
```

### loopzero-infra exports (consumed by platform + control-plane):

```typescript
export interface InfraStackOutputs {
  // Network
  vpcId: string;
  networkId: string;

  // Clusters
  mgmtClusterName: string;
  mgmtClusterEndpoint: string;
  mgmtKubeconfig: string; // secret
  buildClusterName: string;
  buildClusterEndpoint: string;
  buildKubeconfig: string; // secret
  runtimeClusterName: string;
  runtimeClusterEndpoint: string;
  runtimeKubeconfig: string; // secret

  // Database
  dbHost: string;
  dbPort: number;
  dbUri: string; // secret
  dbPassword: string; // secret

  // Storage
  bucketName: string;
  bucketEndpoint: string;
  buildsBucketName: string;
  logsBucketName: string;
  staticBucketName: string;
  staticBucketEndpoint: string;
  spacesEndpoint: string;
  s3Endpoint: string;

  // Registry
  registryEndpoint: string;
  registryName: string;
}
```

### loopzero-platform exports (consumed by control-plane):

```typescript
export interface PlatformStackOutputs {
  // Ingress
  mgmtIngressEndpoint: string;
  runtimeIngressEndpoint: string;

  // Cross-cluster
  lokiEndpoint: string;
  argoMetricsEndpoint: string;

  // Cloudflare (optional)
  cfTunnelId?: string;
  cfKvNamespaceId?: string;
  cfWorkerName?: string;

  // Readiness
  giteaReady: boolean;
  argoCdReady: boolean;
  prometheusReady: boolean;
  grafanaReady: boolean;
}
```

---

## Stack Naming Convention (INFRA-SPLIT-008)

| Project | Stack Name | Format |
|---|---|---|
| loopzero-infra | `loopzero-infra/dev` | `{project}/{env}` |
| loopzero-platform | `loopzero-platform/dev` | `{project}/{env}` |
| loopzero-control-plane | `loopzero-control-plane/dev` | `{project}/{env}` |

StackReference format: `new pulumi.StackReference("loopzero-infra/dev")`

Each project gets its own `Pulumi.yaml`, `Pulumi.dev.yaml`, `Pulumi.staging.yaml`, `Pulumi.prod.yaml`.

---

## Migration Strategy by Resource Type (INFRA-SPLIT-009)

### Strategy 1: `pulumi state mv` (CRITICAL resources)

Use for resources that CANNOT be recreated. Pulumi moves the resource from the old state to the new project's state without touching the cloud provider.

```bash
# Export from monolith
pulumi stack export --stack loopzero-infra/dev > state-backup.json

# Import specific resource to new project
cd infra/pulumi-infra
pulumi import digitalocean:index/databaseCluster:DatabaseCluster loopzero-dev-db <resource-id>
```

**Applies to:** PostgreSQL, Spaces buckets ×4, Container Registry, CF Tunnel, CF KV

### Strategy 2: `recreate safe` (stateless K8s resources)

Delete from old state, let new project create fresh. No data loss.

**Applies to:** All Deployments, Services, ConfigMaps, Secrets, RBAC, Namespaces, Helm releases without PVCs, DNS records

### Strategy 3: `recreate with PVC retention` (Helm with PVCs)

K8s PVCs use `ReclaimPolicy: Retain` by default on DO. When Helm release is deleted, PVC persists. New Helm release mounts the same PVC.

**Applies to:** Gitea, MinIO, Loki, Prometheus, Grafana, Tempo

### Strategy 4: `import existing` (shared resources)

For resources that exist before any project (Container Registry). Use `pulumi import` with `protect: true` and `retainOnDelete: true`.

**Applies to:** Container Registry (global per DO account)

---

## Execution Sequence

```
1. Create loopzero-infra project scaffold
2. Move CRITICAL resources via state export/import
3. Validate loopzero-infra: pulumi preview shows 0 changes
4. Create loopzero-platform project scaffold
5. Add StackReference to loopzero-infra
6. Move platform resources (recreate safe for Helm, state mv for CF)
7. Validate loopzero-platform: pulumi preview shows only expected creates
8. Create loopzero-control-plane project scaffold
9. Add StackReference to loopzero-infra + loopzero-platform
10. Move control-plane resources (recreate safe)
11. Validate full chain: infra → platform → control-plane
12. Update CI pipeline to 3-step deploy
13. Deprecate monolith
```

---

## Risks & Mitigations

| Risk | Severity | Mitigation |
|---|---|---|
| State mv fails on Helm releases | HIGH | Test on dev first; keep state backup |
| StackReference circular dependency | MEDIUM | Strict unidirectional: infra → platform → control-plane |
| Spaces credentials duplicated in 3 projects | LOW | Accept duplication; same CI secret populates all |
| Pipeline takes 3x longer (3 sequential deploys) | MEDIUM | Only deploy projects that changed (git diff-based) |
| `bootstrapMgmtCluster` decomposition breaks wiring | HIGH | Extract functions first, validate, THEN split projects |

## Alternatives Considered

### 1. Two projects (infra + everything else)
**Rejected:** mgmt-bootstrap.ts (81 commits) still mixes platform and control-plane. The core problem (blast radius) remains.

### 2. Four projects (infra + platform + apps + auth)
**Rejected:** YAGNI. Ory Kratos/Hydra change alongside API auth features. Separating auth from control-plane creates unnecessary StackReference complexity for 2 Helm releases.

### 3. Keep monolith, improve with component resources
**Rejected:** Pulumi ComponentResources reduce code duplication but don't isolate blast radius. A failed `pulumi up` still blocks everything.
