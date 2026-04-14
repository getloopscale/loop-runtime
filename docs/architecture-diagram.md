# LoopZero Platform — Complete Architecture Diagram

> Generated: 2026-04-12 | Environment: dev | Region: SFO3 | VPC: 10.20.0.0/16

---

## System Overview

```
                                    INTERNET
                                       │
                         ┌─────────────┴──────────────────┐
                         │    CLOUDFLARE EDGE (WAF/DDoS)   │
                         │                                  │
                         │  Zone: loopzero.example               │
                         │  Account: 362767409c...          │
                         │                                  │
                         │  ┌──────────────────────┐       │
                         │  │ CF Tunnel: loopzero-dev   │       │
                         │  │ ID: e8e49a0f-2549...  │       │
                         │  │ Status: healthy        │       │
                         │  └──────────┬───────────┘       │
                         │             │                    │
                         │  ┌──────────┴───────────┐       │
                         │  │ CF Worker             │       │
                         │  │ loopzero-static-delivery  │       │
                         │  │ -dev                  │       │
                         │  │                       │       │
                         │  │ KV: loopzero-routing-dev  │       │
                         │  │ ID: fcafd0d755a5      │       │
                         │  └──────────┬───────────┘       │
                         │             │                    │
                         │  DNS Records:                    │
                         │  *.dev.loopzero.example → CNAME       │
                         │    → e8e49a0f...cfargotunnel.com │
                         │  api.dev.loopzero.example  → A 134... │
                         │  git.dev.loopzero.example  → A 134... │
                         │  grafana.dev...       → A 134... │
                         │  argocd.dev...        → A 134... │
                         │  auth.dev...          → A 134... │
                         │  cloud.dev...         → A 134... │
                         └─────────────┬──────────────────┘
                                       │
          ┌────────────────────────────┼────────────────────────────┐
          │             DIGITALOCEAN — SFO3 REGION                  │
          │             VPC: loopzero-infra-dev-sfo3-net                │
          │             CIDR: 10.20.0.0/16                          │
          │                                                         │
          │  ┌──────────────────────────────────────────────────┐   │
          │  │          MANAGEMENT CLUSTER (mgmt)               │   │
          │  │          loopzero-infra-dev-sfo3-mgmt                │   │
          │  │          3 nodes × small (2 vCPU / 4 GB)         │   │
          │  │          10.20.0.16, 10.20.0.17, 10.20.0.18      │   │
          │  │                                                   │   │
          │  │   ┌─── LoadBalancer ──────────────────────────┐   │   │
          │  │   │ mgmt-traefik (HA, 2 replicas)             │   │   │
          │  │   │ 134.199.142.85:80,443                     │   │   │
          │  │   │                                            │   │   │
          │  │   │ IngressRoutes:                             │   │   │
          │  │   │   api.dev.loopzero.example    → loopzero-api:8080   │   │   │
          │  │   │   git.dev.loopzero.example    → gitea:3000      │   │   │
          │  │   │   argocd.dev.loopzero.example → argocd:443      │   │   │
          │  │   │   grafana.dev.loopzero.example→ grafana:3000    │   │   │
          │  │   │   auth.dev.loopzero.example   → kratos:80       │   │   │
          │  │   │   cloud.dev.loopzero.example  → loopzero-cloud:3000 │   │   │
          │  │   │   harbor.dev.loopzero.example → harbor-core:80  │   │   │
          │  │   └────────────────────────────────────────────┘   │   │
          │  │                                                   │   │
          │  │   Namespace: platform-system (37 pods)            │   │
          │  │                                                   │   │
          │  │   ┌─── Platform Services ──────────────────────┐  │   │
          │  │   │ loopzero-api        ×2  (Go, chi, pgx)         │  │   │
          │  │   │ loopzero-controller ×1  (reconcile loops)      │  │   │
          │  │   │ loopzero-cloud      ×1  (OAuth2, dashboard)    │  │   │
          │  │   │ loopzero-dashboard  ×1  (React frontend)       │  │   │
          │  │   │ tenant-controller×1 (K8s operator)         │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   ┌─── GitOps + Build ─────────────────────────┐  │   │
          │  │   │ ArgoCD 2.11.7    (7 pods, manual sync)     │  │   │
          │  │   │ Gitea 1.22.3     (1 pod, internal git)     │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   ┌─── Auth ───────────────────────────────────┐  │   │
          │  │   │ Ory Kratos 26.2  (identity, signup/login)  │  │   │
          │  │   │ Ory Hydra 26.2   (OAuth2, JWT, PKCE)       │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   ┌─── Container Registry ─────────────────────┐  │   │
          │  │   │ Harbor 2.12.0 (tenant images)              │  │   │
          │  │   │   core ×1, registry ×1, jobservice ×1      │  │   │
          │  │   │   exporter ×1 (metrics:8001), redis ×1     │  │   │
          │  │   │   portal ×1                                │  │   │
          │  │   │   Storage: S3 → container-images bucket    │  │   │
          │  │   │   DB: harbor_db on managed PG              │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   ┌─── Storage ────────────────────────────────┐  │   │
          │  │   │ MinIO 2024.12   (1 pod, S3-compatible)     │  │   │
          │  │   │   Buckets: loopzero-artifacts, loopzero-builds     │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   ┌─── Observability ──────────────────────────┐  │   │
          │  │   │ Prometheus 2.54  (server + alertmanager)   │  │   │
          │  │   │   Scrape: platform pods, kube-state,       │  │   │
          │  │   │           harbor-exporter:8001,            │  │   │
          │  │   │           argo-workflows (cross-cluster)   │  │   │
          │  │   │   PVC: 10Gi (retention: 15d)               │  │   │
          │  │   │   Alerts: LoopZeroAPIDown, DeployFailing,      │  │   │
          │  │   │           HighAPILatency, PodCrashLooping  │  │   │
          │  │   │                                            │  │   │
          │  │   │ Grafana 11.2    (dashboards + datasources) │  │   │
          │  │   │   DS: Prometheus, Loki, Tempo              │  │   │
          │  │   │                                            │  │   │
          │  │   │ Tempo 2.6       (distributed tracing)      │  │   │
          │  │   │ OTEL Collector 0.103 (trace pipeline)      │  │   │
          │  │   │ kube-state-metrics                         │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   ┌─── TLS ────────────────────────────────────┐  │   │
          │  │   │ cert-manager 1.14.5                        │  │   │
          │  │   │   ClusterIssuer: Let's Encrypt (HTTP-01)   │  │   │
          │  │   │   Certificates: api, git, argocd, grafana, │  │   │
          │  │   │                 auth, cloud, harbor         │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  └───────────────────────────────────────────────────┘   │
          │                                                         │
          │  ┌──────────────────────────────────────────────────┐   │
          │  │            BUILD CLUSTER                          │   │
          │  │            loopzero-infra-dev-sfo3-build              │   │
          │  │            2 nodes × medium (4 vCPU / 8 GB)       │   │
          │  │            10.20.0.14, 10.20.0.15                 │   │
          │  │                                                   │   │
          │  │   Namespace: argo (24 pods)                       │   │
          │  │   ┌─── Build Pipeline ─────────────────────────┐  │   │
          │  │   │ Argo Workflows 1.0.5                       │  │   │
          │  │   │   CWTs: loopzero-clone, cache-restore,         │  │   │
          │  │   │         cache-save, summary, resource-quota │  │   │
          │  │   │   BuildKit: rootless, single-container     │  │   │
          │  │   │   SA: loopzero-build-sa (DOCR push creds)      │  │   │
          │  │   │   Registry: buildkit-registry-config       │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   Namespace: monitoring                           │   │
          │  │   ┌─── Log Aggregation ────────────────────────┐  │   │
          │  │   │ Loki 6.24.0     (build log storage)        │  │   │
          │  │   │   Queried by loopzero-api via LogQL             │  │   │
          │  │   │ Promtail 6.16.6 (log shipper → Loki)       │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  └───────────────────────────────────────────────────┘   │
          │                                                         │
          │  ┌──────────────────────────────────────────────────┐   │
          │  │           RUNTIME CLUSTER                         │   │
          │  │           loopzero-infra-dev-sfo3-runtime              │   │
          │  │           2 nodes × small (2 vCPU / 4 GB)         │   │
          │  │           10.20.0.19, 10.20.0.20                  │   │
          │  │                                                   │   │
          │  │   ┌─── LoadBalancer ──────────────────────────┐   │   │
          │  │   │ runtime-traefik (HA, 2 replicas)          │   │   │
          │  │   │ 129.212.164.100:80,443                    │   │   │
          │  │   │                                            │   │   │
          │  │   │ CF Tunnel → cloudflared ×2 → Traefik      │   │   │
          │  │   │   (all *.dev.loopzero.example user app traffic) │   │   │
          │  │   └────────────────────────────────────────────┘   │   │
          │  │                                                   │   │
          │  │   Namespace: platform-system (29 pods)            │   │
          │  │   ┌─── Ingress + TLS ──────────────────────────┐  │   │
          │  │   │ Traefik 28.3.0  (HA, 2 replicas)           │  │   │
          │  │   │ cert-manager 1.14.5                        │  │   │
          │  │   │ cloudflared ×2  (CF Tunnel agent)          │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   Namespace: external-secrets                     │   │
          │  │   ┌─── Secret Sync ────────────────────────────┐  │   │
          │  │   │ External Secrets Operator 0.14.3           │  │   │
          │  │   │   ClusterSecretStore → mgmt PG             │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   Namespace: monitoring                           │   │
          │  │   ┌─── Log Collection ─────────────────────────┐  │   │
          │  │   │ Promtail 6.16.6 (log shipper → build Loki) │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   Namespace: loopzero-{tenant}-{env}  (per-tenant)   │   │
          │  │   ┌─── User Applications ──────────────────────┐  │   │
          │  │   │ Managed by ArgoCD (manual sync from mgmt)  │  │   │
          │  │   │ Deployed after: loopzero deploy → build →      │  │   │
          │  │   │   gitops push → ArgoCD sync                │  │   │
          │  │   └────────────────────────────────────────────┘  │   │
          │  └───────────────────────────────────────────────────┘   │
          │                                                         │
          │  ┌──────────────────────────────────────────────────┐   │
          │  │          MANAGED POSTGRESQL 16 (HA)              │   │
          │  │          loopzero-infra-dev-sfo3-db                   │   │
          │  │          2 nodes × db-s-1vcpu-2gb                 │   │
          │  │          Port: 25060 (SSL required)               │   │
          │  │          Host: private-loopzero-infra-dev-sfo3-db-... │   │
          │  │                                                   │   │
          │  │   ┌─────────────┬──────────────────────────────┐  │   │
          │  │   │ loopzero        │ Platform state, builds,      │  │   │
          │  │   │             │ releases, tenants, pg_locks   │  │   │
          │  │   ├─────────────┼──────────────────────────────┤  │   │
          │  │   │ gitea       │ Git repository metadata      │  │   │
          │  │   ├─────────────┼──────────────────────────────┤  │   │
          │  │   │ kratos      │ Identity mgmt (users, flows) │  │   │
          │  │   ├─────────────┼──────────────────────────────┤  │   │
          │  │   │ hydra       │ OAuth2 grants, tokens, keys  │  │   │
          │  │   ├─────────────┼──────────────────────────────┤  │   │
          │  │   │ harbor_db   │ Container registry metadata  │  │   │
          │  │   └─────────────┴──────────────────────────────┘  │   │
          │  │                                                   │   │
          │  │   Firewall: allow only DOKS cluster VPC IPs       │   │
          │  └───────────────────────────────────────────────────┘   │
          │                                                         │
          │  ┌──────────────────────────────────────────────────┐   │
          │  │          DO SPACES (S3-compatible, sfo3)          │   │
          │  │                                                   │   │
          │  │   loopzero-infra-dev-sfo3-artifacts    (source tarballs)│  │
          │  │   loopzero-infra-dev-sfo3-builds       (build cache)  │  │
          │  │   loopzero-infra-dev-sfo3-logs          (build logs)  │  │
          │  │   loopzero-infra-dev-sfo3-static-assets (frontend JS) │  │
          │  │   loopzero-infra-dev-sfo3-container-images (Harbor)   │  │
          │  └───────────────────────────────────────────────────┘   │
          │                                                         │
          │  ┌──────────────────────────────────────────────────┐   │
          │  │          DO CONTAINER REGISTRY (DOCR)             │   │
          │  │          loopzero-infra-registry (Professional)       │   │
          │  │          registry.digitalocean.com/               │   │
          │  │            loopzero-infra-registry                    │   │
          │  │                                                   │   │
          │  │   Platform images:                                │   │
          │  │     loopzero-api:dev, loopzero-cloud:dev,                 │   │
          │  │     loopzero-dashboard:dev, tenant-controller:dev     │   │
          │  │                                                   │   │
          │  │   Shared across all environments                  │   │
          │  │   (protect: true, retainOnDelete: true)           │   │
          │  └───────────────────────────────────────────────────┘   │
          └─────────────────────────────────────────────────────────┘
```

---

## Deploy Flow: `loopzero deploy`

```
  Developer
     │
     ▼
  loopzero deploy ──► CLI validates loopzero.yaml + monorepo detection
     │              (30 frameworks, path traversal protection)
     ▼
  Upload source ──► loopzero-api (mgmt, port 8080)
     │               │
     │               ├─► pg_advisory_xact_lock(tenant, project, env)
     │               ├─► Store build record in `loopzero` DB
     │               ├─► Upload bundle to Spaces (artifacts bucket)
     │               └─► Submit Argo Workflow → build cluster
     │
     ▼
  Argo Workflow (build cluster)
     │
     ├─► loopzero-clone CWT: download source from Spaces
     ├─► cache-restore CWT: restore build cache layers
     ├─► BuildKit: build container image (rootless)
     │     ├─► Push to DOCR (platform) or Harbor (tenant)
     │     └─► Push cache layers to Spaces (builds bucket)
     ├─► cache-save CWT: persist cache manifest
     └─► Callback to loopzero-api: build SUCCEEDED/FAILED
           │
           ▼
  loopzero-api receives callback
     │
     ├─► Generate K8s manifests (Deployment, Service, Ingress)
     ├─► Push manifests to Gitea (gitops repo)
     ├─► Trigger ArgoCD sync (manual sync policy)
     └─► SSE event → CLI shows progress
           │
           ▼
  ArgoCD (mgmt) ──► syncs manifests to runtime cluster
     │
     ▼
  Runtime cluster
     │
     ├─► Pod starts with image from DOCR/Harbor
     ├─► Traefik IngressRoute routes traffic
     └─► CF Tunnel (cloudflared) exposes via CF edge
           │
           ▼
  App live at: https://{app}.dev.loopzero.example
     (Static apps: CF Worker serves from Spaces CDN)
```

---

## Network Topology

```
  ┌─────────────────────────────────────────────────────┐
  │                 VPC: 10.20.0.0/16                    │
  │                                                      │
  │  mgmt nodes:    10.20.0.16-18  ──┐                   │
  │  build nodes:   10.20.0.14-15  ──┼── private network │
  │  runtime nodes: 10.20.0.19-20  ──┤   (inter-cluster) │
  │  DB (private):  10.20.x.x      ──┘                   │
  │                                                      │
  │  Public IPs (LoadBalancers only):                     │
  │    mgmt-traefik:    134.199.142.85  (platform admin) │
  │    runtime-traefik: 129.212.164.100 (user apps)      │
  │                                                      │
  │  Traffic routing:                                     │
  │    User apps  → CF Tunnel → cloudflared → rt-traefik │
  │    Admin UIs  → CF DNS A → mgmt-traefik              │
  │    Build logs → loopzero-api → Loki (build:30100)        │
  │    Metrics    → Prometheus scrape (cross-cluster)     │
  └─────────────────────────────────────────────────────┘
```

---

## Resource Count Summary

| Category           | Count | Details                                         |
|--------------------|-------|-------------------------------------------------|
| DOKS Clusters      | 3     | mgmt (3n), build (2n), runtime (2n) = 7 nodes   |
| PostgreSQL         | 1     | 2-node HA, 5 databases, SSL required             |
| Spaces Buckets     | 5     | artifacts, builds, logs, static-assets, container-images |
| Container Registry | 1     | DOCR Professional (shared, protected)            |
| Load Balancers     | 2     | mgmt-traefik, runtime-traefik                    |
| CF Tunnel          | 1     | loopzero-dev (healthy)                               |
| CF KV Namespace    | 1     | loopzero-routing-dev (static frontend routing)       |
| CF Workers         | 1     | loopzero-static-delivery-dev                         |
| CF DNS Records     | 6+    | A records (platform) + CNAME (wildcard+tunnel)   |
| Helm Releases      | 23    | mgmt:13 + build:5 + runtime:5                   |
| Running Pods       | 90    | mgmt:37 + build:24 + runtime:29                  |
| Pulumi Resources   | 191   | Total managed by Pulumi state                    |

---

## Helm Chart Inventory

| Chart                 | Version | Cluster  | Namespace        |
|-----------------------|---------|----------|------------------|
| argo-cd               | 7.3.11  | mgmt     | platform-system  |
| gitea                 | 10.6.0  | mgmt     | platform-system  |
| grafana               | 8.5.2   | mgmt     | platform-system  |
| loopzero-harbor           | 0.1.0   | mgmt     | platform-system  |
| cert-manager          | 1.14.5  | mgmt     | platform-system  |
| traefik               | 28.3.0  | mgmt     | platform-system  |
| minio                 | 5.4.0   | mgmt     | platform-system  |
| hydra                 | 0.61.0  | mgmt     | platform-system  |
| kratos                | 0.61.0  | mgmt     | platform-system  |
| otel-collector        | 0.96.0  | mgmt     | platform-system  |
| prometheus            | 25.27.0 | mgmt     | platform-system  |
| tempo                 | 1.14.0  | mgmt     | platform-system  |
| metrics-server        | 3.12.1  | all (×3) | kube-system      |
| argo-workflows        | 1.0.5   | build    | argo             |
| loopzero-build-cwts       | 0.1.0   | build    | argo             |
| loki                  | 6.24.0  | build    | monitoring       |
| promtail              | 6.16.6  | build    | monitoring       |
| promtail              | 6.16.6  | runtime  | monitoring       |
| external-secrets      | 0.14.3  | runtime  | external-secrets |
| traefik               | 28.3.0  | runtime  | platform-system  |
| cert-manager          | 1.14.5  | runtime  | platform-system  |

---

## Registry Architecture

```
  ┌─────────────────────┐           ┌──────────────────────┐
  │ DOCR                │           │ Harbor               │
  │ loopzero-infra-registry │           │ harbor.dev.loopzero.  │
  │ (Professional)      │           │ dev                  │
  ├─────────────────────┤           ├──────────────────────┤
  │ loopzero-api:dev        │           │ tenants/{id}/{app}   │
  │ loopzero-cloud:dev      │           │   (user-built        │
  │ loopzero-dashboard:dev  │           │    container images)  │
  │ tenant-controller   │           │                      │
  │   :dev              │           │ Storage:             │
  │                     │           │   S3 → container-    │
  │ Pull: DOCR secret   │           │   images bucket      │
  │ Push: CI pipeline   │           │ DB: harbor_db        │
  └─────────────────────┘           └──────────────────────┘
   Platform images                    Tenant build images
   (shared, protected)                (per-environment)
```

---

## Pulumi IaC Pipeline

```
  pulumi destroy --yes    (12m39s)
         │
         ├── Deletes: Helm releases → K8s resources → DB → clusters → VPC
         ├── Retains: DOCR (protect+retainOnDelete)
         └── Orphans handled by adopt-or-create on next up
         │
         ▼
  pulumi up --yes         (18m06s)
         │
         ├── Phase 1: Adopt-or-create lookups (VPC, CF Tunnel, CF KV, CF DNS)
         ├── Phase 2: Base infra (VPC → 3 DOKS + DB + Spaces) — 5 min
         ├── Phase 3: Cloudflare (Tunnel + Worker + KV + DNS) — 30 sec
         ├── Phase 4: Bootstrap mgmt (13 Helm + Deployments) — 8 min
         ├── Phase 5: Bootstrap build (5 Helm) — 3 min
         ├── Phase 6: Bootstrap runtime (5 Helm + cloudflared) — 3 min
         └── Phase 7: Expose (DNS records, IngressRoutes, TLS) — 1 min
         │
         ▼
  191 resources, 90 pods, 0 errors
  Total: ~31 minutes destroy+up
```
