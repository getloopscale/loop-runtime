// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Coverage for CellPinner's Tenant CR patch path: the database move, the
// patch retry and the all-or-nothing revert. The Kubernetes merge patch
// itself is adapters.TenantCRCellPatcher and is tested there.

package placement

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeCRPatcher records patches and fails every one of them when err is set.
type fakeCRPatcher struct {
	err     error
	patched []string
}

func (f *fakeCRPatcher) PatchTenantCellID(_ context.Context, tenantID, cellID string) error {
	f.patched = append(f.patched, tenantID+"="+cellID)
	return f.err
}

func (f *fakeCRPatcher) Namespace() string { return "loop-runtime-system" }

func noBackoff(int) time.Duration { return 0 }

func TestCellPinner_NilPatcherPinsTheDatabaseOnly(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	p := NewCellPinner(store).WithTenantCRPatcher(nil)

	prev, err := p.Pin(context.Background(), "tn-x", "cell-1")

	if err != nil || prev != "cell-0" {
		t.Fatalf("prev=%q err=%v, want cell-0 and no error", prev, err)
	}
}

func TestCellPinner_PatchesTheTenantCROnSuccess(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	patcher := &fakeCRPatcher{}
	p := NewCellPinner(store).WithTenantCRPatcher(patcher)

	prev, err := p.Pin(context.Background(), "tn-acme", "cell-1")

	if err != nil || prev != "cell-0" {
		t.Fatalf("prev=%q err=%v", prev, err)
	}
	if strings.Join(patcher.patched, ",") != "tn-acme=cell-1" {
		t.Errorf("patches = %v, want tn-acme=cell-1", patcher.patched)
	}
}

func TestCellPinner_StoreErrorSkipsTheCRPatch(t *testing.T) {
	patcher := &fakeCRPatcher{}
	p := NewCellPinner(&mockPinnerStore{err: errors.New("db down")}).WithTenantCRPatcher(patcher)

	if _, err := p.Pin(context.Background(), "tn-acme", "cell-1"); err == nil {
		t.Fatal("expected the store error")
	}

	if len(patcher.patched) != 0 {
		t.Errorf("no patch may run when the store fails; got %v", patcher.patched)
	}
}

func TestCellPinner_CRPatchIsRetriedThreeTimes(t *testing.T) {
	patcher := &fakeCRPatcher{err: errors.New("apiserver election")}
	p := NewCellPinner(&mockPinnerStore{prevCell: "cell-0"}).WithTenantCRPatcher(patcher)
	p.patchBackoff = noBackoff

	_, _ = p.Pin(context.Background(), "tn-x", "cell-1")

	if len(patcher.patched) != 3 {
		t.Errorf("patch attempts = %d, want 3", len(patcher.patched))
	}
}

func TestCellPinner_CRPatchFailure_RevertsTheDatabaseAndReportsNotApplied(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0"}
	p := NewCellPinner(store).WithTenantCRPatcher(&fakeCRPatcher{err: errors.New("not found")})
	p.patchBackoff = noBackoff

	_, err := p.Pin(context.Background(), "tn-missing", "cell-1")

	if !errors.Is(err, ErrCellPinNotApplied) {
		t.Fatalf("want ErrCellPinNotApplied, got %v", err)
	}
	if want := []string{"cell-1", "cell-0"}; strings.Join(store.pinnedTo, ",") != strings.Join(want, ",") {
		t.Errorf("store pins = %v, want the move then its reversal %v", store.pinnedTo, want)
	}
}

func TestCellPinner_CRPatchFailure_WithFailedRevert_ReportsDivergence(t *testing.T) {
	store := &mockPinnerStore{prevCell: "cell-0", errOnCell: map[string]error{"cell-0": ErrCellNotPinnable}}
	p := NewCellPinner(store).WithTenantCRPatcher(&fakeCRPatcher{err: errors.New("not found")})
	p.patchBackoff = noBackoff

	_, err := p.Pin(context.Background(), "tn-missing", "cell-1")

	if !errors.Is(err, ErrCellPinDiverged) {
		t.Fatalf("want ErrCellPinDiverged, got %v", err)
	}
	if !strings.Contains(err.Error(), "loop-runtime-tenant-cr-shape") || !strings.Contains(err.Error(), "-n loop-runtime-system") {
		t.Errorf("a divergence must carry the repair hint with the CR namespace; got %v", err)
	}
}

func TestCellPinner_CRPatchFailure_WithNoPreviousCell_ReportsDivergence(t *testing.T) {
	store := &mockPinnerStore{prevCell: ""}
	p := NewCellPinner(store).WithTenantCRPatcher(&fakeCRPatcher{err: errors.New("not found")})
	p.patchBackoff = noBackoff

	_, err := p.Pin(context.Background(), "tn-missing", "cell-1")

	if !errors.Is(err, ErrCellPinDiverged) {
		t.Fatalf("a tenant with no previous cell cannot be reverted; want ErrCellPinDiverged, got %v", err)
	}
	if len(store.pinnedTo) != 1 {
		t.Errorf("no revert may be attempted without a previous cell; store pins = %v", store.pinnedTo)
	}
}
