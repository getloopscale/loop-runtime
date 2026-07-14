-- Forward-only discipline: down provided per house convention but never run in
-- prod. Drops the M7 storage_objects table.
DROP TABLE IF EXISTS storage_objects;
