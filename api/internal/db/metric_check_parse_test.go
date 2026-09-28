// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"sort"
	"strings"
	"testing"
)

// Fixtures trimmed from the real migrations. Each one exists because ONE of them broke the
// previous parser (#241) — they are not hypothetical variations.
const (
	// 000017 — the column is born inline in the CREATE TABLE.
	fxCreateCostCounters = `
CREATE TABLE IF NOT EXISTS cost_counters (
    tenant_id TEXT NOT NULL,
    plan_tier TEXT NOT NULL CHECK (plan_tier IN ('starter', 'pro')),
    metric TEXT NOT NULL CHECK (metric IN ('egress_bytes', 'docr_pulls', 'build_minutes'))
);`

	// 000046 — the CHECK is widened via DROP/ADD CONSTRAINT.
	fxWidenCostCounters = `
ALTER TABLE cost_counters DROP CONSTRAINT cost_counters_metric_check;
ALTER TABLE cost_counters ADD CONSTRAINT cost_counters_metric_check
    CHECK (metric IN ('egress_bytes', 'docr_pulls', 'build_minutes', 'cpu_core_seconds', 'mem_gb_seconds'));`

	// 000056 — ANOTHER table gains a `metric` column, and also references the first one by FK.
	// This is the defect's fixture: it comes AFTER, so it became "the last occurrence".
	fxCreateLensFired = `
CREATE TABLE lens_fired_alerts (
    id            TEXT PRIMARY KEY,
    rule_id       TEXT NOT NULL REFERENCES lens_alert_rules(id) ON DELETE CASCADE,
    metric        TEXT NOT NULL CHECK (metric IN ('error_rate', 'cost', 'p95_latency'))
);`

	// 000059 — and then it also swaps its CHECK via ALTER, again after cost_counters.
	fxRenameLensMetric = `
ALTER TABLE lens_alert_rules DROP CONSTRAINT IF EXISTS lens_alert_rules_metric_check;
ALTER TABLE lens_alert_rules ADD CONSTRAINT lens_alert_rules_metric_check
  CHECK (metric IN ('error_rate', 'cost', 'p95_latency', 'monitor_breach'));`

	// 000032 — a `metric IN (...)` that is NOT a constraint. If it gets in, it contaminates the set.
	fxUpdateWhereMetricIn = `
UPDATE plan_limits SET hard_limit = NULL
WHERE plan = 'enterprise' AND metric IN ('environments', 'projects');`

	// A prose comment quoting the old CHECK — genuinely present in the lens migrations.
	fxCommentMentionsCheck = `
-- CHECK (metric IN ('old_value_only_in_the_comment'))
ALTER TABLE cost_counters ADD CONSTRAINT noop_check CHECK (length(tenant_id) > 0);`
)

// The set the drift guard needs to read: cost_counters', and only that one — even with four
// LATER migrations touching `metric` on other tables.
func TestMetricCheckSetFor_ScopesToTheRequestedTable(t *testing.T) {
	// Arrange — real chronological order: cost_counters first, lens afterwards.
	sqls := []string{
		fxCreateCostCounters,
		fxWidenCostCounters,
		fxUpdateWhereMetricIn,
		fxCreateLensFired,
		fxRenameLensMetric,
	}

	// Act
	got := metricCheckSetFor("cost_counters", sqls)

	// Assert
	want := []string{"build_minutes", "cpu_core_seconds", "docr_pulls", "egress_bytes", "mem_gb_seconds"}
	assertSet(t, got, want)

	// The lens vocabulary must NOT leak: this is exactly what the guard used to report as drift.
	for _, leaked := range []string{"error_rate", "cost", "p95_latency", "monitor_breach", "environments"} {
		if got[leaked] {
			t.Errorf("value %q from ANOTHER table leaked into the cost_counters set", leaked)
		}
	}
}

// The same parser read from the other side: asking for the lens table returns the lens vocabulary,
// not the cost one. Without this direction, a parser that always returned the FIRST occurrence would
// pass the test above for the wrong reason.
func TestMetricCheckSetFor_ReadsTheOtherTableToo(t *testing.T) {
	sqls := []string{fxCreateCostCounters, fxWidenCostCounters, fxRenameLensMetric}

	got := metricCheckSetFor("lens_alert_rules", sqls)

	assertSet(t, got, []string{"cost", "error_rate", "monitor_breach", "p95_latency"})
}

// `UPDATE ... WHERE metric IN (...)` constrains no column at all; if it is read as a CHECK, the guard
// reports drift that does not exist.
func TestMetricCheckSetFor_IgnoresNonConstraintMetricIn(t *testing.T) {
	got := metricCheckSetFor("plan_limits", []string{fxUpdateWhereMetricIn})

	if got != nil {
		t.Errorf("UPDATE ... WHERE metric IN (...) was read as a CHECK: %v", got)
	}
}

// A comment is prose, not schema. Editing a comment must not change the set that is read.
func TestMetricCheckSetFor_IgnoresCommentedOutCheck(t *testing.T) {
	got := metricCheckSetFor("cost_counters", []string{fxCreateCostCounters, fxCommentMentionsCheck})

	if got["old_value_only_in_the_comment"] {
		t.Error("a value quoted only in a comment got into the set")
	}
	assertSet(t, got, []string{"build_minutes", "docr_pulls", "egress_bytes"})
}

// A table that appears in no migration: nil, not a silent empty set — the caller must be able to
// tell "no CHECK" from "empty CHECK".
func TestMetricCheckSetFor_ReturnsNilWhenTableAbsent(t *testing.T) {
	if got := metricCheckSetFor("nonexistent_table", []string{fxCreateCostCounters}); got != nil {
		t.Errorf("expected nil for an absent table, got %v", got)
	}
}

func assertSet(t *testing.T, got map[string]bool, want []string) {
	t.Helper()
	var keys []string
	for k := range got {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sort.Strings(want)
	if strings.Join(keys, ",") != strings.Join(want, ",") {
		t.Errorf("set read = [%s]; want [%s]", strings.Join(keys, ","), strings.Join(want, ","))
	}
}
