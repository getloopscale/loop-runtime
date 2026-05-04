// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package tenant

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	got, err := buildDockerConfigJSON("harbor.example.com", "robot$build-acme", "s3cr3t")
	if err != nil {
		t.Fatalf("buildDockerConfigJSON returned error: %v", err)
	}
	// Must be valid JSON
	if !json.Valid([]byte(got)) {
		t.Fatalf("buildDockerConfigJSON produced invalid JSON: %s", got)
	}
	// Verify structure
	var parsed harborDockerConfig
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("failed to unmarshal docker config: %v", err)
	}
	auth, ok := parsed.Auths["harbor.example.com"]
	if !ok {
		t.Fatal("expected auths entry for harbor.example.com")
	}
	if auth.Username != "robot$build-acme" {
		t.Errorf("username = %q, want %q", auth.Username, "robot$build-acme")
	}
	if auth.Password != "s3cr3t" {
		t.Errorf("password = %q, want %q", auth.Password, "s3cr3t")
	}
	wantAuth := base64.StdEncoding.EncodeToString([]byte("robot$build-acme:s3cr3t"))
	if auth.Auth != wantAuth {
		t.Errorf("auth = %q, want %q", auth.Auth, wantAuth)
	}
}

func TestBuildDockerConfigJSON_SpecialChars(t *testing.T) {
	// Verify JSON-unsafe characters are properly escaped by json.Marshal.
	specialCases := []struct {
		name     string
		password string
	}{
		{"quotes and backslash", `pass"word\slash`},
		{"newline and tab", "pass\nword\ttab"},
		{"carriage return", "pass\rword"},
		{"null byte", "pass\x00word"},
		{"unicode control", "pass\b\fword"},
	}
	for _, tc := range specialCases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildDockerConfigJSON("harbor.example.com", "robot$test", tc.password)
			if err != nil {
				t.Fatalf("buildDockerConfigJSON returned error for password %q: %v", tc.password, err)
			}
			if !json.Valid([]byte(got)) {
				t.Fatalf("invalid JSON for password %q: %s", tc.password, got)
			}
			var parsed harborDockerConfig
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("unmarshal failed for password %q: %v\njson: %s", tc.password, err, got)
			}
			auth := parsed.Auths["harbor.example.com"]
			if auth.Password != tc.password {
				t.Errorf("password roundtrip failed: got %q, want %q", auth.Password, tc.password)
			}
		})
	}
}

func TestEnsureHarborProject_NilClient(t *testing.T) {
	// When HarborClient is nil, ensureHarborProject should be a no-op (return nil).
	r := &TenantReconciler{HarborClient: nil}
	tenant := &loopzerov1.Tenant{}
	tenant.Name = "test-tenant"
	if err := r.ensureHarborProject(context.TODO(), tenant); err != nil {
		t.Errorf("expected nil error for nil HarborClient, got: %v", err)
	}
}

func TestEnsureHarborRobotAccounts_NilClient(t *testing.T) {
	r := &TenantReconciler{HarborClient: nil}
	tenant := &loopzerov1.Tenant{}
	tenant.Name = "test-tenant"
	if err := r.ensureHarborRobotAccounts(context.TODO(), tenant); err != nil {
		t.Errorf("expected nil error for nil HarborClient, got: %v", err)
	}
}

func TestSecretExists_NilClient_NoOp(t *testing.T) {
	// When HarborClient is nil, ensureHarborRobotAccounts is a no-op.
	// secretExists should never be called. This test verifies the nil guard.
	r := &TenantReconciler{HarborClient: nil}
	tenant := &loopzerov1.Tenant{}
	tenant.Name = "test-tenant"
	if err := r.ensureHarborRobotAccounts(context.TODO(), tenant); err != nil {
		t.Errorf("expected nil error for nil HarborClient, got: %v", err)
	}
}

func TestDeleteHarborProject_NilClient(t *testing.T) {
	r := &TenantReconciler{HarborClient: nil}
	tenant := &loopzerov1.Tenant{}
	tenant.Name = "test-tenant"
	if err := r.deleteHarborProject(context.TODO(), tenant); err != nil {
		t.Errorf("expected nil error for nil HarborClient, got: %v", err)
	}
}

