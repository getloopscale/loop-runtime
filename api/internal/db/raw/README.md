<!-- SPDX-License-Identifier: Apache-2.0 -->
<!-- Copyright 2025-2026 The LoopZero Authors -->

# `api/internal/db/raw` — Escape Hatch for sqlc-Unparseable Queries

This subpackage hosts the handful of SQL queries that **sqlc 1.31.1
cannot parse**, primarily window-function-based ranked subqueries
(`ROW_NUMBER() OVER (PARTITION BY … ORDER BY …)` inside DELETE/UPDATE
WHERE IN subqueries), recursive CTEs, and a small set of dynamic-WHERE
shapes where `sqlc.narg()` would explode into too many branches.

Per ADR-1 §6 D5 of `docs/decisions/2026-05-29-sqlc-adoption.md`, the
raw subpackage is the **canonical escape hatch**, not a fallback for
"sqlc is too verbose" or "I prefer raw SQL". The bar to add a query
here is: sqlc fails to generate code for it, AND the query is not
addressing a sensitive table (see `sensitive_tables.go`).

## Scope (hard limit)

- ≤ **5%** of all queries in `api/internal/db/queries/` may have raw
  counterparts. As of the initial scaffold, the raw catalogue has
  **1 entry** (`DeleteOldBuilds` window function) of 21 queries
  total = ~5%. If a future contribution would push this above 5%,
  open a T2 governance request to either (a) raise the cap with
  justification, or (b) refactor the query into sqlc-parseable shape.

## Forbidden tables (enforced by AST gate)

The following tables CANNOT be touched by raw queries — they go
through sqlc-generated code exclusively, with no exception path:

- `secrets` — AES-256-GCM encryption boundary at the wrapper
  (SEC-001); any raw access path bypasses the helper and risks
  ciphertext/plaintext leak
- `audit_log` — M-3 audit-callsite-diff invariant requires uniform
  shape across all 6 prod call sites; raw access would let one
  caller drift
- `bootstrap` / `users` / `tenants` / `tenant_members` /
  `subscriptions` — the bootstrap chain has TOCTOU race fixed via
  MEET-053 D-01 SERIALIZABLE-via-TxFromContext; raw insertion would
  miss the serializer
- `tenants_pending_deletion` / `tenant_tombstones` — T1.4
  introduced these tables with explicit lifecycle invariants that
  must dispatch through TenantDeletionStoreSqlc
- `cells` — placement primitives; admin override path goes through
  PostgresCellStore.PinTenantToCell with the 4-step pre-flight

The committed list lives in `sensitive_tables.go`. The
`pkg/quality/tenantpredicate/` gate scans every `.go` file under
`api/internal/db/raw/` and rejects any SQL that references a
sensitive table.

## When to add a query here

You may add a raw query ONLY IF all of the following hold:

1. The query needs SQL features sqlc 1.31.1 cannot generate code
   for: window functions in DELETE/UPDATE subqueries, recursive
   CTEs, multi-column dynamic WHERE with N>3 nullable columns.
2. The target table is NOT in `sensitive_tables.go`.
3. You ran `sqlc generate` against the query in a `queries/*.sql`
   variant and confirmed the failure (attach the error message in
   the wrapper's docstring).
4. Adding this query keeps `len(raw/) / len(queries/) ≤ 0.05`.

Then:
- Implement the query in a new file `raw/{table_name}_raw.go`.
- Document the sqlc failure mode + the workaround in the file
  docstring.
- Add a tombstone comment in the relevant `queries/*.sql` file
  pointing to the raw implementation.

## Current contents

- `delete_old_builds.go` — Build GC ranked DELETE. Window function
  `ROW_NUMBER() OVER (PARTITION BY project_id, environment ORDER BY
  created_at DESC)` inside a DELETE … WHERE IN (SELECT id FROM
  (subquery) ranked WHERE rn <= $2) — sqlc 1.31.1 cannot parse
  the `rn` column reference across multiple CTE / scalar-subquery
  rewrites.

## Maintenance

Run `task quality:gates` to verify the AST gate is not violated.
The gate fails with a clear message if any raw query references a
sensitive table OR if the 5% cap would be exceeded.
