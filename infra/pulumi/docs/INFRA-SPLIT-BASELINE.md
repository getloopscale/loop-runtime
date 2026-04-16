# INFRA-SPLIT — Baseline Document (Epic 0)

> Generated: 2026-04-10 | Epic 0: Discovery & Baseline
> Status: COMPLETE — Ready for Epic 1 (Architecture Design)

---

## 1. Resource Inventory by Phase (INFRA-SPLIT-001)

### Phase 1: INFRA (Cloud Primitives) — 16 resources

| Resource Type | Logical Name | Source | Stateful |
|---|---|---|---|
| digitalocean.Vpc | `{baseName}-net` | providers/digitalocean/network.ts | Yes |
| digitalocean.KubernetesCluster ×3 | `{baseName}-mgmt/build/runtime` | providers/digitalocean/kubernetes.ts | Yes |
| digitalocean.DatabaseCluster | `{baseName}-db` | providers/digitalocean/database.ts | **CRITICAL** |
| digitalocean.DatabaseDb ×4 | `gitea/loopzero/kratos/hydra` | providers/digitalocean/database.ts | **CRITICAL** |
| digitalocean.DatabaseFirewall | `{baseName}-db-firewall` | providers/digitalocean/database.ts | No |
| digitalocean.SpacesBucket ×4 | `artifacts/builds/logs/static-assets` | components/infra.ts | **CRITICAL** |
| digitalocean.Cdn | `static-cdn` | providers/digitalocean/storage.ts | No |
| digitalocean.ContainerRegistry | `{project}-registry` | providers/digitalocean/registry.ts | Yes (protected) |

### Phase 1.5: Cloudflare (Conditional) — 6 resources

| Resource Type | Logical Name | Source | Stateful |
|---|---|---|---|
| cloudflare.ZeroTrustTunnelCloudflared | `loopzero-tunnel-{env}` | providers/cloudflare/tunnel.ts | **CRITICAL** |
| cloudflare.ZeroTrustTunnelCloudflaredConfig | `loopzero-tunnel-config` | providers/cloudflare/tunnel.ts | No |
| cloudflare.DnsRecord | `loopzero-tunnel-dns` | providers/cloudflare/tunnel.ts | No |
| cloudflare.WorkersKvNamespace | `loopzero-routing-{env}` | providers/cloudflare/static-delivery.ts | **CRITICAL** |
| cloudflare.WorkersScript | `loopzero-static-delivery-{env}` | providers/cloudflare/static-delivery.ts | No |
| cloudflare.WorkersRoute | `loopzero-static-delivery-route-{env}` | providers/cloudflare/static-delivery.ts | No |

### Phase 2: PLATFORM (Bootstrap Services) — ~110 resources

**Management Cluster:**
- cert-manager, Traefik, ArgoCD, Gitea, MinIO, Ory Kratos, Ory Hydra
- Prometheus, Grafana, Tempo, OTEL Collector
- Gitea bootstrap Job, MinIO credentials, cluster issuers
- metrics-server

**Build Cluster:**
- Argo Workflows + ServiceAccount, CWTs, Loki, Promtail
- Loki NodePort, Argo metrics NodePort
- MinIO credentials, monitoring namespace

**Runtime Cluster:**
- cert-manager, Traefik, ESO + ClusterSecretStore
- Promtail, RBAC for ESO writer, scoped SA tokens
- monitoring namespace

### Phase 3: CONTROL-PLANE (Application Services) — ~35 resources

- LoopZero API (Deployment, Service, ConfigMap, Secrets, PDB, RBAC, scoped kubeconfigs)
- Cloud Service (Deployment, Service, ConfigMap, Secret)
- Cloud Dashboard (Deployment, Service)
- Tenant Controller (Deployment, Service, PDB, CRDs, RBAC, scoped kubeconfig, credentials)
- IngressRoutes (api, argocd, cloud gateway certs + routes)
- NetworkPolicies (deny-all, allow rules)
- DNS records (6 mgmt subdomains + wildcard runtime)
- ArgoCD cluster secret (runtime registration)

**Total: ~160+ Pulumi resources**

---

## 2. Stack Outputs → Consumers (INFRA-SPLIT-002)

### Critical Outputs (consumed by CI/CD pipelines)

| Output | Source Phase | Consumers | Impact |
|---|---|---|---|
| `mgmtKubeconfig` | Phase 1 | infra-deploy.yml (5 refs), pre-destroy-cleanup.sh | **CRITICAL**: all post-deploy ops |
| `buildKubeconfig` | Phase 1 | infra-deploy.yml, pre-destroy-cleanup.sh | **CRITICAL**: Loki verification |
| `runtimeKubeconfig` | Phase 1 | infra-deploy.yml, pre-destroy-cleanup.sh | **CRITICAL**: CF cleanup |
| `registryEndpoint` | Phase 1 | build-images.yml (4 refs), infra-deploy.yml | **CRITICAL**: all image builds |
| `dbUri` | Phase 1 | infra-deploy.yml (DB seed) | **CRITICAL**: database access |
| `cfKvNamespaceId` | Phase 1.5 | infra-deploy.yml (CF Worker deploy, secret creation) | Worker KV binding |
| `mgmt/build/runtimeClusterName` | Phase 1 | infra-deploy.yml, build-images.yml | Registry integration |
| `execution` | All phases | infra-deploy.yml (health verification) | Deployment validation |
| `wiring` | All phases | infra-deploy.yml (summary) | Operator reference |

### Internal Data Flow

| Flow | Mechanism | Split Challenge |
|---|---|---|
| Phase 1 → Phase 1.5 | `infra.runtimeCluster.kubeconfig` → `createTunnel()` | StackReference needed |
| Phase 1 → Phase 2 | `InfraOutputs` → `bootstrapCluster()` | StackReference needed |
| Phase 1.5 → Phase 2 | `CloudflareOutputs.kvNamespaceId` → `bootstrapMgmtCluster()` | StackReference needed |
| Phase 2 → Phase 2 | `build.lokiEndpoint` → `mgmt.installPrometheus()` | Same project OK |
| Phase 2 → Phase 3 | `bootstrap.mgmt/runtime.ingressEndpoint` → `exposePlatform()` | StackReference needed |

**Key finding: No StackReference usage today** — all data flows via function parameters within the monolith.

---

## 3. Configs & Secrets by Domain (INFRA-SPLIT-003)

### INFRA Domain (13 fields)
`environment`, `provider`, `region`, `domain`, `dnsZone`, `infraVersion`, `mgmtK8sVersion`, `mgmtNodeClass`, `mgmtMinNodes`, `mgmtMaxNodes`, `runtimeK8s*`, `buildK8s*`, `dbNodeClass`, `dbVersion`, `dbNodeCount`, `sshKeyIds`

### PLATFORM Domain (6 fields)
`spacesAccessKeyId` (secret), `spacesSecretAccessKey` (secret), `digitaloceanToken` (secret), `cfApiToken` (secret), `cfAccountId`, `cfZoneId`

### CONTROL-PLANE Domain (14 fields)
`giteaAdminPassword`, `minioRootPassword`, `loopzeroApiDbPassword`, `stripeApiKey`, `stripeWebhookSecret`, `stripePriceProId`, `stripePriceTeamId`, `sessionTokenSecret`, `serviceAuthSecret`, `resendApiKey`, `githubOAuthClientId/Secret`, `googleOAuthClientId/Secret`

### SHARED / Cross-Domain (3 fields)
| Field | Why Shared | Split Strategy |
|---|---|---|
| `lokiPushUrl` | Build cluster IP consumed by runtime | StackReference from platform → control-plane |
| `tempoStorageBucket` | Observability uses infra storage | StackReference from infra → platform |
| `spacesAccessKeyId/Secret` | Used by infra (buckets), platform (Loki, MinIO creds), control-plane (API) | Duplicate in each project OR single secret store |

### CI Environment Variables (13 secrets in GitHub Actions)
`DIGITALOCEAN_TOKEN`, `SPACES_ACCESS_KEY_ID`, `SPACES_SECRET_ACCESS_KEY`, `PULUMI_CONFIG_PASSPHRASE`, `CLOUDFLARE_API_TOKEN`, `LOOPZERO_STRIPE_API_KEY`, `LOOPZERO_STRIPE_WEBHOOK_SECRET`, `LOOPZERO_SESSION_TOKEN_SECRET`, `LOOPZERO_GITHUB_CLIENT_ID/SECRET`, `LOOPZERO_GOOGLE_CLIENT_ID/SECRET`, `LOOPZERO_RESEND_API_KEY`, `LOOPZERO_SERVICE_AUTH_SECRET`

---

## 4. Stateful Resources & Migration Risk (INFRA-SPLIT-004)

### CRITICAL (Data Loss if Recreated)

| Resource | Data Held | Migration Strategy |
|---|---|---|
| PostgreSQL Cluster | All platform state, user accounts, OAuth tokens | `state move` + `pg_dump` before migration |
| DO Spaces (artifacts) | Source bundles, build cache | `state move` + `s3cmd sync` |
| DO Spaces (logs) | Loki build logs (7d retention) | `state move` + `s3cmd sync` |
| DO Spaces (static-assets) | Frontend builds served via CF Worker | `state move` + `s3cmd sync` |
| CF Tunnel | Ingress routing, tunnel secret | `import` + preserve tunnel secret |
| CF KV Namespace | Static routing table (app→asset mapping) | `state move` + CF API export |

### HIGH (Service Disruption)

| Resource | Data Held | Migration Strategy |
|---|---|---|
| Gitea StatefulSet + PVC | Git repos (desired state) | PVC retained by K8s; backup via git clone |
| MinIO StatefulSet + PVC | S3 objects in-cluster | PVC retained; match root password |
| Loki StatefulSet + PVC | Log index (S3 backend has chunks) | PVC retained; recoverable from S3 |
| Prometheus StatefulSet + PVC | Metrics (15d retention) | PVC retained; mount on new pod |
| Grafana StatefulSet + PVC | Dashboards, sessions, tokens | PVC retained; provisioned dashboards auto-recreated |
| K8s Secrets (generated tokens) | SA tokens, kubeconfigs, API keys | `state move` + kubectl export |
| Tempo + PVC | Trace data | PVC retained |

### MEDIUM (Temporary Disruption)

| Resource | Impact | Strategy |
|---|---|---|
| K8s Clusters ×3 | 2-5 min workload outage | Recreate + auto-deploy via deployments |
| Container Registry | Shared across stacks | **Protected** (`protect: true`, `retainOnDelete: true`, `import`) |

### LOW (Safe to Recreate)
All Deployments, Services, ConfigMaps, RBAC, Namespaces, NetworkPolicies, DNS records, Worker scripts — ~120 resources.

---

## 5. Proposed Phase → Project Mapping

Based on the discovery data, the natural boundaries are:

```
loopzero-infra (Phase 1)
├── VPC, 3 K8s clusters, PostgreSQL, 4 Spaces buckets, CDN, Container Registry
├── Outputs: kubeconfigs, DB connection, bucket names/endpoints, registry endpoint
└── ~16 resources, ALL stateful

loopzero-platform (Phase 1.5 + Phase 2)
├── CF Tunnel + Worker + KV
├── cert-manager ×2, Traefik ×2, ArgoCD, Gitea, MinIO
├── Argo Workflows + CWTs, Loki, Promtail ×2, ESO
├── Prometheus, Grafana, Tempo, OTEL Collector, metrics-server ×3
├── DNS records (mgmt subdomains + runtime wildcard)
├── Consumes: loopzero-infra outputs via StackReference
└── ~110 resources, mostly stateless (PVCs on Helm releases)

loopzero-control-plane (Phase 3)
├── LoopZero API + RBAC + scoped kubeconfigs
├── Cloud Service + Dashboard
├── Tenant Controller + CRDs + RBAC
├── Ory Kratos + Hydra
├── IngressRoutes, NetworkPolicies
├── ArgoCD cluster secret (runtime registration)
├── Consumes: loopzero-infra + loopzero-platform outputs via StackReference
└── ~35 resources, all stateless (K8s manifests)
```

---

## 6. Key Risks for Epic 1

| Risk | Severity | Mitigation |
|---|---|---|
| `pulumi state mv` fails on Helm releases | HIGH | Test on dev stack first; backup state to S3 |
| Cross-project secret sharing (Spaces creds) | MEDIUM | Duplicate in each project config OR use ESO |
| `bootstrapMgmtCluster` has 2600 lines mixing phases | HIGH | Extract control-plane resources BEFORE state move |
| CF Tunnel ID changes during migration | CRITICAL | Preserve tunnel secret; use `import` not recreate |
| Container Registry shared across all stacks | MEDIUM | Keep in loopzero-infra; `import` in all projects |
| Pipeline order (infra → platform → control-plane) | MEDIUM | Enforce in CI with job dependencies |
| `lokiPushUrl` is a hardcoded IP | LOW | Replace with StackReference to build node IP |

---

## Epic 0 DoD Checklist

- [x] Resource → project matrix (160+ resources classified)
- [x] Output → consumer matrix (19 stack outputs, 3 workflows)
- [x] Secret/config → domain matrix (33 fields, 13 CI secrets)
- [x] Stateful resources listed with migration strategy (6 CRITICAL, 7 HIGH, 2 MEDIUM)
- [x] Baseline document reviewed and ready for Epic 1
