# ADR-0049: Immutable artifact promotion strategy

**Date:** 2026-05-09
**Status:** Accepted
**Plan:** docs/plans/deep-review-remediation-plan.md (T4.2)
**Closes:** find_cicd_003 HIGH

---

## Context

Pre-T4.2 build-images.yml pushed two tag families per build:

- **Mutable env tag**: `:dev`, `:staging`, `:prod` — overwritten on every push to the corresponding branch.
- **Immutable SHA tag**: `:<7-char-git-sha>` — unique per commit.

Production deployment used `kubectl rollout restart deploy/loopzero-api`,
which re-pulls the current `:prod` tag. The new image bytes can differ
from what was tested in `:staging` because the `:prod` tag is mutable
between staging promotion and production rollout.

find_cicd_003 (HIGH) flagged this as an artifact-promotion gap: the
"exact bytes ran in staging AND production" invariant cannot be proven.
Compliance regulators (SOC 2, PCI Level 1) require this invariant for
the production deploy chain.

## Decision

**Two-tier tag policy + SHA-as-canonical for production:**

1. **Build pipeline keeps both tag families** (`:dev`/`:staging`/`:prod` +
   `:<sha>`). The mutable env tag is the convenience layer for Tilt's
   `live_update`, `kubectl set image`, and ArgoCD ImageUpdater. The SHA
   tag is the immutable artifact reference.

2. **Production deployment MUST use `:sha`** via `kubectl set image
   deploy/loopzero-api loopzero-api=registry.digitalocean.com/loopzero/loopzero-api:<sha>`.
   `kubectl rollout restart` against the `:prod` tag is **forbidden**
   for production rollouts (acceptable only for emergency manual rollback
   when an immediate restart with last-known-good is required).

3. **infra-restart.yml refactor** (separate work item per EC-4 staging
   pattern):
   - Add input `image_sha` (required for production rollouts).
   - Replace `kubectl rollout restart` with `kubectl set image` keyed on
     the SHA.
   - Workflow_run trigger from build-images.yml passes the SHA through.

4. **Pre-prod (develop, homolog)** deliberately keeps the mutable env
   tag for Tilt + dev loop ergonomics — "Local equals production" rule
   does not extend to image immutability for dev convenience.

## Rationale

| Alternative | Rejected because |
|---|---|
| Drop env tags, use only `:sha` | Loses Tilt `live_update` + ArgoCD ImageUpdater convenience for dev loop. Half the team's daily workflow becomes manual SHA lookup. |
| Make `:prod` immutable (signed + DOCR retention lock) | DOCR doesn't support tag immutability natively; would require external signing service + verification gate. High engineering cost for low marginal benefit over SHA pinning. |
| Use OCI image signature only (no SHA tag) | Cosign signature verification requires extra deploy-time infrastructure. SHA tag + immutable digest is simpler. |
| Status quo (mutable everywhere) | What we had — silent artifact divergence between staging and prod. |

## Consequences

**Enables:**
- Auditable production deploy chain (SHA → image bytes is 1:1).
- Reproducible rollback (production prior SHA always retrievable).
- Compliance answer for SOC 2 / PCI Level 1 (cited in audits).

**Constrains:**
- infra-restart.yml refactor required before this ADR moves from
  Accepted to Live (separate work item — see Migration Plan).
- Production runbooks must be updated: emergency rollback procedure
  remains "kubectl set image with last-known-good SHA"; no `kubectl
  rollout restart` against mutable tags in prod.
- Pre-prod tags stay mutable — operators must remember the dev/prod
  boundary in their mental model.

## Migration plan

1. **Land this ADR + build-images.yml comment** (this commit). Documents
   the policy without behavioral change.
2. **Refactor infra-restart.yml** (separate PR) to accept `image_sha`
   input + `kubectl set image` instead of rollout restart. Tag-pinned
   actions get the SHA via workflow_run output from build-images.yml.
3. **Update docs/runbooks/production-deploy.md** with the new rollout
   command. Remove `kubectl rollout restart` from production runbook
   sections (keep for emergency only).
4. **Update CLAUDE.md "Bash blocked" section** to add `kubectl rollout
   restart` against mgmt deploys (conditional — allowed for dev, blocked
   for prod cluster contexts).

## References

- find_cicd_003 (deep-review-remediation T4.2)
- ADR-0048 (loopzero-forge supply-chain trust — same plan iteration)
- CLAUDE.md Rule 10 — "Local equals production" (with Tilt convenience exception documented here)
