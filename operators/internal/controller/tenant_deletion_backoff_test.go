// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
	"github.com/loopzero/loopzero/operators/internal/constants"
)

func TestDeletionBackoff_ExponentialGrowth(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 5 * time.Second},   // base
		{1, 10 * time.Second},  // 5*2
		{2, 20 * time.Second},  // 5*4
		{3, 40 * time.Second},  // 5*8
		{4, 80 * time.Second},  // 5*16
		{5, 160 * time.Second}, // 5*32
		{6, 5 * time.Minute},   // capped at max
		{7, 5 * time.Minute},   // stays at max
		{20, 5 * time.Minute},  // stays at max
	}

	for _, tt := range tests {
		got := deletionBackoff(tt.attempt)
		if got != tt.expected {
			t.Errorf("deletionBackoff(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestGetSetDeletionRetryCount(t *testing.T) {
	tenant := &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{Name: "test-tenant"},
	}

	// Initial count is 0
	if got := getDeletionRetryCount(tenant); got != 0 {
		t.Errorf("initial retry count = %d, want 0", got)
	}

	// Set count
	setDeletionRetryCount(tenant, 3)
	if got := getDeletionRetryCount(tenant); got != 3 {
		t.Errorf("retry count after set(3) = %d, want 3", got)
	}

	// Increment
	setDeletionRetryCount(tenant, 4)
	if got := getDeletionRetryCount(tenant); got != 4 {
		t.Errorf("retry count after set(4) = %d, want 4", got)
	}
}

func TestGetDeletionRetryCount_InvalidAnnotation(t *testing.T) {
	tenant := &loopzerov1.Tenant{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "test-tenant",
			Annotations: map[string]string{constants.AnnotationDeletionRetryCount: "notanumber"},
		},
	}

	if got := getDeletionRetryCount(tenant); got != 0 {
		t.Errorf("invalid annotation should return 0, got %d", got)
	}
}

func TestDeletionMaxAttempts_Value(t *testing.T) {
	// Verify that the max attempts constant is 10 as mandated by MEET-052
	if constants.DeletionMaxAttempts != 10 {
		t.Errorf("DeletionMaxAttempts = %d, want 10 (MEET-052 mandate)", constants.DeletionMaxAttempts)
	}
}

func TestRequeueDeletionStuck_ContinuesRetrying(t *testing.T) {
	// After DeletionMaxAttempts, the controller should requeue at a slow rate
	// instead of stopping entirely — allows self-healing if dependencies recover.
	if constants.RequeueDeletionStuck != 1*time.Hour {
		t.Errorf("RequeueDeletionStuck = %v, want 1h", constants.RequeueDeletionStuck)
	}
	if constants.RequeueDeletionStuck <= constants.DeletionBackoffMax {
		t.Errorf("RequeueDeletionStuck (%v) should be greater than DeletionBackoffMax (%v)",
			constants.RequeueDeletionStuck, constants.DeletionBackoffMax)
	}
}
