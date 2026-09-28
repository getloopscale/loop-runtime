-- Migration 000032: M2 runtime-envs per-project caps adjustment
--
-- Plan: .claude/knowledge-base/plans/2026-06-07-runtime-envs-per-project-m2-plan.md
-- ADR D4 — Sponsor request 2026-06-07 (translated): "Keep evolving the plan
-- until it passes" (T0.2 batch-approval, docs/operations.md).
--
-- NOTE on numbering — SEPA CRITICAL #1 (2026-06-07):
-- Plan body originally cited migration number 000029. Post-merge with
-- origin/develop the memory migrations occupy 000029/000030/000031
-- (renumbered in commit aa895b27). This M2 migration takes the next
-- free slot = 000032. T0.1 baseline doc enumerates the on-disk state.
--
-- This migration is forward-only per CLAUDE.md Rule 11. The companion
-- down migration is dev-only and MUST NOT run against production.
--
-- Semantic change (NOT schema change):
--   Pre-M2: plan_limits.metric='environments' was a flat tenant-wide cap.
--   Post-M2: same row is reinterpreted as the per-(project, env) cap.
--   The aggregate ceiling becomes (projects.hard_limit * environments.hard_limit).
--   Cap values adjusted per ADR D4:
--     - starter:    1 env/project, 1 project   →  1 aggregate
--     - pro:        5 envs/project, 10 projects →  50 aggregate
--     - team:      20 envs/project, 10 projects → 200 aggregate
--     - enterprise: unlimited × unlimited      → no rows changed (NULL)
--
-- Strategy: GRANDFATHER (Option A of T0.2 sign-off doc):
--   Existing (project, env) tuples above the new cap are preserved.
--   The new caps gate NEW writes only — no archive sweep needed.
--
-- T7.2 in api side updates pkg/enforcement to interpret `environments`
-- as per-(project, env) tuple count. The query change (SELECT COUNT(*)
-- vs COUNT DISTINCT) lands in that companion task.

BEGIN;

-- T7.1 — per ADR D4 cap values. UPDATE existing rows in-place; no
-- ALTER TABLE needed (semantic reinterpretation only).
UPDATE plan_limits SET hard_limit =  1 WHERE plan = 'starter' AND metric = 'environments';
UPDATE plan_limits SET hard_limit =  5 WHERE plan = 'pro'     AND metric = 'environments';
UPDATE plan_limits SET hard_limit = 20 WHERE plan = 'team'    AND metric = 'environments';

-- The 'team' tier is referenced in the plan ADR D4 but the CRD enum
-- (operators/api/v1/tenant_types.go) currently accepts only
-- starter|growth|enterprise (Task #73 tracks the enum drift). The UPDATE
-- above is idempotent — if no team row exists, zero rows affected.
--
-- The 'projects' cap row is also re-asserted to the ADR D4 ceilings:
UPDATE plan_limits SET hard_limit =  1 WHERE plan = 'starter' AND metric = 'projects';
UPDATE plan_limits SET hard_limit = 10 WHERE plan = 'pro'     AND metric = 'projects';
UPDATE plan_limits SET hard_limit = 10 WHERE plan = 'team'    AND metric = 'projects';

-- Forensic guard: enterprise stays unlimited (NULL hard_limit) — verify.
-- If somebody set enterprise to a finite cap by mistake, this asserts
-- the canonical "no cap" semantic. Idempotent: NULL where already NULL.
UPDATE plan_limits SET hard_limit = NULL
WHERE plan = 'enterprise' AND metric IN ('environments', 'projects');

-- Lock the new caps in a comment that auditors can grep. The actual
-- semantic — per-(project, env) tuple — is enforced by the application
-- layer in pkg/enforcement (T7.2). No SQL change beyond cap numbers.
COMMENT ON TABLE plan_limits IS 'Plan caps. M2 v1.2 (migration 000032): the `environments` metric is interpreted per-(project, env) tuple, NOT tenant-wide aggregate. The aggregate ceiling per tenant becomes projects.hard_limit * environments.hard_limit. Grandfather strategy: existing over-cap tuples are preserved; new writes blocked at 402 PLAN_LIMIT_EXCEEDED.';

COMMIT;
