-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Unifies cell and cluster 1:1. ADR docs/decisions.md,
-- BACKLOG.md B-005.
--
-- The catalogue had THREE cells for TWO machines. Three independent sources measured that
-- `cell-default` and `cell-1` are the same cluster under two names:
--   * infra/helmfile/environments/dev.yaml       — "runtime cluster maps to cell-1"
--   * infra/helmfile/environments/drill.yaml     — "cell-default + cell-1 -> runtimeCluster"
--   * 000006_cells.up.sql:120                    — cell-1 is "Canonical first cell. Maps to runtime-1"
-- and the LIVE audit of 2026-05-01 found three `active` cells for TWO ClusterCredentials.
-- The third identity never had a machine.
--
-- That is why this migration MERGES identities rather than just renaming them. The order of the
-- two phases is MANDATORY: `cell-2` may only become `cell-1` AFTER the old `cell-1` has ceased to
-- exist. Doing both at once would collapse distinct identities without leaving a trace.
--
-- There is no foreign key to `cells(cell_id)` on any of the six columns (`git grep
-- "REFERENCES cells"` returns nothing), so the merge is a set of UPDATEs and needs no
-- downtime window. It also means nothing in the database prevents an orphan `cell_id` —
-- the reason phase 4 replaces the sentinel with an explicit failure.

-- ---------------------------------------------------------------------------------------------
-- Phase 1 — merges `cell-default` + `cell-1` into the cell the machine really is: `cell-0`.
-- ---------------------------------------------------------------------------------------------
UPDATE tenants                  SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE builds                   SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE releases                 SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE leader_generations       SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE preview_environments     SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');
UPDATE release_app_attestations SET cell_id = 'cell-0' WHERE cell_id IN ('cell-default', 'cell-1');

-- The data rows have already left the two identities above, so deleting the duplicates is
-- safe. `cell-1` may only be dropped HERE while it still means the old identity — after
-- phase 2 it becomes the renumbering of `cell-2`, and deleting it would destroy the second
-- cell. That is why the guard below exists: this migrator REAPPLIES every file on each
-- run (infra/scripts/production/run-migrations.sh), so 000006 inserts
-- `cell-default`/`cell-1`/`cell-2` again and this migration runs on top once more. Measured on
-- the app-dev droplet on 2026-08-10: without the guard, the second pass tried to rename
-- `cell-default` to a `cell-0` that already existed. This whole phase is written to be
-- re-runnable without changing the result.
DELETE FROM cells WHERE cell_id = 'cell-1'
   AND EXISTS (SELECT 1 FROM cells c WHERE c.cell_id = 'cell-2');

UPDATE cells
   SET cell_id      = 'cell-0',
       display_name = 'Cell 0',
       notes        = 'Primeira cell. 1:1 com o cluster loopruntime-cell-0. Absorveu cell-default ' -- english-only: applied migration history stays verbatim; migration 000067 rewrites this text in English
                   || '(que era sentinela de "ainda nao resolvido", nao uma cell) e cell-1 ' -- english-only: applied migration history stays verbatim; migration 000067 rewrites this text in English
                   || '(que nomeava esta mesma maquina). ADR 2026-08-09.' -- english-only: applied migration history stays verbatim; migration 000067 rewrites this text in English
 WHERE cell_id = 'cell-default'
   AND NOT EXISTS (SELECT 1 FROM cells c WHERE c.cell_id = 'cell-0');

-- If `cell-0` already existed (re-run), the old row simply disappears.
DELETE FROM cells WHERE cell_id = 'cell-default';

-- ---------------------------------------------------------------------------------------------
-- Phase 2 — only now is `cell-1` free. Renumbers `cell-2`, preserving its `status`.
-- ---------------------------------------------------------------------------------------------
UPDATE tenants                  SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE builds                   SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE releases                 SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE leader_generations       SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE preview_environments     SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';
UPDATE release_app_attestations SET cell_id = 'cell-1' WHERE cell_id = 'cell-2';

UPDATE cells
   SET cell_id      = 'cell-1',
       display_name = 'Cell 1',
       notes        = 'Segunda cell. 1:1 com o cluster loopruntime-cell-1. Era cell-2; renumerada ' -- english-only: applied migration history stays verbatim; migration 000067 rewrites this text in English
                   || 'quando a fusao liberou o numero. O status nao muda aqui — quem admite ' -- english-only: applied migration history stays verbatim; migration 000067 rewrites this text in English
                   || 'trafego continua sendo decidido pelo canary. ADR 2026-08-09.' -- english-only: applied migration history stays verbatim; migration 000067 rewrites this text in English
 WHERE cell_id = 'cell-2'
   AND NOT EXISTS (SELECT 1 FROM cells c WHERE c.cell_id = 'cell-1');

-- Likewise: on a re-run `cell-1` is already the renumbered one, and the `cell-2` reinserted by
-- 000006 only needs to disappear.
DELETE FROM cells WHERE cell_id = 'cell-2';

-- ---------------------------------------------------------------------------------------------
-- Phase 3 — the defaults stop writing a sentinel.
--
-- `tenants` and `leader_generations` do NOT have the trigger (tenants is the source it reads
-- from), so they need a real default: `cell-0`, which is now a real cell and not a marker.
-- The four tables WITH the trigger lose their default on purpose — omitting the column now
-- produces NULL, which from here on is the only signal of "not yet resolved".
-- ---------------------------------------------------------------------------------------------
ALTER TABLE tenants                  ALTER COLUMN cell_id SET DEFAULT 'cell-0';
ALTER TABLE leader_generations       ALTER COLUMN cell_id SET DEFAULT 'cell-0';

ALTER TABLE builds                   ALTER COLUMN cell_id DROP DEFAULT;
ALTER TABLE releases                 ALTER COLUMN cell_id DROP DEFAULT;
ALTER TABLE preview_environments     ALTER COLUMN cell_id DROP DEFAULT;
ALTER TABLE release_app_attestations ALTER COLUMN cell_id DROP DEFAULT;

-- ---------------------------------------------------------------------------------------------
-- Phase 4 — the trigger stops using a magic value and starts failing loudly.
--
-- Before, `cell-default` meant TWO things at once: "inheritance pending" and "last resort
-- when the tenant has no cell". The merge makes that untenable — `cell-0` is now a real cell
-- with tenants, and keeping it as a sentinel would make the trigger OVERWRITE every row
-- legitimately placed in cell-0.
--
-- `NULL` becomes the only signal of "not resolved". The trigger is BEFORE INSERT and runs before
-- the NOT NULL check, so an INSERT that omits the column is resolved before the constraint looks
-- at the row — documented Postgres behaviour, and proven by
-- api/tests/integration/cell_identity_unification_test.go, which exists because asserting without
-- proving is what this work avoids.
--
-- And when the tenant has no cell either, the row is no longer written. A build whose cell does
-- not resolve is a build that should not exist; writing it with a sentinel pushes the defect to
-- the deploy, where it shows up as wrong routing instead of a data error
-- (.claude/rules/error-handling.md § 2 — fail fast, loud and clear).
CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN
            RAISE EXCEPTION
                'cell_id nao resolvido para tenant % na tabela %: o tenant nao existe ou nao tem cell', -- english-only: applied migration history stays verbatim; migration 000067 rewrites this text in English
                NEW.tenant_id, TG_TABLE_NAME;
        END IF;
    END IF;
    RETURN NEW;
END;
$$;
