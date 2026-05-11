# Resilience Roadmap — Path to Maturity 4.0

**Date:** 2026-05-06 (updated 2026-05-11)
**Author:** Engineering team
**Status:** IN PROGRESS — P0 experiments created, P1 schedules configured, P2 DONE, P3 pending
**Objective:** Close all resilience gaps blocking maturity 4.0

---

## Context

LoopZero is at maturity **3.49/4.0** (modular audit baseline 2026-05-11, `docs/audits/2026-05-11/SUMMARY.md`). The path to higher maturity requires:

- Full chaos engineering suite (validated, not just installed)
- 50+ real tenants
- Published SLA
- Validated incident runbooks (DR drill completed)
- 99.9% uptime measured over 30 days

This roadmap addresses the **technical resilience gaps** through 4 targeted integrations from the CNCF Landscape.

---

## Integrations Summary

| # | Project | CNCF Status | Priority | Gap Closed | Status |
|---|---------|-------------|----------|------------|--------|
| 1 | Chaos Mesh | Incubating | **P0** | Chaos engineering suite | 🟡 Experiments created, CI + LIVE validation pending |
| 2 | Velero | CNCF project | **P1** | Backup/DR for K8s resources | 🟡 Schedules configured, restore validation + DR drill pending |
| 3 | Argo Rollouts | CNCF (Argo) | **P2** | Progressive delivery (canary) | ✅ DONE (2026-05-07) |
| 4 | Kuberhealthy | Sandbox | **P3** | Synthetic health probes for SLA | ❌ Not started |

---

## P0 — Chaos Mesh (Experiments)

### Objective

Validate that LoopZero self-heals under real failure conditions. Prove resilience with evidence, not assumptions.

### Current State (updated 2026-05-11)

- Chaos Mesh **2.7.0 CRDs registered** in all 4 clusters (Sprint 8 T0.3, `infra/helm/chaos-mesh/`).
- **9 experiment manifests created** in `infra/tests/chaos/manifests/`:
  - `pod-kill-controller.yaml` — kill controller pod, verify API + builds survive
  - `pod-kill-api-leader.yaml` — kill API leader, verify failover <90s
  - `pg-network-latency.yaml` — inject 500ms PG latency, verify timeouts <30s
  - `argocd-crash.yaml` — kill argocd-server, verify other cells unaffected
  - `build-cluster-network-partition.yaml` — isolate build cluster, verify graceful queuing
  - `gitea-outage.yaml` — drop Gitea traffic, verify fail-fast with clear error
  - `pg-connection-storm.yaml` — StressChaos on PG pool, verify circuit breaker fires
  - `node-unschedulable.yaml` — cordon build node, verify workflow reschedule
  - `simultaneous-api-controller-crash.yaml` — dual crash, verify system recovery
- **Ginkgo test runner** exists: `infra/tests/chaos/chaos_test.go`
- **Missing:** CI workflow `chaos-suite.yml` (weekly, like `cell2-soaks.yml`)
- **Missing:** LIVE execution evidence — experiments never ran against real system

### Remaining Work

| Task | Status | Description |
|------|--------|-------------|
| Create `chaos-suite.yml` CI workflow | ❌ Pending | Weekly scheduled run in GitHub Actions |
| Execute experiments LIVE (at least 1 round) | ❌ Pending | Apply manifests, observe, record evidence |
| Validate steady-state SLO assertions | ❌ Pending | Prometheus alerts fire correctly during faults |
| Document results in evidence format | ❌ Pending | JUnit XML + Prometheus screenshots |

### Success Criteria

- All 9 experiments executed LIVE at least once with documented results.
- `chaos-suite.yml` runs weekly in CI.
- Steady-state SLO assertions pass (API health, build success rate, cell isolation).

### Estimated Remaining Effort

Low — experiments exist. Effort is execution + CI wiring, not design.

---

## P1 — Velero (Backup & Disaster Recovery)

### Objective

Guarantee that a catastrophic cluster failure does not result in permanent data loss. Enable validated restore runbooks for SLA compliance.

### Current State (updated 2026-05-11)

- **PostgreSQL:** DO Managed PG has automatic daily backups + PITR. ✅ Covered.
- **Velero Helm chart:** `infra/helm/velero/` exists (Chart.yaml, values.yaml, values-schedules.yaml). ✅ Created.
- **5 backup schedules configured** in `values-schedules.yaml`:
  - `loopzero-crds-backup` — 10 CRDs across all namespaces, every 6h, 30d retention
  - `loopzero-argocd-backup` — argocd namespace, every 6h, 30d retention
  - `loopzero-gitea-backup` — gitea metadata (no PVC data, repos re-pushable), daily, 14d retention
  - `loopzero-secrets-backup` — loopzero-system secrets, every 6h, 30d retention
  - `loopzero-full-backup` — all namespaces (excl kube-*), weekly Sunday 03:00 UTC, 7d retention
- **CI workflow:** `velero-backup-check.yml` deployed. ✅ Monitoring in place.
- **Missing:** Restore validation — never tested a real restore.
- **Missing:** DR drill — never destroyed + restored from Velero backup.
- **Missing:** DO Spaces bucket `loopzero-velero-backups` provisioned via Pulumi.

### Remaining Work

| Task | Status | Description |
|------|--------|-------------|
| Provision DO Spaces bucket via Pulumi L1 | ❌ Pending | `loopzero-velero-backups` + IAM credentials |
| Deploy Velero to production clusters | ❌ Pending | Helm install with DO Spaces backend |
| Execute first backup round | ❌ Pending | Verify all 5 schedules produce successful backups |
| Restore validation | ❌ Pending | Restore CRDs + ArgoCD state to clean namespace |
| DR drill | ❌ Pending | Destroy mgmt cluster → restore from Velero → validate |
| Prometheus metrics + alert | ❌ Pending | `VeleroBackupStale` alert if last backup >12h |

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

### Success Criteria

- All 5 schedules producing successful backups for 7+ days.
- At least 1 restore validation completed with documented results.
- Quarterly DR drill established (first drill completes successfully).
- `VeleroBackupStale` alert configured and tested.

### Estimated Remaining Effort

Medium — provisioning + deploy + restore validation + DR drill documentation.

---

## P2 — Argo Rollouts (Progressive Delivery) — ✅ DONE (2026-05-07)

Completed. Full details in CLAUDE.md § Sprint Plan "Argo Rollouts Progressive Delivery".

**Summary:** Argo Rollouts 1.7.2 with TraefikService weighted routing, per-plan canary steps (Starter 2-step, Pro 3-step, Team 4-step), AnalysisTemplate hooks, auto-abort on readiness failure (`progressDeadlineSeconds=300`), KEDA targets Rollout. 80/80 GREEN warm-infra, 71/72 GREEN fresh-cleanroom. 15 tasks (T0.1-T5.2), 6 fixes (F-16 through F-22).

---

## P3 — Kuberhealthy (Synthetic Health Probes)

### Objective

Provide continuous, automated proof that the platform WORKS — not just that pods are UP. Enable SLA measurement and compliance.

### Current State

- No synthetic tests that exercise real user flows continuously.
- SLA claim of 99.9% has no measurement infrastructure.
- `loopzero_*` Prometheus metrics track internal state but not user-visible functionality.

### What This Enables

| Without Kuberhealthy | With Kuberhealthy |
|---|---|
| "Pods are running" = "platform is healthy" | "Synthetic deploy succeeded at 14:32" = "platform is healthy" |
| SLA based on pod uptime (misleading) | SLA based on actual user-flow success rate |
| Failures detected when users report them | Failures detected in < 5 minutes by synthetic probes |

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
3. `KuberhealthyCheck` CRDs for S1-S8.
4. Prometheus `ServiceMonitor` for Kuberhealthy metrics.
5. Grafana dashboard `Synthetic Health` with success rates and latency trends.
6. PrometheusRule alerts: `LoopZeroSyntheticDeployFailing`, `LoopZeroSyntheticCellUnreachable`, `LoopZeroSLABelowTarget`.

### Estimated Effort

Low-Medium — Kuberhealthy is lightweight. Main effort is writing custom check images.

---

## Execution Order (Updated)

The original dependency chain assumed P2 would wait for P1. In practice, P2 was completed first (2026-05-07). Updated order:

```
P2 Argo Rollouts ─────── ✅ DONE (2026-05-07)

P0 Chaos Mesh ─────────── 🟡 Experiments exist, need CI + LIVE execution
       │                       (independent, start immediately)
       │
P1 Velero ─────────────── 🟡 Schedules exist, need deploy + restore validation
       │                       (independent, can parallel with P0)
       │
       │  backup + chaos both validated BEFORE synthetic probes
       ▼
P3 Kuberhealthy ────────── ❌ Depends on P0+P1 (chaos validates resilience,
                                backup validates recovery — both needed for
                                synthetic probes to be meaningful)
```

**P0 and P1 can run in parallel.** P3 should wait for both P0+P1 (synthetic probes are only meaningful if the system has proven resilience and recovery).

---

## Rejected Alternatives

| Project | Why Rejected |
|---------|-------------|
| **Keptn** | Overlaps with Prometheus + ArgoCD + Argo Rollouts. Last release March 2025 (stale). Violates KISS/YAGNI. |
| **Flagger** | Flux ecosystem — mixing Flux + Argo creates operational tension. Argo Rollouts is more coherent with our Argo stack. |
| **Istio / Linkerd** | Service mesh overhead unjustifiable. Traefik + NetworkPolicy already provide traffic isolation. |
| **Gremlin** | Paid SaaS. Chaos Mesh (already installed) provides the same capabilities, open source. |
| **Kyverno / OPA** | Nice-to-have policy governance. policy-controller + PSA restricted already cover essentials. |
| **CloudNativePG** | Using DO Managed PG (automated failover included). Only relevant if self-hosted. |
| **Longhorn / Rook / Ceph** | DO managed volumes sufficient. Distributed storage adds complexity without resilience value at our scale. |

---

## Maturity Impact Projection

Starting baseline: **3.49/4.0** (modular audit 2026-05-11).

| Integration | Maturity increment (est.) | Gate it closes |
|-------------|:---:|---|
| **P0** Chaos Mesh (LIVE validated) | +0.05 | "Full chaos engineering suite" |
| **P1** Velero (restore validated + DR drill) | +0.05 | "Validated incident runbooks" |
| **P2** Argo Rollouts ✅ | (already counted in 3.49) | "Progressive delivery" |
| **P3** Kuberhealthy (synthetic probes) | +0.05 | "99.9% uptime measured", "Published SLA" |
| **Business** (50+ tenants, 30-day uptime) | +0.36 | "Real production validation" |

> Note: Maturity is capped at 4.0 per modular audit methodology. The largest remaining gap is **business validation** (real tenants + real uptime), not technical tooling. P0+P1+P3 together contribute ~+0.15; the remaining +0.36 requires production usage.

---

## References

- `docs/audits/2026-05-11/SUMMARY.md` — current modular audit baseline (12 modules, cap 4.0)
- `infra/helm/chaos-mesh/` — Chaos Mesh installation (Sprint 8 T0.3)
- `infra/tests/chaos/manifests/` — 9 chaos experiment YAMLs + Ginkgo runner
- `infra/helm/velero/` — Velero chart + 5 backup schedules
- `.github/workflows/velero-backup-check.yml` — Velero CI monitoring
- CLAUDE.md § Sprint Plan "Argo Rollouts Progressive Delivery" — P2 completion details
