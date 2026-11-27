-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverse 000059. Forward-only discipline: provided per house convention, never run in prod.
--
-- The mirror image of the up: widen, migrate rows back, narrow, then rename. Rows are RENAMED back
-- rather than deleted — unlike 000058's down, nothing here is unrepresentable under the old binary,
-- because a monitor_breach rule is a guard_breach rule under a different name.
SET LOCAL lock_timeout = '3s';

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_monitor_pairing_check;

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach', 'monitor_breach'));
ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach', 'monitor_breach'));

UPDATE lens_alert_rules SET metric = 'guard_breach' WHERE metric = 'monitor_breach';
UPDATE lens_fired_alerts SET metric = 'guard_breach' WHERE metric = 'monitor_breach';

ALTER TABLE lens_alert_rules DROP CONSTRAINT lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach'));
ALTER TABLE lens_fired_alerts DROP CONSTRAINT lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach'));

ALTER INDEX IF EXISTS idx_lens_alert_rules_monitor RENAME TO idx_lens_alert_rules_guard;
ALTER TABLE lens_alert_rules RENAME COLUMN monitor_id TO guard_id;

ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_guard_pairing_check
  CHECK ((metric = 'guard_breach') = (guard_id IS NOT NULL));
