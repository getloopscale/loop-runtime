# Operator Capability Matrix

> **Per ADR-0027 D5 / plan T5.1.** Each of LoopZero's 8 controllers gets an explicit Operator SDK Capability Level (I-V) declaration with a 3-column evidence matrix (capability → file:line → verifying test). The module makes no single "LoopZero is Level X" claim; controllers are heterogeneous and averaging hides exactly the gaps Sprint 9 needs to close.

**Version:** 1.0 (2026-05-04)
**Source:** ADR-0027 + [Operator SDK Capability Levels](https://sdk.operatorframework.io/docs/overview/operator-capabilities/) + [CloudNativePG capability matrix](https://cloudnative-pg.io/) (reference for L5 declaration rigor).

## Capability levels (rubric — ADR-0027 follow-up F1)

| Level | Name | Required (per LoopZero rubric) |
|---|---|---|
| **I** | Basic Install | Reconcile installs/deploys the resource via CRD spec. CRD validation. |
| **II** | Seamless Upgrades | CRD spec changes trigger reconcile-driven update without human action. |
| **III** | Full Lifecycle | Create + Update + Delete with finalizer cleanup; idempotent reconcile. |
| **IV** | Deep Insights | ≥10 controller-specific Prometheus metrics + ≥3 SLO alerts + per-controller runbook in `docs/runbooks/operator-<name>.md` + envtest spec proving deep insights surface. kstatus emission required. |
| **V** | Auto Pilot | Controller itself self-heals in-process without operator action (orphan reaper, drift correction, auto-tuning). Heal scripts (operator-invoked) do NOT count as L5 (per ADR-0027 R4). |

## Per-controller declaration

### TenantReconciler — Level **IV** declared

| Capability | Evidence (file:line) | Verifying test |
|---|---|---|
| L1 Basic Install | `internal/tenant/tenant_controller.go:83` (Reconcile) | `internal/tenant/tenant_controller_test.go::TestTenantReconciler_*` (envtest) |
| L2 Seamless Upgrades | spec change → Generation bump → predicate fires (`internal/shared/predicates.go`) | `internal/shared/predicates_test.go::Test_OperatorPredicates_GenerationChange_Reconciled` |
| L3 Finalizer cleanup | `internal/tenant/tenant_deletion.go::reconcileDelete` (verify-then-remove finalizer pattern) | `internal/tenant/tenant_deletion_backoff_test.go` |
| L3 Idempotent reconcile | `controllerutil.CreateOrUpdate` for all child resources | `internal/tenant/tenant_controller_test.go::TestTenantReconciler_Idempotent` |
| L4 kstatus summary | `internal/tenant/tenant_controller.go::Reconcile defer` (per ADR-0025 D3) | `internal/conditions/summary_test.go::Test_SetReadinessSummary_AllTrue` |
| L4 Metrics | `internal/metrics/metrics.go` — 43+ loopzero_* + per-controller counters via shared.ReconcileOutcome | `internal/shared/predicates_test.go` (predicate filtering verified at unit level) |
| L4 Runbook | `docs/runbooks/operator-cutover-runbook.md` (legacy multi-binary cutover) | manual reference |
| L4 Predicate filtering | `internal/shared/predicates.go::OperatorPredicates` | `internal/shared/predicates_test.go::Test_OperatorPredicates_StatusOnlyUpdate_Filtered` |

**L5 candidates (deferred):**
- Tenant CRs in unexpected namespaces auto-corrected via VAP (currently warn-only via `loopzero_misplaced_tenant_total` alert).

### EnvironmentReconciler — Level **IV** declared

| Capability | Evidence | Verifying test |
|---|---|---|
| L1-L3 standard | `internal/tenant/environment_controller.go::Reconcile` + `reconcileDelete` | `internal/tenant/environment_controller_test.go` |
| L3 Cell-aware reconcile | `internal/tenant/environment_controller.go::runtimeClientForEnv` (CellAwareRuntimeClientFactory) | `internal/shared/runtime_client_factory_test.go` |
| L4 kstatus | defer SetReadinessSummary in Reconcile | `internal/conditions/summary_test.go` |
| L4 4 detail conditions | `environmentDetailTypes` (NamespaceReady, RBACReady, NetworkPolicyReady, QuotaReady) | per-suite envtest |

### ApplicationReconciler — Level **IV** declared

| Capability | Evidence | Verifying test |
|---|---|---|
| L1-L3 standard | `internal/app/application_controller.go::Reconcile` | `internal/app/application_controller_test.go` |
| L3 Hostname assignment | `reconcileHostname` (idempotent via spec.Hostname check) | TestApplicationReconciler_HostnameAssignment |
| L4 5 detail conditions | ServiceResolved, IngressReady, CertificateIssued, RouteReachable, ApplicationHealthy | envtest spec |
| L4 Phase calculation | `computePhase` (uses typed constants per AP-2 fix) | TestComputePhase_DegradedReason / TestComputePhase_FailedReason |
| L5 Application Reaper (in-process) | `internal/app/application_reaper.go::ShouldReap + ApplicationOrphaned event → terminal Cleaned state` | `internal/app/application_reaper_test.go::TestShouldReap_Orphan` ⭐ |

**Level V evidence:** the application reaper runs as part of the controller's reconcile loop (no operator action needed). When a LoopZeroApplication's manifest disappears from the gitops repo, the reaper detects orphan (after grace period) and transitions phase → Cleaned via state-machine event. This satisfies the L5 "Auto Pilot" criterion per ADR-0027 (in-process self-healing, not operator-invoked script).

### ExposureReconciler — Level **III** declared (target IV post-Sprint 9)

| Capability | Evidence | Verifying test |
|---|---|---|
| L1-L3 standard | `internal/app/exposure_controller.go::Reconcile` | `internal/app/exposure_controller_test.go` |
| L4 kstatus | defer SetReadinessSummary | `internal/conditions/summary_test.go` |
| **GAP** (L4 Metrics) | only generic shared.ReconcileOutcome; no per-route latency histogram | TODO Sprint 9 |
| **GAP** (L4 Runbook) | none | TODO Sprint 9 |

### PreviewReconciler — Level **V** declared (highest tier)

| Capability | Evidence | Verifying test |
|---|---|---|
| L1-L3 standard | `internal/preview/preview_controller.go::Reconcile` | envtest specs (12+) |
| L3 NetworkPolicy + ResourceQuota + LimitRange | `reconcilePreviewNetworkPolicies` + `reconcilePreviewQuota` + `reconcilePreviewLimitRange` | `internal/preview/preview_controller_envtest_test.go` |
| L4 7 detail conditions | full per-step (NamespaceReady → BuildSubmitted → BuildCompleted → WorkloadHealthy) | envtest |
| L5 TTL-based auto-cleanup ⭐ | `reconcileExpiring` triggered by TTL expiry; in-process state machine (Active → Expiring → Deleted) | TestPreviewExpiry_AfterTTL |
| L5 Build grace period | `previewBuildGracePeriod` (90s) — auto-fail if BuildSubmitted missing | TestPreviewBuildGracePeriod_Failed |

**Level V evidence:** TTL expiry triggers in-process transition to Deleted (no human action); finalizer ensures runtime resources cleaned via state machine; preview namespaces auto-pruned. This is the model for what other controllers should target in Sprint 10+.

### PlatformBootstrapReconciler — Level **III** declared

| Capability | Evidence | Verifying test |
|---|---|---|
| L1-L3 standard | `internal/platform/platformbootstrap_controller.go::Reconcile` | `platformbootstrap_controller_test.go` |
| L3 4-phase state machine | Pending → ConfiguringGitea → ConfiguringArgoCD → WaitingForAPI → Complete | envtest |
| **GAP** (L4 Deep Insights) | no per-bootstrap-step metrics; no runbook | TODO post-Sprint 9 |

### GitRepositoryBindingReconciler — Level **III** declared

| Capability | Evidence | Verifying test |
|---|---|---|
| L1-L3 standard | `internal/platform/gitrepositorybinding_controller.go::Reconcile` | `gitrepositorybinding_controller_test.go` |
| L3 Reachability probe | `reconcileRepositoryReachable` HTTP HEAD | envtest |
| **GAP** (L4 metrics) | no per-binding latency histogram | TODO Sprint 9 |

### ClusterCredentialReconciler — Level **IV** declared

| Capability | Evidence | Verifying test |
|---|---|---|
| L1-L3 standard | `internal/platform/clustercredential_controller.go::Reconcile` | `clustercredential_controller_test.go` |
| L3 ALWAYS-call ArgoCD secret sync (port #1) | `Reconcile` Step 2 (BEFORE connectivity check, fix-all-platform-issues-plan T1.2) | envtest spec for cell-2 token rotation |
| L4 Watches Secret + reconcile-on-change | `SetupClusterCredentialController::Watches(&corev1.Secret{}, ...)` | envtest |
| L4 Connectivity probe | `checkConnectivity` (REST /healthz with timeout) | envtest |
| L4 Inverted-polarity guard | TypeSecretNotFound excluded from kstatus detail types (per ADR-0025 condition #3) | `internal/platform/clustercredential_controller.go::clusterCredentialDetailTypes` |
| **L5 candidate** (deferred) | auto-mint SA token when cached token expired (currently external `task k3d:heal H13`) | next sprint |

## Summary

| Controller | Level | Notable |
|---|---|---|
| Tenant | IV | finalizer + cell-aware + 6 detail conditions |
| Environment | IV | cell-aware via factory |
| Application | **IV** + L5 evidence | application reaper (in-process) |
| Exposure | III → IV target | gaps: metrics, runbook |
| **Preview** | **V** | TTL auto-cleanup + build grace period |
| PlatformBootstrap | III | gaps: per-step metrics |
| GitRepositoryBinding | III | gaps: per-binding latency |
| ClusterCredential | IV | inverted-polarity guard + ALWAYS-call sync |

**LoopZero module aggregate (informal):** L4 baseline (4 of 8 controllers L4+, 1 at L5, 3 at L3 with documented gaps). Path to L4-everywhere: Sprint 9 closes Exposure / PlatformBootstrap / GitRepositoryBinding observability gaps.

## Maintenance

Per ADR-0027 follow-up F2: this matrix is machine-checkable via
`infra/scripts/capability-matrix-audit.sh` (TODO — to land in T5.2).
Every controller PR touching `internal/{tenant,app,preview,platform}/*.go`
that adds/removes a Setup* function must update this matrix in the
same commit; CI gate fails otherwise.

Per ADR-0027 follow-up F3: matrix reviewed by external triad
(platform-architect + sre-guardian + security-reviewer) per major
release.

## References

- ADR-0027 — Capability Levels per-controller
- [Operator SDK Capability Levels](https://sdk.operatorframework.io/docs/overview/operator-capabilities/)
- CloudNativePG capability matrix (FAANG-bar reference)
- `docs/technical/operators/improvement-roadmap.md` §S9-T3 (origin)
- `docs/technical/operators/sota-gates.md` OP-G20 (gate satisfied by this doc)
