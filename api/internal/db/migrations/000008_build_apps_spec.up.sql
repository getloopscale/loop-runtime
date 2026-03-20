-- Migration 000008: Store app specs on builds for reconcile loop
-- C-01 fix: The build reconcile loop needs to know what apps were submitted
-- with each build (name, type, port, secrets) to generate manifests and compute
-- image references when the workflow completes. Previously this data was only
-- available in the HTTP callback payload.
ALTER TABLE builds ADD COLUMN IF NOT EXISTS apps_spec JSONB;
