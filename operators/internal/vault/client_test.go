// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"errors"
	"testing"
)

// The shredder must delete the key the api actually encrypts with. The api
// creates "loopzero-tenants-<id>"; this path used to name "tenant-<id>", a key
// nobody creates, so a delete through it would have erased nothing and
// reported success.
func TestTenantKeyPath_NamesTheKeyTheAPIEncryptsWith(t *testing.T) {
	cases := []struct {
		tenantID string
		want     string
	}{
		{"abc123", "transit/keys/loopzero-tenants-abc123"},
		{"tenant_with_underscore", "transit/keys/loopzero-tenants-tenant_with_underscore"},
	}
	for _, c := range cases {
		got := TenantKeyPath(c.tenantID)
		if got != c.want {
			t.Errorf("TenantKeyPath(%q) = %q, want %q", c.tenantID, got, c.want)
		}
	}
}

func TestErrors_DistinctIdentities(t *testing.T) {
	// errors.Is must correctly identify each error
	if !errors.Is(ErrVaultQuotaExceeded, ErrVaultQuotaExceeded) {
		t.Error("ErrVaultQuotaExceeded should be its own identity")
	}
	if errors.Is(ErrVaultQuotaExceeded, ErrVaultUnreachable) {
		t.Error("ErrVaultQuotaExceeded should NOT match ErrVaultUnreachable")
	}
}

// mockClient implements TenantTransitClient for testing.
type mockClient struct {
	deleteCalls  int
	lastTenantID string
	deleteError  error
}

func (m *mockClient) DeleteTenantKey(_ context.Context, tenantID string) error {
	m.deleteCalls++
	m.lastTenantID = tenantID
	return m.deleteError
}

func TestMockClient_SatisfiesInterface(t *testing.T) {
	var _ TenantTransitClient = (*mockClient)(nil)
}
