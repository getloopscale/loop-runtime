-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverts 000061 AS FAR AS IT IS HONEST TO REVERT.
--
-- Phase 1 of the up migration was a MERGE: the rows that were in `cell-default` and the ones in
-- `cell-1` all became `cell-0`, and the information of which was which no longer exists anywhere.
-- A down that "undid" that would have to invent the split, and a down that invents data is worse
-- than a down that does not exist: it hands back a database that looks like the old one and is not.
--
-- So this down does what can be done without lying:
--   * restores the numbering (`cell-1` goes back to `cell-2`, `cell-0` goes back to `cell-default`);
--   * restores the defaults and the trigger to their previous form, with the sentinel;
--   * does NOT recreate the `cell-1` catalogue row, and does NOT try to split the merged rows.
-- The result is the old schema with the merged data. Whoever needs the exact previous state
-- restores a backup — which is the right operation to undo a merge.

UPDATE tenants                  SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE builds                   SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE releases                 SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE leader_generations       SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE preview_environments     SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE release_app_attestations SET cell_id = 'cell-2' WHERE cell_id = 'cell-1';
UPDATE cells SET cell_id = 'cell-2', display_name = 'Cell 2 (runtime-2)' WHERE cell_id = 'cell-1';

UPDATE tenants                  SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE builds                   SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE releases                 SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE leader_generations       SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE preview_environments     SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE release_app_attestations SET cell_id = 'cell-default' WHERE cell_id = 'cell-0';
UPDATE cells SET cell_id = 'cell-default', display_name = 'Default Cell' WHERE cell_id = 'cell-0';

ALTER TABLE tenants                  ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE leader_generations       ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE builds                   ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE releases                 ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE preview_environments     ALTER COLUMN cell_id SET DEFAULT 'cell-default';
ALTER TABLE release_app_attestations ALTER COLUMN cell_id SET DEFAULT 'cell-default';

CREATE OR REPLACE FUNCTION populate_cell_id_from_tenant() RETURNS trigger
    LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.cell_id = 'cell-default' OR NEW.cell_id IS NULL THEN
        SELECT cell_id INTO NEW.cell_id FROM tenants WHERE id = NEW.tenant_id;
        IF NEW.cell_id IS NULL THEN NEW.cell_id := 'cell-default'; END IF;
    END IF;
    RETURN NEW;
END;
$$;
