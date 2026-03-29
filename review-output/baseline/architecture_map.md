# Architecture Map — Phase 1 Baseline

Generated: 2026-03-29
Target: /home/paulo/loopzero

## System Overview

LoopZero is a CLI-first Kubernetes PaaS for monorepo deployments. It provides a Vercel-like DX for backend services — deploy with a single command, get TLS, preview environments, and rollback.

## Four-Plane Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  DEVELOPER PLANE                                                 │
│  CLI (TypeScript/Commander) ←HTTP→ API (Go/chi/pgx)             │
│  Entry: loopzero login → loopzero init → loopzero deploy                    │
├─────────────────────────────────────────────────────────────────┤
│  CONTROL PLANE  (mgmt cluster — DOKS)                            │
│  7 Controllers (kubebuilder):                                    │
│    Tenant · Environment · Application · Exposure                 │
│    PlatformBootstrap · GitRepositoryBinding · ClusterCredential  │
│  Services: Gitea 1.22.6 · PostgreSQL 16 · MinIO · ArgoCD 2.13.3│
│            Traefik · cert-manager · Prometheus · Grafana · Loki  │
│            ESO · OpenTelemetry                                   │
├─────────────────────────────────────────────────────────────────┤
│  BUILD PLANE  (build cluster — DOKS)                             │
│  Argo Workflows + BuildKit (rootless, single-container)          │
│  CWTs: clone, cache-restore, cache-save, summary, resource-quota│
├─────────────────────────────────────────────────────────────────┤
│  WORKLOAD PLANE  (runtime cluster — DOKS)                        │
│  User apps managed by ArgoCD (GitOps sync from Gitea)            │
│  Traefik ingress + cert-manager TLS                              │
│  Cloudflare Tunnel for external traffic                          │
│  Static assets: DO Spaces + CF Worker                            │
└─────────────────────────────────────────────────────────────────┘
```

## Component Relationships

```
                    ┌──────────┐
                    │   CLI    │ TypeScript
                    │ (cli/)   │ Commander
                    └────┬─────┘
                         │ HTTP/SSE
                    ┌────▼─────┐
                    │   API    │ Go 1.25
                    │ (api/)   │ chi + pgx
                    └──┬─┬─┬──┘
          ┌────────────┘ │ └──────────────┐
          │              │                │
    ┌─────▼────┐  ┌──────▼─────┐  ┌──────▼──────┐
    │PostgreSQL│  │Argo Wflows │  │   Gitea     │
    │(state)   │  │(build)     │  │(gitops/src) │
    └──────────┘  └──────┬─────┘  └──────┬──────┘
                         │               │
                   ┌─────▼────┐   ┌──────▼──────┐
                   │ BuildKit │   │   ArgoCD    │
                   │(rootless)│   │(manual sync)│
                   └──────────┘   └──────┬──────┘
                                         │
                                  ┌──────▼──────┐
                                  │  Workload   │
                                  │  Cluster    │
                                  └─────────────┘

    ┌───────────┐     ┌──────────────┐
    │ Operators │────→│ K8s CRDs (9) │
    │(operators/)│    │ mgmt cluster │
    └───────────┘     └──────────────┘

    ┌───────────┐     ┌──────────────┐
    │ CF Worker │────→│ DO Spaces    │
    │(static)   │     │(CDN assets)  │
    └───────────┘     └──────────────┘
```

## Data Flow: Deploy (Happy Path)

```
1. loopzero deploy
   │
2. CLI: validate loopzero.yaml → detect framework → bundle source
   │
3. CLI → POST /api/v1/builds (Idempotency-Key header)
   │
4. API: withEnvironmentLock(tenant, project, env)
   │   ├── Check no concurrent build (inside lock!)
   │   ├── Create build record (QUEUED)
   │   ├── Upload bundle to Gitea source repo
   │   └── Submit Argo Workflow
   │
5. API → SSE stream to CLI (build progress)
   │
6. Argo Workflow (build cluster):
   │   ├── Clone source from Gitea
   │   ├── Restore BuildKit cache
   │   ├── Build with BuildKit (rootless)
   │   ├── Push image to DO Container Registry
   │   └── Save cache
   │
7. API: BuildReconcileLoop detects workflow completion
   │   ├── Transition build: RUNNING → RELEASING
   │   ├── Generate K8s manifests
   │   ├── Push manifests to Gitea gitops repo
   │   ├── Create release record
   │   ├── Trigger ArgoCD manual sync
   │   ├── [Static apps: upload to DO Spaces, write CF KV]
   │   └── Transition build: RELEASING → SUCCEEDED
   │
8. CLI receives SSE event → shows live URL
```

## Key Technology Decisions

| Decision | Rationale |
|----------|-----------|
| pg_advisory_xact_lock (not session) | Connection pool safety — session locks leak |
| Manual ArgoCD sync (not auto) | Explicit control over deployment timing |
| BuildKit rootless single-container | Security: no privileged containers for builds |
| Forward-only migrations | Simplicity, but requires careful migration authoring |
| GitHub token as auth | Leverages existing developer identity, no separate auth system |
| 3-cluster separation | Blast radius isolation: mgmt/build/workload planes |
| Cloudflare Tunnel | Zero public IPs on runtime cluster, DDoS/WAF protection |
| SSE for build streaming | Simpler than WebSocket, built-in reconnection support |

## Cross-Cutting Concerns

| Concern | Implementation |
|---------|----------------|
| Concurrency control | pg_advisory_xact_lock per (tenant, project, env) |
| Idempotency | Idempotency-Key middleware + idempotency_store |
| Correlation | Correlation-ID header forwarded through all components |
| Observability | Prometheus metrics + Grafana dashboards + Loki logs + OTel |
| Secret management | ESO for cross-cluster sync, DO Spaces for state backend |
| Error handling | LoopZeroError with 24 typed error codes (pkg/errors) |
| State machines | 7 strict state machines in pkg/statemachine |
| Rate limiting | In-memory fixed-window per tenant |

## Boundary Analysis

| Boundary | Trust Level | Validation |
|----------|-------------|------------|
| CLI → API | Untrusted | Auth middleware + input validation |
| API → PostgreSQL | Trusted | Parameterized queries via pgx |
| API → Gitea | Trusted (internal) | Token auth, cross-cluster URL |
| API → Argo Workflows | Trusted (internal) | K8s RBAC |
| API → ArgoCD | Trusted (internal) | Token auth |
| API → CF KV | Semi-trusted (external) | API token, HTTPS |
| Argo Workflow → DO Registry | Trusted | Registry credentials via K8s secret |
| ArgoCD → Gitea | Trusted (internal) | Token via K8s secret |
| CF Worker → DO Spaces | Trusted | S3 credentials in Worker env |
| External → CF Tunnel | Untrusted | Cloudflare WAF/DDoS |
