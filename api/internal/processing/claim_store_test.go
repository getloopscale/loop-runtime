// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package processing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// scriptedDBTX is a DBTX fake that scripts Exec RowsAffected and Query row
// iterator behavior. Copied in-package to avoid cross-package test helpers.
type scriptedDBTX struct {
	lastExecSQL   string
	lastExecArgs  []any
	lastQuerySQL  string
	lastQueryArgs []any
	nextExecRows  int64
	nextExecErr   error
	nextQueryRows []string
	nextQueryErr  error
}

func (s *scriptedDBTX) Query(_ context.Context, sql string, args ...any) (db.Rows, error) {
	s.lastQuerySQL = sql
	s.lastQueryArgs = args
	if s.nextQueryErr != nil {
		return nil, s.nextQueryErr
	}
	return &stringIDRows{ids: s.nextQueryRows}, nil
}

func (s *scriptedDBTX) Exec(_ context.Context, sql string, args ...any) (db.ExecResult, error) {
	s.lastExecSQL = sql
	s.lastExecArgs = args
	if s.nextExecErr != nil {
		return db.ExecResult{}, s.nextExecErr
	}
	return db.ExecResult{RowsAffected: s.nextExecRows}, nil
}

func (s *scriptedDBTX) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return errRow{}
}

type errRow struct{}

func (errRow) Scan(_ ...any) error { return db.ErrNoRows }

// stringIDRows returns a slice of string ids through the db.Rows iterator.
type stringIDRows struct {
	ids []string
	idx int
}

func (r *stringIDRows) Next() bool {
	if r.idx >= len(r.ids) {
		return false
	}
	r.idx++
	return true
}

func (r *stringIDRows) Scan(dest ...any) error {
	if len(dest) == 0 {
		return nil
	}
	if s, ok := dest[0].(*string); ok {
		*s = r.ids[r.idx-1]
	}
	return nil
}

func (r *stringIDRows) Err() error { return nil }
func (r *stringIDRows) Close()     {}

// =============================================================================
// Claim
// =============================================================================

func TestClaim_OpenSlot_Succeeds(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 1}
	store := NewClaimStore(s)

	if err := store.Claim(context.Background(), KindBuild, "bld-1", "pod-a"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if !strings.Contains(s.lastExecSQL, "UPDATE builds") {
		t.Errorf("expected UPDATE builds, got:\n%s", s.lastExecSQL)
	}
	if !strings.Contains(s.lastExecSQL, "processing_by    = $1") {
		t.Errorf("expected processing_by = $1 binding, got:\n%s", s.lastExecSQL)
	}
	// Three args: holder, id, staleSeconds.
	if len(s.lastExecArgs) != 3 {
		t.Fatalf("expected 3 args, got %d: %v", len(s.lastExecArgs), s.lastExecArgs)
	}
	if s.lastExecArgs[0] != "pod-a" || s.lastExecArgs[1] != "bld-1" {
		t.Errorf("arg order (holder, id) mismatch: %v", s.lastExecArgs)
	}
}

func TestClaim_LiveClaimHeldByPeer_ReturnsErrClaimHeld(t *testing.T) {
	// CAS matches zero rows → claim belongs to a live peer.
	s := &scriptedDBTX{nextExecRows: 0}
	store := NewClaimStore(s)

	err := store.Claim(context.Background(), KindBuild, "bld-1", "pod-b")
	if !errors.Is(err, ErrClaimHeld) {
		t.Errorf("expected ErrClaimHeld, got %v", err)
	}
}

func TestClaim_OrphanedClaim_Reclaimable(t *testing.T) {
	// Simulates: prior holder crashed, processing_since is old. The CAS
	// predicate allows reclaim. With the fake that means nextExecRows=1.
	s := &scriptedDBTX{nextExecRows: 1}
	store := NewClaimStoreWithStale(s, 50*time.Millisecond)

	if err := store.Claim(context.Background(), KindBuild, "bld-orphan", "pod-new"); err != nil {
		t.Errorf("expected reclaim to succeed, got %v", err)
	}
	if s.lastExecArgs[2] != int64(0) {
		// int64(0.050) == 0; that's fine — just documents the coercion.
		// The important part is that stale is passed as an int64 arg.
		if _, ok := s.lastExecArgs[2].(int64); !ok {
			t.Errorf("stale arg must be int64, got %T", s.lastExecArgs[2])
		}
	}
}

func TestClaim_ReleaseKind_UsesReleasesTable(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 1}
	store := NewClaimStore(s)

	if err := store.Claim(context.Background(), KindRelease, "rel-1", "pod-a"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(s.lastExecSQL, "UPDATE releases") {
		t.Errorf("expected UPDATE releases, got:\n%s", s.lastExecSQL)
	}
}

func TestClaim_UnknownKind_IsValidationError(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 1}
	store := NewClaimStore(s)

	err := store.Claim(context.Background(), Kind("tenants"), "x", "pod-a")
	if err == nil || !strings.Contains(err.Error(), "unsupported kind") {
		t.Errorf("expected validation error, got %v", err)
	}
	if s.lastExecSQL != "" {
		t.Error("unsupported kind must NOT issue a SQL statement")
	}
}

func TestClaim_DriverError_IsWrapped(t *testing.T) {
	s := &scriptedDBTX{nextExecErr: errors.New("conn reset")}
	store := NewClaimStore(s)

	err := store.Claim(context.Background(), KindBuild, "bld-1", "pod-a")
	if err == nil || errors.Is(err, ErrClaimHeld) {
		t.Errorf("expected wrapped driver error (not ErrClaimHeld), got %v", err)
	}
	if !strings.Contains(err.Error(), "conn reset") {
		t.Errorf("error must wrap original, got %v", err)
	}
}

// =============================================================================
// Release
// =============================================================================

func TestRelease_ByHolder_Succeeds(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 1}
	store := NewClaimStore(s)

	if err := store.Release(context.Background(), KindBuild, "bld-1", "pod-a"); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	if !strings.Contains(s.lastExecSQL, "processing_by    = NULL") {
		t.Errorf("expected processing_by cleared, got:\n%s", s.lastExecSQL)
	}
	if !strings.Contains(s.lastExecSQL, "processing_by = $2") {
		t.Errorf("expected holder predicate, got:\n%s", s.lastExecSQL)
	}
}

func TestRelease_ByNonHolder_ReturnsErrNotClaimHolder(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 0}
	store := NewClaimStore(s)

	err := store.Release(context.Background(), KindBuild, "bld-1", "pod-impostor")
	if !errors.Is(err, ErrNotClaimHolder) {
		t.Errorf("expected ErrNotClaimHolder, got %v", err)
	}
}

// =============================================================================
// FindOrphanedClaims
// =============================================================================

// TestProcessingClaim_OrphanDetection is the test explicitly named in
// roadmap 02 DoD.
func TestProcessingClaim_OrphanDetection(t *testing.T) {
	s := &scriptedDBTX{
		nextQueryRows: []string{"bld-orphan-1", "bld-orphan-2", "bld-orphan-3"},
	}
	store := NewClaimStoreWithStale(s, 30*time.Second)

	ids, err := store.FindOrphanedClaims(context.Background(), KindBuild)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 3 {
		t.Errorf("expected 3 orphaned ids, got %d: %v", len(ids), ids)
	}
	// SQL must filter on processing_by IS NOT NULL and age predicate.
	if !strings.Contains(s.lastQuerySQL, "processing_by IS NOT NULL") {
		t.Errorf("expected IS NOT NULL predicate, got:\n%s", s.lastQuerySQL)
	}
	if !strings.Contains(s.lastQuerySQL, "processing_since < now()") {
		t.Errorf("expected age predicate, got:\n%s", s.lastQuerySQL)
	}
	if s.lastQueryArgs[0] != int64(30) {
		t.Errorf("expected stale seconds = 30, got %v", s.lastQueryArgs[0])
	}
}

func TestFindOrphanedClaims_EmptyResult_ReturnsNilSlice(t *testing.T) {
	s := &scriptedDBTX{nextQueryRows: nil}
	store := NewClaimStore(s)

	ids, err := store.FindOrphanedClaims(context.Background(), KindRelease)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("expected empty slice, got %v", ids)
	}
}

func TestFindOrphanedClaims_DriverError_IsWrapped(t *testing.T) {
	s := &scriptedDBTX{nextQueryErr: errors.New("query failed")}
	store := NewClaimStore(s)

	_, err := store.FindOrphanedClaims(context.Background(), KindBuild)
	if err == nil || !strings.Contains(err.Error(), "query failed") {
		t.Errorf("expected wrapped driver error, got %v", err)
	}
}

func TestNewClaimStoreWithStale_DefaultsOnNonPositive(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 1}
	store := NewClaimStoreWithStale(s, 0)
	_ = store.Claim(context.Background(), KindBuild, "bld-1", "pod-a")
	// Stale default is 2 minutes = 120 seconds.
	if s.lastExecArgs[2] != int64(120) {
		t.Errorf("expected default stale = 120s, got %v", s.lastExecArgs[2])
	}
}
