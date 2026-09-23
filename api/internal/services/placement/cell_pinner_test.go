// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors
//
// Phase 5 T5.5 — CellPinner service tests (mock store).
// Live SQL behaviour is exercised by integration tests in
// db/cell_pinner_store_test.go (sibling of services tests, ships when
// the store implementation lands; this file covers the orchestration).

package placement

import (
	"context"
	"errors"
	"testing"
)

type mockPinnerStore struct {
	prevCell  string
	err       error
	errOnCell map[string]error // per-target-cell error, checked before err
	pinnedTo  []string         // every cellID the store was asked to pin to, in order
	captured  struct {
		tenantID string
		cellID   string
		called   bool
	}
}

func (m *mockPinnerStore) PinTenantToCell(_ context.Context, tenantID, cellID string) (string, error) {
	m.captured.tenantID = tenantID
	m.captured.cellID = cellID
	m.captured.called = true
	m.pinnedTo = append(m.pinnedTo, cellID)
	if e, ok := m.errOnCell[cellID]; ok {
		return "", e
	}
	return m.prevCell, m.err
}

func TestCellPinner_Pin_DelegatesToStore(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	p := NewCellPinner(store)
	prev, err := p.Pin(context.Background(), "tn-x", "cell-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if prev != "cell-0" {
		t.Errorf("prev = %q, want cell-0", prev)
	}
	if !store.captured.called {
		t.Error("store not called")
	}
	if store.captured.tenantID != "tn-x" || store.captured.cellID != "cell-1" {
		t.Errorf("captured: tenant=%q cell=%q", store.captured.tenantID, store.captured.cellID)
	}
}

func TestCellPinner_Pin_PropagatesStoreError(t *testing.T) {
	cases := []struct {
		name     string
		storeErr error
	}{
		{"tenant not found", ErrTenantNotFound},
		{"cell not found", ErrCellNotFound},
		{"cell not pinnable", ErrCellNotPinnable},
		{"cell at tenant capacity", ErrCellAtTenantCapacity},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &mockPinnerStore{err: tc.storeErr}
			p := NewCellPinner(store)
			_, err := p.Pin(context.Background(), "tn-x", "cell-1")
			if !errors.Is(err, tc.storeErr) {
				t.Errorf("error = %v, want %v", err, tc.storeErr)
			}
		})
	}
}

func TestCellPinner_Pin_NilStore_Errors(t *testing.T) {
	p := NewCellPinner(nil)
	_, err := p.Pin(context.Background(), "tn-x", "cell-1")
	if err == nil {
		t.Fatal("expected error when store nil")
	}
}

func TestCellPinner_SQL_ReferencesAllConditions(t *testing.T) {
	// The SQL is the contract. If a future refactor drops a condition,
	// this test catches it before merge.
	for _, must := range []string{
		"SELECT id, cell_id AS prev_cell_id FROM tenants WHERE id = $1",
		"SELECT cell_id, status, capacity_tenants_max FROM cells WHERE cell_id = $2",
		"COUNT(*) AS current_count FROM tenants",
		"WHERE id = $1",
		"tc.status = 'active'",
		"capacity_tenants_max",
		"RETURNING (SELECT prev_cell_id FROM target_tenant)",
	} {
		if !stringsContains(pinTenantToCellSQL, must) {
			t.Errorf("pin SQL missing required clause: %q", must)
		}
	}
}

// stringsContains is a local helper to avoid colliding with `contains`
// already defined elsewhere in the services test suite. Trivial substring
// matcher (no regex, no allocs beyond Go's string comparison).
func stringsContains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	if len(haystack) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
