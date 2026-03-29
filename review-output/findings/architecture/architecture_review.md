# Architecture Review

**Date:** 2026-03-29
**Target:** /home/paulo/loopzero (scoped: Cluster management, integrations, tools, and Control Plane)
**Reviewer:** architecture-analyst
**Scope:** operators/, infra/pulumi/src/

---

## Executive Summary

- **Overall architecture health:** GOOD
- **Total findings:** 10 (critical: 0, high: 1, medium: 3, low: 6)
- **Key concern:** `bootstrap.ts` is a 2,526-line god file with 22 top-level functions covering all three clusters and every platform service. All other findings are refinements to an architecturally sound foundation.

The Control Plane architecture is well-designed. The multi-cluster client pattern is clean and correctly enforced. The provisioner interface pattern (Gitea, ArgoCD, MinIO) is properly applied with compile-time checks. The constants package prevents the scattered-strings anti-pattern. State machines are used correctly. No circular dependencies exist. The main structural risk is in the Pulumi IaC layer, not the Go operator code.

---

## Codebase Structure

```
operators/
├── api/v1/          — 9 CRD types (Tenant, Environment, LoopZeroApplication, ExposedRoute,
│                       PlatformBootstrap, GitRepositoryBinding, ClusterCredential,
│                       AppSecret, PreviewEnvironment, ReconciliationReport)
├── cmd/main.go      — 250 lines: multi-cluster setup + provisioner wiring + controller registration
├── internal/
│   ├── argocd/      — Provisioner interface + RealProvisioner (dynamic K8s client)
│   ├── conditions/  — Typed condition constants for all controllers
│   ├── constants/   — Centralized string identifiers (labels, ports, timeouts, resources)
│   ├── controller/  — 7 controllers, file-split by responsibility
│   ├── gitea/       — GitRepoProvisioner + BootstrapClient interfaces + RealProvisioner
│   └── minio/       — CredentialProvisioner interface + AdminProvisioner

infra/pulumi/src/
├── index.ts         — 199 lines: 3-phase orchestrator
├── components/
│   ├── bootstrap.ts — 2526 lines: all cluster bootstrapping (GOD FILE)
│   ├── infra.ts     — 256 lines: Phase 1 infrastructure
│   └── expose.ts    — 113 lines: Phase 3 DNS
├── contracts/       — 6 ISP interfaces (IProviderNetwork, IProviderKubernetes, etc.)
└── providers/
    ├── digitalocean/ — fully implemented (8 files, ~350 lines total)
    ├── cloudflare/   — CF Tunnel + CF Worker
    └── aws/         — stub (all methods throw)
```

**Language breakdown (relevant scope):** Go (operators), TypeScript (Pulumi IaC)

---

## Layer Analysis

### Operators: Controller / Service / Repository

The operators follow the standard controller-runtime pattern correctly.

| Layer | File | Assessment |
|-------|------|-----------|
| Reconcile loop | `tenant_controller.go` | Orchestration only — no business logic inline |
| Build provisioning | `tenant_build.go` | Pure Kubernetes resource management, no external calls |
| GitOps provisioning | `tenant_gitops.go` | Delegates all external calls to provisioner interfaces |
| Credential management | `tenant_credentials.go` | Self-contained credential value object, HTTP call isolated |
| Priority scheduling | `tenant_priority.go` | Clean, single-purpose |

Controllers do not contain direct database queries. No HTTP calls are made inline in reconcile logic — all external calls go through injected interfaces. This is correct.

### Pulumi IaC: Phase Orchestrator / Component Installers

| Phase | File | Lines | Assessment |
|-------|------|-------|-----------|
| Phase 1: Infrastructure | `infra.ts` | 256 | Clean — delegates to factory |
| Phase 2: Bootstrap | `bootstrap.ts` | 2,526 | GOD FILE — see ARCH-001 |
| Phase 3: Expose | `expose.ts` | 113 | Clean |
| Orchestrator | `index.ts` | 199 | Clean — thin entry point |

---

## Dependency Analysis

### Import Graph (operators)

```
cmd/main.go
  → internal/controller   (SetupXxxController functions)
  → internal/argocd       (provisioner construction)
  → internal/gitea        (provisioner construction)
  → internal/minio        (provisioner construction)
  → internal/constants    (DefaultXxx values)

internal/controller (all files)
  → operators/api/v1      (CRD types — inward dependency: correct)
  → internal/argocd       (Provisioner interface — inward)
  → internal/gitea        (GitRepoProvisioner, BootstrapClient — inward)
  → internal/conditions   (typed constants — inward)
  → internal/constants    (string constants — inward)
  → pkg/naming            (shared naming — inward)
  → pkg/statemachine      (state machine — inward)

internal/argocd
  → internal/constants    (label keys)
  [creates its own k8s dynamic client — see ARCH-003]

internal/gitea
  → internal/constants    (HTTPTimeoutDefault)
```

**No circular dependencies detected.** The dependency direction is correct throughout: cmd → controller → provisioners → constants/shared. Inner layers do not import outer layers.

### Import Graph (Pulumi IaC)

```
index.ts
  → components/infra      (createInfra)
  → components/bootstrap  (bootstrapCluster)
  → components/expose     (exposePlatform)
  → providers/cloudflare  (createTunnel, createStaticDelivery)

components/infra.ts
  → contracts/provider    (CloudProviderFactory interface)
  → providers/digitalocean (DigitalOceanProviderFactory)
  → providers/aws         (AwsProviderFactory)
  → providers/cloudflare/dns
  [also: direct @pulumi/digitalocean import for ContainerRegistry — see ARCH-008]

components/bootstrap.ts
  → components/infra      (InfraOutputs input type)
```

**No circular dependencies.** Phase dependency direction is correct: Phase 3 imports Phase 2 types, Phase 2 imports Phase 1 types.

---

## God Classes / Files

| File | Lines | Assessment |
|------|-------|-----------|
| `infra/pulumi/src/components/bootstrap.ts` | 2,526 | GOD FILE — 22 functions, 3 clusters |
| `operators/internal/controller/tenant_controller_test.go` | 1,776 | Test file — acceptable |
| `operators/internal/controller/environment_controller.go` | 532 | Borderline — all one controller |
| `operators/internal/controller/tenant_build.go` | 385 | Acceptable — single concern |

The `environment_controller.go` at 532 lines is borderline but not a problem because all the code is within a single controller's reconcile logic. The `tenant_build.go` at 385 lines handles 10 sequential idempotent steps for build namespace provisioning — all cohesive.

`bootstrap.ts` at 2,526 lines is the only genuine god file. It installs: cert-manager, ArgoCD, Gitea, MinIO, Prometheus, Grafana, Traefik, Argo Workflows, Loki, Promtail, ESO, the LoopZero API, and the TenantController — across management, build, and runtime clusters.

---

## Pattern Usage

### Provisioner Interface Pattern (Gitea, ArgoCD, MinIO)

**Assessment: Correctly applied.**

- All three have interface definitions separate from implementations.
- Compile-time checks (`var _ Provisioner = (*RealProvisioner)(nil)`) enforce compliance.
- Fakes live in `fake_provisioners_test.go` (package-internal) and `operatortest/setup.go` (public for E2E).
- Injection happens in `cmd/main.go` — no controller creates its own provisioner.

One deviation: `argocd.NewRealProvisioner` creates its own Kubernetes client internally rather than accepting one via injection. See ARCH-003.

### Multi-Cluster Client Pattern

**Assessment: Correctly applied.**

Three explicit clients (`r.Client`, `r.BuildClient`, `r.RuntimeClient`) are passed as constructor parameters. There is no global state. Each reconcile function uses the correct client for its target cluster. Cross-cluster resource cleanup uses label-based listing + finalizer pattern (since ownerReferences cannot cross clusters), which is the correct approach.

### State Machine Pattern

**Assessment: Correctly applied.**

`statemachine.NewTenantMachine()` and `statemachine.NewPlatformBootstrapMachine()` are used for all phase transitions. Direct assignment to `tenant.Status.Phase` only occurs in one place for the empty state initialization, and it goes through `tenantSM.Transition()`. Invalid transitions panic rather than silently succeeding, which is the correct semantics for programming errors.

### Reconcile Step Pipeline Pattern

**Assessment: Good pattern, consistently applied.**

Both `TenantReconciler.runSteps()` and `reconcileBuildNamespace()` use an anonymous struct slice pattern:
```go
steps := []reconcileStep{
    {conditions.TypeXxx, func() error { return r.reconcileX(ctx, ...) }},
    ...
}
```
This makes it easy to add, remove, or reorder steps without touching the loop logic. The pattern is consistent across `tenant_controller.go` and `tenant_build.go`.

### Provider Factory Pattern (Pulumi)

**Assessment: Correctly applied with one gap.**

The `CloudProviderFactory` interface correctly segregates capabilities via ISP (IProviderNetwork, IProviderKubernetes, IProviderStorage, IProviderDns, IProviderCompute, IProviderDatabase). The DigitalOcean implementation is complete. The AWS stub correctly documents implementation steps.

Gap: `ContainerRegistry` and `Cdn` resources in `infra.ts` bypass the factory (see ARCH-008).

---

## Domain Model Assessment

The operator CRD types are well-designed and follow Kubernetes API conventions:

- `+kubebuilder:subresource:status` on all top-level types.
- `+kubebuilder:printcolumn` on all types for `kubectl get` usability.
- `ObservedGeneration` field on all Status types.
- `Conditions []metav1.Condition` on all Status types.
- Enum validation on phase fields.

No ORM annotations leak into the domain model. The types are pure Go structs with kubebuilder markers, which is the correct pattern.

**One naming design debt:** `EnvironmentSpec` and `EnvironmentCRSpec` coexist in the same package (see ARCH-004).

**One validation gap:** `GitOpsSpec` fields lack validation markers (see ARCH-009).

---

## Module Cohesion

| Module | Cohesion | Assessment |
|--------|----------|-----------|
| `internal/controller/` | HIGH | All files reconcile CRD resources |
| `internal/argocd/` | HIGH | All ArgoCD provisioning |
| `internal/gitea/` | HIGH | All Gitea provisioning |
| `internal/minio/` | HIGH | All MinIO provisioning |
| `internal/conditions/` | HIGH | All condition type constants |
| `internal/constants/` | HIGH | All string identifiers (5 well-separated files) |
| `operators/api/v1/` | HIGH | All CRD type definitions |
| `infra/pulumi/src/contracts/` | HIGH | All provider interfaces |
| `infra/pulumi/src/components/` | LOW | `bootstrap.ts` breaks cohesion (see ARCH-001) |

---

## Findings (Detailed)

### ARCH-001: bootstrap.ts is a 2526-line god file with 22 installation responsibilities

- **Severity:** high
- **Location:** `infra/pulumi/src/components/bootstrap.ts:1`
- **Description:** bootstrap.ts contains 22 top-level functions covering all three clusters and every platform service (cert-manager, ArgoCD, Gitea, MinIO, Traefik, Prometheus, Grafana, Loki, Promtail, Argo Workflows, ESO, LoopZero API, TenantController). Any engineer changing any component must open this single file. The file has multiple reasons to change: management cluster services, build cluster services, runtime cluster services, and Helm chart version bumps.
- **Evidence:** Functions at lines 84, 100, 201, 241, 353, 376, 409, 567, 635, 715, 1080, 1157, 1452, 1488, 1734, 2036, 2084, 2188, 2315, 2346, 2447 all in one file; `wc -l = 2526`
- **Recommendation:** Split into per-cluster files: `mgmt-bootstrap.ts`, `build-bootstrap.ts`, `runtime-bootstrap.ts`. Each cluster file owns its installer functions. The current `bootstrapCluster()` entry point at line 84 becomes a thin orchestrator that imports from the three files. This brings each file under 600 lines and creates a clear mapping between cluster and file.

---

### ARCH-002: TenantReconciler struct spans 5 distinct infrastructure domains

- **Severity:** medium
- **Location:** `operators/internal/controller/tenant_controller.go:43`
- **Description:** TenantReconciler has 8 fields spanning GitOps (GiteaURL, GitProvisioner), ArgoCD (ArgoCDProvisioner, WorkloadClusterURL), MinIO credential isolation (MinIOProvisioner), build cluster access (BuildClient, Credentials), and runtime cluster access (RuntimeClient). While the controller is split across 5 files, all files share the same struct, giving every method access to every field. The `tenant_build.go` methods use only BuildClient, MinIOProvisioner, and Credentials; `tenant_gitops.go` uses only GiteaURL, GitProvisioner, and ArgoCDProvisioner.
- **Evidence:** `tenant_controller.go:43-54` struct definition; `tenant_build.go` never references GiteaURL; `tenant_gitops.go` never references BuildClient
- **Recommendation:** The current file-split is directionally correct and the `BuildCredentials` extraction is good practice. The next step is documenting the expected field usage per file as a comment on the struct, making the implicit coupling explicit. A deeper refactor would extract a `buildNamespaceReconciler` sub-struct that accepts only `(buildClient, minIOProvisioner, credentials)` — but this should be deferred until test friction justifies it.

---

### ARCH-003: ArgoCD provisioner creates its own K8s client independently of the controller manager

- **Severity:** medium
- **Location:** `operators/internal/argocd/real_provisioner.go:42`
- **Description:** `NewRealProvisioner()` calls `config.GetConfig()` and `dynamic.NewForConfig()` internally to build its own dynamic Kubernetes client. The controller manager in `cmd/main.go` already has a `rest.Config` that was constructed with leader election, QPS/burst settings, and scheme registration. The ArgoCD provisioner bypasses this to create an independent, uncached connection to the same cluster.
- **Evidence:** `real_provisioner.go:43-56` creates dynamic client internally; `cmd/main.go:87` calls `argocd.NewRealProvisioner(argoCDNamespace)` without passing any client or config
- **Recommendation:** Change `NewRealProvisioner(namespace string, client dynamic.Interface)` to accept the dynamic client as a dependency. In `cmd/main.go`, create `dynamic.NewForConfig(mgr.GetConfig())` once and pass it. This is a one-line change at the call site and makes the provisioner testable without a real cluster config.

---

### ARCH-004: EnvironmentSpec naming collision: two types with overlapping names in api/v1

- **Severity:** medium
- **Location:** `operators/api/v1/tenant_types.go:72`
- **Description:** `api/v1` package contains `EnvironmentSpec` (a reference entry within a Tenant spec) and `EnvironmentCRSpec` (the full spec of a standalone Environment CR). The `EnvironmentCRSpec` name is a workaround, acknowledged by a comment. This indicates the domain model was extended incrementally without reconciling the original type name.
- **Evidence:** `tenant_types.go:72` defines `EnvironmentSpec`; `environment_types.go:41` defines `EnvironmentCRSpec` with comment "Named EnvironmentCRSpec to avoid collision with the existing EnvironmentSpec in tenant_types.go"
- **Recommendation:** Rename `EnvironmentSpec` in `tenant_types.go` to `TenantEnvironmentEntry` or `EnvironmentRef`. Run `make generate` and `make manifests` after the change. Update all controller usages (they all use `tenant.Spec.Environments[i]`, so the change is mechanical).

---

### ARCH-005: InsecureSkipVerify is hardcoded in 4 separate locations

- **Severity:** medium
- **Location:** `operators/cmd/main.go:217`
- **Description:** TLS verification bypass (`InsecureSkipVerify: true`) appears in 4 independent `http.Transport` constructions: the shared httpClient in `cmd/main.go`, the probe client created inline in `exposure_controller.go:SetupExposureController`, the per-probe client in `exposure_controller.go:performProbe`, and the fallback client in `platformbootstrap_controller.go:httpClient()`. No shared constructor or environment toggle exists.
- **Evidence:** `cmd/main.go:211-219`, `exposure_controller.go:207-215`, `exposure_controller.go:239-247`, `platformbootstrap_controller.go:162-169`
- **Recommendation:** Create `internal/httpclient/internal_client.go` with `New()` returning a pre-configured `*http.Client` with appropriate timeout and InsecureSkipVerify. All four call sites become `httpclient.New()`. When internal services get proper cert-manager issued certificates, one change hardens all clients simultaneously.

---

### ARCH-006: DORegistryTokenExpirySeconds constant is located in ports.go instead of timeouts.go

- **Severity:** low
- **Location:** `operators/internal/constants/ports.go:57`
- **Description:** `DORegistryTokenExpirySeconds = 604800` is a time duration value (7-day token TTL) placed in `ports.go` alongside TCP port numbers. The constants package has a well-organized file-per-concept structure. This one constant breaks the pattern: engineers looking for time-related constants will find it in `timeouts.go`, not `ports.go`.
- **Evidence:** `constants/ports.go:54-59` contains a section titled "DO Registry Credential Expiry" with a duration constant; `constants/timeouts.go` contains all other time-related constants
- **Recommendation:** Move `DORegistryTokenExpirySeconds` to `timeouts.go`. Single-line change. Update the import in `tenant_credentials.go` if the package alias changes.

---

### ARCH-007: PlatformBootstrap status has duplicate state communication via Message string and Conditions

- **Severity:** low
- **Location:** `operators/api/v1/platformbootstrap_types.go:187`
- **Description:** `PlatformBootstrapStatus` has both `Conditions []metav1.Condition` (typed, structured, machine-readable) and `Message string` (freeform, human-readable). The controller sets both in parallel. For example, at line 62-63: `pb.Status.Phase = Pending` AND `pb.Status.Message = "Bootstrap initialized"`. The Kubernetes API convention is to express all state via Conditions, using the Condition's Message field for human-readable text.
- **Evidence:** `platformbootstrap_types.go:187-191` defines Message field; `platformbootstrap_controller.go:62-63, 78-79, 97` sets Message in parallel with phase transitions
- **Recommendation:** Deprecate the `Message string` field. Move its content into the most relevant Condition's Message field. If a top-level human summary is needed for `kubectl get pbs` output, add a `+kubebuilder:printcolumn` for a condition status rather than a freeform string.

---

### ARCH-008: infra.ts bypasses CloudProviderFactory for ContainerRegistry and CDN resources

- **Severity:** low
- **Location:** `infra/pulumi/src/components/infra.ts:209`
- **Description:** `infra.ts` imports and uses `@pulumi/digitalocean` directly to create `digitalocean.ContainerRegistry` (line 220) and `digitalocean.Cdn` (line 209), bypassing the `CloudProviderFactory` abstraction that handles all other resources. A comment at line 196 acknowledges this bypass. This means the AWS provider stub cannot satisfy these resource requirements without modifying `infra.ts` itself.
- **Evidence:** `infra.ts:2,209,220` direct `@pulumi/digitalocean` imports alongside factory-mediated resources; `contracts/provider.ts` has no `createContainerRegistry` or `createCdn` methods
- **Recommendation:** Add `createContainerRegistry(spec)` and optionally `createCdn(spec)` to `CloudProviderFactory` and the contracts. Implement in `DigitalOceanProviderFactory`. Stub in `AwsProviderFactory`. This closes the abstraction leak and makes the provider boundary complete.

---

### ARCH-009: GitOpsSpec fields lack kubebuilder validation markers

- **Severity:** low
- **Location:** `operators/api/v1/tenant_types.go:82`
- **Description:** `GitOpsSpec.RepoName` and `GitOpsSpec.Branch` have no kubebuilder validation markers. An empty `RepoName` will pass admission validation and reach the reconciler, where it will attempt to call `EnsureRepo(ctx, "", ...)` on the Gitea API — an error that could be caught at admission time instead.
- **Evidence:** `tenant_types.go:82-88` GitOpsSpec struct has no `+kubebuilder:validation:*` markers; compare with `tenant_types.go:52` DisplayName which has `MinLength=1`
- **Recommendation:** Add `// +kubebuilder:validation:MinLength=1` to both `RepoName` and `Branch`. Optionally add a Pattern marker to `RepoName` matching valid Gitea repository names. Run `make manifests` to regenerate CRD YAML.

---

### ARCH-010: PriorityClass provisioning only targets runtime cluster, not build cluster

- **Severity:** low
- **Location:** `operators/internal/controller/tenant_priority.go:44`
- **Description:** `reconcilePriorityClass` creates a PriorityClass on `r.RuntimeClient` only. Argo Workflow pods run on the build cluster and are also subject to multi-tenant resource contention. A Growth-tier tenant's build pods cannot be scheduled with higher priority than a Starter-tier tenant's build pods because no PriorityClass exists on the build cluster.
- **Evidence:** `tenant_priority.go:44` uses `r.RuntimeClient` exclusively; `tenant_build.go` provisions build namespace with quota and network policy but no priority class
- **Recommendation:** Evaluate whether the build cluster should also receive plan-based PriorityClasses. If yes, add `controllerutil.CreateOrUpdate(ctx, r.BuildClient, pc, ...)` alongside the existing RuntimeClient call. If build priority is intentionally flat (all builds are equal), document this decision with a comment explaining why.

---

## Recommendations (Prioritized)

1. **Split bootstrap.ts** (ARCH-001): Highest maintenance impact. Split by cluster: `mgmt-bootstrap.ts`, `build-bootstrap.ts`, `runtime-bootstrap.ts`. This is a pure structural refactor — no behavior changes required.

2. **Fix ArgoCD provisioner client injection** (ARCH-003): One-line change in `NewRealProvisioner` signature, one-line change in `cmd/main.go`. Aligns the ArgoCD provisioner with the established DIP pattern used by Gitea and MinIO provisioners.

3. **Resolve EnvironmentSpec naming collision** (ARCH-004): Rename `EnvironmentSpec` → `TenantEnvironmentEntry` in `tenant_types.go`. Mechanical rename, regenerate manifests. Eliminates a documented workaround.

4. **Centralize InsecureSkipVerify** (ARCH-005): Create `internal/httpclient/` package. Four call sites become one-liners. Enables single-point TLS hardening.

5. **Add validation markers to GitOpsSpec** (ARCH-009): Two `+kubebuilder:validation:MinLength=1` annotations. Prevents invalid Tenant CRs from reaching the reconciler.

6. **Move DORegistryTokenExpirySeconds to timeouts.go** (ARCH-006): Single-line move. Cosmetic but improves constants package consistency.

7. **Remove or deprecate PlatformBootstrap Message field** (ARCH-007): Eliminate parallel state communication. Low risk, improves API hygiene.

8. **Add ContainerRegistry/CDN to CloudProviderFactory** (ARCH-008): Closes the provider abstraction leak. Required before AWS provider can be implemented.

9. **Document TenantReconciler field usage per file** (ARCH-002): Lowest effort. Add a comment to the struct documenting which fields each sub-file uses. Deferred full refactor until test friction justifies it.

10. **Decide build cluster priority class policy** (ARCH-010): Either add build cluster PriorityClass provisioning or document the intentional omission.
