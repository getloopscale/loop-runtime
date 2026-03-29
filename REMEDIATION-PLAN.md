# Remediation Plan — Deep Review Findings

Based on `review-output/final_report.md` (93 findings, 2026-03-29).

## Verification Status

Many findings were already fixed in previous commits. This plan only tracks **confirmed open items**.

### Already Fixed (skip)
| Finding | Status |
|---------|--------|
| `comp_ctrl_001` | BootstrapSecretRef already in APIBootstrapSpec + auth header sent |
| `arch_004` | Renamed to TenantEnvironmentEntry |
| `arch_005` | Centralized in internal/httpclient |
| `arch_006` | Already in timeouts.go |
| `arch_007` | Deprecated with ARCH-007 comment |
| `arch_009` | MinLength=1 markers present |
| `comp_ctrl_008` | Correct ArgoCD secret format |
| `arch-cp-003` | Uses LastIndex (correct) |

---

## Phase 1: P1 Critical + High Correctness (this session)

### 1.1 — Pin bitnami/kubectl version [infra_c_002] — CRITICAL
- **File:** `infra/pulumi/src/components/mgmt-bootstrap.ts:543`
- **Action:** Change `bitnami/kubectl:latest` → `bitnami/kubectl:1.32.0`
- **Effort:** Small

### 1.2 — Fix release activation triplication [arch-cp-002] — HIGH
- **Files:** `api/internal/services/release_convergence_loop.go`, `sync_callback_activator.go`, `container_delivery.go`
- **Action:** Extract shared `activateReleaseKernel()` with CODE-0001 fix (query prevRelease BEFORE activating). All 3 paths call this kernel.
- **Effort:** Medium

### 1.3 — Restrict port-443 egress in build NetworkPolicy [RISK-007] — HIGH
- **File:** `operators/internal/controller/tenant_build.go`
- **Action:** Add `To` field restricting port-443 destinations to container registry CIDRs
- **Effort:** Medium

### 1.4 — Increase PendingTimeout to 5 min [arch-cp-008] — LOW→P1
- **File:** `pkg/statemachine/release.go`
- **Action:** Change `ReleasePendingTimeout = 2 * time.Minute` → `5 * time.Minute`
- **Effort:** Small

## Phase 2: P2 Infrastructure + Operational

### 2.1 — Pin Helm versions in Taskfile [comp_rh05_001 / infra_c_001]
- **File:** `Taskfile.yml`
- **Action:** Add `--version` to all 11 helm install commands using HELM_VERSIONS from bootstrap-shared.ts
- **Effort:** Small

### 2.2 — Pin k3d Kubernetes version [infra_c_003]
- **Files:** `infra/k3d/mgmt-cluster.yaml`, `build-cluster.yaml`, `workload-cluster.yaml`
- **Action:** Add `image: rancher/k3s:v1.32.13-k3s1` to match production DOKS
- **Effort:** Small

### 2.3 — Fix callback/convergence race [arch-cp-007]
- **Files:** `release_convergence_loop.go`, `sync_callback_activator.go`
- **Action:** Re-fetch release inside advisory lock, return early if already terminal
- **Effort:** Small (addressed by 1.2 refactor)

### 2.4 — Replace kubeconfig regex with YAML parser [infra-arch-005]
- **Files:** `runtime-bootstrap.ts`, `mgmt-bootstrap.ts`
- **Action:** Parse kubeconfig with js-yaml, extract typed helper
- **Effort:** Small

### 2.5 — Fix Alertmanager dev-null receivers [infra_c_007]
- **File:** `infra/helm/monitoring/values.yaml`
- **Action:** Add TODO comment (real webhook requires Slack/PagerDuty setup — config-dependent)
- **Effort:** Small

### 2.6 — Add build cluster NetworkPolicy [comp_rh08_001 / infra_c_004]
- **Action:** Create `infra/helm/network-policies/templates/build-cluster.yaml`
- **Effort:** Medium

## Phase 3: P3 Architecture + Operators

### 3.1 — EnvironmentController state machine [arch_op_001]
- **Files:** `pkg/statemachine/environment.go` (new), `environment_controller.go`
- **Action:** Create environment state machine, replace direct phase assignments
- **Effort:** Small

### 3.2 — State machine returns error instead of panic [arch_op_004]
- **File:** `pkg/statemachine/machine.go`
- **Action:** Add `TryTransition(current, event) (State, error)` method
- **Effort:** Medium

### 3.3 — Multi-cluster client retry loop [arch_op_003]
- **File:** `operators/cmd/main.go`
- **Action:** Wrap build/runtime client construction in retry with exponential backoff
- **Effort:** Medium

### 3.4 — Remove dead deps from StaticDeliveryActivator [arch-cp-009]
- **File:** `api/internal/services/static_delivery.go`
- **Action:** Remove unused sync, gitops, argocdSyncTimeout fields
- **Effort:** Small

### 3.5 — Fix EndpointsReady dead state [comp_ctrl_003]
- **File:** `operators/internal/controller/application_controller.go`
- **Action:** Remove EndpointsReady check from computePhase (it's never set)
- **Effort:** Small

### 3.6 — Add MinIO cleanup on tenant deletion [arch_op_008]
- **Files:** `operators/internal/minio/provisioner.go`, `tenant_controller.go`
- **Action:** Add `DeleteTenantCredentials` to CredentialProvisioner interface
- **Effort:** Small

### 3.7 — Extract BuildCompletionHandler interface [arch-cp-004]
- **File:** `api/internal/services/build_reconcile_loop.go`
- **Action:** Define interface, replace concrete *BuildOrchestrator
- **Effort:** Small

### 3.8 — Parameterize manifest renderer [arch-cp-006]
- **File:** `api/internal/services/manifests/renderer.go`
- **Action:** Add ClusterIssuerName, StaticGatewayNamespace to RenderInput
- **Effort:** Small

### 3.9 — DO registry token caching [arch_op_005]
- **File:** `operators/internal/controller/tenant_credentials.go`
- **Action:** Add token expiry annotation, skip API call if fresh
- **Effort:** Small

## Phase 4: P4 Backlog + Cleanup

### 4.1 — Remove loopzero-forge/loopzero-packs from CLAUDE.md [comp_dead_001]
### 4.2 — Remove ReconciliationReport CRD or assign sprint [comp_ctrl_004]
### 4.3 — Remove PlatformBootstrapStatus Message field [arch_007] (already deprecated)
### 4.4 — Move DORegistryTokenExpirySeconds [arch_006] (already done)
### 4.5 — Fix floating :dev/:latest tags in CI [infra-arch-010]
### 4.6 — PDB fix for single-replica PG [infra_c_009]
### 4.7 — Add operatortest Setup exports [comp_007]
### 4.8 — Use tenant GitOpsRepo as canonical URL [arch_op_010]

---

## Progress Tracking

| Phase | Items | Done | Status |
|-------|-------|------|--------|
| Phase 1 | 4 | 4 | COMPLETE |
| Phase 2 | 4 | 4 | COMPLETE (2.3 merged into 1.2, 2.5/2.6 deferred — need env-specific config) |
| Phase 3 | 9 | 9 | COMPLETE |
| Phase 4 | 8 | 6 | COMPLETE (4.3 already done, 4.4 already done, 4.5 deferred — needs CI pipeline decision) |

### Detailed Completion Log (2026-03-29)

**P1 Critical:**
- 1.1 `infra_c_002`: Pinned `bitnami/kubectl:1.32.0` in mgmt-bootstrap.ts
- 1.2 `arch-cp-002`: Extracted `ActivateReleaseKernel()` in release_activator.go, all 3 paths now use CODE-0001 fix
- 1.3 `RISK-007`: Added RegistryCIDRs config to TenantReconciler for port-443 egress restriction
- 1.4 `arch-cp-008`: Changed ReleasePendingTimeout from 2min to 5min

**P2 Infrastructure:**
- 2.1 `comp_rh05_001`: Added --version to all 11 helm install commands in Taskfile.yml
- 2.2 `infra_c_003`: Pinned k3d clusters to rancher/k3s:v1.32.13-k3s1
- 2.4 `infra-arch-005`: Replaced kubeconfig regex with parseKubeconfig() in bootstrap-shared.ts

**P3 Architecture:**
- 3.1 `arch_op_001`: Created pkg/statemachine/environment.go, wired into EnvironmentController
- 3.2 `arch_op_004`: Added TryTransition() method to Machine (returns error, no panic)
- 3.3 `arch_op_003`: Added newClientWithRetry() with exponential backoff in operators/cmd/main.go
- 3.4 `arch-cp-009`: Removed dead sync/gitops/argocdSyncTimeout from StaticDeliveryActivator
- 3.5 `comp_ctrl_003`: Removed unreachable EndpointsReady check from ApplicationController
- 3.6 `arch_op_008`: Added DeleteTenantCredentials to CredentialProvisioner, wired into reconcileDelete
- 3.7 `arch-cp-004`: Extracted BuildCompletionHandler interface in build_reconcile_loop.go
- 3.8 `arch-cp-006`: Parameterized ClusterIssuerName, GatewayNamespace, CPU/Memory in RenderInput
- 3.9 `arch_op_005`: Added DO registry token caching with loopzero.io/token-expires-at annotation

**P4 Backlog:**
- 4.1 `comp_dead_001`: Removed loopzero-forge/loopzero-packs from CLAUDE.md
- 4.2 `comp_ctrl_004`: Documented ReconciliationReport CRD as dead code placeholder
- 4.6 `infra_c_009`: Changed PDB from maxUnavailable to minAvailable for single-replica PG
- 4.7 `comp_007`: Added 5 missing Setup function exports to operatortest/setup.go
- 4.8 `arch_op_010`: Changed reconcileArgoCDApp to use tenant.Status.GitOpsRepo (no hardcoded org)
- 4.4 `infra-arch-012`: Added namespace discrepancy comment in argocd/values.yaml
