// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/getloopscale/loopzero/pkg/enforcement"
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

	checkSet := effectiveMetricCheckSet(t)

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

// effectiveMetricCheckSet parses the migration files in ascending order and
// returns the metric values from the LAST `metric IN (...)` CHECK — i.e. the
// constraint that is in effect after all migrations apply.
func effectiveMetricCheckSet(t *testing.T) map[string]bool {
	t.Helper()
	dir := "migrations"
	ups, err := filepath.Glob(filepath.Join(dir, "*.up.sql"))
	if err != nil || len(ups) == 0 {
		t.Fatalf("no up migrations found in %s: %v", dir, err)
	}
	sort.Strings(ups) // zero-padded numeric prefixes sort chronologically

	// Match: metric ... CHECK (metric IN ('a', 'b', ...))  — capture the list.
	reCheck := regexp.MustCompile(`(?is)metric[^)]*?IN\s*\(([^)]*)\)`)
	reVal := regexp.MustCompile(`'([^']+)'`)

	var lastList string
	for _, f := range ups {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, m := range reCheck.FindAllStringSubmatch(string(b), -1) {
			lastList = m[1] // keep overwriting → ends on the highest-numbered migration's CHECK
		}
	}
	if lastList == "" {
		t.Fatal("no `metric IN (...)` CHECK found in any migration")
	}
	set := map[string]bool{}
	for _, v := range reVal.FindAllStringSubmatch(lastList, -1) {
		set[v[1]] = true
	}
	return set
}
