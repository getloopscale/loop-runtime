// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package surfaces

import (
	"strings"
	"testing"
)

func TestRegistry_DisabledSurfaceCarriesItsReason(t *testing.T) {
	r := NewRegistry()
	if err := r.Record("github_webhook", false, "GITHUB_WEBHOOK_SECRET missing"); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if err := r.Record("billing", true, ""); err != nil {
		t.Fatalf("Record: %v", err)
	}

	off := r.Disabled()
	if len(off) != 1 {
		t.Fatalf("Disabled() = %d surfaces, want 1: %+v", len(off), off)
	}
	if off[0].Name != "github_webhook" {
		t.Errorf("name = %q, want github_webhook", off[0].Name)
	}
	// The reason is what makes the metric actionable: whoever sees the zero
	// must know what to provision without opening the startup log.
	if !strings.Contains(off[0].Reason, "GITHUB_WEBHOOK_SECRET") {
		t.Errorf("reason = %q, want it to name the missing variable", off[0].Reason)
	}
}

func TestRegistry_RefusesDisablingWithoutAReason(t *testing.T) {
	r := NewRegistry()
	err := r.Record("github_webhook", false, "   ")
	if err == nil {
		t.Fatal("disabling without a reason was accepted — a surface off with no reason " +
			"is a zero nobody knows how to fix")
	}
	if !strings.Contains(err.Error(), "github_webhook") {
		t.Errorf("error does not name the surface: %v", err)
	}
}

func TestRegistry_RefusesADuplicateName(t *testing.T) {
	r := NewRegistry()
	if err := r.Record("github_webhook", true, ""); err != nil {
		t.Fatalf("first Record: %v", err)
	}
	err := r.Record("github_webhook", false, "something else")
	if err == nil {
		t.Fatal("a duplicate name was accepted — two surfaces with one name make " +
			"the series ambiguous and the operator cannot tell which one is at zero")
	}
}

func TestRegistry_OrderIsDeterministic(t *testing.T) {
	// Without a stable order the startup summary changes on every boot and
	// becomes noise in a log diff — the opposite of what observability buys.
	r := NewRegistry()
	for _, n := range []string{"zeta", "alpha", "middle"} {
		if err := r.Record(n, false, "reason"); err != nil {
			t.Fatalf("Record(%s): %v", n, err)
		}
	}
	got := r.Disabled()
	want := []string{"alpha", "middle", "zeta"}
	if len(got) != len(want) {
		t.Fatalf("Disabled() = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i] {
			t.Errorf("position %d = %q, want %q", i, got[i].Name, want[i])
		}
	}
}

func TestRegistry_AllReturnsEnabledAndDisabled(t *testing.T) {
	// The publisher needs both: an ENABLED surface must emit 1, not merely no
	// series. Absence and zero look the same to whoever alerts, and that
	// ambiguity is the defect this registry exists to remove.
	r := NewRegistry()
	_ = r.Record("enabled", true, "")
	_ = r.Record("disabled", false, "reason")

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d, want 2", len(all))
	}
	byName := map[string]bool{}
	for _, s := range all {
		byName[s.Name] = s.Enabled
	}
	if !byName["enabled"] || byName["disabled"] {
		t.Errorf("states inverted or missing: %+v", byName)
	}
}

func TestRecorded_ReturnsTheDecisionAndRecordsIt(t *testing.T) {
	r := NewRegistry()

	if Recorded(r, "enabled", true, "") != true {
		t.Error("Recorded returned false for an enabled surface — the return value " +
			"IS the guard's condition, so inverting it changes routing")
	}
	if Recorded(r, "disabled", false, "measured reason") != false {
		t.Error("Recorded returned true for a disabled surface")
	}

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All() = %d, want 2 — the decision must be recorded, not only returned", len(all))
	}
	if len(r.Disabled()) != 1 {
		t.Errorf("Disabled() = %d, want 1", len(r.Disabled()))
	}
}

func TestRecorded_NilRegistryDoesNotPanic(t *testing.T) {
	// Hundreds of tests build the router without a registry. If Recorded
	// panicked on a nil receiver, encapsulating it would have traded a
	// fragile convention for a hard break.
	if Recorded(nil, "any", true, "") != true {
		t.Error("Recorded with a nil registry did not return the decision")
	}
}

func TestRegistry_RefusesAnEmptyName(t *testing.T) {
	r := NewRegistry()
	if err := r.Record("  ", true, ""); err == nil {
		t.Fatal("an empty name was accepted — it would become a series with no usable label")
	}
}
