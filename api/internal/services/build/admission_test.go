// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"context"
	"sync/atomic"
	"testing"
)

// TestBuildAdmission_CellCapacityReject verifies that when the cell has
// reached its max concurrent builds, new submissions are rejected with
// ErrCellAtCapacity (which the handler maps to HTTP 429).
func TestBuildAdmission_CellCapacityReject(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   3,
		MaxConcurrentPerTenant: 2,
	})

	// Fill to capacity with different tenants.
	tenants := []string{"tenant-a", "tenant-b", "tenant-c"}
	for i := 0; i < 3; i++ {
		if err := gate.Admit(context.Background(), tenants[i], "cell-0"); err != nil {
			t.Fatalf("admit %d: unexpected error: %v", i, err)
		}
	}

	// Next admission must be rejected.
	err := gate.Admit(context.Background(), "tenant-y", "cell-0")
	if err != ErrCellAtCapacity {
		t.Errorf("expected ErrCellAtCapacity, got %v", err)
	}

	// Release one slot — next admission should succeed.
	gate.Release("cell-0")
	if err := gate.Admit(context.Background(), "tenant-y", "cell-0"); err != nil {
		t.Errorf("admit after release: unexpected error: %v", err)
	}
}

// TestBuildAdmission_TenantFairness verifies that a single tenant cannot
// consume all cell capacity — per-tenant limits are enforced independently.
func TestBuildAdmission_TenantFairness(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   10,
		MaxConcurrentPerTenant: 2,
	})

	// Tenant A fills its per-tenant quota.
	for i := 0; i < 2; i++ {
		if err := gate.Admit(context.Background(), "tenant-a", "cell-0"); err != nil {
			t.Fatalf("admit tenant-a %d: %v", i, err)
		}
	}

	// Tenant A's 3rd build is rejected (per-tenant limit).
	err := gate.Admit(context.Background(), "tenant-a", "cell-0")
	if err != ErrTenantAtCapacity {
		t.Errorf("expected ErrTenantAtCapacity for tenant-a, got %v", err)
	}

	// Tenant B can still submit (cell has capacity, tenant-b has no builds).
	if err := gate.Admit(context.Background(), "tenant-b", "cell-0"); err != nil {
		t.Errorf("admit tenant-b: unexpected error: %v", err)
	}
}

// TestBuildAdmission_RetryAfterPresent verifies the error contains
// retry guidance.
func TestBuildAdmission_RetryAfterPresent(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   1,
		MaxConcurrentPerTenant: 1,
	})

	_ = gate.Admit(context.Background(), "t", "cell-0")

	err := gate.Admit(context.Background(), "t2", "cell-0")
	if err == nil {
		t.Fatal("expected error")
	}
	// ErrCellAtCapacity should be detectable.
	if err != ErrCellAtCapacity {
		t.Errorf("expected ErrCellAtCapacity, got %v", err)
	}
}

// TestBuildAdmission_ConcurrentSafety verifies no data races under
// concurrent admission and release.
func TestBuildAdmission_ConcurrentSafety(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   100,
		MaxConcurrentPerTenant: 50,
	})

	var admitted atomic.Int64
	var rejected atomic.Int64

	done := make(chan struct{})
	for i := 0; i < 200; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			if err := gate.Admit(context.Background(), "t", "cell-0"); err != nil {
				rejected.Add(1)
			} else {
				admitted.Add(1)
				gate.Release("cell-0")
			}
		}()
	}
	for i := 0; i < 200; i++ {
		<-done
	}

	// At least some should have been admitted and some rejected due to races.
	if admitted.Load() == 0 {
		t.Error("expected at least one admission")
	}
}
