// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package plan

import (
	"errors"
	"testing"
	"time"
)

// M1.2 (plan m1-decouple-in-place T2.1) — pkg/plan holds the value vocabulary
// extracted from pkg/enforcement. These tests lock the moved values byte-for-byte
// so the strangler aliases in enforcement remain behavior-preserving.

func TestCapFor_StarterBuildMinutes(t *testing.T) {
	if got := CapFor(PlanStarter, MetricBuildMinutes); got != 100 {
		t.Errorf("CapFor(Starter, build_minutes) = %d, want 100", got)
	}
	if got := CapFor(PlanEnterprise, MetricEgressBytes); got != Unlimited {
		t.Errorf("CapFor(Enterprise, egress_bytes) = %d, want Unlimited(%d)", got, Unlimited)
	}
	if got := CapFor("nonexistent", MetricDOCRPulls); got != Unlimited {
		t.Errorf("CapFor(unknown plan) = %d, want Unlimited (fail-open)", got)
	}
}

func TestCurrentPeriod_MonthBoundaries(t *testing.T) {
	// 2026-05-17T13:45:00Z → period [2026-05-01T00:00Z, 2026-06-01T00:00Z).
	now := time.Date(2026, 5, 17, 13, 45, 0, 0, time.UTC)
	start, end := CurrentPeriod(now)
	wantStart := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Unix()
	wantEnd := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC).Unix()
	if start != wantStart {
		t.Errorf("CurrentPeriod start = %d, want %d", start, wantStart)
	}
	if end != wantEnd {
		t.Errorf("CurrentPeriod end = %d, want %d", end, wantEnd)
	}
}

func TestExceededError_FormatAndUnwrap(t *testing.T) {
	e := &ExceededError{
		TenantID: "tn-1",
		Plan:     PlanPro,
		Metric:   MetricEgressBytes,
		Cap:      1000,
		Current:  1500,
		Period:   "2026-05",
	}
	want := "cost cap exceeded: tenant=tn-1 plan=pro metric=egress_bytes cap=1000 current=1500 period=2026-05"
	if e.Error() != want {
		t.Errorf("Error() = %q, want %q", e.Error(), want)
	}
	if !errors.Is(e, ErrCostCapExceeded) {
		t.Error("errors.Is(ExceededError, ErrCostCapExceeded) must hold via Unwrap")
	}
}

func TestProjectStatus_WireValues(t *testing.T) {
	// Wire-format strings are CONTRACT (consumed by the dashboard).
	cases := map[ProjectStatus]string{
		ProjectStatusLive:     "live",
		ProjectStatusBuilding: "building",
		ProjectStatusFailed:   "failed",
		ProjectStatusIdle:     "idle",
		ProjectStatusQueued:   "queued",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("ProjectStatus %q != wire %q", got, want)
		}
	}
}
