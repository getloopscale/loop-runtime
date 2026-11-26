// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package enforcement

import (
	"errors"
	"math"
	"testing"
)

// J-20 Scale plan v1.1 T1.2 + T1.3 hermetic tests.
//
// Covers EC-3 (KEDA plan tier check), EC-4 (HPA target MIN bounds), and
// EC-2 (server-authoritative cost computation). The handler boundary tests
// at api/internal/routes/scale_test.go invoke these assertions; here we
// pin the semantics without DB/HTTP dependency.

// ---- AssertKEDAAllowed ----

func TestAssertKEDAAllowed_RejectsStarter(t *testing.T) {
	// EC-3: Free user bypassing client UI via raw API MUST be rejected.
	err := AssertKEDAAllowed("starter")
	if err == nil {
		t.Fatalf("expected ErrKEDARequiresPro for starter tier, got nil")
	}
	if !errors.Is(err, ErrKEDARequiresPro) {
		t.Fatalf("expected errors.Is(err, ErrKEDARequiresPro), got %v", err)
	}
}

func TestAssertKEDAAllowed_RejectsUnknownAndEmpty(t *testing.T) {
	// EC-3 fail-closed semantics: empty / unrecognized plan strings
	// from a malformed session context MUST be rejected, not silently
	// allowed.
	cases := []string{"", "  ", "free", "unknown", "Hobby"}
	for _, tier := range cases {
		err := AssertKEDAAllowed(tier)
		if !errors.Is(err, ErrKEDARequiresPro) {
			t.Errorf("AssertKEDAAllowed(%q): expected ErrKEDARequiresPro, got %v",
				tier, err)
		}
	}
}

func TestAssertKEDAAllowed_AllowsProTeamEnterprise(t *testing.T) {
	// Pro/Team/Enterprise are the canonical eligible tiers per CLAUDE.md
	// MEET-2026-05-12 D4. Normalization MUST handle uppercase + whitespace
	// (Stripe webhook payloads occasionally carry "Pro" with leading space).
	cases := []string{
		"pro", "PRO", "Pro", "  pro  ",
		"team", "TEAM", " Team ",
		"enterprise", "Enterprise",
	}
	for _, tier := range cases {
		if err := AssertKEDAAllowed(tier); err != nil {
			t.Errorf("AssertKEDAAllowed(%q): expected nil, got %v", tier, err)
		}
	}
}

// ---- AssertHPATargetBounds ----

func TestAssertHPATargetBounds_AcceptsValidRange(t *testing.T) {
	// EC-4 happy path: canonical Kubernetes-default target values.
	if err := AssertHPATargetBounds(70, 80); err != nil {
		t.Fatalf("expected nil for cpu=70 memory=80, got %v", err)
	}
	// Bounds inclusive at both extremes.
	if err := AssertHPATargetBounds(HPACPUTargetMin, HPAMemoryTargetMin); err != nil {
		t.Errorf("expected nil at min boundaries, got %v", err)
	}
	if err := AssertHPATargetBounds(HPACPUTargetMax, HPAMemoryTargetMax); err != nil {
		t.Errorf("expected nil at max boundaries, got %v", err)
	}
}

func TestAssertHPATargetBounds_RejectsCPUBelowMinAndZero(t *testing.T) {
	// EC-4: cpu_target=1 → HPA never triggers (useless HPA). Reject.
	for _, cpu := range []int{0, 1, 9, -5} {
		err := AssertHPATargetBounds(cpu, 70)
		var boundsErr *HPATargetBoundsError
		if !errors.As(err, &boundsErr) {
			t.Errorf("cpu=%d: expected *HPATargetBoundsError, got %T (%v)", cpu, err, err)
			continue
		}
		if boundsErr.Field != "cpu_target" {
			t.Errorf("cpu=%d: expected field cpu_target, got %s", cpu, boundsErr.Field)
		}
		if boundsErr.Got != cpu {
			t.Errorf("cpu=%d: expected Got=%d, got %d", cpu, cpu, boundsErr.Got)
		}
		if boundsErr.Min != HPACPUTargetMin || boundsErr.Max != HPACPUTargetMax {
			t.Errorf("cpu=%d: expected bounds [%d,%d], got [%d,%d]",
				cpu, HPACPUTargetMin, HPACPUTargetMax, boundsErr.Min, boundsErr.Max)
		}
		// Sentinel unwrap holds for downstream errors.Is checks.
		if !errors.Is(err, ErrHPATargetOutOfBounds) {
			t.Errorf("cpu=%d: errors.Is(err, ErrHPATargetOutOfBounds) failed", cpu)
		}
	}
}

func TestAssertHPATargetBounds_RejectsCPUAboveMax(t *testing.T) {
	// cpu_target=99 leaves no headroom for transient spikes.
	for _, cpu := range []int{96, 99, 100, 200} {
		err := AssertHPATargetBounds(cpu, 70)
		var boundsErr *HPATargetBoundsError
		if !errors.As(err, &boundsErr) {
			t.Errorf("cpu=%d: expected *HPATargetBoundsError, got %v", cpu, err)
			continue
		}
		if boundsErr.Field != "cpu_target" || boundsErr.Got != cpu {
			t.Errorf("cpu=%d: bounds error context wrong (field=%s got=%d)",
				cpu, boundsErr.Field, boundsErr.Got)
		}
	}
}

func TestAssertHPATargetBounds_RejectsMemoryOutOfRange(t *testing.T) {
	// Memory upper bound is 90 (more conservative than CPU's 95) because
	// memory cannot be reclaimed as quickly under pressure.
	cases := []struct {
		cpu, mem int
	}{
		{70, 0},   // memory zero
		{70, 9},   // memory below min
		{70, 91},  // memory above max
		{70, 100}, // memory above max
	}
	for _, c := range cases {
		err := AssertHPATargetBounds(c.cpu, c.mem)
		var boundsErr *HPATargetBoundsError
		if !errors.As(err, &boundsErr) {
			t.Errorf("cpu=%d memory=%d: expected *HPATargetBoundsError, got %v",
				c.cpu, c.mem, err)
			continue
		}
		if boundsErr.Field != "memory_target" {
			t.Errorf("cpu=%d memory=%d: expected field memory_target, got %s",
				c.cpu, c.mem, boundsErr.Field)
		}
		if boundsErr.Got != c.mem {
			t.Errorf("cpu=%d memory=%d: expected Got=%d, got %d",
				c.cpu, c.mem, c.mem, boundsErr.Got)
		}
		if boundsErr.Min != HPAMemoryTargetMin || boundsErr.Max != HPAMemoryTargetMax {
			t.Errorf("cpu=%d memory=%d: expected memory bounds [%d,%d], got [%d,%d]",
				c.cpu, c.mem, HPAMemoryTargetMin, HPAMemoryTargetMax,
				boundsErr.Min, boundsErr.Max)
		}
	}
}

func TestAssertHPATargetBounds_CPUCheckedFirst(t *testing.T) {
	// Deterministic ordering: when BOTH fields out of bounds, CPU reported
	// first (test contract for downstream UX showing one error at a time).
	err := AssertHPATargetBounds(5, 5)
	var boundsErr *HPATargetBoundsError
	if !errors.As(err, &boundsErr) {
		t.Fatalf("expected *HPATargetBoundsError, got %v", err)
	}
	if boundsErr.Field != "cpu_target" {
		t.Errorf("expected cpu_target reported first, got %s", boundsErr.Field)
	}
}

// ---- ComputeMonthlyCost ----

func TestComputeMonthlyCost_HappyPath(t *testing.T) {
	// EC-2 server-authoritative: replicas=3, podCostPerHour=$0.05 →
	// 3 * 0.05 * 730 = $109.50/mo
	got := ComputeMonthlyCost(3, 0.05)
	want := 3.0 * 0.05 * 730.0
	if math.Abs(got-want) > 0.001 {
		t.Errorf("ComputeMonthlyCost(3, 0.05): want=%.4f, got=%.4f", want, got)
	}
}

func TestComputeMonthlyCost_DefensiveZeroForNonPositive(t *testing.T) {
	// Defensive against malformed inputs from downstream callers. Returns
	// 0 instead of panicking on negative replicas (avoids ResourceQuota
	// admission-time crash on Tenant CR with a bad spec).
	cases := []struct {
		replicas int
		cost     float64
	}{
		{0, 0.05},
		{3, 0.0},
		{-1, 0.05},
		{3, -0.05},
		{0, 0},
	}
	for _, c := range cases {
		got := ComputeMonthlyCost(c.replicas, c.cost)
		if got != 0 {
			t.Errorf("ComputeMonthlyCost(%d, %.4f): expected 0, got %.4f",
				c.replicas, c.cost, got)
		}
	}
}
