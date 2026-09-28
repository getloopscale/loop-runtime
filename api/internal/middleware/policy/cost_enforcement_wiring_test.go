// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package policy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/api/internal/middleware/auth"
	"github.com/getloopscale/loopzero/pkg/enforcement"
)

// The metric must be FED by the real path, not merely exist.
//
// B-033. A metric registered and never incremented is the "implemented but not
// wired" pattern this repository's CLAUDE.md names — and it would be especially
// perverse here, because the item exists precisely because a protection was
// installed and inert.
func TestCostEnforcer_FeedsTheDecisionMetric(t *testing.T) {
	const metric = enforcement.MetricBuildMinutes

	before := testutil.ToFloat64(
		metrics.CostCapDecisionsTotal.WithLabelValues(string(metric), "unmeasured"))

	// An empty store = no counter rows = exactly the state measured on app-dev.
	enforcer := NewCostEnforcer(
		enforcement.NewCapChecker(emptyCounterStore{}), fixedPlan("starter"), nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/builds", nil)
	req = req.WithContext(context.WithValue(req.Context(),
		auth.UserContextKeyPublic(), &auth.UserContext{TenantID: "tn-metric"}))
	rec := httptest.NewRecorder()

	passed := false
	enforcer.Middleware(metric)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		passed = true
	})).ServeHTTP(rec, req)

	if !passed {
		t.Fatal("with no counter the gate must let the request through — this slice does not change the verdict")
	}

	after := testutil.ToFloat64(
		metrics.CostCapDecisionsTotal.WithLabelValues(string(metric), "unmeasured"))
	if after <= before {
		t.Errorf("the `unmeasured` series was not fed by the middleware: %v -> %v", before, after)
	}
}

type emptyCounterStore struct{}

func (emptyCounterStore) GetCounter(context.Context, string, enforcement.CostMetric) (enforcement.CostCounter, bool, error) {
	return enforcement.CostCounter{}, false, nil
}

type fixedPlan string

func (p fixedPlan) GetTenantPlan(context.Context, string) (enforcement.Plan, error) {
	return enforcement.Plan(p), nil
}
