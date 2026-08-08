---
type: runbook
title: "Runbook: Database Migration Safety & Rollback"
---

# Runbook: Database Migration Safety & Rollback

> Scope: the API-owned migration set in `api/internal/db/migrations/` (the cloud
> service does NOT run migrations). Addresses find_data_005 and find_data_008.

## Conventions (mandatory)

1. **Forward-only.** Migrations are `NNNNNN_<slug>.up.sql` only. There are no
   `.down.sql` files (find_data_008 — accepted policy). Rollback is via PITR, not
   reverse migrations. The CI `migration-safety` job rejects `migrate ... force`.

2. **Data changes use `ON CONFLICT DO UPDATE`, never `DELETE + INSERT`.**
   A bare `DELETE FROM <table>;` followed by `INSERT` (as in the archived
   `000023_update_plan_limits_tiers`) is **prohibited**: if the INSERT fails
   mid-run the table is left empty and plan enforcement **fails open**
   (find_data_005). The active seed (`000002`) correctly uses
   `INSERT ... ON CONFLICT DO NOTHING`; value changes must use
   `INSERT ... ON CONFLICT (<pk>) DO UPDATE SET col = EXCLUDED.col`.

   Correct pattern for a plan-limit change:
   ```sql
   INSERT INTO plan_limits (plan, metric, hard_limit, soft_limit)
   VALUES ('pro', 'projects', 5, NULL)
   ON CONFLICT (plan, metric) DO UPDATE
     SET hard_limit = EXCLUDED.hard_limit, soft_limit = EXCLUDED.soft_limit;
   ```

3. **Additive, idempotent DDL.** Use `ADD COLUMN IF NOT EXISTS` /
   `CREATE TABLE IF NOT EXISTS` so re-running a migration is a no-op
   (e.g., `000006_add_stripe_subscription_item_id` — verified idempotent LIVE).

4. **Privileges.** `000001` creates the `pg_stat_statements` extension, which
   requires a **superuser**. Apply migrations as the `postgres` role (not the
   app role `loopzero`). The `task db:migrate` credential must match the running
   instance's secret (`postgresql`/`loopzero-db-credentials`), not a hardcoded value.

## Pre-Migration Gate (PITR)

Before applying any migration to production:

1. Confirm a recent verified PITR snapshot of the managed PostgreSQL exists
   (DigitalOcean managed PG: daily backups + PITR window). Record the snapshot
   timestamp in the change record.
2. Apply the migration in a transaction where possible; for non-transactional
   DDL, apply during a low-traffic window.
3. Smoke-test plan enforcement and billing reads after apply.

> **CI follow-up (T7.2):** add a release-workflow gate that fails the prod
> migration step unless a snapshot newer than the deploy SHA's commit time is
> confirmed. The existing `migration-safety` job covers `migrate force`
> rejection but not the snapshot precondition.

## Rollback (RTO)

There is no reverse migration. To roll back a bad migration:

1. **Schema-additive migration** (e.g., a new nullable column): usually no
   rollback needed — the column is ignored by the previous app version.
2. **Destructive/data migration:** restore from the pre-migration PITR snapshot.
   Expected RTO on DO managed PG: minutes-to-tens-of-minutes depending on DB
   size. This is the reason destructive data changes MUST be gated on a verified
   snapshot (above).

## Verification log

- `000006_add_stripe_subscription_item_id`: applied LIVE on k3d-loopzero-mgmt PG
  (full chain 000001-000006, exit 0) + idempotent re-apply confirmed (2026-05-22).
