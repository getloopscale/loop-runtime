-- Migration 000007: activate cell-2 in dev (flip status from 'maintenance' to 'active').
--
-- cell-2-activation-plan T2.2 / ADR D6 (production deferred to Sprint 9).
-- ADR: docs/meetings/adr/MEET-INFRA-NORMALIZE-T3-multi-cell.md
-- Plan: docs/plans/cell-2-activation-plan.md
--
-- WARNING: This migration MUST NOT be applied to production until Sprint 9
-- prod canary readiness review APPROVES Stage 4 (per ADR D6 in cell-2-
-- activation-plan). Pre-release governance: engineering applies migrations
-- to prod manually via `task db:migrate` against the prod PG. Reviewer of
-- the PR that introduces this file MUST verify it lands in dev only.
--
-- Pre-conditions (verified before applying):
--   * cell2-activation-gates.sh 12/12 PASS in the target environment
--   * Stage 1 + Stage 2 canary scripts ready to fire after this migration
--   * Operator sign-off in PR description (cell-2-activation.md checklist)
--
-- Idempotent: WHERE clause filters status='maintenance', so re-running the
-- migration after status has already changed is a no-op. Re-running while
-- status='maintenance' commits the flip exactly once (UPDATE returns 0/1
-- depending on prior state).
--
-- Reversal: forward-only migrations only (CLAUDE.md Rule). To revert
-- cell-2 to 'maintenance' or 'draining' issue a NEW migration
-- 000008_<purpose>.up.sql with the desired UPDATE statement.

UPDATE cells
   SET status = 'active',
       notes = notes || E'\n[' || now()::text || '] Activated by cell-2-activation-plan T2.2 (DEV ONLY — see ADR D6 for prod gating).',
       updated_at = now()
 WHERE cell_id = 'cell-2'
   AND status = 'maintenance';
