// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package platform

import (
	"context"
	"testing"
)

type mockKVRoutingSource struct {
	keys map[string]bool
	err  error
}

func (m *mockKVRoutingSource) ExpectedStaticKeys(_ context.Context) (map[string]bool, error) {
	return m.keys, m.err
}

func TestKVReconciler_NilClient(t *testing.T) {
	// When kvClient is nil (local dev), reconcile should be a no-op
	r := NewKVReconciler(nil, &mockKVRoutingSource{keys: map[string]bool{"key1": true}}, nil, 0)
	// Should not panic
	r.reconcile(context.Background())
}
