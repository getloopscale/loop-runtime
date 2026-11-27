-- SPDX-License-Identifier: Apache-2.0
-- Copyright 2025-2026 The LoopRuntime Authors

-- ============================================================================
-- 000060_lens_alert_rules_prompt_scope
--
-- loop-runtime-promptly M21 DoD-5 — "a monitor warns when production changes".
--
-- The monitor → incident → webhook chain ALREADY EXISTS (000054 / 000056 + the
-- evaluator in loop-console/internal/lens_alert_evaluator). What is missing is
-- DIMENSIONAL, not structural: today's metrics cover the whole workspace, and
-- the M15 metric is per PROMPT. See the umbrella's ADR 0004 and getloopscale/loop-console#450.
--
-- Additive by construction: a new nullable column + a widened CHECK. No existing
-- rule changes behaviour, and a prompt-scoped rule is unrepresentable without
-- the prompt name (see the pairing CHECK below).
--
-- Same lock discipline as 000058/000059: the risk is ACQUIRING the lock behind
-- an idle-in-transaction session, not the length of the scan. A timeout here is
-- the EXPECTED outcome under contention — retry, never raise the ceiling.
-- ============================================================================

SET LOCAL lock_timeout = '3s';
SET LOCAL statement_timeout = '60s';

-- 1. The missing dimension.
ALTER TABLE lens_alert_rules ADD COLUMN IF NOT EXISTS prompt_name TEXT;

-- 2. BOTH metric CHECKs move TOGETHER. This is not zeal — it is the lesson 000058
--    documents and 000059 followed: lens_fired_alerts is the dangerous half. Its
--    CHECK rejects the history INSERT that runs INSIDE the fire transaction, so
--    the (correct) rollback undoes the status flip and the rule retries forever,
--    never delivering — while the customer's screen reads 'OK'.
--    From this migration on it no longer depends on the author's discipline:
--    lens_alert_metric_lockstep_test.go locks the pairing.
ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach', 'prompt_resolutions'));

ALTER TABLE lens_fired_alerts DROP CONSTRAINT IF EXISTS lens_fired_alerts_metric_check;
ALTER TABLE lens_fired_alerts ADD CONSTRAINT lens_fired_alerts_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach', 'prompt_resolutions'));

-- 3. The pairing, for the same reason as 000058: without it TWO states are
--    representable and BOTH fail in silence.
--
--    (a) metric='prompt_resolutions' with prompt_name NULL — the evaluator has
--        nothing to query in loop-runtime-lens and skips the rule forever; the customer
--        sees an armed rule that never fires.
--    (b) prompt_name filled in on a 'cost' rule — simply ignored, and the next
--        reader cannot tell whether the scope applied or not.
--
--    One line of DDL makes both unrepresentable in the only layer able to
--    really enforce it.
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_prompt_pairing_check
  CHECK ((metric = 'prompt_resolutions') = (prompt_name IS NOT NULL));

-- 4. Partial index: only prompt-scoped rules are queried by prompt.
CREATE INDEX IF NOT EXISTS idx_lens_alert_rules_prompt
  ON lens_alert_rules (prompt_name) WHERE prompt_name IS NOT NULL;
