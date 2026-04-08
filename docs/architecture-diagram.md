# LoopZero Platform — Architecture Diagram (DigitalOcean Production)

> Generated: 2026-04-08 | Region: SFO3 (dev) / NYC3 (prod)

---

## High-Level Overview

```
                          INTERNET
                             |
                    +--------+--------+
                    |   CLOUDFLARE    |
                    |   (DDoS/WAF)   |
                    +---+--------+---+
                        |        |
              +---------+        +-----------+
              |                              |
     CF Worker (KV)                   CF Tunnel
     loopzero-static-delivery             loopzero-tunnel
     (static frontends)               (all other traffic)
              |                              |
              v                              v
     DO Spaces CDN                    +--------------+
     loopzero-static-assets               | RUNTIME      |
                                      | CLUSTER      |
                                      | (user apps)  |
                                      +--------------+

     +------------------+     +------------------+     +------------------+
     |   MANAGEMENT     |     |     BUILD        |     |    RUNTIME       |
     |   CLUSTER        |     |     CLUSTER      |     |    CLUSTER       |
     |                  |     |                  |     |                  |
     | LoopZero API         |     | Argo Workflows   |     | User Apps        |
     | Operators (7)    |     | BuildKit         |     | Traefik Ingress  |
     | ArgoCD           |     | Loki (logs)      |     | cert-manager     |
     | Gitea            |     |                  |     | ESO              |
     | PostgreSQL       |     |                  |     | cloudflared (2x) |
     | MinIO            |     |                  |     |                  |
     | Traefik          |     |                  |     |                  |
     | Prometheus       |     |                  |     |                  |
     | Grafana          |     |                  |     |                  |
     | Ory Kratos       |     |                  |     |                  |
     | Ory Hydra        |     |                  |     |                  |
     | cert-manager     |     |                  |     |                  |
     | OTel Collector   |     |                  |     |                  |
     +--------+---------+     +--------+---------+     +--------+---------+
              |                        |                        |
              +------------------------+------------------------+
                                       |
                              +--------+--------+
                              |  DO Managed PG  |
                              |  PostgreSQL 16  |
                              |  (VPC-only)     |
                              +-----------------+
                              DBs: loopzero, gitea,
                                   kratos, hydra
```

---

## Cluster Detail: MANAGEMENT

```
+====================================================================+
|  MANAGEMENT CLUSTER  (loopzero-infra-{env}-{region}-mgmt)              |
|  Dev: 2-3x s-2vcpu-4gb | Prod: 3-5x s-4vcpu-8gb                  |
+====================================================================+
|                                                                    |
|  namespace: platform-system                                        |
|  +--------------------------------------------------------------+  |
|  |                                                              |  |
|  |  +------------------+    +------------------+                |  |
|  |  | LOOPZERO API (Go)    |    | TENANT OPERATOR  |                |  |
|  |  | chi + pgx        |    | (kubebuilder)    |                |  |
|  |  |                  |    |                  |                |  |
|  |  | Services:        |    | Controllers:     |                |  |
|  |  | - BuildReconcile |    | - Tenant         |                |  |
|  |  | - BuildReaper    |    | - Environment    |                |  |
|  |  | - ReleaseReaper  |    | - Application    |                |  |
|  |  | - Convergence    |    | - Exposure       |                |  |
|  |  | - CircuitBreaker |    | - Bootstrap      |                |  |
|  |  | - StaticDelivery |    | - GitRepoBind    |                |  |
|  |  | - DomainProv     |    | - ClusterCred    |                |  |
|  |  | - DomainHealth   |    |                  |                |  |
|  |  | - IdempClean     |    +------------------+                |  |
|  |  | - RollbackOrch   |                                        |  |
|  |  +------------------+                                        |  |
|  |                                                              |  |
|  |  +-----------+ +-----------+ +-----------+ +-----------+     |  |
|  |  | Gitea     | | ArgoCD    | | MinIO     | | Traefik   |     |  |
|  |  | v1.22.6   | | v2.13.3   | | v5.4.0    | | v28.3.0   |     |  |
|  |  | (git)     | | (gitops)  | | (S3)      | | (ingress) |     |  |
|  |  +-----------+ +-----------+ +-----------+ +-----------+     |  |
|  |                                                              |  |
|  |  +-----------+ +-----------+ +-----------+ +-----------+     |  |
|  |  | Kratos    | | Hydra     | | cert-mgr  | | OTel      |     |  |
|  |  | v0.61.0   | | v0.61.0   | | v1.14.5   | | v0.96.0   |     |  |
|  |  | (authn)   | | (oauth2)  | | (TLS)     | | (traces)  |     |  |
|  |  +-----------+ +-----------+ +-----------+ +-----------+     |  |
|  |                                                              |  |
|  |  +-----------+ +-----------+ +-----------+ +-----------+     |  |
|  |  |Prometheus | | Grafana   | | Loki      | | Promtail  |     |  |
|  |  | v25.27.0  | | v8.5.2    | | v6.24.0   | | v6.16.6   |     |  |
|  |  | (metrics) | | (dash)    | | (logs)    | | (ship)    |     |  |
|  |  +-----------+ +-----------+ +-----------+ +-----------+     |  |
|  +--------------------------------------------------------------+  |
|                                                                    |
|  CRDs managed here (all in platform-system namespace):             |
|  - Tenant, Environment, LoopZeroApplication, ExposedRoute              |
|  - PlatformBootstrap, GitRepositoryBinding, ClusterCredential      |
|  - AppSecret (CRD only, no controller)                             |
|  - PreviewEnvironment (CRD only, no controller)                    |
|  - ReconciliationReport (placeholder)                              |
+====================================================================+
```

---

## Cluster Detail: BUILD

```
+====================================================================+
|  BUILD CLUSTER  (loopzero-infra-{env}-{region}-build)                  |
|  Dev: 2-3x s-2vcpu-4gb | Prod: 2-4x s-4vcpu-8gb                  |
+====================================================================+
|                                                                    |
|  namespace: argo                                                   |
|  +--------------------------------------------------------------+  |
|  |  Argo Workflows (v1.0.5 custom CWT wrapper)                 |  |
|  |                                                              |  |
|  |  ClusterWorkflowTemplates (CWTs):                            |  |
|  |  +-------------+ +----------------+ +--------------+         |  |
|  |  | clone-repo  | | cache-restore  | | install-deps |         |  |
|  |  | (git clone  | | (best-effort   | | (lang-spec   |         |  |
|  |  |  from MinIO)| |  from S3)      | |  npm/go/pip) |         |  |
|  |  +-------------+ +----------------+ +--------------+         |  |
|  |                                                              |  |
|  |  +-------------+ +----------------+ +--------------+         |  |
|  |  | build-image | | upload-assets  | | cache-save   |         |  |
|  |  | (BuildKit   | | (static→S3    | | (best-effort |         |  |
|  |  |  rootless)  | |  DO Spaces)    | |  to S3)      |         |  |
|  |  +-------------+ +----------------+ +--------------+         |  |
|  |                                                              |  |
|  |  +-------------+                                             |  |
|  |  | summary     |  ResourceQuota: 8 CPU / 16Gi req            |  |
|  |  | (exit hook) |  LimitRange: 1 CPU / 2Gi per container      |  |
|  |  +-------------+  Max pods: 50                               |  |
|  +--------------------------------------------------------------+  |
|                                                                    |
|  namespace: build-{tenant-id} (per-tenant isolation)               |
|  +--------------------------------------------------------------+  |
|  |  Build workflows run here with:                              |  |
|  |  - NetworkPolicy: deny-all + allow (DNS, MinIO, registry)    |  |
|  |  - SecurityContext: non-root (uid 1000), drop ALL caps       |  |
|  |  - BuildKit: moby/buildkit:v0.18.2-rootless (single-container)|  |
|  |  - Registry auth: docker config.json mounted read-only       |  |
|  +--------------------------------------------------------------+  |
|                                                                    |
|  Loki (log aggregation for build logs, queried by API via LogQL)   |
+====================================================================+
```

---

## Cluster Detail: RUNTIME

```
+====================================================================+
|  RUNTIME CLUSTER  (loopzero-infra-{env}-{region}-runtime)              |
|  Dev: 2-3x s-2vcpu-4gb | Prod: 3-6x s-4vcpu-8gb                  |
+====================================================================+
|                                                                    |
|  namespace: platform-system                                        |
|  +--------------------------------------------------------------+  |
|  |  +--------------+ +--------------+ +--------------+          |  |
|  |  | Traefik      | | cert-manager | | ESO          |          |  |
|  |  | v28.3.0      | | v1.14.5      | | v0.14.3      |          |  |
|  |  | (2 replicas) | | (TLS certs)  | | (secret sync)|          |  |
|  |  | PDB, HA      | | Dual solver: | | ClusterStore |          |  |
|  |  +--------------+ | DNS-01 (CF)  | +--------------+          |  |
|  |                   | HTTP-01 (any)| +------------------+      |  |
|  |  +--------------+ +--------------+ | cloudflared      |      |  |
|  |  | Admission    |                  | v2024.12.2       |      |  |
|  |  | Policies     |                  | (2 replicas)     |      |  |
|  |  | (VAP)        |                  | CF Tunnel ingress|      |  |
|  |  +--------------+                  +------------------+      |  |
|  +--------------------------------------------------------------+  |
|                                                                    |
|  namespace: {tenant-id}-{environment} (per-tenant, per-env)        |
|  +--------------------------------------------------------------+  |
|  |  USER APPLICATIONS (managed by ArgoCD from mgmt cluster)     |  |
|  |                                                              |  |
|  |  +-----------+  +-----------+  +-----------+                 |  |
|  |  | Deployment|  | Service   |  | Ingress   |                 |  |
|  |  | (app pods)|  | (ClusterIP|  | Route     |                 |  |
|  |  |           |  |  :port)   |  | (Traefik) |                 |  |
|  |  +-----------+  +-----------+  +-----------+                 |  |
|  |                                                              |  |
|  |  NetworkPolicy: deny-all + allow from platform               |  |
|  |  ResourceQuota: per-plan limits                              |  |
|  |  PSA: restricted (non-root, drop ALL, readOnlyRootFilesystem)|  |
|  +--------------------------------------------------------------+  |
+====================================================================+
```

---

## CRD Map — All 10 Types

```
+===========================================================================+
|                        CRD HIERARCHY & OWNERSHIP                          |
+===========================================================================+
|                                                                           |
|  Tenant (TenantController)                                                |
|  ├── spec: displayName, plan, environments[], gitops, cellID              |
|  ├── status: phase (Provisioning→Active→Suspended→Deleting)               |
|  ├── CREATES: Environment CRs, Gitea org+repos, RBAC, namespaces         |
|  │                                                                        |
|  ├── Environment (EnvironmentController)                                  |
|  │   ├── spec: tenantRef, environment, namespace, resourceQuota, plan     |
|  │   ├── status: phase (Pending→Active→Deleting→Failed)                   |
|  │   └── CREATES: Namespace, RoleBindings, ResourceQuota, NetworkPolicy   |
|  │                                                                        |
|  ├── LoopZeroApplication (ApplicationController)                              |
|  │   ├── spec: tenantRef, project, environment, serviceName, servicePort  |
|  │   ├── status: phase (Progressing→Serving→Reachable→Degraded→Down)      |
|  │   ├── CREATED BY: API (ensureApplicationCRs on build success)          |
|  │   └── CREATES: ExposedRoute CRs                                       |
|  │       │                                                                |
|  │       └── ExposedRoute (ExposureController)                            |
|  │           ├── spec: applicationRef, hostname, serviceRef, tls          |
|  │           ├── status: phase (Pending→CertIssued→Reachable→Failed)      |
|  │           └── CREATES: IngressRoute (Traefik), Certificate (cert-mgr)  |
|  │                                                                        |
|  └── PreviewEnvironment (NO CONTROLLER — Sprint 5)                        |
|      ├── spec: tenantID, projectID, prNumber, branch, expiresAt           |
|      └── status: phase (Creating→Active→Expiring→Deleted)                 |
|                                                                           |
|  PlatformBootstrap (PlatformBootstrapController)                          |
|  ├── spec: gitea config, argocd config, api config, timeouts              |
|  ├── status: phase (Pending→ConfiguringGitea→ConfiguringArgoCD→Complete)  |
|  └── CONFIGURES: Gitea admin, ArgoCD JWT, API seed (idempotent)           |
|                                                                           |
|  ClusterCredential (ClusterCredentialController)                          |
|  ├── spec: clusterName, apiServer, secretRef, scope (argocd/operators)    |
|  ├── status: connected, lastCheckedAt                                     |
|  └── CREATES: ArgoCD Cluster Secret (kubeconfig distribution)             |
|                                                                           |
|  GitRepositoryBinding (GitRepositoryBindingController)                    |
|  ├── spec: repoURL, credentialSecretRef, consumer (type=argocd)           |
|  ├── status: credentialHash (rotation detection)                          |
|  └── CREATES: ArgoCD Repository Secret (git credentials sync)            |
|                                                                           |
|  AppSecret (NO CONTROLLER — secrets work via ESO directly)                |
|  ├── spec: tenantID, projectID, environment, secretRef, rotation          |
|  └── status: syncedAt, hashMatch                                          |
|                                                                           |
|  ReconciliationReport (PLACEHOLDER — drift detection, deferred)           |
|  ├── spec: tenantID, checkInterval, driftThreshold                        |
|  └── status: driftDetails[]                                               |
+===========================================================================+
```

---

## Deploy Flow — End to End

```
DEVELOPER                     MANAGEMENT CLUSTER                BUILD CLUSTER               RUNTIME CLUSTER
    |                               |                               |                           |
    |  loopzero deploy                  |                               |                           |
    |------------------------------>|                               |                           |
    |  1. Upload source bundle      |                               |                           |
    |  POST /api/v1/source/upload   |                               |                           |
    |         |                     |                               |                           |
    |  2. Submit build              |                               |                           |
    |  POST /api/v1/builds          |                               |                           |
    |         |                     |                               |                           |
    |         |  pg_advisory_xact_lock(tenant:project:env)          |                           |
    |         |  Check concurrent build (INSIDE lock)               |                           |
    |         |  Create Build record (QUEUED)                       |                           |
    |         |  Commit (release lock)                              |                           |
    |         |                     |                               |                           |
    |         |  Submit Argo Workflow ------------------------------>|                           |
    |         |  QUEUED → RUNNING   |                               |                           |
    |         |                     |                               |                           |
    |  3. SSE stream                |                               |                           |
    |  GET /api/v1/builds/{id}/stream                               |                           |
    |<- - - - - - - - - - - - - - -|                               |                           |
    |  build.started                |                               |                           |
    |         |                     |    +---------------------+    |                           |
    |         |                     |    | clone-repo          |    |                           |
    |         |                     |    | cache-restore       |    |                           |
    |         |                     |    | install-deps        |    |                           |
    |         |                     |    | build (framework)   |    |                           |
    |         |                     |    | build-image         |----+---> DO Container Registry |
    |         |                     |    | upload-assets       |----+---> DO Spaces (static)    |
    |         |                     |    | cache-save (onExit) |    |                           |
    |         |                     |    +---------------------+    |                           |
    |         |                     |                               |                           |
    |         |  BuildReconcileLoop (polls every 30s)               |                           |
    |         |  detects Workflow Succeeded                         |                           |
    |         |                     |                               |                           |
    |         |  4. onWorkflowSucceeded:                            |                           |
    |         |     a. Generate K8s manifests                       |                           |
    |         |     b. ensureApplicationCRs (LoopZeroApplication CR)    |                           |
    |         |     c. Push manifests to Gitea (gitops)             |                           |
    |         |     d. Create Release (READY→PENDING)               |                           |
    |         |     e. Trigger ArgoCD sync  ---------------------------------------->           |
    |         |     f. RUNNING→RELEASING                            |            |              |
    |         |                     |                               |  ArgoCD    |              |
    |<- - - - - - - - - - - - - - -|                               |  syncs     |              |
    |  deploy.verifying             |                               |  manifests |              |
    |         |                     |                               |            v              |
    |         |  5. ArgoCD confirms sync                            |     +-------------+      |
    |         |     Release: PENDING→ACTIVE                         |     | Deployment  |      |
    |         |     Build: RELEASING→SUCCEEDED                      |     | Service     |      |
    |         |     Supersede previous ACTIVE release               |     | IngressRoute|      |
    |         |                     |                               |     +-------------+      |
    |<- - - - - - - - - - - - - - -|                               |                           |
    |  deploy.completed             |                               |                           |
    |  (URL: app.env.tenant.loopzero.example)                            |                           |
    |                               |                               |                           |
    |  6. ApplicationController reconciles LoopZeroApplication CR       |                           |
    |         |  → computes hostname                                |                           |
    |         |  → creates ExposedRoute CR                          |                           |
    |         |                     |                               |                           |
    |  7. ExposureController reconciles ExposedRoute CR             |                           |
    |         |  → creates cert-manager Certificate                 |                           |
    |         |  → runs reachability probes                         |                           |
    |         |  → phase: Pending→CertIssued→Reachable              |                           |
```

---

## Cloudflare Edge Layer

```
+===========================================================================+
|  CLOUDFLARE EDGE                                                          |
+===========================================================================+
|                                                                           |
|  DNS Records:                                                             |
|  +-----------------------------------------------------------------+     |
|  | *.dev.loopzero.example  CNAME → {tunnelId}.cfargotunnel.com (proxied)|     |
|  | cname.dev.loopzero.example CNAME → {tunnelId}.cfargotunnel.com       |     |
|  | api.dev.loopzero.example   → mgmt Traefik (LoopZero API)                 |     |
|  | cloud.dev.loopzero.example → mgmt Traefik (Dashboard+Kratos+Hydra)   |     |
|  +-----------------------------------------------------------------+     |
|                                                                           |
|  CF Worker: loopzero-static-delivery                                          |
|  Route: *.dev.loopzero.example/*                                               |
|  +-------------------------------------------------------------------+   |
|  | 1. Request arrives at edge                                        |   |
|  | 2. Check KV: loopzero-routing[subdomain] → {buildId, s3Prefix}       |   |
|  |    YES → Proxy to DO Spaces CDN (static frontend)                 |   |
|  |    NO  → Check KV: loopzero-routing[cell:{tenant}] → {cellId, ip}    |   |
|  |           YES → Proxy to cell Traefik (multi-cell routing)        |   |
|  |           NO  → Proxy through CF Tunnel (default)                 |   |
|  +-------------------------------------------------------------------+   |
|                                                                           |
|  CF Tunnel: loopzero-tunnel                                                   |
|  +-------------------------------------------------------------------+   |
|  | cloudflared (2 replicas) on runtime cluster                       |   |
|  | Ingress: *.{baseDomain} → runtime Traefik :443                    |   |
|  | Zero public IPs — all traffic through CF edge (DDoS/WAF)          |   |
|  +-------------------------------------------------------------------+   |
|                                                                           |
|  KV Namespace: loopzero-routing                                               |
|  +-------------------------------------------------------------------+   |
|  | Static routes:  key=subdomain    val={buildId, s3Prefix}          |   |
|  | Cell routes:    key=cell:{tenant} val={cellId, ingressIp}         |   |
|  | Written by: StaticDelivery (API) and CellService (API)            |   |
|  +-------------------------------------------------------------------+   |
+===========================================================================+
```

---

## Database Schema (PostgreSQL 16)

```
+===========================================================================+
|  DO Managed PostgreSQL — loopzero-infra-{env}-{region}-db                     |
|  Dev: 2 nodes db-s-1vcpu-2gb | Prod: 3 nodes db-s-2vcpu-4gb (HA)        |
|  VPC-only (no public access) | Firewall: K8s cluster IDs only            |
+===========================================================================+
|                                                                           |
|  Database: loopzero                                                           |
|  +-------------------------------------------------------------------+   |
|  | builds            | Build records (state machine: QUEUED→...→SUCC)|   |
|  | build_apps        | Per-app specs within a build                  |   |
|  | releases          | Release records (READY→PENDING→ACTIVE)        |   |
|  | projects          | Project metadata (UUID PK, name unique/tenant)|   |
|  | environment_targets| Desired vs active release per env            |   |
|  | audit_log         | Audit events (action, actor, resource)        |   |
|  | idempotency_keys  | Request dedup (key, hash, response, TTL)      |   |
|  | sse_events        | SSE event journal (NOTIFY/LISTEN)             |   |
|  | custom_domains    | Custom domain records (status, TLS, CNAME)    |   |
|  | secrets           | Encrypted secret storage                      |   |
|  | users             | User accounts (GitHub/Google/email)            |   |
|  | tenant_members    | Tenant membership (FK→users)                  |   |
|  | subscriptions     | Stripe billing subscriptions                  |   |
|  +-------------------------------------------------------------------+   |
|  | Serialization: pg_advisory_xact_lock(tenant:project:env)          |   |
|  | Isolation: READ COMMITTED + partial unique indexes                |   |
|  | Migrations: 25 forward-only (.up.sql), golang-migrate             |   |
|  +-------------------------------------------------------------------+   |
|                                                                           |
|  Database: gitea     (Gitea internal state)                               |
|  Database: kratos    (Ory Kratos identity store)                          |
|  Database: hydra     (Ory Hydra OAuth2 grants)                            |
+===========================================================================+
```

---

## Component Connection Map

```
+===========================================================================+
|  WHO TALKS TO WHOM                                                        |
+===========================================================================+
|                                                                           |
|  CLI (TypeScript)                                                         |
|   ├──→ API (Go)           HTTPS via api.{domain}                          |
|   └──→ Kratos/Hydra       HTTPS via cloud.{domain} (device code flow)     |
|                                                                           |
|  Dashboard (React)                                                        |
|   ├──→ Kratos              Session auth via cloud.{domain}                |
|   ├──→ Hydra               BFF token exchange                             |
|   └──→ API                 JWT bearer via api.{domain}                    |
|                                                                           |
|  API (Go, mgmt cluster)                                                   |
|   ├──→ PostgreSQL          pgx connection pool (VPC)                      |
|   ├──→ Gitea               HTTP :3000 (cluster-internal) — gitops push    |
|   ├──→ ArgoCD              HTTPS (cluster-internal) — sync trigger        |
|   ├──→ Argo Workflows      K8s API (build cluster) — submit/status        |
|   ├──→ MinIO / DO Spaces   S3 API — source bundles, build cache           |
|   ├──→ Cloudflare KV       CF API — static routing table writes           |
|   ├──→ DO Spaces CDN       S3 API — static asset upload                   |
|   ├──→ K8s API (mgmt)      dynamic client — Tenant CR, LoopZeroApplication CR |
|   └──→ Stripe              HTTPS — billing webhooks, checkout             |
|                                                                           |
|  Operators (Go, mgmt cluster)                                             |
|   ├──→ Gitea               HTTP :3000 — org/repo provisioning             |
|   ├──→ ArgoCD              K8s API — Cluster Secret, Repo Secret          |
|   ├──→ MinIO               S3 API — per-tenant credentials + policies     |
|   └──→ K8s API (mgmt)      CRD reconciliation (all 7 controllers)        |
|                                                                           |
|  ArgoCD (mgmt cluster)                                                    |
|   ├──→ Gitea               Git clone (gitops repos)                       |
|   └──→ K8s API (runtime)   Apply manifests (user app deployments)         |
|                                                                           |
|  Argo Workflows (build cluster)                                           |
|   ├──→ MinIO / DO Spaces   S3 — clone source, cache restore/save         |
|   ├──→ DO Registry         Docker push (built images)                     |
|   └──→ DO Spaces CDN       S3 — upload static assets                     |
|                                                                           |
|  Cloudflare Worker                                                        |
|   ├──→ CF KV               Read routing table                             |
|   ├──→ DO Spaces CDN       Proxy static frontend assets                   |
|   └──→ CF Tunnel           Proxy dynamic requests to runtime              |
|                                                                           |
|  ESO (runtime cluster)                                                    |
|   └──→ K8s API (mgmt)      ClusterSecretStore — sync secrets cross-cluster|
+===========================================================================+
```

---

## State Machines (7 total, pkg/statemachine/)

```
BUILD                           RELEASE                      TENANT
=====                           =======                      ======
QUEUED ──→ RUNNING              READY ──→ PENDING            Provisioning
  │          │ │                   │        │                     │
  │          │ ├──→ RELEASING     │        ├──→ ACTIVE          ├──→ Active
  │          │ │      │           │        │      │              │      │
  │          │ │      ├──→ SUCC   │        │      └──→ SUPERSEDED│      ├──→ Suspended
  │          │ │      └──→ FAIL   │        └──→ CANCELLED        │      │      │
  │          │ └──→ FAILED        └──→ ROLLED_BACK               │      └──→ Deleting
  │          └──→ CANCELLED                                      └──→ Deleting
  └──→ (timeout: 5m)

PREVIEW                         PROMOTION                    ROLLBACK
=======                         =========                    ========
Creating ──→ Active             Validating ──→ Committing    Validating ──→ Reverting
               │                                │                            │
               ├──→ Expiring    Committing ──→ Syncing       Reverting ──→ Syncing
               │      │                         │                            │
               └──→ Deleting    Syncing ──→ Completed        Syncing ──→ Completed
                      │                    └──→ Failed                  └──→ Failed
                      └──→ Deleted

BOOTSTRAP
=========
Pending ──→ ConfiguringGitea ──→ ConfiguringArgoCD ──→ WaitingForAPI ──→ Complete
   └──→ Failed     └──→ Failed       └──→ Failed          └──→ Failed
```

---

## Observability Stack

```
+===========================================================================+
|  OBSERVABILITY                                                            |
+===========================================================================+
|                                                                           |
|  Prometheus (mgmt) ←── ServiceMonitor ←── loopzero-api, traefik, argocd      |
|       │                                                                   |
|       ├── Alertmanager (4 rules):                                         |
|       │   - LoopZeroAPIDown (API pods < 1 for 2m)                             |
|       │   - LoopZeroDeployFailing (deploy fail rate > 50% for 5m)             |
|       │   - LoopZeroHighLatency (p99 > 5s for 5m)                             |
|       │   - LoopZeroCrashLoop (restart count > 3 in 10m)                      |
|       │                                                                   |
|       ├── Key metrics:                                                    |
|       │   loopzero_builds_total, loopzero_build_duration_seconds                  |
|       │   loopzero_deploys_total, loopzero_deploy_duration_seconds                |
|       │   loopzero_application_cr_ensure_total (NEW)                          |
|       │   loopzero_lock_acquired_total, loopzero_lock_conflicts_total             |
|       │   loopzero_argocd_circuit_state, loopzero_drift_detected                  |
|       │   loopzero_custom_domains_total, loopzero_domain_tls_expiry_days          |
|       │                                                                   |
|       └──→ Grafana (dashboards: API, builds, deploys, ArgoCD, locks)      |
|                                                                           |
|  Loki (build cluster) ←── Promtail ←── build workflow pod logs            |
|       └── Queried by API via LogQL (loopzero logs command)                    |
|                                                                           |
|  OTel Collector (mgmt) ←── loopzero-api traces (context propagation)          |
|       └──→ Tempo (distributed tracing storage)                            |
+===========================================================================+
```

---

## Admission & Security Policies

```
+===========================================================================+
|  SECURITY LAYERS                                                          |
+===========================================================================+
|                                                                           |
|  ValidatingAdmissionPolicies (runtime cluster):                           |
|  ├── vap-disallow-latest-tag         (Audit mode)                         |
|  ├── vap-require-resource-limits     (Audit mode)                         |
|  ├── vap-restrict-image-registries   (Audit mode)                         |
|  └── vap-protect-platform-networkpolicies (DENY — strict)                 |
|                                                                           |
|  Pod Security Admission (PSA):                                            |
|  └── restricted profile on tenant namespaces                              |
|      (non-root, drop ALL caps, readOnlyRootFilesystem)                    |
|                                                                           |
|  NetworkPolicy:                                                           |
|  ├── Default deny-all on every tenant namespace                           |
|  ├── Allow ingress from platform-system (Traefik)                         |
|  ├── Allow egress to DNS (53), HTTPS (443)                                |
|  └── Build cluster: allow egress to MinIO, registry, HTTP/S only          |
|                                                                           |
|  Auth:                                                                    |
|  ├── CLI: Device Code Flow (Hydra OIDC) → JWT                            |
|  ├── Dashboard: Kratos session → BFF token exchange → JWT                 |
|  ├── API: JWT validation (RSA-only, audience check, kid+dual-key)         |
|  └── No PAT, no "default" tenant fallback (MEET-039)                     |
|                                                                           |
|  Two-host gateway:                                                        |
|  ├── cloud.{domain} → Kratos + Hydra + Dashboard (browser)               |
|  └── api.{domain}   → LoopZero API engine (CLI)                              |
+===========================================================================+
```
