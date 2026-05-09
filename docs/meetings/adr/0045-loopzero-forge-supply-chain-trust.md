# ADR-0045: loopzero-forge supply-chain trust boundary

**Date:** 2026-05-09
**Status:** Proposed
**Plan:** docs/plans/deep-review-remediation-plan.md (T4.7)
**Closes:** find_dep_loopzero_forge_bus_factor MEDIUM

---

## Context

`loopzero-forge` is the Go package providing Argo Workflow DAG template
generation for the entire build pipeline. It is imported in 7 files in
`api/internal/argo/`. As of the Deep Review Loop (2026-05-08):

- Source: `github.com/getloopscale/loopzero-forge` (org owned by founding team)
- Adoption: zero alternative implementations; every LoopZero build flows through it
- Visibility: no ADR documenting adoption; no mirror in `loopzero` org
- Pinning: `go.mod` pins to a specific version (currently `v0.3.1`)

If `getloopscale/loopzero-forge` becomes unavailable (org takedown, account
compromise, malicious push) every new LoopZero deploy fails at build template
generation. Combined with `loopzero-packs` previously being cloned at HEAD
(closed by T4.3), the build plane had two single-points-of-supply-chain
trust without explicit governance.

## Decision

Establish a documented **supply-chain trust boundary** for `loopzero-forge` and
`loopzero-packs`, plus an operational mirror plan:

1. **loopzero-forge stays in `getloopscale/`** as the upstream source. All
   versioned releases (vX.Y.Z) are tagged in this repo. `getloopscale/` org
   has hardware-key-required signed commits + branch protection on main.

2. **Mirror to `loopzero/loopzero-forge`** as the consumed source for production
   and CI: `loopzero` org adds `loopzero-forge` as a private mirror, syncing
   only tagged releases (no auto-pull-from-main). `go.mod` references
   `github.com/loopzero/loopzero-forge` (replace directive optional).

3. **Production builds reference the mirror**, not the upstream. CI/CD
   updates `go.mod` to `github.com/loopzero/loopzero-forge` once the mirror
   exists. Until then, this ADR documents the CONFIRMED risk and the
   migration path; finding remains MEDIUM until mirror is operational.

4. **loopzero-packs is digest-pinned** (closed by T4.3 — `LOOPZERO_PACKS_REF=v0.4.0`
   build-arg, defaulting to a specific tag). Combined with this ADR, the
   build plane has zero floating supply-chain references.

5. **SBOM diff alert** — CI compares SBOM artifacts of loopzero-packs and
   loopzero-forge between consecutive deploys. Any unexpected dependency
   change triggers an alert. (See infra/helm/monitoring T5.5 wiring for
   `loopzero_release_signed_total` and `loopzero_sbom_generated_total` alerts.)

## Rationale

The bus-factor risk is real but the mitigation cost depends on the failure
mode:

| Failure mode | Probability | Mitigation |
|---|---|---|
| getloopscale/ account suspended | low | Mirror in loopzero provides fallback within 1 day |
| Malicious push to loopzero-forge main | low | Branch protection + signed commits + tagged-only mirror |
| Tag retroactively repointed | very low | go.sum pins module hash; refuses changed content |
| Maintainer abandonment | medium (single-maintainer) | Org-level ownership in loopzero guarantees access continuity |

**Alternatives rejected:**

- **Vendor in-tree** — would bloat the api/ module by ~3000 LOC and
  abandon upstream improvements. Forking is acceptable as last resort
  but not the first defense.
- **Pin to forked SHA in go.mod replace** — works but invisible governance.
  Future contributors won't know why the replace exists.
- **No mirror, document the risk only** — already what happened pre-T4.7.
  Insufficient.

## Consequences

**Enables:**
- Production resilience to upstream takedown (1-day RTO via mirror).
- Auditable supply-chain trust path (every binary in registry traces back
  to a tagged commit in loopzero/).

**Constrains:**
- Mirror needs maintenance (sync new releases). Automation in
  `.github/workflows/sync-loopzero-forge.yml` (separate work item, scoped to
  Sprint 9 readiness).
- `go.mod` URL change requires coordinated update across 7 files in
  `api/internal/argo/` once mirror is in place.

## Status post-ADR

This ADR is **Proposed** until the mirror operationally exists. Once
`loopzero/loopzero-forge` is created and loopzero-forge v0.3.2 is the first release
mirrored, the ADR moves to **Accepted**. find_dep_loopzero_forge_bus_factor
remains MEDIUM until then; downgrades to LOW once mirror is operational.

## References

- find_dep_loopzero_forge_bus_factor (deep-review-remediation Phase 3)
- T4.7 of docs/plans/deep-review-remediation-plan.md
- Threat model: flow_deploy_happy → "Compromised loopzero-forge mirror"
  (review-output/analysis/threat_models/threat_model_report.md)
