---
type: implementation
title: "ADR-5 enumeration source (phantom `tenants.status` column)"
---

# BLOCKER — ADR-5 enumeration source (phantom `tenants.status` column)

**Status:** BLOCKS T2.3, T3.1, T1.3-folded. Requires `cycle-plan` revision of ADR-5 + tenant-guardian re-review before `/implement` resumes.
**Found:** iteration 6 of `/implement metering-compute-loop`, before writing the money-writer.
**Date:** 2026-07-01

## The defect

The plan (ADR-5, `ListActiveTenantsWithCell` in T2.3, T3.1 enumeration, T1.3 state-literal guard, and MEET-002's `'ACTIVE'`→`'Active'` fix) all assume:

```sql
SELECT ... FROM tenants WHERE status = 'Active'
```

**`tenants.status` does not exist.** Verified against the real schema (47 migrations applied):

- `tenants` columns: `id, display_name, slug, owner_id, cell_id, created_at, updated_at, account_id` — no status/state/deleted_at/phase.
- No migration adds a status column to `tenants`.
- `pending-deletion` is a SEPARATE table (`tenants_pending_deletion`, migration 000012), not a status value.
- `subscriptions.status` exists but is BILLING status (active/past_due/canceled/…), NOT tenant lifecycle.

## The reality

Tenant lifecycle status is the **Tenant CR `status.phase`** (Kubernetes), not PG:

- `operators/api/v1/tenant_types.go`: `TenantPhase` = `Provisioning | Active | Suspended | PendingDeletion | Deleting` (mixed-case "Active" — so the *literal* the plan wanted is right; the *source* is wrong).
- Read in the operator control loop: `operators/internal/tenant/tenant_controller.go`, `tenant_deletion.go` (`tenant.Status.Phase`).

## Why 4 meetings + 2 guardians missed it

The reviews reasoned about the *design* + the Go constant `statemachine.TenantActive = "Active"` (which is real), but nobody verified the leap that it maps to a PG `tenants.status` column. This is Rule 12 (CLAUDE.md): "grep the repo FIRST, not memory." The plan's Baseline Context cited the Go constant as evidence but never a migration/schema line for the column.

## Feasible corrected enumeration (basis for the ADR-5 revision)

The API ALREADY has Tenant CR access: `api/internal/k8s/tenant_cr_manager.go` (constructs `apiv1.Tenant{}`), `api/internal/k8s/tenant_evictor.go`.

Proposed ADR-5' (for tenant-guardian review):

1. **Active-tenant source = Tenant CRs (mgmt cluster).** List Tenant CRs, filter `status.phase == TenantPhaseActive` (skip Provisioning/Suspended/PendingDeletion/Deleting). Get `tenant_id` + `cell_id` from the CR (or PG `tenants.cell_id`).
2. **Per active tenant, (project,env) enumeration stays PG** (`ListProjectsByTenant` → `naming.Namespace`) — tenant-guardian's PG-derived-namespace attribution (MEET-003) is UNCHANGED.
3. **`ListActiveTenantsWithCell` is removed as a PG query** — it becomes a K8s list. This also removes T1.3 (no SQL status literal to guard). A replacement guard could assert the enumeration filters on the `TenantPhaseActive` constant (not a hardcoded string).
4. `plan_tier` still comes from PG `subscriptions` (LEFT JOIN, EC-4) — that part of the query stays, keyed by the tenant IDs the CR list produced.

## Impact on committed work (none — all sound)

T0.1 (contracts v0.3.0), T1.1 (aliases), T1.2 (metric CHECK drift guard), T2.1 (schema 000046: cursor/config/CHECK/RLS/GRANT), T2.2 (000047 BIGINT) are all independent of this defect and correct. The schema does not reference `tenants.status`. `/implement` resumes from 5/10 once ADR-5 is revised.

## Required actions

1. `cycle-plan` revision: rewrite ADR-5 (+ T2.3 `ListActiveTenantsWithCell`, T3.1 enumeration, drop/replace T1.3) to source active tenants from Tenant CR phase.
2. tenant-guardian re-review (enumeration/attribution is their domain — MEET-003) + re-score (`/plan-confidence`) + re-sign.
3. Resume `/implement` from the 5/10 checkpoint.
