-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopZero Authors

-- Reverts 000060. The order matters: the prompt-scoped rules must GO before
-- the CHECK narrows, otherwise the ALTER itself fails validating rows the new
-- CHECK rejects.
--
-- The history in lens_fired_alerts is deleted along with them, and that is
-- deliberate: an incident for a metric that no longer exists cannot be read later.
-- Same choice 000059 made when rewriting 'guard_breach' → 'monitor_breach'.

SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

DELETE FROM lens_fired_alerts WHERE metric = 'prompt_resolutions';
DELETE FROM lens_alert_rules  WHERE metric = 'prompt_resolutions';

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_prompt_pairing_check;
DROP INDEX IF EXISTS idx_lens_alert_rules_prompt;

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));

ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));

ALTER TABLE lens_alert_rules DROP COLUMN IF EXISTS prompt_name;
