# ADR-0009: ArgoCD Auto-Sync Evaluation

**Status:** PROPOSED
**Date:** 2026-03-31
**Decision Required By:** Phase 4 implementation
**Origin:** ADR-0008 Cell-Based Architecture, Phase 4 (P4-2)
**Deciders:** Principal Architect, GitOps Engineer, Distributed Systems, SRE

---

## Context

LoopZero currently uses ArgoCD with **manual sync policy** (`automated: nil`). The API
explicitly triggers sync via `TriggerSync()` after pushing manifests to Gitea.

With ApplicationSets (Phase 4), ArgoCD can manage Applications declaratively.
The question is whether to also enable **auto-sync** — ArgoCD automatically syncs
when it detects a Git change, eliminating the need for `TriggerSync()`.

## Options

### Option A: Enable Auto-Sync (`selfHeal: false`)

```yaml
syncPolicy:
  automated:
    prune: true
    selfHeal: false
  retry:
    limit: 3
    backoff:
      duration: 5s
      factor: 2
      maxDuration: 1m
```

**What changes:**
- ArgoCD detects Gitea push → auto-syncs (no API `TriggerSync` needed)
- `selfHeal: false` — ArgoCD does NOT revert manual changes (avoids conflicts)
- `prune: true` — ArgoCD removes resources deleted from Git

**Simplification in LoopZero code:**
- Remove: `TriggerSync()` calls from `ContainerDeliveryActivator`
- Remove: `triggerSyncWithRetry()` in delivery flow
- Simplify: `ReleaseConvergenceLoop` — only monitors health, no sync trigger
- Remove: `retriggerSyncIfNeeded()` — ArgoCD handles retries
- Keep: `SyncCallbackActivator` — still needed for push-based activation

**Risks:**
- Race condition: API pushes to Gitea, ArgoCD auto-syncs BEFORE release record is created
- Timing: ArgoCD polling interval (default 3min) vs webhook (near-instant)
- Loss of control: API cannot delay sync (e.g., for staged rollouts)

### Option B: Keep Manual Sync

No code changes. ArgoCD syncs only when API calls `TriggerSync()`.

**Why keep:**
- API controls exact timing of sync
- Correlates sync with release lifecycle
- No race between Git push and release creation
- Proven model (current production)

**Cost of keeping:**
- `TriggerSync()` + retry logic + `TerminateOperation()` remain
- `retriggerSyncIfNeeded()` in convergence loop remains
- 5 compensation mechanisms stay in codebase

## Evaluation

| Criterion | Auto-Sync | Manual Sync |
|-----------|-----------|-------------|
| Code simplicity | Removes ~200 lines of sync trigger code | Current complexity |
| Timing control | ArgoCD decides when (3min poll or webhook) | API decides when |
| Race condition risk | Git push before release record | None (sync after record) |
| Failure recovery | ArgoCD retries automatically | API retries manually (3 attempts) |
| Observability | Less control over correlation | Explicit correlation |

## Recommendation

**DEFERRED — evaluate after ApplicationSets are stable in production.**

Rationale:
1. Changing sync model AND Application model simultaneously is too risky
2. ApplicationSets are a bigger change — validate first
3. Manual sync works and is proven
4. If ApplicationSets work well, auto-sync can be evaluated with real data

**Trigger for re-evaluation:** After 30 days of ApplicationSets in production with
zero sync-related incidents.

## Decision

**PENDING** — requires Phase 4 meeting after ApplicationSets are deployed.
