// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package cost

import (
	"strings"
	"testing"
)

func TestCompute_BurnArithmetic(t *testing.T) {
	// 90 min window = 1.5 h. Total rate 0.552 → 0.83 (0.552*1.5=0.828 → 0.83).
	rep, err := Compute("2026-05-16T12:00:00Z", "2026-05-16T13:30:00Z", DefaultRates())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.DurationSeconds != 5400 {
		t.Errorf("duration seconds: want 5400, got %d", rep.DurationSeconds)
	}
	if rep.DurationMinutes != 90 {
		t.Errorf("duration minutes: want 90, got %d", rep.DurationMinutes)
	}
	if rep.DurationHours != 1.5 {
		t.Errorf("duration hours: want 1.5, got %g", rep.DurationHours)
	}
	if rep.CostTotal != 0.83 {
		t.Errorf("total cost: want 0.83 (0.552*1.5=0.828 round), got %.2f", rep.CostTotal)
	}
	// mgmt 0.214*1.5 = 0.321 → 0.32
	if rep.CostMgmt != 0.32 {
		t.Errorf("mgmt cost: want 0.32, got %.2f", rep.CostMgmt)
	}
}

func TestCompute_ExactHour(t *testing.T) {
	rep, err := Compute("2026-05-16T00:00:00Z", "2026-05-16T01:00:00Z", DefaultRates())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rep.DurationHours != 1.0 {
		t.Errorf("want 1.0h, got %g", rep.DurationHours)
	}
	if rep.CostTotal != 0.55 {
		t.Errorf("total: want 0.55 (0.552 round), got %.2f", rep.CostTotal)
	}
}

func TestCompute_InvalidTimestamp_Fails(t *testing.T) {
	if _, err := Compute("not-a-date", "2026-05-16T01:00:00Z", DefaultRates()); err == nil {
		t.Error("expected error on invalid start timestamp")
	}
	if _, err := Compute("2026-05-16T00:00:00Z", "garbage", DefaultRates()); err == nil {
		t.Error("expected error on invalid end timestamp")
	}
}

func TestCompute_EndBeforeStart_Fails(t *testing.T) {
	// Mirrors cost-report.sh:112 "END_TS must be after START_TS".
	if _, err := Compute("2026-05-16T02:00:00Z", "2026-05-16T01:00:00Z", DefaultRates()); err == nil {
		t.Error("expected error when end <= start")
	}
	if _, err := Compute("2026-05-16T01:00:00Z", "2026-05-16T01:00:00Z", DefaultRates()); err == nil {
		t.Error("expected error when end == start")
	}
}

func TestMarkdown_ParityWithBashFormat(t *testing.T) {
	rep, err := Compute("2026-05-16T12:00:00Z", "2026-05-16T13:30:00Z", DefaultRates())
	if err != nil {
		t.Fatal(err)
	}
	md := rep.Markdown(DefaultRates())
	// Faithful-port assertions against cost-report.sh:130-151 structure.
	for _, want := range []string{
		"## Drill Run Cost Report",
		"**Window:** `2026-05-16T12:00:00Z` → `2026-05-16T13:30:00Z`  (90 min / 1.50 h)",
		"| Resource | Rate ($/hr) | Cost |",
		"| mgmt cluster (3 × s-4vcpu-8gb-amd) | $0.214 | $0.32 |",
		"| managed PG (db-s-2vcpu-4gb) | $0.125 | $0.19 |",
		"| **Total** | **$0.552/hr** | **$0.83** |",
		"**Pricing source:** DO public catalog, 2026-05-16.",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing line:\n  %q\n--- full ---\n%s", want, md)
		}
	}
}
