# ADR-0046: builds table foreign-key trade-off

**Date:** 2026-05-09
**Status:** Accepted
**Plan:** docs/plans/deep-review-remediation-plan.md (T7.4)
**Closes:** find_data_006 MEDIUM

---

## Context

The `builds` table (migration 000001 lines 120-139) declares `tenant_id`
and `project_id` as plain `TEXT NOT NULL` with NO foreign-key references
to `tenants(id)` or `projects(id)`. This is a deliberate design choice
predating this ADR but never formally documented.

Consequences observed in deep-review-remediation Phase 5 (find_data_006):

1. **Orphan rows on tenant deletion:** when a tenant is deleted, its
   builds remain in the database with the dangling `tenant_id`. Without
   FK, no CASCADE/RESTRICT/SET NULL is enforced.
2. **Orphan rows on project rename:** the `projects.name` is the
   `builds.project_id` (CLAUDE.md "Project name vs project ID" pattern).
   Renaming a project leaves stale build rows.
3. **No referential integrity:** a contributor can write an arbitrary
   tenant_id to builds without it existing in tenants.

## Decision

**Accept the no-FK design**, with explicit rationale and three
mitigations:

1. **Reaper-driven cleanup** instead of FK CASCADE: a background
   `BuildGC` service (already exists in api/internal/db/build_gc_store.go)
   handles archival + cleanup of builds older than retention. The reaper
   reads from `tenants` to detect dangling tenant_ids and archive their
   builds.

2. **Build history is INTENTIONALLY independent of tenant lifetime.**
   Per CLAUDE.md "audit trail" requirement and Sprint 11 billing usage
   queries: a build that built an image for tenant X must remain
   queryable EVEN AFTER tenant X is deleted (for compliance + billing
   reconciliation). FK CASCADE would erase audit history. FK RESTRICT
   would block tenant deletion.

3. **Monitoring + invariant test (T0.5 class):**
   `loopzero_orphan_builds_total` metric will be wired to detect dangling
   rows post-tenant-delete. Alert if rate > 0 for 24h indicates the
   reaper is behind.

## Rationale

| Alternative | Rejected because |
|---|---|
| Add FK with `ON DELETE CASCADE` | Erases audit trail required for billing reconciliation + compliance |
| Add FK with `ON DELETE RESTRICT` | Blocks tenant deletion until manual archive — breaks the soft-delete UX of T0.2 |
| Add FK with `ON DELETE SET NULL` | tenant_id is NOT NULL — would require schema change + every consumer of builds.tenant_id to handle NULL |
| Status quo (no FK, no metric) | What we had — silent orphans, find_data_006 baseline |

The chosen design (no FK + reaper + orphan metric) preserves audit
history, preserves soft-delete UX, and provides observability that the
naive FK options sacrificed.

## Consequences

**Enables:**
- Post-deletion build queries for billing/audit (Sprint 11).
- Free tenant lifecycle (no FK blocking soft-delete).

**Constrains:**
- Requires the BuildGC reaper to be running for cleanup.
- Requires `loopzero_orphan_builds_total` metric + alert (separate work item,
  scoped to next observability iteration).
- Contributors must remember that `builds.tenant_id` is referentially
  weak — do NOT assume joinability without filtering archived rows.

## References

- find_data_006 (deep-review-remediation T7.4)
- ADR-0045 (loopzero-forge supply-chain trust — same plan iteration)
- CLAUDE.md "Project name vs project ID" pattern
- api/internal/db/build_gc_store.go (existing reaper)
