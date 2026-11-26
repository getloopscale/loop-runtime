// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
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

	"github.com/getloopscale/loop-runtime/api/internal/db/store"
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

// The production store (store.PostgresCellStore) returns its own sentinels, and
// the admin handler matches the placement ones with errors.Is. They must be the
// same values, or every validation failure of a real pin surfaces as a 500.
func TestCellPinner_Pin_StoreSentinelsMatchThePlacementSentinels(t *testing.T) {
	cases := []struct {
		storeErr, want error
	}{
		{store.ErrCellPinnerTenantNotFound, ErrTenantNotFound},
		{store.ErrCellPinnerCellNotFound, ErrCellNotFound},
		{store.ErrCellPinnerNotPinnable, ErrCellNotPinnable},
		{store.ErrCellPinnerAtCapacity, ErrCellAtTenantCapacity},
	}
	for _, tc := range cases {
		t.Run(tc.want.Error(), func(t *testing.T) {
			p := NewCellPinner(&mockPinnerStore{err: tc.storeErr})

			_, err := p.Pin(context.Background(), "tn-x", "cell-1")

			if !errors.Is(err, tc.want) {
				t.Errorf("errors.Is(%v, %v) = false", err, tc.want)
			}
		})
	}
}
