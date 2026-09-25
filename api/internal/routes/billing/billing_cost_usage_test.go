// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package billing

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/routestest"

	planpkg "github.com/getloopscale/loopzero-contracts/plan"
)

// fakeCostSource implements enforcement.CostStore for unit tests of
// the T1.9 cost usage display path.
type fakeCostSource struct {
	// per metric → counter (or absent for found=false)
	counters map[planpkg.CostMetric]planpkg.CostCounter
	err      error
}

func (f *fakeCostSource) GetCounter(_ context.Context, _ string, metric planpkg.CostMetric) (planpkg.CostCounter, bool, error) {
	if f.err != nil {
		return planpkg.CostCounter{}, false, f.err
	}
	c, ok := f.counters[metric]
	return c, ok, nil
}

// TestBilling_Get_WithCostSource_PopulatesUsage verifies that when a
// CostStore is wired, GET /api/v1/billing returns the 3 T1.9 metrics in
// the `usage` map with their current values + per-plan caps.
func TestBilling_Get_WithCostSource_PopulatesUsage(t *testing.T) {
	cost := &fakeCostSource{
		counters: map[planpkg.CostMetric]planpkg.CostCounter{
			planpkg.MetricBuildMinutes: {Value: 42},
			planpkg.MetricEgressBytes:  {Value: 5 * 1024 * 1024 * 1024}, // 5 GB
			// docr_pulls: absent → found=false → used=0, measured=false (B-033:
			// the zero is displayed, but it is no longer CLAIMED as a measurement)
		},
	}
	h := NewBillingHandler(fakePlanStore{plan: "starter"}, fakeCustomerStore{}).WithCostSource(cost)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp BillingResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(resp.Usage) != 3 {
		t.Fatalf("want 3 metrics in usage map, got %d: %+v", len(resp.Usage), resp.Usage)
	}

	// build_minutes: 42 used, 100 limit (starter)
	bm := resp.Usage["build_minutes"]
	if bm.Used != 42 || bm.Limit != 100 || !bm.Measured {
		t.Errorf("build_minutes: got %+v, want used=42 limit=100 measured=true", bm)
	}

	// egress_bytes: 5GB used, 100GB limit (starter)
	eb := resp.Usage["egress_bytes"]
	if eb.Used != 5*1024*1024*1024 || eb.Limit != 100*1024*1024*1024 || !eb.Measured {
		t.Errorf("egress_bytes: got %+v, want used=5GB limit=100GB measured=true", eb)
	}

	// docr_pulls: 0 used (not found), 1000 limit (starter), and NOT measured —
	// B-033: this row is the shape the whole table had on app-dev, and reading
	// it as a measured zero is the defect the item names.
	dp := resp.Usage["docr_pulls"]
	if dp.Used != 0 || dp.Limit != 1000 || dp.Measured {
		t.Errorf("docr_pulls: got %+v, want used=0 limit=1000 measured=false", dp)
	}
}

// TestBilling_Get_EnterprisePlan_ShowsUnlimitedSentinel verifies that
// Enterprise tier returns the `unlimitedDisplay` sentinel (999999) for
// every cap so the CLI prints "unlimited".
func TestBilling_Get_EnterprisePlan_ShowsUnlimitedSentinel(t *testing.T) {
	cost := &fakeCostSource{
		counters: map[planpkg.CostMetric]planpkg.CostCounter{
			planpkg.MetricBuildMinutes: {Value: 999999},
		},
	}
	h := NewBillingHandler(fakePlanStore{plan: "enterprise"}, fakeCustomerStore{}).WithCostSource(cost)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Get(w, req)

	var resp BillingResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)

	for metric, entry := range resp.Usage {
		if entry.Limit != unlimitedDisplay {
			t.Errorf("metric %s: Limit = %d, want unlimitedDisplay (%d)",
				metric, entry.Limit, unlimitedDisplay)
		}
	}
}

// TestBilling_Get_NoCostSource_EmptyUsage preserves pre-T1.9 behavior:
// when no CostStore is wired, the usage map is empty (CLI shows "no
// usage data" gracefully).
func TestBilling_Get_NoCostSource_EmptyUsage(t *testing.T) {
	h := NewBillingHandler(fakePlanStore{plan: "pro"}, fakeCustomerStore{})
	// No WithCostSource call

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Get(w, req)

	var resp BillingResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if len(resp.Usage) != 0 {
		t.Errorf("want empty usage when no CostStore wired, got %+v", resp.Usage)
	}
}

// TestBilling_Get_CostStoreError_SkipsMetric_Continues verifies fail-open
// posture: a CostStore error logs a warning and skips that metric, the
// response still returns plan + team + any successfully read metrics.
func TestBilling_Get_CostStoreError_SkipsMetric_Continues(t *testing.T) {
	cost := &fakeCostSource{err: errors.New("connection refused")}
	h := NewBillingHandler(fakePlanStore{plan: "pro"}, fakeCustomerStore{}).WithCostSource(cost)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/billing", nil)
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	w := httptest.NewRecorder()
	h.Get(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200 (fail-open), got %d body=%s", w.Code, w.Body.String())
	}

	var resp BillingResponse
	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp.Plan != "pro" {
		t.Errorf("plan still expected on cost error, got %q", resp.Plan)
	}
	// All 3 metrics errored → all skipped → empty map.
	if len(resp.Usage) != 0 {
		t.Errorf("want empty usage on cost error, got %+v", resp.Usage)
	}
}
