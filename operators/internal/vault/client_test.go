// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package vault

import (
	"context"
	"testing"
)

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
