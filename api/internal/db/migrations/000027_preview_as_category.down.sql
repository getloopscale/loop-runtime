-- Down migration 000027 — DEV ONLY.
--
-- WARNING: Per CLAUDE.md Rule 11 (forward-only), this MUST NOT run in production.
-- Forward-only migrations means rollback in prod = data restore from backup.
--
-- This down exists for dev k3d cleanroom + drill cluster reset cycles.

BEGIN;

DROP POLICY IF EXISTS preview_slot_tenant_isolation ON preview_slot;
DROP TABLE IF EXISTS preview_slot;

DROP INDEX IF EXISTS idx_environment_targets_kind;
ALTER TABLE environment_targets DROP COLUMN IF EXISTS kind;

-- Note: deleted preview-pr-N rows from up migration are NOT restored.
-- Clean break per D8 is irreversible by design.

COMMIT;
