# ADR-0048: minio/madmin-go AGPL-3.0 license decision

**Date:** 2026-05-09
**Status:** Proposed (pending legal review)
**Plan:** docs/plans/deep-review-remediation-plan.md (T9.1)
**Closes:** find_dep_madmin_agpl MEDIUM

---

## Context

`operators/go.mod` directly imports `github.com/minio/madmin-go/v3 v3.0.110`,
licensed under **AGPL-3.0**. This is the SDK we use in
`operators/internal/minio/provisioner.go` for tenant MinIO bucket/user
provisioning (264 LOC, AWS SigV4 authentication).

`minio-go` (the object storage data-plane client) is Apache-2.0 and is
NOT affected by this ADR.

AGPL-3.0 specific concerns for SaaS:
- AGPL §13 ("Remote Network Interaction"): when AGPL software is offered
  to users over a network, the operator MUST offer those users access
  to the corresponding source code.
- LoopZero offers admin operations (bucket provisioning, IAM management) to
  tenants over a network. Whether tenants are "users" of the AGPL'd
  madmin code is the legal question.

The Deep Review Loop flagged this as MEDIUM (find_dep_madmin_agpl). Until
this ADR is **Accepted** by legal counsel, the finding remains open.

## Options

### Option A — Accept AGPL with isolation (lowest engineering cost)

Keep madmin-go but enforce isolation:

1. **Trust boundary**: madmin operations are exclusively server-side, in
   the platform-operator binary. Tenants never receive the AGPL code or
   the linked binary.
2. **Source disclosure**: LoopZero's source repository remains public on
   GitHub satisfies the §6 (Conveying Non-Source Forms) requirement
   for the operator binaries. Tenants who wish to inspect can do so.
3. **ADR explicit**: this ADR is the legal record that the operator
   (LoopZero platform team) has assessed §13 applicability and concluded
   tenants are NOT direct users of the AGPL code (madmin runs in our
   namespace, not theirs; the tenant interface is `loopzero` CLI which is
   Apache-2.0).

**Risk:** legal counsel may disagree with the tenant-not-a-user
interpretation.

### Option B — Replace madmin with self-implemented AWS SigV4 client

Re-implement the subset of madmin we use (~10 admin operations: bucket
create/delete, user create/delete, policy attach/detach) using `aws-sdk-go-v2`
(Apache-2.0) or hand-rolled SigV4. Estimated effort: 200-400 LOC + tests.

**Cost:** engineering time + ongoing maintenance. We'd own bug fixes.

### Option C — Remove MinIO admin features

Delegate bucket/user provisioning to operator-as-a-script: a one-shot
job that runs mc CLI commands. mc is also AGPL but as a separate process
(not linked) the §0 "modified version" trigger doesn't apply.

**Cost:** loss of programmatic provisioning; operational complexity.

### Option D — Switch to S3-compatible alternative without AGPL admin

Use S3 native (DO Spaces, AWS S3) where bucket/user mgmt is via cloud
SDK (Apache-2.0). Already partially true in production (DO Spaces).

**Cost:** drop MinIO support from local k3d dev clusters; "Local equals
production" rule (CLAUDE.md) breaks.

## Decision

**Tentative Option A**, pending legal counsel review. Until counsel
signs off, the finding stays Proposed and:

1. New code MUST NOT add new madmin-go imports outside
   `operators/internal/minio/provisioner.go`.
2. We add a CI lint to prevent madmin-go imports in api/, cli/, cloud/,
   and pkg/ (where Apache-2.0 is the only acceptable license).
3. The lint EXCEPTION_LIST has a single entry: the existing import in
   `operators/internal/minio/provisioner.go` (operator-binary-only).

If legal counsel rejects Option A, the fallback is Option B (re-implement
the ~10 admin operations on top of aws-sdk-go-v2). Option B is engineering
cost only — not legally blocking.

## Rationale

| Option | Eng cost | Legal cost | Operational cost |
|---|---|---|---|
| A | 0 (lint to prevent new imports) | requires counsel | requires ADR + tracked exception |
| B | 200-400 LOC + tests | 0 | 0 |
| C | medium (delete code + operational scripts) | 0 | local dev breaks |
| D | low | 0 | drops MinIO from local stack |

Option A is chosen as default because it's reversible (we can switch to
Option B at any time without data migration). The lint locks scope.

## Consequences

**Enables:**
- Continued use of madmin-go's typed admin API (no rewrite cost).
- Local k3d dev parity (MinIO with full admin) preserved.

**Constrains:**
- New code MUST go through the lint check to ensure no AGPL leakage.
- Legal review is a prerequisite for moving Status: Proposed → Accepted.
- Re-evaluation triggers documented below.

## Re-evaluation triggers

This ADR should be revisited if:
- Legal counsel rejects the tenant-not-a-user interpretation.
- madmin-go drops AGPL or relicenses (improbable).
- LoopZero offers the operator binary directly to tenants (e.g., self-hosted
  edition) — the §13 applicability flips immediately.
- Tenants gain shell access to operator pods (currently impossible).

## References

- find_dep_madmin_agpl (deep-review-remediation T9.1)
- AGPL-3.0 §13 (https://www.gnu.org/licenses/agpl-3.0.en.html)
- operators/internal/minio/provisioner.go (the sole consumer)
