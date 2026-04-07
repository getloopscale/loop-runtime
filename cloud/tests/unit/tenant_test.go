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

// B2: DeriveTenantSlug is now display-only. Collisions are harmless because
// tenant identity uses UUID, not slug. Verify slug derivation is deterministic.
func TestDeriveTenantSlug_SameEmailSameSlug(t *testing.T) {
	slug1 := auth.DeriveTenantSlug("ana@empresa.com")
	slug2 := auth.DeriveTenantSlug("ana@gmail.com")
	// Both derive to "ana" — this is fine because slug is display-only.
	// Tenant IDs are UUID-based, so no collision on identity.
	if slug1 != slug2 {
		t.Errorf("same local-part should produce same slug: %q vs %q", slug1, slug2)
	}
	if slug1 != "ana" {
		t.Errorf("slug = %q, want %q", slug1, "ana")
	}
}

// B2: Verify slug length limit for K8s label safety.
func TestDeriveTenantSlug_LongEmail(t *testing.T) {
	long := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz@test.com" // 78-char local part
	slug := auth.DeriveTenantSlug(long)
	if len(slug) > 60 {
		t.Errorf("slug length = %d, want <= 60", len(slug))
	}
}
