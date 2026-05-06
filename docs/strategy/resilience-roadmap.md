# Resilience Roadmap — Path to Maturity 5.0

**Date:** 2026-05-06
**Author:** Engineering team
**Status:** PROPOSED (requires T2/T3 meeting approval per pre-release governance)
**Objective:** Close all resilience gaps blocking maturity 5.0

---

## Context

LoopZero is at maturity **3.9/5.0**. The path to 5.0 requires (from `docs/maturity-assessment.md`):

- Full chaos engineering suite
- 50+ real tenants
- Published SLA
- Validated incident runbooks
- 99.9% uptime measured over 30 days

This roadmap addresses the **technical resilience gaps** through 4 targeted integrations from the CNCF Landscape. Each integration closes a specific gap that cannot be solved by existing tooling.

---

## Integrations Summary

| # | Project | CNCF Status | Priority | Gap Closed | Tier |
|---|---------|-------------|----------|------------|------|
| 1 | Chaos Mesh | Incubating | **P0** | Chaos engineering suite | T1 (CRDs already installed) |
| 2 | Velero | CNCF project | **P1** | Backup/DR for K8s resources | T2 |
| 3 | Argo Rollouts | CNCF (Argo) | **P2** | Progressive delivery (canary) | T2 |
| 4 | Kuberhealthy | Sandbox | **P3** | Synthetic health probes for SLA | T2 |

---

## P0 — Chaos Mesh (Experiments)

### Objective

Validate that LoopZero self-heals under real failure conditions. Prove resilience with evidence, not assumptions.

### Current State

- Chaos Mesh **2.7.0 CRDs already registered** in all 4 clusters (Sprint 8 T0.3, `infra/helm/chaos-mesh/`).
- Zero `PodChaos`, `NetworkChaos`, or `IOChaos` experiments ever created.
- The gap is **usage**, not installation.

### What This Enables

| Without chaos experiments | With chaos experiments |
|---|---|
| "We believe the system self-heals" | "We proved it with evidence under N failure scenarios" |
| Untested advisory lock release on crash | Validated: API pod killed mid-deploy → lock released → next deploy succeeds |
| Assumed cell isolation | Validated: network partition mgmt↔workload → cell-2 unaffected |
| LoopZeroretical circuit breaker | Validated: PG slow → circuit opens → requests fail-fast → PG recovers → circuit closes |

### Experiments to Create

| # | Experiment | Type | Target | Validates |
|---|-----------|------|--------|-----------|
| E1 | API pod kill during active deploy | `PodChaos` (pod-kill) | `loopzero-api` pod in mgmt | Advisory lock release + Argo Workflow completion |
| E2 | Network partition mgmt ↔ workload | `NetworkChaos` (partition) | mgmt ↔ workload clusters | Cell isolation + DynamicRouter fallback |
| E3 | ArgoCD pod kill during sync | `PodChaos` (pod-kill) | `argocd-application-controller` | Sync retry + idempotent reconcile |
| E4 | PostgreSQL latency injection | `NetworkChaos` (delay) | PG service | Circuit breaker activation + recovery |
| E5 | Gitea unreachable | `NetworkChaos` (partition) | Gitea service | GitOps push retry + error propagation to CLI |
| E6 | Random pod kill (steady-state) | `PodChaos` (pod-kill, scheduled) | All tenant namespaces | General self-healing + KEDA re-scaling |
| E7 | Build cluster disk pressure | `IOChaos` (latency) | BuildKit pod | Build timeout + cleanup + re-queue |
| E8 | Operator leader election failover | `PodChaos` (pod-kill) | Operator leader pod | Leader re-election + reconcile resume |

### Success Criteria

- All 8 experiments documented as `ChaosExperiment` CRDs in `infra/chaos/experiments/`.
- Each experiment has a matching Ginkgo E2E test in `infra/tests/chaos/` that asserts recovery.
- CI workflow `chaos-suite.yml` runs weekly (like `cell2-soaks.yml`).
- Regression matrix updated with chaos entries.

### Estimated Effort

Medium — no new tooling to install. Pure experiment design + test writing.

---

## P1 — Velero (Backup & Disaster Recovery)

### Objective

Guarantee that a catastrophic cluster failure does not result in permanent data loss. Enable validated restore runbooks for SLA compliance.

### Current State

- **PostgreSQL:** DO Managed PG has automatic daily backups + PITR. ✅ Covered.
- **Kubernetes resources (CRDs, Secrets, ConfigMaps):** **Zero backup.** If mgmt cluster etcd corrupts, all Tenant/Application/Environment CRs are lost permanently.
- **Gitea repositories:** Stored on PVCs. No backup beyond what the PVC provider offers.
- **ArgoCD state:** Apps, AppProjects, Secrets stored in K8s. No backup.

### What This Enables

| Without Velero | With Velero |
|---|---|
| etcd corruption = total loss of K8s state | Scheduled restore from last backup |
| No DR runbook possible | Tested, documented restore procedure |
| SLA of 99.9% unpublishable (no recovery plan) | Recovery time < 30min documented and validated |
| Gitea PVC failure = repository loss | Gitea data backed up to DO Spaces |

### Architecture

```
                    Velero Server (mgmt cluster)
                         │
              ┌──────────┼──────────┐
              ▼          ▼          ▼
         mgmt backup  build backup  workload backup
              │          │          │
              └──────────┼──────────┘
                         ▼
                   DO Spaces (S3)
                   s3://loopzero-velero-backups/
```

### Backup Scopes

| Scope | What is backed up | Schedule | Retention |
|-------|-------------------|----------|-----------|
| **CRDs** | Tenant, Environment, LoopZeroApplication, ExposedRoute, PreviewEnvironment, ClusterCredential, PlatformBootstrap, GitRepositoryBinding | Every 6h | 30 days |
| **ArgoCD** | `argocd` namespace (Apps, AppProjects, repo Secrets) | Every 6h | 30 days |
| **Gitea** | `gitea` namespace (PVCs + StatefulSet) | Daily | 14 days |
| **Secrets** | `loopzero-system` namespace (cluster kubeconfigs, signing keys) | Every 6h | 30 days |
| **Full cluster** | All namespaces snapshot | Weekly | 7 days |

### Deliverables

1. Helm chart `infra/helm/velero/` with DO Spaces (S3) backend configuration.
2. `Schedule` CRDs for each backup scope.
3. Runbook `docs/runbooks/disaster-recovery-restore.md` with step-by-step restore procedure.
4. Quarterly DR drill: destroy mgmt cluster → restore from Velero → validate all CRDs restored → `loopzero deploy` works.
5. Prometheus metrics: `velero_backup_success_total`, `velero_backup_last_successful_timestamp`.
6. Alert: `VeleroBackupStale` if last successful backup > 12h.

### Dependencies

- DO Spaces bucket `loopzero-velero-backups` (Pulumi L1).
- IAM credentials scoped to S3 read/write (DO Spaces key).

### Estimated Effort

Medium — install + configure + write runbook + validate restore.

---

## P2 — Argo Rollouts (Progressive Delivery)

### Objective

Eliminate "deploy and pray" by replacing all-at-once deployments with automated canary analysis. A bad deploy should never affect 100% of traffic.

### Current State

- ArgoCD syncs `Deployment` manifests — all pods replaced simultaneously.
- Rollback is fast (~3s) but **reactive** — requires detection (human or alert) before triggering.
- Zero traffic-splitting capability for gradual rollout.

### What This Enables

| Without Argo Rollouts | With Argo Rollouts |
|---|---|
| Deploy → 100% pods replaced at once | Deploy → 20% canary → analysis → 50% → 100% |
| Bad deploy = full outage until rollback | Bad deploy = auto-rollback, 80% never affected |
| Rollback is reactive (detect → act) | Rollback is proactive (metrics trigger abort) |
| No deployment confidence metric | `AnalysisTemplate` provides measurable success rate |

### Architecture Change

```
BEFORE:
  loopzero deploy → API → Argo Workflow → build → Gitea push
    → ArgoCD sync → Deployment (all pods) → done

AFTER:
  loopzero deploy → API → Argo Workflow → build → Gitea push
    → ArgoCD sync → Rollout (canary 20%)
      → AnalysisTemplate queries Prometheus
        → success_rate >= 95%? → promote to 50% → 100%
        → success_rate < 95%? → auto-abort → rollback
```

### Manifest Changes

The manifest renderer (`api/internal/manifests/`) changes from generating `Deployment` to generating `Rollout`:

| Resource | Before | After |
|----------|--------|-------|
| Workload | `apps/v1 Deployment` | `argoproj.io/v1alpha1 Rollout` |
| Services | 1 (`my-app`) | 2 (`my-app-stable`, `my-app-canary`) |
| Traffic | Direct to Service | `TraefikService` weighted (Argo Rollouts controls weight) |
| Analysis | None | `AnalysisTemplate` with Prometheus queries |

### Canary Strategy by Plan

| Plan | Replicas Cap | Canary Strategy | Rationale |
|------|:---:|---|---|
| Starter | 2 | `setWeight: 50` → pause 60s → `setWeight: 100` | 2 replicas: canary = 1 pod. Single step. |
| Pro | 5 | `setWeight: 20` → pause 30s → `setWeight: 50` → pause 30s → `setWeight: 100` | 3 steps with analysis. |
| Team | 10 | `setWeight: 10` → pause 30s → `setWeight: 30` → pause 30s → `setWeight: 60` → pause 30s → `setWeight: 100` | 4 steps with full analysis. |
| Enterprise | Unlimited | Configurable via `loopzero.yaml` `canary.steps` | Custom strategy per customer. |

### Analysis Templates

```yaml
# Baseline: HTTP success rate > 95% during canary window
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: loopzero-success-rate
spec:
  metrics:
    - name: success-rate
      interval: 30s
      successCondition: result[0] >= 0.95
      failureLimit: 3
      provider:
        prometheus:
          address: http://prometheus.monitoring:9090
          query: |
            sum(rate(traefik_service_requests_total{
              service=~"{{args.service}}.*",
              code!~"5.."
            }[2m])) /
            sum(rate(traefik_service_requests_total{
              service=~"{{args.service}}.*"
            }[2m]))
    - name: latency-p99
      interval: 30s
      successCondition: result[0] <= 2000
      failureLimit: 3
      provider:
        prometheus:
          address: http://prometheus.monitoring:9090
          query: |
            histogram_quantile(0.99,
              sum(rate(traefik_service_request_duration_seconds_bucket{
                service=~"{{args.service}}.*"
              }[2m])) by (le)
            ) * 1000
```

### Components to Build/Change

| Component | Change | Scope |
|-----------|--------|-------|
| `infra/helm/argo-rollouts/` | New bootstrap chart for workload clusters | New chart |
| `api/internal/manifests/` | Generate `Rollout` + `TraefikService` + 2 Services | Modify |
| `api/internal/manifests/` | Generate `AnalysisTemplate` per environment | New |
| `cli/src/commands/status.ts` | Show canary progress (% weight, analysis status) | Modify |
| `cli/src/commands/deploy.ts` | SSE events for canary steps (promote/abort) | Modify |
| ArgoCD config | Register `argoproj.io` CRDs for health assessment | Config |
| KEDA `ScaledObject` | Target `Rollout` instead of `Deployment` | Modify |

### Risk: ArgoCD Sync Conflict

Argo Rollouts modifies `TraefikService.spec.weighted.services[].weight` at runtime. ArgoCD detects this as drift and tries to reset it.

**Mitigation:** ArgoCD must be configured to ignore the `weight` field:

```yaml
# In ArgoCD Application
spec:
  ignoreDifferences:
    - group: traefik.io
      kind: TraefikService
      jsonPointers:
        - /spec/weighted/services/0/weight
        - /spec/weighted/services/1/weight
```

### Governance

This is a **T2 change** (impacts manifests, deploy pipeline, ArgoCD sync flow, CLI output). Requires meeting approval before implementation.

### Estimated Effort

High — manifest generation rewrite + Traefik integration + analysis templates + CLI updates + E2E tests.

---

## P3 — Kuberhealthy (Synthetic Health Probes)

### Objective

Provide continuous, automated proof that the platform WORKS — not just that pods are UP. Enable SLA measurement and compliance.

### Current State

- Health checks are limited to pod-level liveness/readiness probes.
- No synthetic tests that exercise real user flows continuously.
- SLA claim of 99.9% has no measurement infrastructure.
- `loopzero_*` Prometheus metrics track internal state but not user-visible functionality.

### What This Enables

| Without Kuberhealthy | With Kuberhealthy |
|---|---|
| "Pods are running" = "platform is healthy" | "Synthetic deploy succeeded at 14:32" = "platform is healthy" |
| SLA based on pod uptime (misleading) | SLA based on actual user-flow success rate |
| Failures detected when users report them | Failures detected in < 5 minutes by synthetic probes |
| No canary-in-the-coal-mine for degradation | Early warning: synthetic deploy slowing = something degrading |

### Synthetic Checks to Create

| # | Check | Frequency | What It Exercises | Prometheus Metric |
|---|-------|-----------|-------------------|----|
| S1 | Deploy lifecycle | 10 min | CLI → API → build → deploy → health | `loopzero_synthetic_deploy_success` |
| S2 | Rollback lifecycle | 30 min | Deploy → rollback → verify previous version | `loopzero_synthetic_rollback_success` |
| S3 | Cross-cell connectivity | 5 min | mgmt → cell-default, cell-1, cell-2 API call | `loopzero_synthetic_cell_reachable` |
| S4 | Gitea push/pull | 5 min | Create repo → push → clone → verify | `loopzero_synthetic_gitea_success` |
| S5 | ArgoCD sync | 5 min | Create App → sync → verify healthy | `loopzero_synthetic_argocd_sync_success` |
| S6 | DNS resolution | 2 min | Resolve `*.tenant.loopzero.dev` | `loopzero_synthetic_dns_success` |
| S7 | TLS certificate validity | 1h | Check expiry > 14 days for all endpoints | `loopzero_synthetic_tls_valid` |
| S8 | PG connectivity | 1 min | Connect → query → verify | `loopzero_synthetic_pg_success` |

### SLA Measurement

With Kuberhealthy metrics feeding Prometheus, SLA becomes measurable:

```promql
# SLA = percentage of successful synthetic deploy probes over 30 days
(
  sum(increase(loopzero_synthetic_deploy_success{status="ok"}[30d]))
  /
  sum(increase(loopzero_synthetic_deploy_success[30d]))
) * 100

# Target: >= 99.9%
# Alert if below 99.5% (early warning)
```

### Deliverables

1. Helm chart `infra/helm/kuberhealthy/` for mgmt cluster.
2. Custom check images in `infra/kuberhealthy-checks/` (Go, using Kuberhealthy SDK).
3. `KuberhealthyCheck` CRDs for S1-S8 in `infra/helm/kuberhealthy/templates/`.
4. Prometheus `ServiceMonitor` for Kuberhealthy metrics.
5. Grafana dashboard `Synthetic Health` with success rates and latency trends.
6. PrometheusRule alerts:
   - `LoopZeroSyntheticDeployFailing` — 3 consecutive failures
   - `LoopZeroSyntheticCellUnreachable` — any cell unreachable > 10 min
   - `LoopZeroSLABelowTarget` — 30-day SLA < 99.5%

### Estimated Effort

Low-Medium — Kuberhealthy is lightweight. Main effort is writing custom check images.

---

## Integration Dependencies

```
P0 Chaos Mesh ─────────────────── (independent, start immediately)
       │
       │  validates resilience assumptions BEFORE adding new components
       ▼
P1 Velero ─────────────────────── (independent, can parallel with P0)
       │
       │  backup must exist BEFORE progressive delivery changes manifest structure
       ▼
P2 Argo Rollouts ──────────────── (depends on P1 backup being in place)
       │
       │  canary deploys generate metrics that synthetic checks validate
       ▼
P3 Kuberhealthy ───────────────── (depends on P2 for deploy-synthetic accuracy)
```

**P0 and P1 can run in parallel.** P2 should wait for P1 (need backup before major manifest changes). P3 should wait for P2 (synthetic deploy check needs to test canary flow).

---

## Rejected Alternatives

| Project | Why Rejected |
|---------|-------------|
| **Keptn** | Overlaps with Prometheus + ArgoCD + Argo Rollouts. 3 extra operators for already-covered features. Last release March 2025 (stale). Violates KISS/YAGNI. |
| **Flagger** | Flux ecosystem — mixing Flux + Argo creates operational tension. Argo Rollouts is more coherent with our Argo stack (Workflows + CD + Rollouts). |
| **Kubernetes Autoscaler** | Cluster Autoscaler already managed by DOKS. VPA conflicts with KEDA. Not a resilience gap. |
| **Longhorn / Rook / Ceph** | DO managed volumes sufficient. Distributed storage adds complexity without resilience value at our scale. |
| **CloudNativePG** | Using DO Managed PG (automated failover included). Only relevant if self-hosted. |
| **Istio / Linkerd** | Service mesh overhead unjustifiable. Traefik + NetworkPolicy already provide traffic isolation. |
| **Gremlin** | Paid SaaS. Chaos Mesh (already installed) provides the same capabilities, open source. |
| **Kyverno / OPA** | Nice-to-have policy governance, but not a maturity 5.0 blocker. policy-controller + PSA restricted already cover essentials. |

---

## Maturity Impact Projection

| Current | After P0+P1 | After P2 | After P3 | With 50+ tenants |
|:---:|:---:|:---:|:---:|:---:|
| 3.9 | 4.2 | 4.5 | 4.7 | **5.0** |

- **P0 (Chaos Mesh experiments):** +0.15 — closes "full chaos engineering suite" requirement.
- **P1 (Velero):** +0.15 — closes "validated incident runbooks" requirement. Enables DR drill.
- **P2 (Argo Rollouts):** +0.3 — closes "deploy resilience at scale". Prerequisite for 50+ tenants without risk.
- **P3 (Kuberhealthy):** +0.2 — closes "published SLA" and "99.9% uptime measured" requirements.
- **50+ real tenants:** +0.3 — business validation, not technical. Requires all of the above.

---

## References

- `docs/maturity-assessment.md` — current assessment and path to 5.0
- `infra/helm/chaos-mesh/` — existing Chaos Mesh installation (Sprint 8 T0.3)
- `docs/meetings/adr/0011-replicas-autoscaling-precedence-and-plan-caps.md` — scaling plan caps (relevant for Argo Rollouts canary strategy)
- CNCF Landscape (`landscape.yml`) — source of project evaluation
- Argo Rollouts Traefik integration docs — `TraefikService` weighted routing
- Velero documentation — backup schedules and S3 backend
- Kuberhealthy SDK — custom check authoring
