-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- M135 — a lens alert rule may reference a loop-runtime-lens guard.
--
-- Approved T2 per MEET-2026-07-21-M135 (change-governor + api-domain-architect +
-- distributed-systems-engineer + sre-engineer/absolute-veto).
--
-- A guard-backed rule (metric = 'guard_breach') thresholds the number of traces that breached the
-- guard in the window, instead of a workspace-wide observability aggregate. No FK: guards live in
-- loop-runtime-lens, a separate service and database. The evaluator treats an unknown guard the same way it
-- treats an unreachable one — absence, never a zero (loop-console#247).
--
-- LOCK PROFILE. The risk here is lock ACQUISITION, not scan duration. ACCESS EXCLUSIVE queues
-- behind every open transaction on these tables and blocks all new readers behind it, FIFO — and
-- the alert evaluator opens a transaction on lens_alert_rules every 60s. One `idle in transaction`
-- session turns a 5ms DDL into an unbounded outage of the alert path. lock_timeout is the control.
-- A timeout here is the EXPECTED outcome under contention, not an incident: retry. Never raise it
-- to force the migration through.
--
-- NOT VALID + VALIDATE CONSTRAINT is deliberately NOT used. golang-migrate runs this file in one
-- transaction, so a VALIDATE would scan under the already-held ACCESS EXCLUSIVE — the ceremony with
-- none of the benefit. It only pays when split across two migration files, and these tables do not
-- justify that. Both CHECK changes are WIDENINGS (strict supersets), so validation cannot fail.
SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

ALTER TABLE lens_alert_rules ADD COLUMN IF NOT EXISTS guard_id TEXT;

ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach'));

-- The pairing CHECK is mandatory, not decorative. Without it two states are representable and BOTH
-- fail silently: (a) metric='guard_breach' with guard_id NULL — the evaluator warn-skips it forever,
-- so the customer sees an armed rule that can never fire; (b) guard_id set on a 'cost' rule — simply
-- ignored. One line of DDL makes both unrepresentable at the only layer that can actually enforce it.
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_guard_pairing_check
  CHECK ((metric = 'guard_breach') = (guard_id IS NOT NULL));

-- lens_fired_alerts is the dangerous half of this migration. Its CHECK rejects the history INSERT
-- that runs INSIDE the fire transaction — so the (correct) rollback undoes the status flip and the
-- rule retries forever, never delivering. The feature would fail exactly on the path it exists for,
-- while the rule reads 'OK' on the customer's screen. Both CHECKs move together or neither does.
ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'guard_breach'));

-- Partial index: only guard-backed rules are ever looked up by guard.
CREATE INDEX IF NOT EXISTS idx_lens_alert_rules_guard
  ON lens_alert_rules (guard_id) WHERE guard_id IS NOT NULL;
