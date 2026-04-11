// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package controller

import (
	"testing"

	loopzerov1 "github.com/loopzero/loopzero/operators/api/v1"
)

func TestHarborQuotaForPlan(t *testing.T) {
	const gb = 1024 * 1024 * 1024

	tests := []struct {
		plan loopzerov1.PlanTier
		want int64
	}{
		{loopzerov1.PlanStarter, 5 * gb},
		{loopzerov1.PlanGrowth, 50 * gb},
		{loopzerov1.PlanEnterprise, -1},
		{loopzerov1.PlanTier("unknown"), 5 * gb}, // conservative default
	}

	for _, tt := range tests {
		t.Run(string(tt.plan), func(t *testing.T) {
			got := harborQuotaForPlan(tt.plan)
			if got != tt.want {
				t.Errorf("harborQuotaForPlan(%q) = %d, want %d", tt.plan, got, tt.want)
			}
		})
	}
}

func TestBuildDockerConfigJSON(t *testing.T) {
	got := buildDockerConfigJSON("harbor.example.com", "robot$build-acme", "s3cr3t")
	want := `{"auths":{"harbor.example.com":{"username":"robot$build-acme","password":"s3cr3t","auth":"REDACTED_SECRET_035"}}}`
	if got != want {
		t.Errorf("buildDockerConfigJSON mismatch\ngot:  %s\nwant: %s", got, want)
	}
}

func TestBuildDockerConfigJSON_SpecialChars(t *testing.T) {
	// Verify JSON-unsafe characters in password are escaped.
	got := buildDockerConfigJSON("harbor.example.com", "robot$test", `pass"word\slash`)
	// The password should have " and \ escaped.
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	// Verify it contains the escaped password.
	if !containsSubstring(got, `pass\"word\\slash`) {
		t.Errorf("expected escaped special chars in output: %s", got)
	}
}

func TestEnsureHarborProject_NilClient(t *testing.T) {
	// When HarborClient is nil, ensureHarborProject should be a no-op (return nil).
	r := &TenantReconciler{HarborClient: nil}
	tenant := &loopzerov1.Tenant{}
	tenant.Name = "test-tenant"
	if err := r.ensureHarborProject(nil, tenant); err != nil {
		t.Errorf("expected nil error for nil HarborClient, got: %v", err)
	}
}

func TestEnsureHarborRobotAccounts_NilClient(t *testing.T) {
	r := &TenantReconciler{HarborClient: nil}
	tenant := &loopzerov1.Tenant{}
	tenant.Name = "test-tenant"
	if err := r.ensureHarborRobotAccounts(nil, tenant); err != nil {
		t.Errorf("expected nil error for nil HarborClient, got: %v", err)
	}
}

func TestDeleteHarborProject_NilClient(t *testing.T) {
	r := &TenantReconciler{HarborClient: nil}
	tenant := &loopzerov1.Tenant{}
	tenant.Name = "test-tenant"
	if err := r.deleteHarborProject(nil, tenant); err != nil {
		t.Errorf("expected nil error for nil HarborClient, got: %v", err)
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsCheck(s, substr))
}

func containsCheck(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
