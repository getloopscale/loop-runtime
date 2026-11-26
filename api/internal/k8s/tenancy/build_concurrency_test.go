// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenancy

import (
	"context"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

// The Tenant CR's maxBuildConcurrency is the same per-tenant limit the API
// enforces, taken from configuration — it used to be a literal 2 while the API
// admitted 3 (principle_violations#49).
func TestEnsureTenant_WritesTheConfiguredBuildConcurrency(t *testing.T) {
	mgr := newTenantMgr(t)
	mgr.BuildConcurrency = 5

	if err := mgr.EnsureTenant(context.Background(), "tn-limit", ports.TenantCRSpec{Plan: "starter"}); err != nil {
		t.Fatalf("EnsureTenant: %v", err)
	}

	if got := getTenant(t, mgr, "tn-limit").Spec.Limits.MaxBuildConcurrency; got != 5 {
		t.Errorf("maxBuildConcurrency = %d, want the configured 5", got)
	}
}

// A manager built without a limit would write 0, which the operator reads as
// "no limit declared". The constructor refuses it before touching a cluster.
func TestNewTenantCRManager_RefusesANonPositiveBuildConcurrency(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := NewTenantCRManager("", "", n); err == nil {
			t.Errorf("NewTenantCRManager(buildConcurrency=%d) must fail", n)
		}
	}
}
