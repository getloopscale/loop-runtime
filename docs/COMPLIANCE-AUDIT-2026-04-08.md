# LoopZero Platform — Compliance Audit Report

**Date:** 2026-04-08
**Scope:** Full platform audit — Infrastructure, Security, Pipeline, Database, Operators, Deploy Flow, Helm Charts
**Auditor:** Automated deep audit (7 parallel domain agents)
**Target:** Compliance review for enterprise adoption

---

## Executive Summary

The LoopZero platform demonstrates **strong foundational security and architectural correctness** across all layers. The codebase follows defense-in-depth principles with multi-tenant isolation (RLS + advisory locks + namespace isolation), idempotent reconciliation, proper state machine enforcement, and comprehensive error handling.

**Overall Score: B+ (Production-ready with documented gaps)**

| Domain | Score | Critical | High | Medium | Low |
|--------|-------|----------|------|--------|-----|
| Infrastructure (IaC) | A- | 0 | 0 | 1 | 2 |
| Helm Charts | B | 1 | 4 | 4 | 4 |
| Deploy Flow | A | 0 | 0 | 2 | 3 |
| Build Pipeline | A | 0 | 0 | 1 | 2 |
| Operators & Controllers | A- | 0 | 0 | 2 | 3 |
| Database Layer | A | 0 | 0 | 1 | 2 |
| Security & RBAC | B+ | 0 | 1 | 3 | 5 |
| **TOTAL** | **B+** | **1** | **5** | **14** | **21** |

---

## Table of Contents

1. [Infrastructure (DigitalOcean + Pulumi)](#1-infrastructure)
2. [Helm Charts Configuration](#2-helm-charts)
3. [Deploy Flow (CLI → API → Build → Runtime)](#3-deploy-flow)
4. [Build Pipeline (Argo Workflows)](#4-build-pipeline)
5. [Operators & Controllers](#5-operators--controllers)
6. [Database Layer](#6-database-layer)
7. [Security & RBAC](#7-security--rbac)
8. [Consolidated Findings](#8-consolidated-findings)
9. [Remediation Plan](#9-remediation-plan)

---

## 1. Infrastructure

### 1.1 Architecture Overview

3-phase Pulumi provisioning on DigitalOcean:

| Phase | Resources | Purpose |
|-------|-----------|---------|
| Phase 1: Infra | VPC, 3 DOKS clusters, PostgreSQL, 4 Spaces buckets, Container Registry | Base infrastructure |
| Phase 2: Bootstrap | Helm charts (13 pinned), platform namespaces, service accounts, tokens | Cluster configuration |
| Phase 3: Expose | DNS records, Cloudflare Tunnel, CF Worker + KV | External access |

### 1.2 Cluster Configuration

| Cluster | Purpose | Dev Size | Prod Size | Auto-Scale |
|---------|---------|----------|-----------|------------|
| Management | ArgoCD, Gitea, API, PostgreSQL, Traefik, Ory | s-2vcpu-4gb (2-3) | s-4vcpu-8gb (3-5) | Yes |
| Build | Argo Workflows, BuildKit, Loki | s-2vcpu-4gb (2-3) | s-4vcpu-8gb (2-4) | Yes |
| Runtime | User applications, Traefik ingress | s-2vcpu-4gb (2-3) | s-4vcpu-8gb (3-6) | Yes |

### 1.3 Database Configuration

| Setting | Dev | Production |
|---------|-----|------------|
| Engine | PostgreSQL 16 | PostgreSQL 16 |
| Nodes | 2 (single-zone) | 3 (HA, automatic failover) |
| Sizing | db-s-1vcpu-2gb | db-s-2vcpu-4gb |
| Network | VPC-only (private) | VPC-only (private) |
| Backups | DO-managed (daily, 7-day retention) | DO-managed + CronJob pg_dumpall |
| Firewall | K8s cluster IDs only | K8s cluster IDs only |
| Databases | loopzero, gitea, kratos, hydra | loopzero, gitea, kratos, hydra |

### 1.4 Storage (4 Spaces Buckets)

| Bucket | Purpose | Versioning | CDN |
|--------|---------|------------|-----|
| loopzero-infra-{env}-artifacts | Platform artifacts | Enabled | No |
| loopzero-builds | Source bundles + build cache | Disabled | No |
| loopzero-logs | Loki S3 backend | Disabled | No |
| loopzero-static-assets | Frontend assets | Disabled | Yes (DO CDN, TTL 3600s) |

### 1.5 Cloudflare Integration

- **Tunnel:** Zero public IPs, all runtime traffic via CF edge (DDoS/WAF)
- **cloudflared:** 2 replicas, non-root (uid 65532), readOnlyRootFilesystem
- **Worker:** `loopzero-static-delivery` — KV-based routing for static frontends
- **DNS:** Wildcard CNAME `*.{baseDomain}` → tunnel; `cname.{baseDomain}` for custom domains

### 1.6 Secret Management

- **Pulumi config encryption:** Passphrase-based, stored in DO Spaces
- **Production fail-fast:** Missing `giteaAdminPassword` or `minioRootPassword` throws `InfraError`
- **Resolution order:** Environment variables (CI/CD) → Pulumi config → undefined (disabled)
- **Dual-key rotation:** `serviceAuthSecret` supports current + previous for zero-downtime

### 1.7 Findings

| ID | Severity | Finding | Status |
|----|----------|---------|--------|
| INFRA-001 | MEDIUM | Staging DB has 1 node (no HA) — cost optimization but single point of failure | Accepted risk |
| INFRA-002 | LOW | Container Registry shared across all stacks (one per DO account) | Protected with `retainOnDelete: true` |
| INFRA-003 | LOW | pgBackRest not configured for PITR — relies on pg_dumpall CronJob | Documented, CloudNativePG recommended |

---

## 2. Helm Charts

### 2.1 Chart Compliance Matrix

| Chart | Version | Non-Root | ReadOnly FS | Probes | Resource Limits | Network Policy | TLS | HA | Grade |
|-------|---------|----------|-------------|--------|-----------------|----------------|-----|-----|-------|
| argo-workflows | 0.1.0 | ✓ | - | ✓ | ✓ | ✓ (egress) | Dev: insecure | Single | B+ |
| argocd | v2.13.3 | ✓ | - | ✓ | ✓ | ✓ | Dev: insecure | PDB | B |
| cert-manager | latest | ✓ | - | ✓ | ✓ | ✓ | Self-signed CA | Single | A- |
| gitea | 1.22.6 | ✓ | - | ✓ | ✓ | ✓ | Dev: no SSL | PDB | B |
| minio | 5.4.0 | ✓ | ✓ | ✓ | ✓ | ✓ | Dev: no TLS | Dev: standalone | B+ |
| monitoring | 25.27.0 | ✓ | - | ✓ | ✓ | ✓ | ✓ | Single | A- |
| network-policies | - | - | - | - | - | ✓ Default-deny | - | - | A |
| otel | 0.96.0 | ✓ | - | ✓ | ✓ | ✓ | In-cluster OK | Single | A |
| postgresql | Bitnami | ✓ | - | ✓ | ✓ | ✓ | Dev: no TLS | Dev: single | B+ |
| traefik | 28.3.0 | ✓ | - | ✓ | ✓ | ✓ | ✓ TLS enforced | 2 replicas + PDB | A |
| admission-policies | VAP | - | - | - | - | - | - | - | A- |
| ory-kratos | 0.61.0 | ✓ | - | ✓ | ✓ | ✓ | Dev: HTTP | Single | B |
| ory-hydra | 0.61.0 | ✓ | - | ✓ | ✓ | ✓ | Dev: HTTP | Single | B |

### 2.2 Resource Limits (All Charts)

All charts define CPU/memory requests and limits:

| Component | Requests | Limits |
|-----------|----------|--------|
| loopzero-api | 100m / 64Mi | 500m / 256Mi |
| tenant-controller | 100m / 64Mi | 500m / 256Mi |
| Argo controller | 500m / 512Mi | 2000m / 1Gi |
| ArgoCD server | 100m / 128Mi | 500m / 256Mi |
| PostgreSQL (prod) | 500m / 1Gi | 2000m / 4Gi |
| Traefik | 100m / 128Mi | 500m / 256Mi |
| Prometheus | 100m / 256Mi | 500m / 512Mi |
| Grafana | 50m / 128Mi | 300m / 256Mi |
| cloudflared | 50m / 64Mi | 200m / 256Mi |
| BuildKit step | 500m / 512Mi | 2000m / 4Gi |

### 2.3 Admission Policies (ValidatingAdmissionPolicy)

| Policy | Enforcement | Scope |
|--------|-------------|-------|
| vap-disallow-latest-tag | Audit (non-blocking) | Tenant workloads |
| vap-require-resource-limits | Audit | Tenant workloads |
| vap-restrict-image-registries | Audit | Tenant workloads |
| vap-protect-platform-networkpolicies | **Deny** (strict) | Tenant namespaces |

### 2.4 Traefik Security Middlewares

- **Rate limiting:** 100 avg, 200 burst per second
- **Body limit:** 10 MiB max request
- **Security headers:** HSTS (1 year), Frame-Deny, Content-Type NoSniff, CSP with Stripe integration
- **HTTP → HTTPS redirect:** Enforced on all entrypoints
- **Global timeouts:** Read 60s, Write 60s, Idle 120s

### 2.5 Findings

| ID | Severity | Finding | File | Status |
|----|----------|---------|------|--------|
| HELM-001 | **CRITICAL** | ArgoCD webhook callback secret hardcoded: `Bearer cb-loopzero-dev-2026` | `infra/helm/argocd/values.yaml:156` | MEET-2026-04-04-029 P0 blocker |
| HELM-002 | HIGH | Hardcoded MinIO credentials in lifecycle job and credentials secret | `infra/helm/minio/lifecycle-job.yaml:97-100` | Dev-only, needs ESO for prod |
| HELM-003 | HIGH | Gitea admin password + DB password hardcoded | `infra/helm/gitea/values.yaml:6-7,19` | Dev-only, marked LOCAL DEV ONLY |
| HELM-004 | HIGH | PostgreSQL password hardcoded | `infra/helm/postgresql/values.yaml:14` | Dev-only, Pulumi overrides in prod |
| HELM-005 | HIGH | Ory Kratos/Hydra cookie + cipher secrets hardcoded | `infra/helm/ory-kratos/values.yaml:141-143` | Dev-only, Pulumi overrides |
| HELM-006 | MEDIUM | MinIO `insecure: true` in Argo Workflows config | `infra/helm/argo-workflows/values.yaml:65` | Dev-only, prod overlay required |
| HELM-007 | MEDIUM | ArgoCD `server.insecure: true` | `infra/helm/argocd/values.yaml:98` | TLS terminated at Traefik |
| HELM-008 | MEDIUM | Gitea SSL_MODE: disable for PostgreSQL connection | `infra/helm/gitea/values.yaml:20` | Dev-only |
| HELM-009 | MEDIUM | Admission policies in Audit mode (not Deny) for image registries + latest tag | `infra/helm/admission-policies/` | Gradual rollout acceptable |
| HELM-010 | LOW | Cross-cluster hardcoded IPs (172.18.0.x) in monitoring + Tilt | Multiple files | k3d-specific, prod uses DNS |
| HELM-011 | LOW | AlertManager webhook to localhost dev-null placeholder | `infra/helm/monitoring/values.yaml:143` | Needs real webhook in prod |
| HELM-012 | LOW | Loki S3 credentials hardcoded | `infra/helm/loki/values.yaml:22-23` | Dev-only |
| HELM-013 | LOW | `readOnlyRootFilesystem` not set on most containers | Multiple values.yaml | Defense-in-depth gap |

---

## 3. Deploy Flow

### 3.1 Flow Trace

```
CLI: loopzero deploy
  ├── 1. Auth check (exit 4 if not authenticated)
  ├── 2. Config validation (loopzero.yaml, apps, environment name DA-10)
  ├── 3. Secret pre-check (verify declared secrets exist)
  ├── 4. Source bundle (git bundle, max 500MB, sensitive file warning)
  ├── 5. Upload source → POST /api/v1/source/upload
  ├── 6. Submit build → POST /api/v1/builds (Idempotency-Key header)
  │     └── API: WithEnvironmentLock(tenant, project, env)
  │           ├── Check concurrent build (INSIDE lock — Rule 6)
  │           ├── EnsureProject (idempotent)
  │           ├── Create Build record (QUEUED)
  │           └── Commit (releases lock)
  │     └── OUTSIDE LOCK:
  │           ├── Normalize apps → BuildPlan → WorkflowSpec
  │           ├── Submit Argo Workflow (3 retries, exponential backoff)
  │           ├── Transition QUEUED → RUNNING
  │           └── Emit SSE: build.started
  ├── 7. SSE stream → /api/v1/builds/{id}/stream
  │     └── Events: build.started, deploy.verifying, deploy.completed, build.failed
  │     └── Fallback: GET /api/v1/builds/{id} on stream break (BLOCKER-1)
  └── 8. Exit code: 0=success, 1=validation, 2=build fail, 3=deploy fail, 4=auth, 5=partial

Build Cluster (Argo Workflow):
  ├── clone-repo (source from MinIO)
  ├── cache-restore (best-effort, non-fatal)
  ├── install-deps (language-specific)
  ├── build (framework-specific)
  ├── build-image (BuildKit rootless single-container)
  │   └── Push to DO Container Registry
  ├── upload-assets (static apps → S3)
  └── onExit: cache-save (best-effort)

API Reconcile Loop (polls every 30s):
  ├── Workflow Succeeded → onWorkflowSucceeded()
  │     ├── Build manifests (K8s YAML)
  │     ├── GitOps push to Gitea (git trailers: Release-ID, Build-ID, Correlation-ID)
  │     ├── Create Release (READY → PENDING)
  │     ├── Trigger ArgoCD sync (3 retries)
  │     └── Emit SSE: deploy.verifying
  ├── ArgoCD callback OR convergence loop → Release ACTIVE
  │     ├── Supersede previous ACTIVE release
  │     ├── Transition Build: RELEASING → SUCCEEDED
  │     └── Emit SSE: deploy.completed
  └── Workflow Failed → failBuild() → Emit SSE: build.failed
```

### 3.2 Concurrency Model

- **Lock:** `pg_advisory_xact_lock(hashtext("{tenant}:{project}:{env}"))` — 5s timeout
- **Isolation:** READ COMMITTED, transaction-scoped (safe with pgxpool)
- **Concurrent build check:** INSIDE lock (Rule 6 — prevents race condition)
- **Partial unique index:** `idx_one_active_build_per_env` enforces at most one active build per environment

### 3.3 Safety Nets

| Component | Mechanism | Purpose |
|-----------|-----------|---------|
| Build Reaper | Polls every 30s | Timeout enforcement (QUEUED 5m, RUNNING 30m, RELEASING 10m) |
| Reconcile Loop | Polls every 30s | Catch missed workflow completions |
| Circuit Breaker | 3 failures → open | Prevent cascading failure to Argo API |
| Completion Attempts | Max 3 retries | Prevent permanent RUNNING on manifest/gitops failure |
| Activation Attempts | Max 3 retries | Prevent permanent RELEASING on sync failure |
| SSE Fallback | GET /api/v1/builds/{id} | Recover from broken stream |
| M-03 Fix | Verify workflow status before reaping | Prevent false-positive reaping |

### 3.4 Security Validation

- **SEC-3, CODE-001:** App name regex `^[a-z0-9_-]{1,63}$` (prevents shell injection in build scripts)
- **SEC-3:** App path validation — no `..`, `/`, traversal (prevents path injection)
- **SEC-4:** Tenant isolation — returns 404 (not 403) to avoid leaking build existence
- **SEC-1:** Git auth via `http.extraHeader` (not embedded in URL)

### 3.5 Findings

| ID | Severity | Finding | Impact |
|----|----------|---------|--------|
| FLOW-001 | MEDIUM | ARGO_CALLBACK_SECRET mismatch: API uses `dev-callback-secret`, ArgoCD uses `cb-loopzero-dev-2026` | Webhook auth fails in dev |
| FLOW-002 | MEDIUM | SSE stream has no explicit max-age/keepalive; proxy timeouts may kill long deploys | Stream breaks on slow builds |
| FLOW-003 | LOW | Lock hash collision ~0.001% (int4 range) — acceptable for liveness, not safety | LoopZeroretical only |
| FLOW-004 | LOW | No explicit workflow-level timeout (relies on step-level + reaper) | Covered by reaper |
| FLOW-005 | LOW | `setWorkflowID` failure is non-fatal — reconcile loop finds by labels | Self-healing |

---

## 4. Build Pipeline

### 4.1 Isolation Model

| Layer | Implementation |
|-------|---------------|
| Namespace | Per-tenant `build-{tenant-id}` namespace |
| Rootless BuildKit | `moby/buildkit:v0.18.2-rootless` — single-container pattern (no sidecar) |
| UID/GID | All containers run as uid 1000 (non-root) |
| Resource Limits | ResourceQuota + LimitRange in argo namespace |
| Network Policy | Default-deny egress with explicit allowlist (DNS, MinIO, registry, HTTP/S) |
| Registry Auth | Docker config.json mounted read-only from K8s Secret |

### 4.2 Resource Quotas

```
ResourceQuota (argo-workflow-quota):
  requests.cpu: 8    limits.cpu: 16
  requests.memory: 16Gi    limits.memory: 32Gi
  pods: 50

LimitRange defaults:
  CPU: 1 (limit), 100m (request)
  Memory: 2Gi (limit), 256Mi (request)
```

### 4.3 Retry & Timeout Strategy

| Step | Retries | Backoff | OOM Skip | Timeout |
|------|---------|---------|----------|---------|
| clone-repo | 2 | 5s/2x/30s | Yes | - |
| cache-restore | 0 | - | - | 120s (best-effort) |
| install-deps | 3 | 5s/2x/60s | Yes | - |
| build-image | 3 | 10s/2x/300s | Yes | - |
| upload-assets | 2 | 10s/2x/120s | Yes | - |
| cache-save | 0 | - | - | 120s (best-effort) |

**OOM protection:** `asInt(lastRetry.exitCode) != 137` — skips retry on exit code 137 (OOM kill)

### 4.4 Sidecar False-Failure Fix (MEET-2026-03-26-007)

- **Problem:** Argo kills buildkitd sidecar with SIGTERM → exit 1 → marks workflow Failed
- **Solution:** Single-container pattern (buildkitd + buildctl in same container)
- **Defense-in-depth:** If phase=Failed but all pod main containers exited 0, override to Succeeded

### 4.5 Credential Security

- **Zero credentials in Workflow CRs:** All secrets injected via K8s Secrets + envFrom
- **No callbacks:** Polling model (C-01 fix) — no callback URL/token exposed in workflow specs
- **Registry push:** Docker config.json mounted at `/home/user/.docker/` (read-only)

### 4.6 Findings

| ID | Severity | Finding | Impact |
|----|----------|---------|--------|
| PIPE-001 | MEDIUM | No ingress NetworkPolicy for build pods — may accept arbitrary ingress | Build pod exposure |
| PIPE-002 | LOW | seccompProfile: Unconfined for BuildKit (required for rootlesskit) | Necessary for user namespaces |
| PIPE-003 | LOW | Argo controller/server are single-replica | No HA for build controller |

---

## 5. Operators & Controllers

### 5.1 Controller Status

| Controller | State Machine | Idempotent | Finalizers | Cross-Cluster | Tests |
|------------|--------------|------------|------------|---------------|-------|
| TenantReconciler | ✓ | ✓ (CreateOrUpdate) | ✓ (3-phase cleanup) | ✓ (mgmt+build+runtime) | ✓ |
| EnvironmentReconciler | ✓ | ✓ (CreateOrUpdate) | ✓ (verify-then-remove) | ✓ (mgmt+runtime) | ✓ |
| ApplicationReconciler | **Missing** | ✓ | ✓ | - | ✓ |
| ExposureReconciler | - | ✓ | - | - | ✓ |
| GitRepositoryBindingReconciler | - | ✓ (hash-based) | ✓ | - | ✓ |
| ClusterCredentialReconciler | - | ✓ (CreateOrUpdate) | ✓ | - | ✓ |
| PlatformBootstrapReconciler | ✓ | ✓ | - | - | ✓ |

### 5.2 CRDs (10 total)

| CRD | Controller | Validation | Status Subresource | Print Columns |
|-----|-----------|------------|-------------------|---------------|
| Tenant | ✓ | ✓ (DisplayName, Plan enum) | ✓ | Phase, Plan, Age |
| Environment | ✓ | ✓ | ✓ | Phase, Namespace, Age |
| LoopZeroApplication | ✓ | ✓ | ✓ | Phase, URL, Age |
| ExposedRoute | ✓ | ✓ | ✓ | Phase, Age |
| PlatformBootstrap | ✓ | ✓ (SSRF SEC-004) | ✓ | Phase, Age |
| GitRepositoryBinding | ✓ | ✓ | ✓ | Phase, Age |
| ClusterCredential | ✓ | ✓ (APIServer URL) | ✓ | Cluster, Connected, Age |
| AppSecret | - | ✓ | ✓ | - |
| PreviewEnvironment | - | ✓ | ✓ | - |
| ReconciliationReport | - | - | - | - |

### 5.3 External Provisioners

| Provisioner | Idempotency | Error Handling | Failure Recovery |
|-------------|-------------|----------------|-----------------|
| ArgoCD | ✓ (Get+Update with resourceVersion) | ✓ (wrapped errors) | Finalizer retry loop |
| Gitea | ✓ (delete-before-create tokens) | ✓ (status code checks) | Controller retry |
| MinIO (madmin-go v3) | ✓ (user-exists check) | ✓ (custom isNotFoundError) | Controller retry |

### 5.4 Multi-Cluster Setup

- **3 kubeconfigs:** mgmt (in-cluster), build (`BUILD_KUBECONFIG`), runtime (`RUNTIME_KUBECONFIG`)
- **Client retry:** 3 attempts with exponential backoff (2s, 4s, 8s) on connection
- **Leader election:** Enabled (`loopzero-operators.loopzero.io` lease)
- **Dependency injection:** All provisioners and clients injected — no global state

### 5.5 Findings

| ID | Severity | Finding | Impact |
|----|----------|---------|--------|
| OPS-001 | MEDIUM | ApplicationReconciler missing state machine — uses `computePhase()` instead | Spec §5 violation |
| OPS-002 | MEDIUM | ExposedRoute has no recovery from "Failed" phase after consecutive failures exceeded | Requires manual intervention |
| OPS-003 | LOW | No concurrent reconciliation tests | Covered by leader election |
| OPS-004 | LOW | TenantController cleanup continues if build namespace deletion fails (lines 286-292) | Finalizer prevents premature removal |
| OPS-005 | LOW | MinIO: if AddUser succeeds but ensurePolicy fails, user left without policy | Next reconcile retries |

---

## 6. Database Layer

### 6.1 Schema Overview

- **25 migrations** (forward-only, no destructive operations)
- **8 RLS-enabled tables:** builds, releases, environment_targets, secrets, projects, tenant_members, audit_log, idempotency_keys
- **27 indexes** covering all critical query paths
- **CHECK constraints** on all enum columns (build status, release status, plan, role, domain status)

### 6.2 Security Controls

| Control | Implementation | Status |
|---------|---------------|--------|
| SQL Injection | Parameterized queries ($1, $2) everywhere | ✓ Zero risk |
| Row-Level Security | `tenant_id = current_setting('app.tenant_id')` | ✓ All tenant tables |
| Advisory Locks | `pg_advisory_xact_lock` (transaction-scoped, pool-safe) | ✓ Rule 7 compliant |
| Role Separation | loopzero_api (DML), loopzero_migrate (DDL), loopzero_readonly | ✓ Migration 000009 |
| Audit Trail | Application-level + DB triggers (DEFERRABLE INITIALLY DEFERRED) | ✓ Dual enforcement |
| Encryption in transit | `DATABASE_SSLMODE=require` (default) | ✓ Secure by default |
| Orphan Prevention | FK constraints (NO ACTION / RESTRICT) + partial unique indexes | ✓ |

### 6.3 Critical Security Fixes Applied

| Migration | Issue | Severity | Fix |
|-----------|-------|----------|-----|
| 000009 | Missing role separation | HIGH | Created loopzero_api, loopzero_migrate, loopzero_readonly roles |
| 000009 | No data isolation | HIGH | RLS on all tenant-scoped tables |
| 000012 | RLS bypass after transaction reset | HIGH | Handle empty string in policy |
| 000016 | Only 1 Kratos user possible | CRITICAL | Made github_id nullable with partial unique index |
| 000017 | TOCTOU signup race condition | CRITICAL | Tenant ID → UUID, slug column |
| 000025 | Subdomain takeover via CASCADE delete | CRITICAL | Changed to ON DELETE RESTRICT |

### 6.4 Connection Pool Configuration

| Setting | Purpose |
|---------|---------|
| MaxConns | Upper bound for concurrent connections |
| MinConns | Keep-warm connections |
| MaxConnIdleTime | Reclaim idle connections |
| MaxConnLifetime | Connection rotation |
| HealthCheckPeriod | Stale connection detection |
| ConnectTimeout | Fail-fast on unreachable DB |
| StatementTimeout | Prevent runaway queries |

### 6.5 Race Condition Prevention (Multi-Layer)

1. **Advisory locks:** Serialize mutations on (tenant, project, env)
2. **Partial unique index:** `idx_one_active_build_per_env` — at most one active build per environment
3. **Atomic upserts:** `ON CONFLICT DO UPDATE` with `xmax = 0` detection
4. **DISTINCT ON:** Returns one ACTIVE release per environment

### 6.6 Findings

| ID | Severity | Finding | Impact |
|----|----------|---------|--------|
| DB-001 | MEDIUM | Idempotency key cleanup not automated — `Cleanup()` exists but not called | Expired keys accumulate (low impact, 24h data) |
| DB-002 | LOW | RLS policies check `app.tenant_id IS NULL OR = ''` — intentional complexity | Documented in migration 000012 |
| DB-003 | LOW | Custom domain CNAME checks weekly — up to 7-day drift detection delay | Consider daily for critical domains |

---

## 7. Security & RBAC

### 7.1 Auth Architecture

```
CLI (Device Code Flow) → Ory Hydra (JWT RSA) → API validates via pkg/jwt
Dashboard (Browser) → Ory Kratos (session) → BFF token exchange → HMAC session token
Service-to-Service → X-Service-Auth header (dual-key rotation)
```

### 7.2 Auth Middleware Security

| Feature | Implementation | Status |
|---------|---------------|--------|
| Token cache key protection (SEC-001) | SHA256 hash (not raw token as map key) | ✓ |
| Cache bounds | Max 10,000 entries, 5-min TTL, stale cleanup | ✓ |
| Dual-key rotation (S1.2) | Current + previous secret for zero-downtime | ✓ |
| Role resolution | Always from DB, never trusted from headers | ✓ |
| Rate limiting | Fixed-window per tenant, HTTP 429 + Retry-After | ✓ |
| SSRF prevention (SEC-004) | URL validation pattern on all CRD URL fields | ✓ |

### 7.3 RBAC

| Component | Permissions | Risk |
|-----------|-------------|------|
| PlatformBootstrap controller | Secrets: create/update (no delete), ConfigMaps: read-only | LOW |
| Tenant controller | CRDs, namespaces, RBAC, secrets, ArgoCD apps | LOW (namespace-scoped) |
| Build workflow SA | workflowtaskresults: create/patch, pods: get/watch | LOW |
| ArgoCD loopzero-api account | applications: sync/get/action (no create/delete) | LOW |

### 7.4 Network Policies

- **Default-deny ingress** on all pods in loopzero-system
- **Per-component egress:** DNS, PostgreSQL, Gitea, MinIO, ArgoCD, external HTTPS
- **Build pod egress:** DNS, MinIO (9000), registry (5000), HTTP/S (80, 443)
- **PostgreSQL fix (REVIEW-2026-03-30):** Restricted from "allow all" to specific ports

### 7.5 CI/CD Security

| Control | Implementation |
|---------|---------------|
| Permission scoping | `contents: read` (no write token exposure) |
| Dependency scanning | govulncheck + npm audit + Trivy + TruffleHog |
| Manifest validation | kubeconform --strict |
| Environment protection | infra-prod requires reviewers + main branch only |
| Image tags | SHA-based + environment tags (no `:latest` in workloads) |
| Multi-stage builds | Minimal alpine:3.21 base, non-root (UID 1000) |

### 7.6 Findings

| ID | Severity | Finding | Impact |
|----|----------|---------|--------|
| SEC-001 | HIGH | MinIO credentials stored in plaintext in Git (dev values) | Dev-only, needs ESO for prod |
| SEC-002 | MEDIUM | No image signing/verification (Cosign/SLSA not implemented) | Supply chain gap (Sprint 6) |
| SEC-003 | MEDIUM | Registry token expiry 3600s in build-images.yml | Should be 300-600s |
| SEC-004 | MEDIUM | Database role separation optional in production | Should enforce loopzeroApiDbPassword |
| SEC-005 | LOW | Alpine base image version not consistently pinned (3.21 vs 3.21.3) | Minor consistency |
| SEC-006 | LOW | readOnlyRootFilesystem not set on ArgoCD, Gitea, PostgreSQL containers | Defense-in-depth gap |
| SEC-007 | LOW | Ory services run in dev mode with HTTP | Pulumi overrides for prod |
| SEC-008 | LOW | No egress rate limiting per pod | Data exfiltration risk (loopzeroretical) |
| SEC-009 | LOW | Traefik allowCrossNamespace: true for CRDs | Tenant isolation via NetworkPolicy |

---

## 8. Consolidated Findings

### 8.1 By Severity

#### CRITICAL (1)

| ID | Domain | Finding | Remediation |
|----|--------|---------|-------------|
| HELM-001 | Helm/ArgoCD | Webhook callback secret hardcoded: `Bearer cb-loopzero-dev-2026` in values.yaml | Inject from K8s Secret. MEET-2026-04-04-029 P0 blocker. |

#### HIGH (5)

| ID | Domain | Finding | Remediation |
|----|--------|---------|-------------|
| HELM-002 | Helm/MinIO | Hardcoded MinIO credentials in lifecycle job | Move to Secret reference for prod |
| HELM-003 | Helm/Gitea | Admin + DB password hardcoded in values.yaml | Pulumi override for prod (documented) |
| HELM-004 | Helm/PostgreSQL | DB password hardcoded in values.yaml | Pulumi override for prod (documented) |
| HELM-005 | Helm/Ory | Cookie + cipher secrets hardcoded | Pulumi override for prod (documented) |
| SEC-001 | Security | MinIO credentials in plaintext in Git | ESO or sealed-secrets for prod |

#### MEDIUM (14)

| ID | Domain | Finding |
|----|--------|---------|
| INFRA-001 | Infrastructure | Staging DB: 1 node, no HA |
| HELM-006 | Helm | MinIO insecure:true in Argo config |
| HELM-007 | Helm | ArgoCD server.insecure:true |
| HELM-008 | Helm | Gitea SSL_MODE: disable |
| HELM-009 | Helm | Admission policies in Audit mode |
| FLOW-001 | Deploy Flow | ARGO_CALLBACK_SECRET mismatch between API and ArgoCD |
| FLOW-002 | Deploy Flow | SSE stream no explicit keepalive |
| PIPE-001 | Pipeline | No ingress NetworkPolicy for build pods |
| OPS-001 | Operators | ApplicationReconciler missing state machine |
| OPS-002 | Operators | ExposedRoute no recovery from Failed phase |
| DB-001 | Database | Idempotency key cleanup not automated |
| SEC-002 | Security | No image signing (Cosign/SLSA) |
| SEC-003 | Security | Registry token expiry too long (3600s) |
| SEC-004 | Security | DB role separation optional in prod |

#### LOW (21)

Documented in individual sections above. All are minor defense-in-depth improvements or dev-only concerns with documented production paths.

### 8.2 By Domain

```
Infrastructure:  0 Critical, 0 High, 1 Medium, 2 Low  = 3 total
Helm Charts:     1 Critical, 4 High, 4 Medium, 4 Low  = 13 total
Deploy Flow:     0 Critical, 0 High, 2 Medium, 3 Low  = 5 total
Build Pipeline:  0 Critical, 0 High, 1 Medium, 2 Low  = 3 total
Operators:       0 Critical, 0 High, 2 Medium, 3 Low  = 5 total
Database:        0 Critical, 0 High, 1 Medium, 2 Low  = 3 total
Security:        0 Critical, 1 High, 3 Medium, 5 Low  = 9 total
─────────────────────────────────────────────────────────────────
TOTAL:           1 Critical, 5 High, 14 Medium, 21 Low = 41 total
```

---

## 9. Remediation Plan

### P0 — Before Production (Blocking)

| # | Finding | Owner | Effort |
|---|---------|-------|--------|
| 1 | **HELM-001:** ArgoCD webhook secret → K8s Secret injection | infra | 1h |
| 2 | **FLOW-001:** Fix ARGO_CALLBACK_SECRET mismatch (API ↔ ArgoCD) | infra | 30m |
| 3 | **HELM-002/003/004/005:** Establish production values overlay pattern — all credentials via Pulumi config or ESO | infra | 4h |
| 4 | **SEC-001:** MinIO credentials → External Secrets Operator for prod | infra | 2h |

### P1 — Before GA (High Priority)

| # | Finding | Owner | Effort |
|---|---------|-------|--------|
| 5 | **SEC-004:** Enforce `loopzeroApiDbPassword` as required in production stack | infra | 1h |
| 6 | **SEC-003:** Reduce registry token expiry to 300-600s | ci/cd | 30m |
| 7 | **HELM-006/007/008:** Create `values.production.yaml` per chart with TLS/SSL enabled | infra | 4h |
| 8 | **OPS-001:** Add state machine to ApplicationReconciler per spec §5 | operator | 2h |
| 9 | **DB-001:** Add background job for idempotency key cleanup | api | 1h |
| 10 | **PIPE-001:** Add ingress NetworkPolicy for build pods (default-deny) | infra | 1h |

### P2 — Before Enterprise (Medium Priority)

| # | Finding | Owner | Effort |
|---|---------|-------|--------|
| 11 | **SEC-002:** Implement Cosign image signing in build pipeline (Sprint 6) | pipeline | 8h |
| 12 | **HELM-009:** Switch admission policies from Audit to Deny | infra | 2h |
| 13 | **OPS-002:** Add recovery annotation for ExposedRoute Failed phase | operator | 2h |
| 14 | **FLOW-002:** Add SSE keepalive/ping for long-running deploys | api | 2h |
| 15 | **INFRA-001:** Staging DB HA (2+ nodes) for pre-prod parity | infra | 1h config |

### P3 — Future Improvements (Low Priority)

| # | Finding | Owner | Effort |
|---|---------|-------|--------|
| 16 | `readOnlyRootFilesystem` on all containers | infra | 4h |
| 17 | pgBackRest / CloudNativePG for PITR | infra | 8h |
| 18 | SLSA provenance attestation + syft SBOM | pipeline | 8h |
| 19 | Egress rate limiting per pod | infra | 4h |
| 20 | Daily CNAME checks for custom domains | api | 1h |

---

## Appendix A: Strong Points (Compliance Positives)

These are areas where the platform **exceeds typical compliance requirements:**

1. **Multi-tenant isolation (4 layers):** RLS + advisory locks + namespace isolation + NetworkPolicy
2. **Zero SQL injection vectors:** 100% parameterized queries across 25 migrations and 9 stores
3. **Forward-only migrations:** No DROP TABLE/COLUMN in 25 migrations
4. **Idempotent reconciliation:** All 7 controllers use CreateOrUpdate pattern
5. **State machine enforcement:** Build, Release, Tenant, Bootstrap state machines prevent invalid transitions
6. **Defense-in-depth auth:** Token hashing for cache keys, dual-key rotation, role from DB only
7. **Rootless builds:** BuildKit rootless single-container, all build containers uid 1000
8. **Deterministic IaC:** All Helm chart versions pinned, Pulumi state encrypted
9. **Comprehensive audit trail:** Application-level + DB triggers, append-only enforcement
10. **Circuit breakers:** Argo API circuit breaker prevents cascading failures
11. **OOM-aware retries:** Build steps skip retry on exit code 137
12. **Zero public IPs:** Cloudflare Tunnel for all runtime traffic (DDoS/WAF)
13. **SSRF prevention:** SEC-004 URL validation on all CRD URL fields
14. **Dual audit enforcement:** Application-level writes + DB CONSTRAINT TRIGGERs

## Appendix B: Architecture Decisions Referenced

| ID | Decision | Status |
|----|----------|--------|
| MEET-2026-03-26-007 | BuildKit single-container (no sidecar) | Implemented |
| MEET-2026-03-26-008 | RELEASING state for phase 2 retry | Implemented |
| MEET-2026-03-29-017 | Universal Dockerfile via loopzero-packs | Implemented |
| MEET-2026-03-30-020 | Distributed tracing via Tempo | Implemented |
| MEET-2026-03-30-021 | VAP over Kyverno (native, no webhooks) | Implemented |
| MEET-2026-03-31-024 | Database role separation | Implemented (optional) |
| MEET-2026-04-04-029 | ArgoCD webhook secret from K8s Secret | **P0 BLOCKER** |
| MEET-2026-04-05-033 | Custom domains in manifests | Implemented |
| C-01 | Remove HTTP callbacks, use polling | Implemented |
| C-02 | Manifest generation in control plane | Implemented |
| C-03 | Build cluster is write-only | Implemented |

## Appendix C: File Index

Key files referenced in this audit:

```
Infrastructure:
  infra/pulumi/src/index.ts                           — 3-phase orchestrator
  infra/pulumi/src/config/stack.ts                    — Config + secret loading
  infra/pulumi/Pulumi.{dev,staging,prod}.yaml         — Stack configs
  infra/pulumi/src/providers/cloudflare/tunnel.ts     — CF Tunnel
  infra/scripts/pre-destroy-cleanup.sh                — Finalizer removal

Helm Charts:
  infra/helm/argocd/values.yaml                       — ArgoCD config (CRITICAL: line 156)
  infra/helm/network-policies/loopzero-system.yaml        — NetworkPolicy rules
  infra/helm/admission-policies/manifests/            — VAP policies
  infra/helm/traefik/middlewares.yaml                 — Security headers + rate limit

Deploy Flow:
  cli/src/commands/deploy.ts                          — CLI deploy command
  api/internal/routes/builds.go                       — Build API routes
  api/internal/services/build_orchestrator.go         — Build submission + locking
  api/internal/services/build_reconcile_loop.go       — Polling reconciler
  api/internal/services/container_delivery.go         — 5-phase release activation
  api/internal/lock/environment_lock.go               — Advisory lock wrapper

Build Pipeline:
  api/internal/argo/workflow_builder.go               — Workflow spec generation
  api/internal/argo/buildkit.go                       — BuildKit single-container
  api/internal/argo/languages.go                      — Language strategies + retries
  infra/helm/argo-workflows/templates/cwt-*.yaml      — ClusterWorkflowTemplates

Operators:
  operators/internal/controller/tenant_controller.go  — Tenant lifecycle
  operators/internal/controller/environment_controller.go — Namespace provisioning
  operators/internal/controller/application_controller.go — App routing (missing state machine)
  operators/api/v1/                                    — CRD definitions

Database:
  api/internal/db/migrations/                          — 25 forward-only migrations
  api/internal/db/                                     — 9 stores (all parameterized)
  api/internal/db/audit.go                             — Dual audit enforcement

Security:
  api/internal/middleware/auth.go                      — JWT + service auth
  api/internal/middleware/jwt_validator.go             — Token validation
  .github/workflows/ci.yml                             — Security scanning pipeline
```
