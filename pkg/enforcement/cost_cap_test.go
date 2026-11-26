// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package enforcement

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeCostStore is an in-memory CostStore for tests. Each entry is keyed
// by (tenant, metric). The bool toggle controls "row missing" cases.
type fakeCostStore struct {
	counters map[string]CostCounter
	err      error
}

func newFakeCostStore() *fakeCostStore {
	return &fakeCostStore{counters: map[string]CostCounter{}}
}

func (f *fakeCostStore) key(tenant string, metric CostMetric) string {
	return tenant + "|" + string(metric)
}

func (f *fakeCostStore) put(c CostCounter) {
	f.counters[f.key(c.TenantID, c.Metric)] = c
}

func (f *fakeCostStore) GetCounter(_ context.Context, tenantID string, metric CostMetric) (CostCounter, bool, error) {
	if f.err != nil {
		return CostCounter{}, false, f.err
	}
	c, ok := f.counters[f.key(tenantID, metric)]
	return c, ok, nil
}

// -----------------------------------------------------------------------------
// CapFor matrix
// -----------------------------------------------------------------------------

func TestCapFor_StarterMatrix(t *testing.T) {
	cases := []struct {
		metric CostMetric
		want   int64
	}{
		{MetricEgressBytes, 100 * 1024 * 1024 * 1024},
		{MetricDOCRPulls, 1_000},
		{MetricBuildMinutes, 100},
	}
	for _, c := range cases {
		got := CapFor(PlanStarter, c.metric)
		if got != c.want {
			t.Errorf("CapFor(starter, %s) = %d, want %d", c.metric, got, c.want)
		}
	}
}

func TestCapFor_ProMatrix(t *testing.T) {
	cases := []struct {
		metric CostMetric
		want   int64
	}{
		{MetricEgressBytes, 1024 * 1024 * 1024 * 1024},
		{MetricDOCRPulls, 10_000},
		{MetricBuildMinutes, 1_000},
	}
	for _, c := range cases {
		got := CapFor(PlanPro, c.metric)
		if got != c.want {
			t.Errorf("CapFor(pro, %s) = %d, want %d", c.metric, got, c.want)
		}
	}
}

func TestCapFor_TeamMatrix(t *testing.T) {
	cases := []struct {
		metric CostMetric
		want   int64
	}{
		{MetricEgressBytes, 10 * 1024 * 1024 * 1024 * 1024},
		{MetricDOCRPulls, 100_000},
		{MetricBuildMinutes, 10_000},
	}
	for _, c := range cases {
		got := CapFor(PlanTeam, c.metric)
		if got != c.want {
			t.Errorf("CapFor(team, %s) = %d, want %d", c.metric, got, c.want)
		}
	}
}

func TestCapFor_EnterpriseAlwaysUnlimited(t *testing.T) {
	for _, m := range []CostMetric{MetricEgressBytes, MetricDOCRPulls, MetricBuildMinutes} {
		if got := CapFor(PlanEnterprise, m); got != Unlimited {
			t.Errorf("CapFor(enterprise, %s) = %d, want Unlimited", m, got)
		}
	}
}

func TestCapFor_UnknownPlan_FailsOpen(t *testing.T) {
	if got := CapFor(Plan("legacy-pre-pricing"), MetricEgressBytes); got != Unlimited {
		t.Errorf("CapFor(unknown plan) = %d, want Unlimited (fail-open)", got)
	}
}

func TestCapFor_UnknownMetric_FailsOpen(t *testing.T) {
	if got := CapFor(PlanStarter, CostMetric("new_metric_not_in_table")); got != Unlimited {
		t.Errorf("CapFor(unknown metric) = %d, want Unlimited (fail-open)", got)
	}
}

// TestEnforcement_ComputeMetricAliasesResolve — T1.1 of plan metering-compute-loop.
// pkg/enforcement re-exports the new compute metrics (strangler alias of
// plan.Metric*), and they ship observe-only (Unlimited cap on every tier).
func TestEnforcement_ComputeMetricAliasesResolve(t *testing.T) {
	if MetricCPUCoreSeconds != "cpu_core_seconds" {
		t.Errorf("MetricCPUCoreSeconds = %q, want cpu_core_seconds", MetricCPUCoreSeconds)
	}
	if MetricMemGBSeconds != "mem_gb_seconds" {
		t.Errorf("MetricMemGBSeconds = %q, want mem_gb_seconds", MetricMemGBSeconds)
	}
	for _, p := range []Plan{PlanStarter, PlanPro, PlanTeam, PlanEnterprise} {
		for _, m := range []CostMetric{MetricCPUCoreSeconds, MetricMemGBSeconds} {
			if got := CapFor(p, m); got != Unlimited {
				t.Errorf("CapFor(%s, %s) = %d, want Unlimited — compute is observe-only", p, m, got)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// CapChecker.Check — pkg/enforcement/cost_cap_test.go:TestCostCap_EgressExceeded_HTTP402
// -----------------------------------------------------------------------------

// TestCostCap_EgressExceeded_HTTP402 covers the canonical T1.9 TDD RED
// case: tenant on Starter plan has egressed > 100GB → Check returns
// ExceededError that the middleware will surface as HTTP 402.
func TestCostCap_EgressExceeded_HTTP402(t *testing.T) {
	store := newFakeCostStore()
	store.put(CostCounter{
		TenantID:    "tn-1",
		Plan:        PlanStarter,
		Metric:      MetricEgressBytes,
		PeriodStart: 1714521600, // 2024-05-01
		PeriodEnd:   1717200000,
		Value:       101 * 1024 * 1024 * 1024, // 101 GB — over starter's 100 GB cap
	})
	checker := NewCapChecker(store)

	err := checker.Check(context.Background(), "tn-1", PlanStarter, MetricEgressBytes)
	if err == nil {
		t.Fatal("want ExceededError, got nil")
	}
	if !errors.Is(err, ErrCostCapExceeded) {
		t.Errorf("want errors.Is(ErrCostCapExceeded), got %v", err)
	}
	var ex *ExceededError
	if !errors.As(err, &ex) {
		t.Fatalf("want *ExceededError, got %T", err)
	}
	if ex.Cap != 100*1024*1024*1024 {
		t.Errorf("ExceededError.Cap = %d, want %d", ex.Cap, 100*1024*1024*1024)
	}
	if ex.Plan != PlanStarter {
		t.Errorf("ExceededError.Plan = %q, want starter", ex.Plan)
	}
}

func TestCostCap_UnderCap_Allowed(t *testing.T) {
	store := newFakeCostStore()
	store.put(CostCounter{
		TenantID: "tn-1",
		Plan:     PlanStarter,
		Metric:   MetricEgressBytes,
		Value:    50 * 1024 * 1024 * 1024, // 50 GB on starter's 100 GB cap
	})
	checker := NewCapChecker(store)

	if err := checker.Check(context.Background(), "tn-1", PlanStarter, MetricEgressBytes); err != nil {
		t.Errorf("want allow (under cap), got %v", err)
	}
}

// TestCostCap_BuildMinutes_Atomically_Decremented covers the build_minutes
// path in the same shape as egress. The "atomically decremented" part of
// the test name is forward-looking — refunds (e.g., cancelled builds)
// would decrement the counter atomically; current implementation only
// reads the counter. Test guards the read-side contract.
func TestCostCap_BuildMinutes_Atomically_Decremented(t *testing.T) {
	store := newFakeCostStore()
	store.put(CostCounter{
		TenantID: "tn-pro",
		Plan:     PlanPro,
		Metric:   MetricBuildMinutes,
		Value:    999, // one minute under Pro's 1000 cap
	})
	checker := NewCapChecker(store)

	// First call at 999 — allowed.
	if err := checker.Check(context.Background(), "tn-pro", PlanPro, MetricBuildMinutes); err != nil {
		t.Errorf("want allow at 999/1000, got %v", err)
	}

	// Bump to 1000 — exact cap reached, MUST block (>= semantics).
	store.put(CostCounter{
		TenantID: "tn-pro",
		Plan:     PlanPro,
		Metric:   MetricBuildMinutes,
		Value:    1000,
	})
	if err := checker.Check(context.Background(), "tn-pro", PlanPro, MetricBuildMinutes); err == nil {
		t.Errorf("want block at 1000/1000 (>= cap), got allow")
	}
}

func TestCostCap_MissingCounter_AllowedAsZero(t *testing.T) {
	store := newFakeCostStore() // no counters seeded
	checker := NewCapChecker(store)

	if err := checker.Check(context.Background(), "tn-fresh", PlanStarter, MetricEgressBytes); err != nil {
		t.Errorf("want allow (missing counter = 0 usage), got %v", err)
	}
}

func TestCostCap_Enterprise_AlwaysAllowed(t *testing.T) {
	store := newFakeCostStore()
	store.put(CostCounter{
		TenantID: "tn-ent",
		Plan:     PlanEnterprise,
		Metric:   MetricEgressBytes,
		Value:    100 * 1024 * 1024 * 1024 * 1024, // 100 TB
	})
	checker := NewCapChecker(store)
	if err := checker.Check(context.Background(), "tn-ent", PlanEnterprise, MetricEgressBytes); err != nil {
		t.Errorf("want allow (enterprise unlimited), got %v", err)
	}
}

func TestCostCap_StoreError_FailsOpen(t *testing.T) {
	store := &fakeCostStore{
		counters: map[string]CostCounter{},
		err:      errors.New("connection refused"),
	}
	checker := NewCapChecker(store)
	if err := checker.Check(context.Background(), "tn-1", PlanStarter, MetricEgressBytes); err != nil {
		t.Errorf("want fail-open on store error, got %v", err)
	}
}

// -----------------------------------------------------------------------------
// CurrentPeriod + formatPeriod
// -----------------------------------------------------------------------------

func TestCurrentPeriod_StartsAtMonthBoundaryUTC(t *testing.T) {
	now := time.Date(2026, 5, 17, 14, 30, 0, 0, time.UTC)
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

func TestFormatPeriod(t *testing.T) {
	unixSec := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC).Unix()
	if got := formatPeriod(unixSec); got != "2026-05" {
		t.Errorf("formatPeriod = %q, want %q", got, "2026-05")
	}
}
