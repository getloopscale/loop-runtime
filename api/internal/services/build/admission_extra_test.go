// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"context"
	"testing"
)

func TestBuildAdmission_ReleaseTenant(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   10,
		MaxConcurrentPerTenant: 2,
	})

	// Admit 2 builds for tenant-a (filling per-tenant quota)
	_ = gate.Admit(context.Background(), "tenant-a", "cell-0")
	_ = gate.Admit(context.Background(), "tenant-a", "cell-0")

	// 3rd should be rejected
	err := gate.Admit(context.Background(), "tenant-a", "cell-0")
	if err != ErrTenantAtCapacity {
		t.Errorf("expected ErrTenantAtCapacity, got %v", err)
	}

	// ReleaseTenant should free both cell and tenant slots
	gate.ReleaseTenant("tenant-a", "cell-0")

	// Now tenant-a should be able to admit again
	err = gate.Admit(context.Background(), "tenant-a", "cell-0")
	if err != nil {
		t.Errorf("expected successful admit after ReleaseTenant, got %v", err)
	}
}

func TestBuildAdmission_CellUtilization(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   10,
		MaxConcurrentPerTenant: 5,
	})

	// Initially 0
	if util := gate.CellUtilization("cell-0"); util != 0 {
		t.Errorf("expected 0 utilization, got %d", util)
	}

	// Admit 3 builds
	_ = gate.Admit(context.Background(), "t1", "cell-0")
	_ = gate.Admit(context.Background(), "t2", "cell-0")
	_ = gate.Admit(context.Background(), "t3", "cell-0")

	if util := gate.CellUtilization("cell-0"); util != 3 {
		t.Errorf("expected 3 utilization, got %d", util)
	}

	// Release one
	gate.Release("cell-0")
	if util := gate.CellUtilization("cell-0"); util != 2 {
		t.Errorf("expected 2 utilization after release, got %d", util)
	}

	// Different cell should be 0
	if util := gate.CellUtilization("cell-1"); util != 0 {
		t.Errorf("expected 0 utilization for different cell, got %d", util)
	}
}

func TestBuildAdmission_ReleaseTenant_NeverGoesNegative(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   10,
		MaxConcurrentPerTenant: 5,
	})

	// Release without prior admit — should not panic or go negative
	gate.ReleaseTenant("tenant-x", "cell-0")
	if util := gate.CellUtilization("cell-0"); util != 0 {
		t.Errorf("expected 0, got %d", util)
	}
}

func TestBuildAdmission_Release_NeverGoesNegative(t *testing.T) {
	gate := NewBuildAdmissionGate(BuildAdmissionConfig{
		MaxConcurrentPerCell:   10,
		MaxConcurrentPerTenant: 5,
	})

	// Release without prior admit
	gate.Release("cell-0")
	if util := gate.CellUtilization("cell-0"); util != 0 {
		t.Errorf("expected 0 after excess release, got %d", util)
	}
}
