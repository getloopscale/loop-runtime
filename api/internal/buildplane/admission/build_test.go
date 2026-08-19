// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package admission

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Roadmap 07 — admission gate unit tests.
//
// The five DoD-named tests from the roadmap map here as:
//   - TestBuildAdmission_CellCapacityReject: covered by
//     TestCheck_CellAtCapacity_Rejects
//   - TestBuildAdmission_TenantFairness: covered by
//     TestCheck_TenantQuotaExceeded_WhenCellStillHasRoom +
//     TestCheck_TenantBelowQuota_Admits
//   - TestRetryAfter_HeaderPresent: covered by
//     TestCheck_RetryAfter_ScalesWithSaturation
//
// Tests for the pg-pool circuit breaker (also roadmap 07 DoD
// TestPGCircuitBreaker_RejectAt90Percent) live with the middleware in
// a separate file because the breaker instrument is shared.

type fakeQuerier struct {
	snapshot ActiveBuildsSnapshot
	err      error
	calls    int
}

func (f *fakeQuerier) ActiveBuilds(_ context.Context, _ string) (ActiveBuildsSnapshot, error) {
	f.calls++
	return f.snapshot, f.err
}

func TestCheck_DisabledAlwaysAdmits(t *testing.T) {
	// Even with a cell that is 100 over capacity, a disabled gate
	// must admit — this is the documented rollback path.
	querier := &fakeQuerier{
		snapshot: ActiveBuildsSnapshot{
			Total: 9999, ByTenant: map[string]int{"tn-a": 9999},
		},
	}
	gate := NewBuildGate(querier, DefaultConfig(), false)
	res, err := gate.Check(context.Background(), "cell-0", "tn-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != Admit {
		t.Errorf("disabled gate must Admit, got %v", res.Decision)
	}
	if querier.calls != 0 {
		t.Errorf("disabled gate must not query, got %d calls", querier.calls)
	}
}

func TestCheck_CellAtCapacity_Rejects(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPerCell = 10
	cfg.MaxConcurrentPerTenant = 3
	querier := &fakeQuerier{
		snapshot: ActiveBuildsSnapshot{
			Total: 10, // exactly at cap
			ByTenant: map[string]int{
				"tn-a": 1, "tn-b": 3, "tn-c": 6,
			},
		},
	}
	gate := NewBuildGate(querier, cfg, true)

	res, err := gate.Check(context.Background(), "cell-0", "tn-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != RejectCellAtCapacity {
		t.Errorf("expected RejectCellAtCapacity, got %v (%s)", res.Decision, res.Reason)
	}
	if res.CellActiveBuilds != 10 {
		t.Errorf("expected CellActiveBuilds=10, got %d", res.CellActiveBuilds)
	}
	if res.RetryAfter <= 0 {
		t.Error("expected non-zero RetryAfter on rejection")
	}
	if res.Reason == "" {
		t.Error("expected human-readable Reason")
	}
}

func TestCheck_CellAtCapacityOverBySeveral_LongerRetryAfter(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPerCell = 10
	cfg.BaseRetryAfter = 5 * time.Second
	cfg.MaxRetryAfter = 60 * time.Second

	atCapGate := NewBuildGate(&fakeQuerier{
		snapshot: ActiveBuildsSnapshot{Total: 10},
	}, cfg, true)
	atCap, _ := atCapGate.Check(context.Background(), "cell-0", "tn-a")

	fiveOver := NewBuildGate(&fakeQuerier{
		snapshot: ActiveBuildsSnapshot{Total: 15},
	}, cfg, true)
	over, _ := fiveOver.Check(context.Background(), "cell-0", "tn-a")

	if over.RetryAfter <= atCap.RetryAfter {
		t.Errorf("expected longer Retry-After when further over cap: at_cap=%v over=%v",
			atCap.RetryAfter, over.RetryAfter)
	}
	if over.RetryAfter > cfg.MaxRetryAfter {
		t.Errorf("Retry-After %v must be capped at MaxRetryAfter %v",
			over.RetryAfter, cfg.MaxRetryAfter)
	}
}

func TestCheck_RetryAfter_ScalesWithSaturation(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPerCell = 10
	cfg.BaseRetryAfter = 2 * time.Second
	cfg.MaxRetryAfter = 30 * time.Second

	cases := []struct {
		total int
		want  time.Duration // lower bound, must be >= this
	}{
		{total: 10, want: 2 * time.Second},  // exactly at cap: BaseRetryAfter
		{total: 11, want: 4 * time.Second},  // 1 over: 2×base
		{total: 15, want: 12 * time.Second}, // 5 over: 6×base
		{total: 30, want: 30 * time.Second}, // way over: capped at Max
	}
	for _, tc := range cases {
		gate := NewBuildGate(&fakeQuerier{
			snapshot: ActiveBuildsSnapshot{Total: tc.total},
		}, cfg, true)
		res, _ := gate.Check(context.Background(), "cell-0", "tn-a")
		if res.RetryAfter < tc.want {
			t.Errorf("total=%d: RetryAfter=%v, want >= %v", tc.total, res.RetryAfter, tc.want)
		}
		if res.RetryAfter > cfg.MaxRetryAfter {
			t.Errorf("total=%d: RetryAfter %v exceeds MaxRetryAfter %v",
				tc.total, res.RetryAfter, cfg.MaxRetryAfter)
		}
	}
}

func TestCheck_TenantQuotaExceeded_WhenCellStillHasRoom(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPerCell = 20
	cfg.MaxConcurrentPerTenant = 3
	querier := &fakeQuerier{
		snapshot: ActiveBuildsSnapshot{
			Total:    5, // cell has room
			ByTenant: map[string]int{"tn-noisy": 3},
		},
	}
	gate := NewBuildGate(querier, cfg, true)

	res, err := gate.Check(context.Background(), "cell-0", "tn-noisy")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != RejectTenantQuotaExceeded {
		t.Errorf("expected RejectTenantQuotaExceeded, got %v (%s)", res.Decision, res.Reason)
	}
	if res.TenantActiveBuilds != 3 {
		t.Errorf("expected TenantActiveBuilds=3, got %d", res.TenantActiveBuilds)
	}
	if res.RetryAfter <= 0 {
		t.Error("expected non-zero RetryAfter on tenant rejection")
	}
}

func TestCheck_TenantBelowQuota_Admits(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPerCell = 20
	cfg.MaxConcurrentPerTenant = 3
	querier := &fakeQuerier{
		snapshot: ActiveBuildsSnapshot{
			Total:    5,
			ByTenant: map[string]int{"tn-noisy": 3, "tn-quiet": 1},
		},
	}
	gate := NewBuildGate(querier, cfg, true)

	// tn-quiet has only 1 build — should be admitted even though
	// tn-noisy is at its own quota.
	res, err := gate.Check(context.Background(), "cell-0", "tn-quiet")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != Admit {
		t.Errorf("expected Admit for quiet tenant, got %v (%s)", res.Decision, res.Reason)
	}
}

func TestCheck_NewTenantOnEmptyCell_Admits(t *testing.T) {
	querier := &fakeQuerier{
		snapshot: ActiveBuildsSnapshot{Total: 0, ByTenant: nil},
	}
	gate := NewBuildGate(querier, DefaultConfig(), true)

	res, err := gate.Check(context.Background(), "cell-0", "tn-new")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != Admit {
		t.Errorf("expected Admit for new tenant on empty cell, got %v", res.Decision)
	}
}

func TestCheck_QuerierError_FailsOpen(t *testing.T) {
	// If the querier fails the gate must NOT block builds. The
	// downstream reconcile loop already has its own backpressure and
	// the gate is a soft advisory layer, not a correctness guarantee.
	boom := errors.New("pg down")
	querier := &fakeQuerier{err: boom}
	gate := NewBuildGate(querier, DefaultConfig(), true)

	res, err := gate.Check(context.Background(), "cell-0", "tn-a")
	if err == nil {
		t.Error("expected error to be returned alongside Admit decision")
	}
	if res.Decision != Admit {
		t.Errorf("expected fail-open Admit on querier error, got %v", res.Decision)
	}
}

func TestCheck_ZeroCaps_MeansNoLimit(t *testing.T) {
	// Zero caps are a valid way to disable an individual check without
	// disabling the whole gate (e.g. "enforce per-tenant but no cell cap").
	cfg := Config{
		MaxConcurrentPerCell:   0, // disabled
		MaxConcurrentPerTenant: 2,
		BaseRetryAfter:         1 * time.Second,
		MaxRetryAfter:          10 * time.Second,
	}
	querier := &fakeQuerier{
		snapshot: ActiveBuildsSnapshot{
			Total:    9999, // would trip if cell cap were enforced
			ByTenant: map[string]int{"tn-a": 1},
		},
	}
	gate := NewBuildGate(querier, cfg, true)

	// Cell is "overloaded" but cap is 0 (disabled), and tenant has 1 < 2,
	// so we expect Admit.
	res, err := gate.Check(context.Background(), "cell-0", "tn-a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Decision != Admit {
		t.Errorf("expected Admit with disabled cell cap, got %v (%s)", res.Decision, res.Reason)
	}
}

func TestDecision_Stringer(t *testing.T) {
	cases := map[Decision]string{
		Admit:                     "admit",
		RejectCellAtCapacity:      "reject_cell_at_capacity",
		RejectTenantQuotaExceeded: "reject_tenant_quota_exceeded",
	}
	for d, want := range cases {
		if got := d.String(); got != want {
			t.Errorf("Decision(%d).String() = %q, want %q", d, got, want)
		}
	}
}

func TestDefaultConfig_MatchesBudgetDoc(t *testing.T) {
	// The budget doc (docs/capacity/cell-budget.md) sets the red line
	// for build queue depth at 20 and the per-tenant cap at 3 (matching
	// the BuildReconcileLoop's MaxBuildsPerTenant default). If the
	// budget changes, this test will catch the drift.
	cfg := DefaultConfig()
	if cfg.MaxConcurrentPerCell != 20 {
		t.Errorf("MaxConcurrentPerCell drift: got %d, budget says 20",
			cfg.MaxConcurrentPerCell)
	}
	if cfg.MaxConcurrentPerTenant != 3 {
		t.Errorf("MaxConcurrentPerTenant drift: got %d, budget says 3",
			cfg.MaxConcurrentPerTenant)
	}
}
