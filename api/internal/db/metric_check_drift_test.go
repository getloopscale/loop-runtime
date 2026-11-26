// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/getloopscale/loop-runtime/pkg/enforcement"
)

// TestMetricCheckConstraint_MatchesContractsConstants — T1.2 of plan
// metering-compute-loop (folded into T2.1's schema commit — the guard goes
// GREEN only once migration 000046 widens the CHECK).
//
// Drift guard: the set of metric values admitted by the EFFECTIVE
// cost_counters.metric CHECK (the last DROP/ADD across the migration files)
// MUST equal the set of plan.CostMetric constants re-exported by
// pkg/enforcement. Adding a CostMetric constant without widening the CHECK —
// or vice versa — fails here, before a producer writes an unadmitted metric
// (INSERT would raise a check_violation at runtime).
func TestMetricCheckConstraint_MatchesContractsConstants(t *testing.T) {
	// The authoritative Go set (the enforcement re-exports of plan.Metric*).
	// A new metric constant must be added here AND to the CHECK.
	constSet := map[string]bool{
		string(enforcement.MetricEgressBytes):    true,
		string(enforcement.MetricDOCRPulls):      true,
		string(enforcement.MetricBuildMinutes):   true,
		string(enforcement.MetricCPUCoreSeconds): true,
		string(enforcement.MetricMemGBSeconds):   true,
	}

	checkSet := effectiveMetricCheckSet(t, "cost_counters")

	for m := range constSet {
		if !checkSet[m] {
			t.Errorf("metric %q is a plan.CostMetric constant but is MISSING from the cost_counters.metric CHECK — widen the CHECK in a migration", m)
		}
	}
	for m := range checkSet {
		if !constSet[m] {
			t.Errorf("metric %q is in the cost_counters.metric CHECK but has no plan.CostMetric constant — add the constant or remove it from the CHECK", m)
		}
	}
}

// effectiveMetricCheckSet reads the migration files in ascending order and returns the metric
// values admitted by the EFFECTIVE `cost_counters.metric` CHECK — the one in force after every
// migration applies.
//
// The table is passed EXPLICITLY (#241). This helper used to keep the last `metric IN (...)` it
// found anywhere, regardless of table; once `lens_alert_rules` / `lens_fired_alerts` got a `metric`
// column of their own in later migrations, "the last one" stopped being this table's. The guard was
// not detecting drift — it was reading the wrong table, and stayed red with no reachable green.
// Parsing lives in metric_check_parse.go, where it is exercised by fixtures directly.
// The table is a PARAMETER (not hardcoded) because more than one table now carries a `metric`
// column with a CHECK of its own, and each needs its own effective set read independently.
func effectiveMetricCheckSet(t *testing.T, table string) map[string]bool {
	t.Helper()
	dir := "migrations"
	ups, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(ups) == 0 {
		t.Fatalf("no up migrations found in %s: %v", dir, err)
	}
	sort.Strings(ups) // zero-padded numeric prefixes sort chronologically

	sqls := make([]string, 0, len(ups))
	for _, f := range ups {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		sqls = append(sqls, string(b))
	}

	set := metricCheckSetFor(table, sqls)
	if len(set) == 0 {
		t.Fatalf("no `CHECK (metric IN (...))` found for %s in any migration", table)
	}
	return set
}
