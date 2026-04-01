// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package unit

import (
	"testing"

	"github.com/loopzero/loopzero/cloud/internal/auth"
)

func TestTenantProvisionerNilDB(t *testing.T) {
	prov := auth.NewTenantProvisioner(nil, nil)
	tid, role, err := prov.EnsureTenant(nil, "user-123", "test@example.com")
	if err != nil {
		t.Fatalf("EnsureTenant with nil DB should not error: %v", err)
	}
	if tid != "dev-tenant" {
		t.Errorf("tenant_id = %q, want %q", tid, "dev-tenant")
	}
	if role != "owner" {
		t.Errorf("role = %q, want %q", role, "owner")
	}
}

func TestDeriveTenantSlug(t *testing.T) {
	tests := []struct {
		email string
		want  string
	}{
		{"paulo@example.com", "paulo"},
		{"john.doe@company.com", "john-doe"},
		{"UPPER@TEST.COM", "upper"},
		{"under_score@test.com", "under-score"},
		{"a+b@test.com", "ab"},
		{"@test.com", "user"},
		{"special-chars@test.com", "special-chars"},
	}

	for _, tt := range tests {
		t.Run(tt.email, func(t *testing.T) {
			got := auth.DeriveTenantSlug(tt.email)
			if got != tt.want {
				t.Errorf("DeriveTenantSlug(%q) = %q, want %q", tt.email, got, tt.want)
			}
		})
	}
}
