-- Drops the record of rollbacks. The history it holds is lost.
--
-- CAUTION when running: the engine that writes these rows fails every rollback
-- without the table. Roll the engine back with the schema.

BEGIN;

DROP TABLE IF EXISTS rollbacks;

COMMIT;
