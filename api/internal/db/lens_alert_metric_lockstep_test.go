// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import "testing"

// Migration 000058 wrote the invariant down in PROSE:
//
//	"lens_fired_alerts is the dangerous half of this migration. Its CHECK rejects the history
//	 INSERT that runs INSIDE the fire transaction — so the (correct) rollback undoes the status
//	 flip and the rule retries forever, never delivering. The feature would fail exactly on the
//	 path it exists for, while the rule reads 'OK' on the customer's screen. Both CHECKs move
//	 together or neither does."
//
// A comment prevents nothing. Whoever writes the next metric migration may widen only
// `lens_alert_rules` — and the failure mode is the worst there is: **silent and permanent**. The rule
// fires, the history INSERT violates the CHECK, the whole transaction rolls back, the status goes back
// to 'OK', and the evaluator tries again on the next tick. Forever. Nobody is told, and the screen says
// everything is fine.
//
// Nothing in CI would detect it: both tables compile, the evaluator's unit tests pass
// (they do not touch the database), and the defect only shows up when a REAL alert has to fire in
// production — which is exactly when nobody is looking at the CHECK.
//
// This test is the only thing between the comment and that silence.
func TestLensAlertMetricChecks_MoveInLockstep(t *testing.T) {
	rules := effectiveMetricCheckSet(t, "lens_alert_rules")
	history := effectiveMetricCheckSet(t, "lens_fired_alerts")

	for m := range rules {
		if !history[m] {
			t.Errorf("metric %q is accepted by lens_alert_rules but NOT by lens_fired_alerts — "+
				"a rule with this metric fires, the history INSERT violates the CHECK, the fire "+
				"transaction rolls back and the rule retries forever without delivering, with the screen reading 'OK'. "+
				"Widen BOTH CHECKs in the same migration.", m)
		}
	}
	for m := range history {
		if !rules[m] {
			t.Errorf("metric %q is accepted by lens_fired_alerts but NOT by lens_alert_rules — "+
				"the history admits something no rule can produce; the asymmetry is always a "+
				"migration error, even though it is the harmless half.", m)
		}
	}
}

// The prompt scope of M21 DoD-5. RED before 000060, GREEN after.
//
// It is not enough for the metric to exist: it must be accepted by BOTH tables — which is why this
// test asserts both explicitly instead of relying on the lockstep above. A test that only checked
// `lens_alert_rules` would pass with the dangerous half missing.
func TestPromptMetricIsAcceptedByBothTables(t *testing.T) {
	const metric = "prompt_resolutions"

	if !effectiveMetricCheckSet(t, "lens_alert_rules")[metric] {
		t.Errorf("lens_alert_rules does not accept %q — without it there is no way to DEFINE a prompt monitor", metric)
	}
	if !effectiveMetricCheckSet(t, "lens_fired_alerts")[metric] {
		t.Errorf("lens_fired_alerts does not accept %q — without it the monitor can be defined but the "+
			"incident is never RECORDED, and DoD-5 requires the incident to be recorded", metric)
	}
}
