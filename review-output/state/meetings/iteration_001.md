# Meeting Minutes — Phase 1, Iteration 1
**Date:** 2026-03-29
**Review scope:** Cluster Management, Integrations, Tools, and Control Plane

---

## Status
- Phase: 1/7 (Baseline — Discovery and Mapping)
- Findings: total=7, critical=0, high=4, medium=3, low=0
- Components registered: 18 (3 clusters, 7 controllers, 3 provisioners, 1 state machine lib, 1 entrypoint, 1 workflow, 1 helm-charts group)
- Flows registered: 6 (all status: identified)
- Previous iteration: none (this is iteration 1)

---

## Scope Boundary Confirmed

**IN SCOPE for this review cycle:**
- 3-cluster topology: mgmt (control plane), build (Argo Workflows), workload (runtime)
- 7 operator controllers: Tenant, Environment, Application, Exposure, PlatformBootstrap, GitRepositoryBinding, ClusterCredential
- 3 CRDs without controllers: AppSecret, PreviewEnvironment, ReconciliationReport
- Provisioner integrations: Gitea (RealProvisioner), ArgoCD (RealProvisioner), MinIO (AdminProvisioner)
- Multi-cluster client pattern (operators/cmd/main.go)
- State machines: Tenant, PlatformBootstrap (in pkg/statemachine/)
- Cluster lifecycle: k3d configs (local), Pulumi IaC (DigitalOcean production)
- 13 Helm charts, Taskfile automation, GitHub Actions infra-deploy.yml

**OUT OF SCOPE:**
- CLI (TypeScript) implementation details
- API business logic beyond control plane integration points
- Framework detection (loopzero-packs)
- User-facing deploy/rollback/logs flows

---

## Agent Reports

### Chief Reviewer — Initial Code Survey
Completed direct reading of:
- `operators/cmd/main.go` (250 lines) — full multi-cluster bootstrap
- `operators/internal/controller/tenant_controller.go` (head)
- `operators/internal/controller/tenant_build.go` (complete — 342 lines)
- `operators/internal/controller/environment_controller.go` (head)
- `operators/internal/controller/platformbootstrap_controller.go` (head)
- `operators/internal/controller/application_controller.go` (head)
- `operators/internal/controller/clustercredential_controller.go` (head)
- `operators/internal/controller/gitrepositorybinding_controller.go` (head)
- `operators/internal/argocd/provisioner.go` (complete)
- `operators/internal/minio/provisioner.go` (complete — 304 lines)
- `operators/internal/conditions/types.go` (complete)
- `operators/internal/constants/timeouts.go` (complete)
- `pkg/statemachine/machine.go` (complete)
- `pkg/statemachine/tenant.go` (complete)
- `infra/k3d/mgmt-cluster.yaml` and `build-cluster.yaml`
- `.github/workflows/infra-deploy.yml` (head)

### Architecture Analyst (pending — iteration 2 assignment)
Not yet deployed. Task assigned: map full controller dependency chain and coupling analysis.

### Security Auditor (pending — iteration 2 assignment)
Not yet deployed. Initial risk hypotheses raised by chief reviewer:
- RISK-001: InsecureSkipVerify on shared HTTP client
- RISK-002: MinIO Admin API uses Basic Auth
- RISK-007: Build namespace egress policy allows port 443 to any IP

### Test Auditor (pending — iteration 2 assignment)
Not yet deployed. Key gap identified: RISK-006 (envtest uses single API server for 3-cluster simulation).

### Infrastructure Reviewer (pending — iteration 2 assignment)
Not yet deployed. Focus area: Helm chart version pinning, k3d vs production parity, Taskfile idempotency.

---

## Key Architecture Facts Established

### Multi-Cluster Operator Pattern
The operator process runs entirely in the mgmt cluster but manages resources across 3 clusters:
- `r.Client` (mgmt) — CRDs, status, provisioner calls
- `r.BuildClient` (build) — build-{tenant} namespaces, RBAC, secrets
- `r.RuntimeClient` (runtime) — {tenant}-{env} namespaces, RBAC, NetworkPolicy

Cross-cluster ownerReference is impossible. Cleanup uses label-based listing + finalizer pattern. This is a well-known Kubernetes pattern but creates operational risk on partial failure.

### Controller Dependency Chain
```
Tenant (root) --> Environment --> LoopZeroApplication --> ExposedRoute
```
Independent: GitRepositoryBinding, ClusterCredential, PlatformBootstrap

Wiring order in cmd/main.go follows this chain explicitly.

### State Machine Discipline
- `pkg/statemachine.Machine` panics on invalid transitions (programming errors, not runtime errors)
- Tenant: "" -> Provisioning -> Active -> Deleting (terminal)
- PlatformBootstrap: "" -> Pending -> ConfiguringGitea -> ConfiguringArgoCD -> WaitingForAPI -> Complete/Failed
- Direct phase assignment is forbidden — only `tenantSM.Transition()` and `platformBootstrapSM.Transition()` calls allowed

### TenantController Build Namespace Setup (10 steps, all via CreateOrUpdate)
1. namespace (ensureBuildNs)
2. resourcequota (ensureBuildQuota)
3. networkpolicy (reconcileBuildNetworkPolicy)
4. limitrange (ensureBuildLimitRange)
5. serviceaccount (ensureBuildServiceAccount)
6. storage-credentials (ensureBuildStorageCredentials)
7. registry-credentials (ensureBuildRegistryCredentials)
8. executor-rbac (ensureBuildExecutorRBAC)
9. submitter-rbac (ensureBuildSubmitterRBAC)
10. minio-per-tenant (ensureBuildMinIOPerTenant)

### MinIO Isolation Design
Per-tenant MinIO users (`loopzero-build-{tenantID}`) with policies scoped to `arn:aws:s3:::loopzero-builds/{tenantID}/*`. This is **optional** — nil MinIOProvisioner falls back to global credentials from SPACES_ACCESS_KEY_ID. In production (DO Spaces), per-tenant provisioner is DISABLED because Spaces has no Admin API.

---

## Risk Hypotheses Registered (Phase 1)

| ID | Title | Severity | Category |
|----|-------|----------|----------|
| RISK-001 | InsecureSkipVerify on shared HTTP client | medium | security |
| RISK-002 | MinIO Admin API uses Basic Auth, not SigV4 | medium | security |
| RISK-003 | MinIO user exists but K8s Secret deleted — unrecoverable state | high | operational |
| RISK-004 | No OwnerReference cross-cluster — orphaned build namespace possible | high | operational |
| RISK-005 | PlatformBootstrap API seed may not be idempotent across restart | medium | operational |
| RISK-006 | envtest uses single API server — multi-cluster failures untested | high | testing |
| RISK-007 | Build egress NetworkPolicy allows port 443 to any IP | high | security |

---

## Strategic Assessment

### What worked well
- Direct code reading of entrypoint and key controllers provided a clear architectural picture in one pass.
- The code is well-structured: typed conditions, centralized constants, state machine enforcement, dependency injection via interfaces. These are positive signals for the review.
- The 10-step build namespace setup is comprehensively documented in comments and uses a clean step-slice pattern for readability.

### What to investigate deeper
- The MinIO credential management path (RISK-003) requires tracing the full credential lifecycle from provisioning to K8s Secret to Argo Workflow injection.
- Build namespace egress NetworkPolicy (RISK-007) needs comparison against the spec §17.1 threat model to determine if this is a known accepted risk or a gap.
- The PlatformBootstrap WaitingForAPI step needs reading of `platformbootstrap_api.go` to verify idempotency.
- The tenant deletion finalizer path needs reading of `reconcileDelete` to verify build cluster error handling.

### Cross-cutting concern
The "multi-cluster via separate K8s clients" pattern is used consistently but creates a class of failure modes that are architecturally invisible in envtest. This pattern — well-implemented in the code — requires explicit failure testing. RISK-006 is the test gap; RISK-004 is the runtime gap.

---

## Decisions

1. **Individual controller granularity adopted.** Each of the 7 controllers is a separate component in the review database. Grouping by cluster (mgmt/build/workload) is retained for infrastructure components. Rationale: controllers have distinct failure modes, test coverage gaps, and security surfaces that require individual analysis.

2. **Priority flow order for iteration 2 deep dive:** (1) Tenant Provisioning — highest complexity, most cross-cluster interactions. (2) Platform Bootstrap — single-run critical path with state machine and external dependencies. (3) Build Namespace Network Policy — direct security implication.

3. **Multi-cluster failure modes are the single highest architectural risk gap.** The envtest simulation does not model cluster-level failures. E2E tests (infra/tests/e2e/) should be checked for multi-cluster failure coverage in iteration 2.

4. **Build namespace egress NetworkPolicy** (RISK-007) is designated as the highest-severity security finding requiring immediate deeper investigation. The `PortHTTPS=443` rule with no IP restriction may render the exfiltration protection spec §17.1 ineffective.

5. **No loop-back decision yet** — this is iteration 1 of phase 1. Advance to iteration 2 with assigned tasks below.

---

## Loop-Back Assessment
- Should we loop back? NO — this is phase 1 iteration 1. We advance to iteration 2.
- Confidence for advancing: MEDIUM — baseline is mapped but several components (tenant_controller.go full body, reconcileDelete, platformbootstrap_api.go, exposure_controller.go full body, operatortest/setup.go) not yet read.
- Iteration 2 focus: complete controller-by-controller deep read, architecture coupling analysis, and security investigation of RISK-001, RISK-002, RISK-007.

---

## Task Assignments for Iteration 2

### Architecture Analyst
**Task:** Produce a full dependency coupling map of the 7 controllers.
- Read `operators/operatortest/setup.go` to understand test infrastructure
- Read `operators/api/v1/tenant_types.go`, `environment_types.go`, `application_types.go` for CRD schema and validation markers
- Map: which controllers can reconcile concurrently vs. must be sequential
- Identify: is there any shared mutable state between controllers (besides the K8s API server)?
- Deliverable: component coupling matrix, list of shared dependencies, reconcile concurrency analysis

### Security Auditor
**Task:** Deep investigation of RISK-007 (build egress) and RISK-001 (InsecureSkipVerify).
- Read `tenant_build.go` reconcileBuildNetworkPolicy in full
- Read `operators/internal/gitea/real_provisioner.go` to understand which endpoints the InsecureSkipVerify HTTP client reaches
- Cross-reference against spec §17.1 (exfiltration prevention) and §17.4 (tenant isolation)
- Deliverable: confirmed severity for RISK-007, full list of endpoints reachable via the InsecureSkipVerify client, recommendation on specificity

### Test Auditor
**Task:** Map test coverage for multi-cluster failure scenarios.
- Read `operators/operatortest/setup.go` (full)
- Read `operators/internal/controller/suite_test.go` (full)
- Read `operators/internal/controller/tenant_integration_test.go` (full — continuation)
- Inventory: what failure scenarios are tested vs. missing
- Deliverable: test coverage gap analysis for multi-cluster failure modes (RISK-006)

### Code Reviewer
**Task:** Read tenant_controller.go reconcileDelete and minio credential lifecycle.
- Read full `tenant_controller.go` reconcileDelete path
- Trace MinIO credential path: EnsureTenantCredentials -> K8s Secret -> Argo Workflow env injection
- Verify RISK-003 and RISK-004 with specific line references
- Deliverable: confirmed or dismissed findings for RISK-003 and RISK-004 with code citations

### Infrastructure Reviewer
**Task:** Helm chart version analysis and k3d/production parity gaps.
- Read all Helm chart `Chart.yaml` files in `infra/helm/`
- Read `infra/pulumi/src/components/bootstrap.ts` for production deployment config
- Compare k3d cluster configs (agents, resource limits) with production DOKS specs
- Check: are all 13 Helm charts using pinned versions (no floating tags)?
- Deliverable: version inventory, parity gap list, any floating dependencies

### Completeness Auditor
**Task:** Map the 3 unwired CRDs against their spec stubs.
- Read `operators/api/v1/appsecret_types.go`, `preview_types.go`, `reconciliationreport_types.go`
- Verify: do types exist with no controller? Are there TODO markers or partial implementations?
- Check: is AppSecret critical path (secret sync to workload) blocking any current feature?
- Deliverable: unwired CRD status report with spec references and impact assessment

---

## Next Meeting
- Expected at: Iteration 2 start
- Focus: Review deliverables from all 5 assigned agents above
- Key question to answer: Is RISK-007 (build egress port 443) an accepted risk or a gap that needs remediation?
