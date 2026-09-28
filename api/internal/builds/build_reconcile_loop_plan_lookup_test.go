// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// T6.2 of deep-review-remediation-plan (closes find_code_006 / CODE-006 MEDIUM).

package builds

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

func newPlanTestLogger(_ *testing.T) *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

type fakePlanLookup struct {
	plan string
	err  error
}

func (f *fakePlanLookup) GetTenantPlan(_ context.Context, _ string) (string, error) {
	return f.plan, f.err
}

func TestResolvePlan_NilLookup_ReturnsEmpty(t *testing.T) {
	r := &BuildReconcileLoop{}
	got := r.resolvePlan(context.Background(), "tn-test")
	if got != "" {
		t.Errorf("nil planLookup must return empty (defaults to starter), got %q", got)
	}
}

func TestResolvePlan_HappyPath(t *testing.T) {
	r := &BuildReconcileLoop{
		planLookup: &fakePlanLookup{plan: "pro"},
		logger:     newPlanTestLogger(t),
	}
	got := r.resolvePlan(context.Background(), "tn-pro")
	if got != "pro" {
		t.Errorf("want pro, got %q", got)
	}
}

func TestResolvePlan_LookupError_FallsBackToStarter(t *testing.T) {
	r := &BuildReconcileLoop{
		planLookup: &fakePlanLookup{err: errors.New("db unreachable")},
		logger:     newPlanTestLogger(t),
	}
	got := r.resolvePlan(context.Background(), "tn-error")
	// Empty string → RenderInput default to starter — degraded mode is
	// correct fallback for unrecoverable plan lookup error.
	if got != "" {
		t.Errorf("error path must return empty (degraded fallback), got %q", got)
	}
}
