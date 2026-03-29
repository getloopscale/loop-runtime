# Deep Review Report — Cluster Management, Integrations, Tools & Control Plane

**Target:** /home/paulo/loopzero
**Scope:** Cluster management, integrations, tools, and control plane
**Mode:** Architecture (phases 1, 2, 3, 8)
**Date:** 2026-03-29
**Methodology:** Autonomous deep review with specialist agents (chief-reviewer, completeness-auditor, architecture-analyst, infrastructure-reviewer)

---

## Executive Summary

This review audited LoopZero's cluster management layer — the 3-cluster Kubernetes architecture (mgmt, build, workload), 7 operator controllers, 3 provisioner integrations (Gitea, ArgoCD, MinIO), 13 Helm charts, Pulumi IaC, and Taskfile automation.

The control plane architecture is **well-designed and substantially complete**. All 7 wired controllers have real implementations with proper idempotent reconciliation, typed conditions, and state machine enforcement. The multi-cluster client pattern is correctly applied with clean dependency injection via provisioner interfaces.

However, the review identified **2 critical, 10 high, and 26 medium/low findings** (38 total) that require attention before production hardening:

1. **CRITICAL:** PlatformBootstrapController cannot authenticate to the bootstrap API (SEC-003 auth token missing)
2. **CRITICAL:** `bitnami/kubectl:latest` image tag in Gitea bootstrap Job (floating tag)
3. **HIGH:** Build namespace NetworkPolicy allows unrestricted egress on port 443 (exfiltration risk)
4. **HIGH:** Multi-cluster failure modes untested (envtest uses single API server)
5. **HIGH:** Operator Tilt deployment missing required environment variables

**Overall Health Score: 68/100 — Fair**

| Dimension | Score | Assessment |
|-----------|-------|------------|
| Correto | 75 | Controllers implement documented behavior correctly. State machines enforce valid transitions. CRD design is sound. |
| Completo | 70 | 7/10 CRDs wired. 3 unwired (AppSecret, Preview, ReconciliationReport). Bootstrap auth gap. Dead EndpointsReady field. |
| Confiavel | 55 | Build egress policy ineffective. MinIO credential recovery gap. Multi-cluster failures untested. No orphan GC. |
| Controlavel | 65 | Operator deployment incomplete. Taskfile Helm installs lack --version. No PG backup. Missing network policies on build/workload. |

---

## 1. System Overview

### 1.1 Architecture

```
┌──────────────────────────────────────────────────────────────────┐
│  CONTROL PLANE (mgmt cluster)                                     │
│  7 Controllers: Tenant → Environment → Application → Exposure     │
│  Independent: PlatformBootstrap, GitRepositoryBinding, ClusterCred│
│  Services: PostgreSQL 16, Gitea 1.22.6, MinIO, ArgoCD 2.13.3     │
│  Monitoring: Prometheus, Grafana, Loki, OTel, ESO 0.14.3         │
├──────────────────────────────────────────────────────────────────┤
│  BUILD PLANE (build cluster)                                      │
│  Argo Workflows + BuildKit (rootless). build-{tenant} namespaces  │
│  Provisioned by: TenantController via BUILD_KUBECONFIG            │
├──────────────────────────────────────────────────────────────────┤
│  WORKLOAD PLANE (workload cluster)                                │
│  User apps via ArgoCD. {tenant}-{env} namespaces                  │
│  Provisioned by: EnvironmentController via RUNTIME_KUBECONFIG     │
│  Traefik + cert-manager + Cloudflare Tunnel                       │
└──────────────────────────────────────────────────────────────────┘
```

### 1.2 Components: 18 registered (3 clusters, 7 controllers, 3 provisioners, 5 infrastructure)

### 1.3 Critical Flows: 6 mapped
- Tenant Provisioning (critical) — 10-step build namespace + Gitea + ArgoCD
- Platform Bootstrap (critical) — Gitea → ArgoCD → API seed
- Cluster Credential Registration (critical) — connectivity validation + ArgoCD Secret
- Build Workflow Orchestration (critical) — Argo Workflows + BuildKit
- Git Repository Credential Binding (critical) — SHA256 rotation detection
- Environment Namespace Setup (high) — namespace + RBAC + NetworkPolicy + Quota

---

## 2. Findings Summary

### By Severity

| Severity | Count | Key Examples |
|----------|-------|-------------|
| **Critical** | 2 | Bootstrap auth gap (comp_ctrl_001), kubectl:latest tag (infra_c_002) |
| **High** | 10 | Build egress port 443 (RISK-007), multi-cluster untested (RISK-006), missing Helm versions (infra_c_001) |
| **Medium** | 15 | God controller (arch_002), dead EndpointsReady field (comp_ctrl_003), dead CRDs (comp_ctrl_004) |
| **Low** | 11 | MinIO cleanup missing (comp_ctrl_007), InsecureSkipVerify (RISK-001) |
| **Total** | **38** | |

### By Category

| Category | Count |
|----------|-------|
| Completeness | 21 |
| Architecture | 10 |
| Security | 3 |
| Operational | 3 |
| Testing | 1 |

### By Status

| Status | Count |
|--------|-------|
| Open | 31 |
| Confirmed (with evidence) | 6 |
| False Positive | 1 |

---

## 3. Critical & High Findings (Detail)

### CRITICAL-1: comp_ctrl_001 — PlatformBootstrapController Missing Bootstrap Auth Token

| Field | Value |
|-------|-------|
| **File** | `operators/internal/controller/platformbootstrap_api.go:64-69` |
| **Component** | PlatformBootstrapController |
| **Evidence** | `callBootstrapEndpoint` creates POST with no Authorization header. Target endpoint requires `Bearer <LOOPZERO_BOOTSTRAP_SECRET>` (SEC-003). |
| **Impact** | Platform bootstrap WaitingForAPI phase fails with 401. State machine → Failed. Entire platform initialization broken. |
| **Root Cause** | SEC-003 added to API but PlatformBootstrapController not updated. |
| **Fix** | Add bootstrap secret field to PlatformBootstrap CRD spec or read from K8s Secret. Pass as Bearer token in HTTP header. |
| **Effort** | Medium |

### CRITICAL-2: infra_c_002 — bitnami/kubectl:latest Image Tag

| Field | Value |
|-------|-------|
| **File** | Gitea bootstrap Job |
| **Impact** | Non-deterministic builds. Image may change between deployments. |
| **Fix** | Pin to specific digest or version tag. |
| **Effort** | Low |

### HIGH-1: RISK-007 — Build Namespace Egress Allows Unrestricted Port 443

| Field | Value |
|-------|-------|
| **File** | `operators/internal/controller/tenant_build.go:362-381` |
| **Evidence** | NetworkPolicySpec has Egress rules with Ports but NO `To` field. Port 443 allows egress to any IP. |
| **Impact** | Malicious Dockerfile can exfiltrate source code, secrets to any HTTPS endpoint. Violates spec §17.1. |
| **Fix** | Add IPBlock restriction to port 443 rule scoped to container registry CIDR only. |
| **Effort** | Medium |

### HIGH-2: RISK-006 — Multi-Cluster Failure Modes Untested

| Field | Value |
|-------|-------|
| **File** | `operators/internal/controller/suite_test.go` |
| **Evidence** | envtest uses single API server. Grep for `BuildClient.*error` in test files returns 0 matches. |
| **Impact** | Controller behavior under cluster partitions is unverified. |
| **Fix** | Add mock-based tests for build/runtime client unavailability. |
| **Effort** | Medium |

### HIGH-3: RISK-003 — MinIO Credential Recovery Gap

| Field | Value |
|-------|-------|
| **File** | `operators/internal/controller/tenant_build.go:312-315` |
| **Evidence** | When secretKey=="" and Secret missing, returns `"manual credential rotation required"`. Infinite reconcile retries with no self-healing. |
| **Impact** | Tenant builds fail permanently if K8s Secret accidentally deleted while MinIO user exists. |
| **Fix** | Rotate credentials (generate new secretKey) when Secret missing but user exists. |
| **Effort** | Medium |

### HIGH-4: arch_001 — Operator Tilt Deployment Missing Required Env Vars

| Field | Value |
|-------|-------|
| **File** | `infra/tilt/operator-deployment.yaml:149-164` |
| **Evidence** | Missing BUILD_KUBECONFIG, RUNTIME_KUBECONFIG, BASE_DOMAIN. main.go:101 exits fatally. |
| **Fix** | Add ConfigMap volume mounts and env vars matching API deployment pattern. |
| **Effort** | Low |

### HIGH-5 through HIGH-10 (Summary)

| ID | Title | Category |
|----|-------|----------|
| comp_ctrl_002 | AppSecret CRD has no controller — secret sync unimplemented | Completeness |
| infra_c_001 | Taskfile Helm installs missing --version flag | Completeness |
| infra_c_003 | k3d clusters have no pinned Kubernetes version | Completeness |
| infra_c_004 | Network policies only on mgmt — build/workload unprotected | Completeness |
| infra_c_005 | Gitea backup CronJob does not exist | Completeness |
| infra_c_006 | Production PG backup not applied by Pulumi | Completeness |

---

## 4. Architecture Assessment

### Strengths
1. **Clean provisioner DI** — Gitea, ArgoCD, MinIO behind interfaces with test doubles
2. **State machine enforcement** — Panic-on-invalid-transition prevents silent state corruption
3. **Typed conditions** — No freeform strings; all constants centralized
4. **Multi-file controller decomposition** — TenantController split into 5 cohesive files
5. **Tiltfile/Taskfile ownership** — Clean boundary prevents accidental data loss
6. **Pulumi 3-phase model** — ISP contracts enable multi-cloud abstraction
7. **Security posture** — runAsNonRoot, readOnlyRootFilesystem, resource limits

### Weaknesses
1. **TenantController blast radius** — Single controller manages everything for a tenant
2. **CRD validation gaps** — Only Tenant has comprehensive kubebuilder markers
3. **Dev/prod parity gap** — Operator Tilt deployment is incomplete
4. **Bootstrap.ts god file** — 2526 lines with 22 installation responsibilities

---

## 5. Remediation Plan

### P1 — Critical (fix immediately)
| Finding | Action | Effort |
|---------|--------|--------|
| comp_ctrl_001 | Add bootstrap secret to PlatformBootstrap CRD + controller | Medium |
| infra_c_002 | Pin bitnami/kubectl to specific version digest | Low |

### P2 — High (fix before next hardening sprint)
| Finding | Action | Effort |
|---------|--------|--------|
| RISK-007 | Add IPBlock to build NetworkPolicy port 443 rule | Medium |
| arch_001 | Add BUILD_KUBECONFIG, RUNTIME_KUBECONFIG, BASE_DOMAIN to operator deployment | Low |
| RISK-003 | Add credential rotation when Secret missing but user exists | Medium |
| RISK-006 | Add mock-based multi-cluster failure tests | Medium |
| infra_c_001 | Add --version to all Taskfile helm install commands | Low |
| infra_c_003 | Pin k3d Kubernetes version in cluster configs | Low |
| infra_c_004 | Deploy network policies to build and workload clusters | Medium |
| infra_c_005 | Implement Gitea backup CronJob | Medium |
| infra_c_006 | Apply PG backup values via Pulumi | Low |
| comp_ctrl_002 | Implement AppSecret controller or document as not needed (ESO covers) | Medium |

### P3 — Medium (address in upcoming sprints)
12 medium-severity findings — see full list in review.db

### P4 — Low (backlog)
7 low-severity findings — see full list in review.db

---

## 6. Review Metadata

| Field | Value |
|-------|-------|
| Total Iterations | 3 |
| Components Mapped | 18 |
| Flows Mapped | 6 |
| Findings Total | 38 |
| Evidence Items | 5 |
| Quality Scores | Phase 2: 0.85, Phase 3: 0.82 |
| Agents Used | chief-reviewer, completeness-auditor, architecture-analyst, infrastructure-reviewer |
| Review Mode | Architecture (phases 1, 2, 3, 8) |
| Database | review-output/review.db |
