// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"errors"
	"testing"

	planpkg "github.com/getloopscale/contracts/plan"
)

// fakeLimitsStore satisfies enforcement.LimitsStore for PlanReader tests.
type fakeLimitsStore struct {
	plan string
	err  error
}

func (f *fakeLimitsStore) GetTenantPlan(_ context.Context, _ string) (string, error) {
	return f.plan, f.err
}

func (f *fakeLimitsStore) GetLimits(_ context.Context, _, _ string) (int, int, error) {
	return 0, 0, nil
}

// -----------------------------------------------------------------------------
// SQLCostStore nil-safety
// -----------------------------------------------------------------------------

func TestSQLCostStore_NilStore_ReturnsError(t *testing.T) {
	var s *SQLCostStore // nil
	_, found, err := s.GetCounter(context.Background(), "tn-1", planpkg.MetricEgressBytes)
	if err == nil {
		t.Error("nil store must return error (CapChecker treats as fail-open)")
	}
	if found {
		t.Error("nil store must return found=false")
	}
}

func TestSQLCostStore_NilDB_ReturnsError(t *testing.T) {
	s := NewSQLCostStore(nil)
	_, found, err := s.GetCounter(context.Background(), "tn-1", planpkg.MetricEgressBytes)
	if err == nil {
		t.Error("nil db must return error")
	}
	if found {
		t.Error("nil db must return found=false")
	}
}

// -----------------------------------------------------------------------------
// PlanReader adapter
// -----------------------------------------------------------------------------

func TestPlanReader_ReturnsStarterDefaultOnEmpty(t *testing.T) {
	store := &fakeLimitsStore{plan: ""}
	reader := NewPlanReader(store)
	plan, err := reader.GetTenantPlan(context.Background(), "tn-x")
	if err != nil {
		t.Errorf("empty plan should not return error, got %v", err)
	}
	if plan != planpkg.PlanStarter {
		t.Errorf("empty plan = %q, want starter (default)", plan)
	}
}

func TestPlanReader_ReturnsTypedPlan(t *testing.T) {
	store := &fakeLimitsStore{plan: "pro"}
	reader := NewPlanReader(store)
	plan, _ := reader.GetTenantPlan(context.Background(), "tn-x")
	if plan != planpkg.PlanPro {
		t.Errorf("want pro, got %q", plan)
	}
}

func TestPlanReader_StorePropagatesError(t *testing.T) {
	store := &fakeLimitsStore{err: errors.New("connection refused")}
	reader := NewPlanReader(store)
	plan, err := reader.GetTenantPlan(context.Background(), "tn-x")
	if err == nil {
		t.Error("expected store error to propagate")
	}
	// On error, returns conservative default (starter).
	if plan != planpkg.PlanStarter {
		t.Errorf("error path = %q, want starter (conservative default)", plan)
	}
}

func TestPlanReader_NilReader_ReturnsError(t *testing.T) {
	var p *PlanReader
	_, err := p.GetTenantPlan(context.Background(), "tn-x")
	if err == nil {
		t.Error("nil PlanReader must return error")
	}
}

func TestPlanReader_NilStore_ReturnsError(t *testing.T) {
	reader := NewPlanReader(nil)
	_, err := reader.GetTenantPlan(context.Background(), "tn-x")
	if err == nil {
		t.Error("nil store must return error")
	}
}

// -----------------------------------------------------------------------------
// Integration test placeholder
// -----------------------------------------------------------------------------

// TestSQLCostStore_GetCounter_Integration is the live-DB path covered
// by api/tests/integration once migration 000017 is applied. Skipped
// here because go test ./internal/db/... doesn't connect to PG.
func TestSQLCostStore_GetCounter_Integration(t *testing.T) {
	t.Skip("Integration: requires migration 000017 applied — covered by api/tests/integration/cost_store_integration_test.go")
}
