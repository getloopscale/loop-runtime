// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leader

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/domain"
)

// scriptedDBTX is a DBTX fake that lets each test script the next
// QueryRow/Exec response. It is deliberately minimal — the leader package
// calls only Exec and QueryRow, never Query.
type scriptedDBTX struct {
	lastExecSQL   string
	lastExecArgs  []any
	lastQuerySQL  string
	lastQueryArgs []any
	// Next call programmed responses.
	nextExecRows int64
	nextExecErr  error
	// The scan function is called by the fake Row.Scan with the dest args.
	nextRowScan func(dest ...any) error
}

func (s *scriptedDBTX) Query(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return nil, errors.New("Query not used by leader package")
}

func (s *scriptedDBTX) Exec(_ context.Context, sql string, args ...any) (db.ExecResult, error) {
	s.lastExecSQL = sql
	s.lastExecArgs = args
	if s.nextExecErr != nil {
		return db.ExecResult{}, s.nextExecErr
	}
	return db.ExecResult{RowsAffected: s.nextExecRows}, nil
}

func (s *scriptedDBTX) QueryRow(_ context.Context, sql string, args ...any) db.Row {
	s.lastQuerySQL = sql
	s.lastQueryArgs = args
	fn := s.nextRowScan
	if fn == nil {
		fn = func(_ ...any) error { return db.ErrNoRows }
	}
	return scriptedRow{scan: fn}
}

type scriptedRow struct {
	scan func(dest ...any) error
}

func (r scriptedRow) Scan(dest ...any) error { return r.scan(dest...) }

// =============================================================================
// Acquire
// =============================================================================

func TestAcquire_NewLease_ReturnsGenerationFromPG(t *testing.T) {
	// Arrange: PG returns (generation_id=1, acquired_at=<something>).
	s := &scriptedDBTX{
		nextRowScan: func(dest ...any) error {
			if len(dest) < 2 {
				return errors.New("unexpected scan arity")
			}
			*dest[0].(*int64) = 1
			// Leave acquired_at zero — the lease caller doesn't use it for logic.
			return nil
		},
	}
	acq := NewAcquirer(s, DefaultConfig())

	// Act
	lease, err := acq.Acquire(context.Background(), "build-reconcile", "cell-0", "pod-a")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease.GenerationID != 1 {
		t.Errorf("expected generation=1, got %d", lease.GenerationID)
	}
	if lease.Holder != "pod-a" {
		t.Errorf("expected holder=pod-a, got %s", lease.Holder)
	}
	if lease.Name != "build-reconcile" {
		t.Errorf("expected name=build-reconcile, got %s", lease.Name)
	}
	// SQL shape sanity — must be an UPSERT with ON CONFLICT DO UPDATE.
	if !strings.Contains(s.lastQuerySQL, "ON CONFLICT") {
		t.Errorf("expected UPSERT SQL, got:\n%s", s.lastQuerySQL)
	}
	if !strings.Contains(s.lastQuerySQL, "RETURNING") {
		t.Errorf("expected RETURNING clause, got:\n%s", s.lastQuerySQL)
	}
}

func TestAcquire_LiveLeaseHeldByPeer_ReturnsErrNotLeader(t *testing.T) {
	// The UPSERT ON CONFLICT clause has WHERE heartbeat_at < now() - stale.
	// If the existing row's heartbeat is fresh, the update's predicate
	// fails and RETURNING yields zero rows → driver returns ErrNoRows.
	s := &scriptedDBTX{
		nextRowScan: func(_ ...any) error { return db.ErrNoRows },
	}
	acq := NewAcquirer(s, DefaultConfig())

	_, err := acq.Acquire(context.Background(), "build-reconcile", "cell-0", "pod-b")
	if !errors.Is(err, ErrNotLeader) {
		t.Errorf("expected ErrNotLeader, got %v", err)
	}
}

func TestAcquire_EmptyCellIDDefaultsToCellDefault(t *testing.T) {
	s := &scriptedDBTX{
		nextRowScan: func(dest ...any) error {
			*dest[0].(*int64) = 7
			return nil
		},
	}
	acq := NewAcquirer(s, DefaultConfig())
	lease, err := acq.Acquire(context.Background(), "release-convergence", "", "pod-c")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if lease.CellID != domain.DefaultCellID {
		t.Errorf("expected cellID=%q, got %q", domain.DefaultCellID, lease.CellID)
	}
	// The second positional parameter is cell_id.
	if len(s.lastQueryArgs) < 2 || s.lastQueryArgs[1] != domain.DefaultCellID {
		t.Errorf("expected cell_id arg to be %q, got %v", domain.DefaultCellID, s.lastQueryArgs)
	}
}

func TestAcquire_ScanError_IsWrappedNotRetried(t *testing.T) {
	// Any scan error that is NOT ErrNoRows should propagate wrapped.
	boom := errors.New("boom")
	s := &scriptedDBTX{nextRowScan: func(_ ...any) error { return boom }}
	acq := NewAcquirer(s, DefaultConfig())
	_, err := acq.Acquire(context.Background(), "build-reaper", "cell-0", "pod-d")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Errorf("expected wrapped 'boom' error, got %v", err)
	}
}

// =============================================================================
// Heartbeat
// =============================================================================

func TestHeartbeat_RowsAffectedOne_Succeeds(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 1}
	acq := NewAcquirer(s, DefaultConfig())
	lease := &Lease{
		Name: "build-reconcile", CellID: "cell-0",
		Holder: "pod-a", GenerationID: 3,
	}

	if err := acq.Heartbeat(context.Background(), lease); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	// WHERE clause must bind all four identity columns.
	if !strings.Contains(s.lastExecSQL, "WHERE name") || !strings.Contains(s.lastExecSQL, "generation_id") {
		t.Errorf("expected WHERE binding on name+generation, got:\n%s", s.lastExecSQL)
	}
	if len(s.lastExecArgs) != 4 {
		t.Errorf("expected 4 args, got %d: %v", len(s.lastExecArgs), s.lastExecArgs)
	}
	if s.lastExecArgs[3] != int64(3) {
		t.Errorf("expected generation arg = 3, got %v", s.lastExecArgs[3])
	}
}

func TestHeartbeat_ZeroRows_ReturnsErrFenced(t *testing.T) {
	// Another pod bumped the generation. Our UPDATE finds zero rows.
	s := &scriptedDBTX{nextExecRows: 0}
	acq := NewAcquirer(s, DefaultConfig())
	lease := &Lease{Name: "x", CellID: "cell-0", Holder: "pod-a", GenerationID: 5}

	err := acq.Heartbeat(context.Background(), lease)
	if !errors.Is(err, ErrFenced) {
		t.Errorf("expected ErrFenced, got %v", err)
	}
}

func TestHeartbeat_DriverError_IsWrapped(t *testing.T) {
	s := &scriptedDBTX{nextExecErr: errors.New("connection refused")}
	acq := NewAcquirer(s, DefaultConfig())
	lease := &Lease{Name: "x", CellID: "cell-0", Holder: "pod-a", GenerationID: 1}

	err := acq.Heartbeat(context.Background(), lease)
	if err == nil || errors.Is(err, ErrFenced) {
		t.Errorf("expected wrapped driver error (not ErrFenced), got %v", err)
	}
}

func TestHeartbeat_NilLease_IsError(t *testing.T) {
	acq := NewAcquirer(&scriptedDBTX{}, DefaultConfig())
	if err := acq.Heartbeat(context.Background(), nil); err == nil {
		t.Error("expected error for nil lease")
	}
}

// =============================================================================
// Validate — the critical fencing test
// =============================================================================

func TestValidate_SameGenerationSameHolder_Succeeds(t *testing.T) {
	s := &scriptedDBTX{
		nextRowScan: func(dest ...any) error {
			*dest[0].(*int64) = 5
			*dest[1].(*string) = "pod-a"
			return nil
		},
	}
	acq := NewAcquirer(s, DefaultConfig())
	lease := &Lease{Name: "x", CellID: "cell-0", Holder: "pod-a", GenerationID: 5}

	if err := acq.Validate(context.Background(), lease); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

// TestLeaderGeneration_StaleWriteRejected is the test explicitly named in
// roadmap 02 DoD. A stale leader's Validate must be rejected as fenced so
// the loop refuses to execute the state-transition write.
func TestLeaderGeneration_StaleWriteRejected(t *testing.T) {
	// Arrange: PG has moved to generation 10 under pod-b, but our lease
	// still thinks we are generation 5 / pod-a.
	s := &scriptedDBTX{
		nextRowScan: func(dest ...any) error {
			*dest[0].(*int64) = 10
			*dest[1].(*string) = "pod-b"
			return nil
		},
	}
	acq := NewAcquirer(s, DefaultConfig())
	staleLease := &Lease{
		Name: "build-reconcile", CellID: "cell-0",
		Holder: "pod-a", GenerationID: 5,
	}

	err := acq.Validate(context.Background(), staleLease)
	if !errors.Is(err, ErrFenced) {
		t.Fatalf("stale write must be rejected as ErrFenced, got %v", err)
	}
}

func TestValidate_RowMissing_ReturnsErrFenced(t *testing.T) {
	s := &scriptedDBTX{nextRowScan: func(_ ...any) error { return db.ErrNoRows }}
	acq := NewAcquirer(s, DefaultConfig())
	lease := &Lease{Name: "x", CellID: "cell-0", Holder: "pod-a", GenerationID: 1}

	err := acq.Validate(context.Background(), lease)
	if !errors.Is(err, ErrFenced) {
		t.Errorf("missing row must fence, got %v", err)
	}
}

// =============================================================================
// Release
// =============================================================================

func TestRelease_Nil_IsNoop(t *testing.T) {
	acq := NewAcquirer(&scriptedDBTX{}, DefaultConfig())
	if err := acq.Release(context.Background(), nil); err != nil {
		t.Errorf("release(nil) must be no-op, got %v", err)
	}
}

func TestRelease_RowsAffectedOne_Succeeds(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 1}
	acq := NewAcquirer(s, DefaultConfig())
	lease := &Lease{Name: "x", CellID: "cell-0", Holder: "pod-a", GenerationID: 2}

	if err := acq.Release(context.Background(), lease); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
	// Release must zero the heartbeat, not delete the row.
	if !strings.Contains(s.lastExecSQL, "to_timestamp(0)") {
		t.Errorf("expected heartbeat to be zeroed, got SQL:\n%s", s.lastExecSQL)
	}
}

func TestRelease_ZeroRows_ReturnsErrFenced(t *testing.T) {
	s := &scriptedDBTX{nextExecRows: 0}
	acq := NewAcquirer(s, DefaultConfig())
	lease := &Lease{Name: "x", CellID: "cell-0", Holder: "pod-a", GenerationID: 1}

	err := acq.Release(context.Background(), lease)
	if !errors.Is(err, ErrFenced) {
		t.Errorf("release on non-matching lease must fence, got %v", err)
	}
}

// =============================================================================
// Config defaults
// =============================================================================

func TestNewAcquirer_ZeroConfigGetsDefaults(t *testing.T) {
	a := NewAcquirer(&scriptedDBTX{}, Config{})
	if a.cfg.StaleAfter == 0 {
		t.Error("StaleAfter must default to non-zero")
	}
	if a.cfg.HeartbeatInterval == 0 {
		t.Error("HeartbeatInterval must default to non-zero")
	}
}

// Sanity: verify the produced SQL binds exactly the args the Acquire
// contract promises. Catches accidental parameter-order swaps.
func TestAcquire_SQLArgOrder(t *testing.T) {
	s := &scriptedDBTX{
		nextRowScan: func(dest ...any) error { *dest[0].(*int64) = 1; return nil },
	}
	acq := NewAcquirer(s, Config{StaleAfter: 45 * 1_000_000_000})
	_, _ = acq.Acquire(context.Background(), "loop-x", "cell-sfo3-02", "pod-x")

	if len(s.lastQueryArgs) != 4 {
		t.Fatalf("expected 4 args, got %d: %v", len(s.lastQueryArgs), s.lastQueryArgs)
	}
	if s.lastQueryArgs[0] != "loop-x" {
		t.Errorf("arg[0] = name, expected loop-x, got %v", s.lastQueryArgs[0])
	}
	if s.lastQueryArgs[1] != "cell-sfo3-02" {
		t.Errorf("arg[1] = cell, expected cell-sfo3-02, got %v", s.lastQueryArgs[1])
	}
	if s.lastQueryArgs[2] != int64(45) {
		t.Errorf("arg[2] = stale seconds, expected 45, got %v", s.lastQueryArgs[2])
	}
	if s.lastQueryArgs[3] != "pod-x" {
		t.Errorf("arg[3] = holder, expected pod-x, got %v", s.lastQueryArgs[3])
	}
	// Belt and braces: ensure the UPSERT uses the stale interval.
	if !strings.Contains(s.lastQuerySQL, "interval '1 second'") {
		t.Errorf("expected stale predicate using interval '1 second', got:\n%s", s.lastQuerySQL)
	}
}

// Guard: fmt import is only used for the Validate test scaffolding that might
// be added later. Silence unused-import linter by referencing it.
var _ = fmt.Sprintf
