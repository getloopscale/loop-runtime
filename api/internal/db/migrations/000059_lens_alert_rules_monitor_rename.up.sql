-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- M136 Phase 4b — finish the Guards → Monitors rename on the alert-rule side.
--
-- Approved T2 as an extension of MEET-2026-07-21-M135 (same tables, same constraints, same DDL
-- conditions carried verbatim). This changes only NAMES: a column and an enum value. No new
-- failure mode, no new coupling.
--
-- Leaving `guard_id` and `metric='guard_breach'` behind would split the vocabulary permanently
-- across the loopruntime-lens / loopruntime-cloud boundary, and the next reader could not tell whether a "guard"
-- is a different concept from a "monitor". Half a rename is worse than none.
--
-- Same lock discipline as 000058: the risk is lock ACQUISITION behind an idle-in-transaction
-- session, not scan duration. A timeout here is the EXPECTED outcome under contention — retry,
-- never raise it. NOT VALID + VALIDATE is deliberately NOT used: golang-migrate runs this file in
-- one transaction, so the VALIDATE would scan under the already-held ACCESS EXCLUSIVE.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

-- 1. The column.
ALTER TABLE lens_alert_rules RENAME COLUMN guard_id TO monitor_id;
ALTER INDEX IF EXISTS idx_lens_alert_rules_guard RENAME TO idx_lens_alert_rules_monitor;

-- 2. Drop the pairing CHECK FIRST. It asserts (metric='guard_breach') = (monitor_id IS NOT NULL),
--    so the moment step 4 rewrites metric to 'monitor_breach' on a row that HAS a monitor_id, the
--    old predicate reads false = true and rejects it. Verified the hard way: this migration failed
--    against a seeded row, which is the only reason the ordering bug surfaced at all — an empty
--    table would have let it ship and broken on the first customer with a configured rule.
ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_guard_pairing_check;

-- 3. Widen both CHECKs to accept the new value BEFORE migrating rows to it. Order matters: an
--    UPDATE to 'monitor_breach' under the old CHECK would be rejected row-by-row.
ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach', 'monitor_breach'));

ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach', 'monitor_breach'));

-- 4. Migrate existing rows. They are RENAMED, not discarded — a customer's configured rule must
--    survive a vocabulary change. lens_fired_alerts carries the history of what already fired and
--    is migrated for the same reason: a renamed metric must not orphan its own audit trail.
UPDATE lens_alert_rules SET metric = 'monitor_breach' WHERE metric = 'guard_breach';
UPDATE lens_fired_alerts SET metric = 'monitor_breach' WHERE metric = 'guard_breach';

-- 5. Narrow both CHECKs to the final vocabulary, now that no row holds the old value.
ALTER TABLE lens_alert_rules DROP CONSTRAINT lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));

ALTER TABLE lens_fired_alerts DROP CONSTRAINT lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));

-- 6. Re-create the pairing CHECK against the new column and value. It is preserved across the
--    rename, never quietly dropped: it is the reason a rule cannot exist claiming to watch a
--    monitor while pointing at nothing — an armed rule that can never fire.
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_monitor_pairing_check
  CHECK ((metric = 'monitor_breach') = (monitor_id IS NOT NULL));
