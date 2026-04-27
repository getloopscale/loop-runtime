-- Migration 000010: EMERGENCY RLS DISABLE
-- Forward-fix migration for MEET-2026-03-31-024.
-- DO NOT APPLY unless RLS is causing production issues.
-- This file exists as a pre-written rollback — apply only if needed.
--
-- To use: rename to enable sequential ordering, then run task db:migrate.
-- After RLS issue is fixed, re-enable via a new forward migration.

-- IMPORTANT: This migration is intentionally a NO-OP.
-- To activate, uncomment the lines below:

-- ALTER TABLE builds DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE releases DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE environment_targets DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE secrets DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE projects DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE tenant_members DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE audit_log DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE idempotency_keys DISABLE ROW LEVEL SECURITY;

SELECT 1; -- no-op placeholder
