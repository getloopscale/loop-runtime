# ADR-0050: ClusterImagePolicy mode default flip — warn → enforce

**Date:** 2026-05-10
**Status:** Accepted
**Plan:** docs/plans/deep-review-2026-05-10-remediation-plan.md (T0.5)
**Closes:** find_infra_008 + find_sec_supply_chain_toxic_combo (P0 CRITICAL)
**Supersedes:** prior deferral noted in `infra/helm/admission-policies/values.yaml` comment
that scoped enforce to Sprint 9. Production override in
`infra/helmfile/environments/prod.yaml` (`policyMode: warn`) is retained
until Sprint 9 cosign keyless cutover.

---

## Context

The deep review of 2026-05-10 (`review-output/final_report.md`) flagged
the `ClusterImagePolicy` in warn-mode as one half of a CRITICAL toxic
combination: unsigned platform images get admitted with only an audit
log, and the audit log is itself a low-signal channel that operators do
not monitor in real time. Combined with the absence of cosign keyless
verification (Sprint 9 scope), this leaves a window where a compromised
build pipeline could push an unsigned image to DOCR/Harbor and have it
admitted to the cluster.

The previous position (deep-review-remediation T8.6, March 2026) deferred
the deny-flip to Sprint 9 to land atomically with cosign keyless
infrastructure. The new deep review (May 2026) elevates this to P0
because: (1) the flip is independent of keyless — key-based cosign
signing in the build pipeline already produces signed images for all
platform components; (2) the toxic combination with `find_sec_idor_buildstore`
(closed in T0.4 of the same plan) widens the blast radius if an attacker
ever pivots through an unsigned image; (3) the lint added in
deep-review-remediation T8.6 (`check_clusterimagepolicy_mode.sh`) requires
an ADR for any flip, and the ADR is now justified.

## Decision

**Flip `clusterImagePolicy.mode` default from `warn` to `enforce` in the
bootstrap chart `infra/helm/admission-policies/values.yaml`. Production
keeps `warn` via `prod.yaml` override until Sprint 9 readiness review.**

Effective scope:
- `dev.yaml` + `standalone.yaml` + `standalone-multi.yaml` inherit the new
  default — local k3d immediately runs in enforce mode.
- `prod.yaml` keeps `policyMode: warn` until the Sprint 9 cutover ADR
  documents the dual-signing window + rollback runbook.

The lint `check_clusterimagepolicy_mode.sh` is updated to recognize
ADR-0050 (current) and the Sprint 9 ADR (future) as valid bypass
references.

## Rationale

| Alternative | Rejected because |
|---|---|
| Flip default + flip prod at once | Production cutover needs the staged dual-signing window (warn + enforce side-by-side ≥ 7 days), runbook, and PagerDuty for `policy.violation` alerts — none of which are ready yet |
| Wait for Sprint 9 | Local + dev get zero benefit from the lint that already exists; deep review marked the gap as CRITICAL |
| Add a per-image allow-list bypass | Adds complexity and an audit blind spot; the in-cluster cosign signing already covers all platform images |

## Consequences

**Enables:**
- Local + dev clusters reject unsigned images at admission (T0.5 DoD).
- The toxic combination flagged in find_sec_supply_chain is half-closed
  (other half = keyless verification in Sprint 9).
- `check_clusterimagepolicy_mode.sh` references this ADR via the
  `find_infra_008|deny-flip` pattern — no lint regression.

**Constrains:**
- Anyone building a custom image and deploying it locally MUST sign it
  with the local cosign key (`cosign-signing-key` Secret) OR push to the
  `k3d-loopzero-registry:5000` exception namespace.
- Production rollout (`prod.yaml` flip) is now governed by the Sprint 9
  readiness review ADR (yet to be written) which must include:
  1. 7-day dual-signing window confirmation.
  2. Rollback runbook with the exact `helm upgrade` revert command.
  3. PagerDuty alerting on `policy_controller_audit_total` rate.
  4. Confirmation that all 4 split operators + loopzero-api + loopzero-cloud
     have keyless signing verified.

## Re-evaluation triggers

- Any production incident traced to admission policy false-positive.
- A platform image scanner finds an unsigned image admitted post-flip.
- Sprint 9 readiness review completes — at which point a new ADR replaces
  the `prod.yaml warn` override.

## References

- find_infra_008 (deep-review-2026-05-10)
- find_sec_supply_chain_toxic_combo (deep-review-2026-05-10)
- `infra/scripts/check_clusterimagepolicy_mode.sh` — governance lint
- `infra/helm/admission-policies/values.yaml` — applied flip
