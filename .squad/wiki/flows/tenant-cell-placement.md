---
type: Flow
title: "Which cell does a new tenant land in?"
description: "Decision flow, 10 steps, traced through real call sites."
resource: "api/internal/bootstrap/bootstraphttp/bootstrap.go"
tags:
  - flow
  - decision
  - tenant-cell-placement
sources:
  - id: flow-root
    resource: "api/internal/placement/engine.go#L180"
  - id: step-1
    resource: "api/internal/bootstrap/bootstraphttp/bootstrap.go#L134"
  - id: step-2
    resource: "api/internal/placement/serializer.go#L96"
  - id: step-3
    resource: "api/internal/placement/serializer.go#L98"
  - id: step-4
    resource: "api/internal/placement/engine.go#L145"
  - id: step-5
    resource: "api/internal/placement/engine.go#L153"
  - id: step-6
    resource: "api/internal/placement/engine.go#L167"
  - id: step-7
    resource: "api/internal/placement/engine.go#L174"
  - id: step-8
    resource: "api/internal/bootstrap/bootstraphttp/bootstrap.go#L136"
  - id: step-9
    resource: "api/internal/placement/serializer.go#L92"
  - id: step-10
    resource: "api/internal/placement/dynamic_router.go#L145"
generated:
  by: loop-system-cartography/0.1.0
  at: 2026-09-11T20:08:10-03:00
status: stable
---

# What it is

**Which cell does a new tenant land in?** — a `decision` flow of 10 steps,
traced from the call site that starts it.[^flow-root]

| | |
|---|---|
| **Trigger** | POST /internal/bootstrap — a tenant is being created and has no cell_id yet |
| **Outcome** | exactly one cell_id, written to tenants.cell_id in the same serialized transaction as the tenant INSERT; or a hard failure when every cell is full |


A decision made **once per tenant**, inside one serialized transaction, and never revisited.

The TOCTOU fix is the shape of the whole flow: the cell choice and the tenant `INSERT` run in the
same transaction, under `pg_advisory_xact_lock` with `SET LOCAL lock_timeout = '30s'`. Two
concurrent bootstraps cannot both read the same counts and both pick the same cell.

Afterwards, routing has no decision left in it. `Resolve(cellID)` is a hash lookup.
`tenants.cell_id` is the source of truth; the Tenant CR's `spec.cellID` is a mirror.

There is **no automatic rebalancing, by design** — DNS continuity, state affinity, a hot spot
being an upgrade-tier signal, and auditability are the four reasons recorded.


# Schema

The steps, in order. Every row is anchored to the line that performs it.


| # | Actor | Step | Hands to | Condition | Data | Anchor |
|---|---|---|---|---|---|---|
| 1 | `api.routes` | the bootstrap handler calls AssignAndExecute so the cell pick and the tenant INSERT share one transaction — the TOCTOU fix for concurrent tenant creation | `api.placement` | — | `callback closing over BootstrapRequest` | `api/internal/bootstrap/bootstraphttp/bootstrap.go:134` |
| 2 | `api.placement` | enter the critical section: BEGIN, SET LOCAL lock_timeout = '30s', then SELECT pg_advisory_xact_lock(PlacementLockID) | `ds.postgres` | e.serialize != nil — production wires NewAdvisoryLockSerializer; a nil serializer is single-writer test mode | `PlacementLockID 0x706C636D746C636B` | `api/internal/placement/serializer.go:96` |
| 3 | `api.placement` | lock acquisition timed out — return the typed ErrPlacementLockTimeout so the caller can tell 'system busy' from 'system broken' | `api.routes` | SQLSTATE 55P03 after 30s | `ErrPlacementLockTimeout` | `api/internal/placement/serializer.go:98` |
| 4 | `api.placement` | ListCells enumerates the cells the router currently knows | `api.placement` | len(cells) == 0 -> 'placement: no cells available', the decision cannot be made | `[]CellConfig` | `api/internal/placement/engine.go:145` |
| 5 | `api.placement` | CountByCell reads the current tenant count per cell from Postgres | `ds.postgres` | — | `map[cellID]int` | `api/internal/placement/engine.go:153` |
| 6 | `api.placement` | bin-pack: walk every cell and keep the one with the fewest tenants that is still under maxPerCell | `api.placement` | count < e.maxPerCell && count < bestCount | `bestCell, bestCount` | `api/internal/placement/engine.go:167` |
| 7 | `api.placement` | no cell qualified — every cell is at capacity; fail loudly rather than overfill one | `api.routes` | bestCell == "" | `error 'placement: all cells at capacity (%d tenants each)'` | `api/internal/placement/engine.go:174` |
| 8 | `api.db.store` | the callback runs INSIDE the same locked transaction: Bootstrap writes the user, the tenant with cell_id, and the membership | `ds.postgres` | a cell was picked | `req.CellID -> tenants.cell_id (the canonical source of truth)` | `api/internal/bootstrap/bootstraphttp/bootstrap.go:136` |
| 9 | `api.placement` | the transaction commits and the advisory lock is released with it — a concurrent creation that was waiting now sees the updated counts | `ds.postgres` | — | `committed assignment` | `api/internal/placement/serializer.go:92` |
| 10 | `api.placement` | afterwards the assignment is STICKY: GetCell reads tenants.cell_id and Resolve is a pure hash lookup with no decision left in it | `api.placement` | the tenant already has a cell_id | `CellConfig{kubeconfig, ArgoCD URL+token, base domain}` | `api/internal/placement/dynamic_router.go:145` |

# Relationships


- The engine and the router are in
  [`/components/multi-cell-routing.md`](/components/multi-cell-routing.md).
- The lock is `api/internal/lock` in [`/components/persistence.md`](/components/persistence.md);
  the row written is `tenants` in
  [`/entities/database-schema.md`](/entities/database-schema.md).
- The CR that mirrors the decision is [`/entities/crd-tenant.md`](/entities/crd-tenant.md).
- The cells themselves are [`/components/cluster-topology.md`](/components/cluster-topology.md).
- Moving a tenant afterwards is `cap.admin_operate` in
  [`/entities/api-capabilities.md`](/entities/api-capabilities.md) — the only path that does.


# Evidence

Every step in the table above carries the `file:line` that performs it. Every step of this flow is listed in the `sources` block below.

[^flow-root]: `api/internal/placement/engine.go:180`
[^step-1]: `api/internal/bootstrap/bootstraphttp/bootstrap.go:134`
[^step-2]: `api/internal/placement/serializer.go:96`
[^step-3]: `api/internal/placement/serializer.go:98`
[^step-4]: `api/internal/placement/engine.go:145`
[^step-5]: `api/internal/placement/engine.go:153`
[^step-6]: `api/internal/placement/engine.go:167`
[^step-7]: `api/internal/placement/engine.go:174`
[^step-8]: `api/internal/bootstrap/bootstraphttp/bootstrap.go:136`
[^step-9]: `api/internal/placement/serializer.go:92`
[^step-10]: `api/internal/placement/dynamic_router.go:145`
