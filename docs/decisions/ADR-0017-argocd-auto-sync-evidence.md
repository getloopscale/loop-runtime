# ADR-0017 — ArgoCD Auto-Sync Re-enable Evidence Pack

**Status:** Proposed (governance meeting required)
**Date:** 2026-04-29
**Supersedes:** Decisions documented in `infra/argocd-bootstrap/README.md` post-CHG-2026-04-28-LAYERED-BOOTSTRAP iter 25.
**Related:** `docs/plans/end-to-end-deploy-validation-plan.md` Phase 6 / T6.1.

## Context

CHG-2026-04-28-LAYERED-BOOTSTRAP iter 24 discovered B53 — ArgoCD Applications
in `infra/argocd-bootstrap/apps/` were missing `helm.values` / `helm.valuesObject`,
causing `automated.selfHeal: true` to overwrite helmfile state with chart
defaults (e.g., minio went from 1 standalone pod to 16 distributed pods,
gitea got recreated with HA Postgres subchart).

Iter 25 mitigated by removing the `automated:` block from all 38 Apps,
converting ArgoCD into drift-detection-only mode (Day-2 observability).

Iter 26-27 closed the structural gap: 29 Apps now have `helm.valuesObject`
inline aligned to helmfile values, and the remaining 9 Apps with `path:`
sources have a populated Gitea repo (`loopzero-platform/charts`) with all 32
charts pushed.

This ADR captures the empirical evidence accumulated through iter 22-30 + 1-4
(end-to-end-deploy-validation-plan) to inform a future governance meeting on
re-enabling `automated.selfHeal: true`.

## Decision Required

Should the 38 ArgoCD Applications re-enable `automated.selfHeal: true` AND
`automated.prune: true` to provide Day-2 drift correction?

## Options Considered

### A. Re-enable selfHeal + prune on ALL 38 Apps

**Pros:**
- True Day-2 management (drift heals automatically).
- Matches LoopZero Cloud spec intent (CHG-2026-04-28 ADR D11).

**Cons:**
- Requires every chart's helmfile values to be perfectly mirrored in
  `helm.valuesObject`. Iter 26 covered 29/38 (76%) — the 9 Apps with
  `path:` sources depend on Gitea being permanently populated, which is
  fragile (Gitea repo loss = Apps go OutOfSync immediately).
- Risk: any future helmfile values change without simultaneous Apps update
  causes drift detection to fire (alert noise) or selfHeal to revert
  the helmfile change (data loss).

### B. Re-enable selfHeal + prune only on the 29 Apps with valuesObject

**Pros:**
- Lower risk: covered Apps have explicit values aligned to helmfile.
- Mixed mode is operationally clear: Apps with valuesObject are
  Day-2 managed; Apps with path: source are Day-1 managed (helmfile only).

**Cons:**
- Inconsistent behavior across 38 Apps may confuse operators ("which
  Apps trigger selfHeal?").
- Doesn't move the needle on the 9 path-based Apps — they need a
  different fix (multi-source, ApplicationSet, or migration to
  valuesObject form).

### C. Keep current state (drift-detection-only, no automated:)

**Pros:**
- Zero risk of overwriting helmfile state.
- Status quo since iter 25, validated through Phase 1-3 of the
  end-to-end plan without incident.

**Cons:**
- Manual sync required for every change (operator runs `argocd app sync`
  or kubectl apply).
- Day-2 drift detection without correction means alerts must be
  observed and acted upon by humans.

## Empirical Evidence (iter 22-30 + 1-4)

| Metric | Value |
|---|---|
| Total Apps in argocd-bootstrap/apps/ | 38 |
| Apps with helm.valuesObject (iter 26) | 29 |
| Apps with path: source (require Gitea populated) | 9 |
| Apps that previously caused drift catastrophe (iter 24) | 4 (gitea, minio, ory-kratos, ory-hydra) |
| Bugs introduced by ArgoCD overwrite (B52-B57, all auto-fixable IF Apps had valuesObject) | 6 |
| Days of stable drift-detection-only operation (iter 25 → end-to-end iter 4) | ~1 day local validation |
| Days of stable operation in DOKS production with selfHeal | 0 (never enabled with current Apps spec) |

**Bugs B52-B57 root cause** is entirely B53 (Apps without valuesObject).
With iter 26 fix (29/38 Apps have valuesObject) those specific bugs
should NOT recur on those 29 Apps.

The remaining 9 Apps with `path:` source (loopzero-api, loopzero-controller,
tenant-controller, loopzero-cloud, smoke-test, runtime-rbac, runtime-admission-
policies, runtime-cluster-issuer, runtime-network-policies) are local
charts that point to Gitea-hosted source. Those require either:
- Multi-source App spec (chart upstream + git overlay)
- ApplicationSet with valuesFrom ConfigMap
- Refactor to valuesObject form (less ideal for non-published charts)

## Recommendation

**Adopt Option B + plan Option C migration:**

1. **Immediate (no governance approval needed)**: Keep drift-detection-only
   for the 9 `path:` Apps. They remain in current state.

2. **Governance approval required**: Enable `automated.selfHeal: true` +
   `automated.prune: true` for the 29 Apps with `helm.valuesObject`. These
   have empirical protection against B53 (their values match helmfile).

3. **Sprint 9 follow-up**: Refactor the 9 `path:` Apps to multi-source or
   inline valuesObject before adopting Option A. This requires populating
   Gitea consistently as part of `loopzero platform install` (extending
   end-to-end-deploy-validation-plan T3.2 work).

## Acceptance Criteria for Re-enabling

Before flipping selfHeal:true on ANY App, the following must be true:

- [ ] App has `spec.source.helm.valuesObject` matching helmfile values
  (programmatically verified by `TestB53ArgocdAppsValuesObject`).
- [ ] Helmfile sync against the same profile (standalone-multi, dev, prod)
  produces identical K8s objects to ArgoCD render (validated by
  `helmfile template -e <env>` vs `argocd app diff <app>`).
- [ ] Smoke test passes after 5min of selfHeal:true (no rogue
  StatefulSets, no replicas:16 minio, no HA postgres subchart).

## Consequences

**If adopted (Option B):**
- 29 Apps gain Day-2 drift correction.
- Operators can safely apply hotfix changes via PR → Git → ArgoCD
  reconciles. No manual `argocd app sync`.
- 9 Apps remain manual until Sprint 9 follow-up.

**If rejected (status quo):**
- Continued operational burden (manual sync per change).
- Day-2 drift accumulates undetected unless humans monitor.

## Tracking

- Plan-of-record: `docs/plans/end-to-end-deploy-validation-plan.md` T6.1
- Governance meeting: TBD — this ADR is the input pack.
- Implementation if approved: PR opening `automated:` blocks on the 29 Apps
  identified by `TestB53ArgocdAppsValuesObject`.
