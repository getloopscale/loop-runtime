// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package admission

import (
	"context"
	"errors"
	"testing"

	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// The route adapter's job is to translate a BuildGate Decision into the
// (admitted, retryAfter, code, message) tuple the routes package wants.
// These tests drive the adapter with controlled gate snapshots and
// assert the exact mapping.

func TestRouteAdapter_Admit(t *testing.T) {
	gate := NewBuildGate(&fakeQuerier{
		snapshot: ActiveBuildsSnapshot{Total: 0},
	}, DefaultConfig(), true)
	adapter := NewRouteAdapter(gate)

	admitted, retryAfter, code, _, err := adapter.Check(context.Background(), "cell-0", "tn-a")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !admitted {
		t.Error("expected admitted=true for empty cell")
	}
	if retryAfter != 0 {
		t.Errorf("expected retryAfter=0 on admit, got %v", retryAfter)
	}
	if code != "" {
		t.Errorf("expected empty code on admit, got %q", code)
	}
}

func TestRouteAdapter_RejectCellAtCapacity_ReturnsCELL_AT_CAPACITY(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPerCell = 5
	gate := NewBuildGate(&fakeQuerier{
		snapshot: ActiveBuildsSnapshot{Total: 5},
	}, cfg, true)
	adapter := NewRouteAdapter(gate)

	admitted, retryAfter, code, message, err := adapter.Check(context.Background(), "cell-0", "tn-a")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if admitted {
		t.Error("expected admitted=false")
	}
	if code != loopzeroerrors.ErrCellAtCapacity.Code {
		t.Errorf("code = %q, want %q", code, loopzeroerrors.ErrCellAtCapacity.Code)
	}
	if retryAfter <= 0 {
		t.Error("expected positive retryAfter")
	}
	// Message should include the gate reason for context.
	if message == "" {
		t.Error("message must not be empty")
	}
	if !containsSubstring(message, "cell has") {
		t.Errorf("message should carry the gate reason, got: %q", message)
	}
}

func TestRouteAdapter_RejectTenantQuota_ReturnsTENANT_BUILD_QUOTA(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxConcurrentPerCell = 20
	cfg.MaxConcurrentPerTenant = 2
	gate := NewBuildGate(&fakeQuerier{
		snapshot: ActiveBuildsSnapshot{
			Total: 5, ByTenant: map[string]int{"tn-noisy": 2},
		},
	}, cfg, true)
	adapter := NewRouteAdapter(gate)

	admitted, _, code, _, err := adapter.Check(context.Background(), "cell-0", "tn-noisy")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if admitted {
		t.Error("expected admitted=false for noisy tenant")
	}
	if code != loopzeroerrors.ErrTenantBuildQuota.Code {
		t.Errorf("code = %q, want %q", code, loopzeroerrors.ErrTenantBuildQuota.Code)
	}
}

func TestRouteAdapter_NilGate_Admits(t *testing.T) {
	// NewRouteAdapter(nil) returns nil, and nil adapter.Check must
	// always admit so call sites can pass the adapter unconditionally.
	var adapter *RouteAdapter // nil adapter, not zero-value
	admitted, _, _, _, err := adapter.Check(context.Background(), "cell-0", "tn-a")
	if err != nil {
		t.Errorf("unexpected err: %v", err)
	}
	if !admitted {
		t.Error("nil adapter must always admit")
	}
}

func TestRouteAdapter_PropagatesGateError(t *testing.T) {
	boom := errors.New("db down")
	gate := NewBuildGate(&fakeQuerier{err: boom}, DefaultConfig(), true)
	adapter := NewRouteAdapter(gate)

	admitted, _, _, _, err := adapter.Check(context.Background(), "cell-0", "tn-a")
	// The gate fails-open (Admit) on query error but still surfaces
	// the error to the caller. The adapter must forward both.
	if err == nil {
		t.Error("expected error to be surfaced from the gate")
	}
	if !admitted {
		// On error, adapter should STILL admit (fail-open) so the
		// caller can log and continue.
		t.Error("adapter must fail-open on gate error")
	}
}

// containsSubstring is a tiny helper so the test file does not import
// strings just for one Contains call.
func containsSubstring(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
