-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- Reverse 000058. Forward-only discipline: provided per house convention, never run in prod.
--
-- Order matters and is not arbitrary. The narrowed metric CHECK cannot be restored while any
-- 'guard_breach' row exists, and the pairing CHECK cannot survive the column it references. So:
-- delete the guard-backed rows first (their history rows go with them via ON DELETE CASCADE on
-- lens_fired_alerts.rule_id), then drop the pairing CHECK, then narrow the metric CHECKs, then
-- drop the column.
--
-- Deleting rather than blocking is the deliberate choice: a guard-backed rule has no meaning under
-- a binary that cannot evaluate it, and leaving it would present the customer an armed rule that
-- silently never fires — the failure mode this whole milestone exists to eliminate.
SET LOCAL lock_timeout = '3s';

DELETE FROM lens_alert_rules WHERE metric = 'guard_breach';

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_guard_pairing_check;

ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency'));

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency'));

DROP INDEX IF EXISTS idx_lens_alert_rules_guard;
ALTER TABLE lens_alert_rules DROP COLUMN IF EXISTS guard_id;
