# ADR-0007: ArgoCD Application Model — Per-Environment vs Per-Project

**Status:** PROPOSED
**Date:** 2026-03-31
**Decision Required By:** Before multi-tenant scale
**Origin:** MEET-2026-03-31-023 (Architect vs Distributed Systems divergence, governance Rule 4)
**Deciders:** Principal Architect, Distributed Systems Engineer, SRE, GitOps Engineer

---

## Context

The LoopZero platform currently creates **one ArgoCD Application per (tenant, environment)** pair.
All projects within a tenant-environment share the same Application. This was documented as a
known systemic pattern in CLAUDE.md:

> "Single ArgoCD Application per tenant — One Application manages all projects in an environment.
> If ANY resource is Degraded, ALL notifications are blocked."

Code: `operators/internal/controller/tenant_gitops.go:62-88`
Naming: `{tenantName}-{envName}` (e.g., `tn-loopzero-production`)

### Problem

When one project deploys a broken manifest, ArgoCD marks the entire Application as Degraded.
This blocks:
1. **Notification callbacks** — ArgoCD only fires webhooks when the Application is Synced+Healthy at the Application level
2. **Convergence loop** — `GetAppStatus` returns unhealthy, preventing activation of other projects' releases

Result: one broken project blocks ALL projects in that environment for that tenant.

### Divergence

In MEET-2026-03-31-023, Principal Architect and Distributed Systems Engineer disagreed:

- **Principal Architect:** Keep per-environment model. Fix via notification scoping at resource level. Per-project would multiply Applications (150 vs 30 for moderate scale) with operational overhead.
- **Distributed Systems:** Per-project Applications required. Blast radius is unbounded. Notification scoping doesn't fix the convergence loop path.

---

## Options

### Option A: Per-Project ArgoCD Applications

**Model:** One Application per `(tenant, project, environment)`.
**Naming:** `{tenantName}-{envName}-{projectName}`

**Changes required:**
- `operators/internal/controller/tenant_gitops.go` — create N Applications per environment (one per project)
- `api/internal/argocd/sync_client.go` — app name derivation changes
- `api/internal/services/sync_callback_activator.go` — `ParseArgoAppName` parsing changes
- `api/internal/services/release_convergence_loop.go` — app name resolution changes
- `api/internal/services/container_delivery.go` — app name for TriggerSync changes
- `api/internal/services/rollback_orchestrator.go` — app name resolution changes

**Pros:**
- Eliminates blast radius completely — one project's failure is isolated
- Simpler health model — Application health = project health
- Notification callbacks are project-specific
- Convergence loop checks per-project health

**Cons:**
- Application count scales as `tenants x environments x projects` (could reach 150+ quickly)
- Migration from old model requires deleting shared Applications (ArgoCD deletes managed resources)
- ArgoCD controller load increases with Application count
- More ArgoCD RBAC complexity (AppProject per tenant already exists)

**Blast radius analysis (10 tenants, 3 envs, 5 projects):**
- Current: 30 Applications
- Per-project: 150 Applications
- ArgoCD documentation suggests performance degrades above 300 Applications without sharding

### Option B: Notification Scoping at Resource Level

**Model:** Keep per-environment Applications. Fix notification controller to filter by resource labels.

**Changes required:**
- ArgoCD notification ConfigMap — add resource-level filters
- `api/internal/services/release_convergence_loop.go` — per-resource health filtering in `checkSync`
- `api/internal/services/sync_callback_activator.go` — filter callback by resource annotations

**Pros:**
- No Application count increase
- No migration risk
- Simpler operational model
- ArgoCD performance unaffected

**Cons:**
- Convergence loop `GetAppStatus` already does per-resource filtering (partially implemented)
- Notification path still fires at Application level — ArgoCD notification controller doesn't support resource-level triggering natively
- Requires custom notification controller or webhook transformation layer
- Does NOT fix the fundamental issue that Application health = worst resource health

### Option C: Per-Resource Health Filtering in Convergence Loop (Interim Mitigation)

**Model:** Keep per-environment Applications. Convergence loop already filters health per-project via `deployedApps` parameter. Extend to fully ignore health of resources not belonging to the current release.

**Changes required:**
- `api/internal/argocd/sync_client.go` — `GetAppStatus` already filters by `deployedApps` list
- Verify this filtering is complete and correct

**Pros:**
- Minimal change (may already be working)
- No migration risk
- Addresses the polling path (convergence loop)

**Cons:**
- Does NOT address the notification callback path (push-based activation)
- Partial solution — push-based activation remains blocked by Application-level health
- Users experience 10s delay (convergence loop interval) instead of real-time activation when callback is blocked

---

## Evaluation Criteria

| Criterion | Weight | Option A (Per-Project) | Option B (Notification Scoping) | Option C (Interim) |
|-----------|--------|------------------------|--------------------------------|--------------------|
| Blast radius elimination | 40% | Full (10/10) | Partial (5/10) | Partial for polling only (4/10) |
| Migration risk | 20% | High — Application rename causes resource recreation (3/10) | None (10/10) | None (10/10) |
| Operational complexity | 15% | Higher — more Applications to manage (5/10) | Medium — custom notification logic (6/10) | Low (9/10) |
| ArgoCD performance | 10% | Risk above 300 Apps (6/10) | No impact (10/10) | No impact (10/10) |
| Implementation effort | 15% | High — 6 files, T3 change (4/10) | High — requires ArgoCD notification customization (4/10) | Low — verify existing code (9/10) |

**Weighted scores:**
- Option A: 6.15
- Option B: 6.35
- Option C: 7.15

---

## Recommendation

**Implement Option C now (interim). Plan Option A for post-launch (when scale justifies it).**

Rationale:
1. Option C is already partially implemented (`GetAppStatus` filters by `deployedApps`)
2. The convergence loop (polling path) is the safety net — even if callbacks are blocked, releases activate within 10s
3. Option A's migration risk is significant (Application rename = resource recreation)
4. The problem only becomes critical at scale (multiple tenants, multiple projects per environment)
5. Pre-release phase: we should fix what breaks NOW, not what breaks at 100 tenants

**Trigger for Option A:** When any tenant has > 5 projects in a single environment, OR when callback-based activation failure rate exceeds 10%.

---

## Decision

**PENDING** — requires meeting vote.

---

## Consequences

If Option C is chosen:
- Document the limitation in operational runbooks
- Add metric `loopzero_callback_activation_blocked` to track how often callbacks fail due to Application-level health
- Set alert threshold for migration trigger
- Add `LoopZeroArgoCDAppCountWarning` alert (already exists at 200 Applications)

If Option A is chosen later:
- Migration plan must be zero-downtime (create new Applications before deleting old ones)
- Test with maximum expected Application count before rollout
- Consider ArgoCD controller sharding if count exceeds 300
