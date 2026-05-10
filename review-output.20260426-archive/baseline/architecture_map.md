# Operators Architecture Map — Phase 1 Baseline

## Overview

The `operators/` module is a Kubernetes operator built with Go 1.25, kubebuilder, and controller-runtime. It manages the lifecycle of 8 Kubernetes controllers across a **three-cluster topology**: mgmt, build, and runtime.

**Module:** `github.com/loopzero/loopzero/operators`
**Shared pkg:** `github.com/loopzero/loopzero/pkg` (replace directive to `../pkg`)

---

## Three-Cluster Topology

```
┌──────────────────────────────────────────────────────────────────┐
│  MGMT CLUSTER (runs the operator binary)                          │
│  ┌─────────────────────────────────────────────────────────┐     │
│  │  operator binary (cmd/main.go, leader election enabled) │     │
│  │                                                          │     │
│  │  Controllers (7 active, 1 unwired):                     │     │
│  │   TenantController          (r.Client = mgmt)           │     │
│  │   EnvironmentController     (r.Client = mgmt)           │     │
│  │   ApplicationController     (r.Client = mgmt)           │     │
│  │   ExposureController        (r.Client = mgmt)           │     │
│  │   PlatformBootstrapCtrl     (r.Client = mgmt)           │     │
│  │   ClusterCredentialCtrl     (r.Client = mgmt)           │     │
│  │   GitRepositoryBindingCtrl  (r.Client = mgmt)           │     │
│  │   PreviewController         (DEFINED but NOT WIRED)     │     │
│  └─────────────────────────────────────────────────────────┘     │
│                                                                   │
│  CRD Storage (10 types):                                         │
│   Tenant, Environment, LoopZeroApplication, ExposedRoute             │
│   PlatformBootstrap, ClusterCredential, GitRepositoryBinding     │
│   PreviewEnvironment (unwired), AppSecret (no ctrl), ReconcReport│
└──────────────────────────────────────────────────────────────────┘
        │                        │                      │
        │ r.BuildClient           │ r.RuntimeClient       │ dynamic.Client
        │ (BUILD_KUBECONFIG)      │ (RUNTIME_KUBECONFIG)  │ (mgr.GetConfig)
        ↓                        ↓                      ↓
┌──────────────┐  ┌──────────────────────────┐  ┌─────────────────┐
│ BUILD CLUSTER │  │   RUNTIME CLUSTER        │  │   Gitea / ArgoCD│
│              │  │                          │  │   (external svc)│
│ build-{tenant}│  │ {tenant}-{env} namespaces│  │                 │
│  namespaces  │  │ Namespaces + RBAC        │  │   Gitea repos   │
│  ResourceQuota│  │ NetworkPolicies          │  │   AppProject    │
│  RBAC        │  │ ResourceQuotas           │  │   Application   │
│  Secrets     │  │ PriorityClass            │  │                 │
│  NetworkPolicy│  │ LoopZeroApplication CRs      │  │                 │
│  LimitRange  │  │ ExposedRoute CRs         │  │                 │
│  ServiceAcct │  │ Preview namespaces        │  │                 │
└──────────────┘  └──────────────────────────┘  └─────────────────┘
```

---

## Controller-to-Provisioner-to-External-System Map

```
TenantController
  ├── gitea.GitRepoProvisioner ──────────────────────────→ Gitea HTTP API
  │     (CircuitBreakerProvisioner wraps RealProvisioner)
  ├── argocd.Provisioner ────────────────────────────────→ mgmt cluster (dynamic.Client)
  │     (CircuitBreakerProvisioner wraps RealProvisioner)   ArgoCD AppProject/Application CRs
  ├── minio.CredentialProvisioner ────────────────────────→ MinIO HTTP API (madmin-go)
  │     (CircuitBreakerProvisioner wraps AdminProvisioner)   [optional: nil = global creds mode]
  ├── harbor.Provisioner ─────────────────────────────────→ Harbor HTTP API
  │     (CircuitBreakerClient wraps Client)                  [optional: nil = DOCR mode]
  ├── r.BuildClient ──────────────────────────────────────→ BUILD cluster
  └── r.RuntimeClient ────────────────────────────────────→ RUNTIME cluster (PriorityClass)

EnvironmentController
  └── r.RuntimeClient ────────────────────────────────────→ RUNTIME cluster
                                                             Namespace, RBAC, NetworkPolicy, Quota

ApplicationController
  └── r.Client (mgmt) ────────────────────────────────────→ MGMT cluster
                                                             ExposedRoute CRs (child, owned)

ExposureController
  ├── r.Client (mgmt) ────────────────────────────────────→ MGMT cluster (cert Secret read)
  └── httpclient.New ─────────────────────────────────────→ External HTTPS probe (application URL)

PlatformBootstrapController
  ├── gitea.BootstrapClient ──────────────────────────────→ Gitea HTTP API
  ├── ArgoCDBootstrapClient ──────────────────────────────→ ArgoCD HTTP API (/api/version, apiKey)
  └── r.Client (mgmt) ────────────────────────────────────→ MGMT cluster (platform-tokens Secret)

ClusterCredentialController
  ├── r.Client (mgmt) ────────────────────────────────────→ MGMT cluster (kubeconfig Secret read)
  ├── kubernetes.NewForConfig(restConfig) ───────────────→ target cluster /healthz check
  └── ArgoCD cluster Secret ───────────────────────────────→ MGMT cluster argocd namespace

GitRepositoryBindingController
  ├── r.Client (mgmt) ────────────────────────────────────→ MGMT cluster (credential Secret + ArgoCD Secret)
  └── httpclient ─────────────────────────────────────────→ Gitea repo API (reachability)

PreviewController [NOT WIRED IN MAIN]
  └── r.RuntimeClient ────────────────────────────────────→ RUNTIME cluster (preview namespace)
```

---

## Circuit Breaker Configuration (All Provisioners)

| Provisioner | CB Name | OpenThreshold | CloseThreshold | RetryInterval |
|-------------|---------|---------------|----------------|---------------|
| Gitea | "gitea" | 5 | 1 | 120s |
| ArgoCD | "argocd" | 5 | 1 | 120s |
| MinIO | "minio" | 5 | 1 | 120s |
| Harbor | "harbor" | 5 | 1 | 120s |

Circuit breaker state changes emit to Prometheus: `TenantCircuitBreakerState.WithLabelValues(name).Set(float64(to))`

---

## State Machine Usage

| Controller | State Machine | Usage |
|------------|--------------|-------|
| TenantController | `tenantSM = statemachine.NewTenantMachine()` | All transitions (Provisioning->Active, any->Deleting) |
| EnvironmentController | `environmentSM = statemachine.NewEnvironmentMachine()` | Empty->Pending->Active->Deleting |
| PlatformBootstrapController | `platformBootstrapSM = statemachine.NewPlatformBootstrapMachine()` | All phase transitions |
| PreviewController | `var _ = statemachine.NewPreviewMachine` | **BLANK IMPORT — UNUSED** |

The PreviewController uses direct phase assignments (`preview.Status.Phase = loopzerov1.PreviewPhaseCreating`) bypassing the state machine entirely.

---

## CRDs and Controller Wiring Summary

| CRD | Controller | Wired in main.go | State Machine | Notes |
|-----|-----------|------------------|---------------|-------|
| Tenant | TenantController | YES | tenantSM | Most complex |
| Environment | EnvironmentController | YES | environmentSM | Created by TenantController |
| LoopZeroApplication | ApplicationController | YES | None (phase computed) | Owns ExposedRoute |
| ExposedRoute | ExposureController | YES | None (phase set directly) | Child of LoopZeroApplication |
| PlatformBootstrap | PlatformBootstrapController | YES | platformBootstrapSM | One-shot setup |
| ClusterCredential | ClusterCredentialController | YES | None | Watches Secrets |
| GitRepositoryBinding | GitRepositoryBindingController | YES | None | Periodic requeue |
| PreviewEnvironment | PreviewController | **NO** | None (SM blank-imported) | Dead code |
| AppSecret | (none) | (none) | (none) | CRD only |
| ReconciliationReport | (none) | (none) | (none) | CRD placeholder |

---

## Dependency Direction (Clean DIP)

```
cmd/main.go
  └── injects concrete provisioners into controllers via interfaces
      interfaces defined in: gitea/provisioner.go, argocd/provisioner.go,
                              minio/provisioner.go, harbor/provisioner.go
      concrete: *RealProvisioner, *AdminProvisioner, *Client
      circuit-breaker: *CircuitBreakerProvisioner, *CircuitBreakerClient
      all implement the interface — controllers see only the interface
```

---

## Anomaly: Compiled Binary in Repository

`/home/paulo/loopzero/operators/tenant-controller` is an ELF 64-bit executable committed to the repository root. This is a compiled artifact and should not be tracked in version control. Risk: stale binary may be used in CI or confuse developers about the canonical build artifact.
