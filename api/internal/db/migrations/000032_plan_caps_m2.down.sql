-- Down migration for 000032_plan_caps_m2 — DEV ONLY.
-- CLAUDE.md Rule 11: forward-only migrations in production. This file
-- supports local fresh-cleanroom tear-down + replay only.
--
-- Restores the pre-M2 plan_limits caps that were observed in the
-- T0.1 baseline (docs/runbooks.md):
--   - starter: environments=1, projects=1
--   - pro:     environments=3, projects=3
--   - team:    environments=10, projects=10  (when present)
--   - enterprise: NULL (unchanged)

BEGIN;

UPDATE plan_limits SET hard_limit = 1  WHERE plan = 'starter' AND metric = 'environments';
UPDATE plan_limits SET hard_limit = 3  WHERE plan = 'pro'     AND metric = 'environments';
UPDATE plan_limits SET hard_limit = 10 WHERE plan = 'team'    AND metric = 'environments';

UPDATE plan_limits SET hard_limit = 1  WHERE plan = 'starter' AND metric = 'projects';
UPDATE plan_limits SET hard_limit = 3  WHERE plan = 'pro'     AND metric = 'projects';
UPDATE plan_limits SET hard_limit = 10 WHERE plan = 'team'    AND metric = 'projects';

COMMENT ON TABLE plan_limits IS NULL;

COMMIT;
