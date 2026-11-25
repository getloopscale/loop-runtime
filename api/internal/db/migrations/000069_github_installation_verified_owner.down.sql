-- Drops the record of who confirmed an installation's owner.
--
-- CAUTION when running: the engine that reads these columns does not start
-- its GitHub queries without them. Roll the engine back with the schema.

BEGIN;

ALTER TABLE github_installations
  DROP COLUMN IF EXISTS verified_at,
  DROP COLUMN IF EXISTS verified_by;

COMMIT;
