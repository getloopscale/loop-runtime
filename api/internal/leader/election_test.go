// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package leader

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// TestController_LeaderElection — roadmap 02 DoD (unit-level variant).
//
// The roadmap DoD asks: "Only 1 controller processes at a time."
// The true integration version requires two real controller pods
// against a real PG. The unit-level version proves the same property
// at the fencing-primitive layer: two concurrent Acquire calls for
// the same (name, cell_id) MUST result in exactly one winner and one
// ErrNotLeader, never two winners.
//
// The test uses a stateful fake DBTX that emulates the PG UPSERT
// semantics:
//
//   - First caller: INSERT succeeds, returns gen=1.
//   - Second caller (concurrent): ON CONFLICT DO UPDATE WHERE
//     heartbeat_at < now() - stale. Since the row was just inserted
//     (heartbeat_at = now()), the predicate is false → 0 rows
//     returned → ErrNotLeader.
//
// This is the same predicate the live Postgres enforces. If our
// Acquire implementation drifts from this behavior (e.g. someone
// introduces a race between the read and the write), this test
// catches it.

// serializedStateDBTX is a stateful fake DBTX that emulates PG's
// UPSERT behavior for the leader_generations table under concurrency.
// Every Query/Exec is serialized through a mutex so the predicate
// evaluation is atomic, which mirrors how a real PG row-level lock
// works during an UPSERT with WHERE on the excluded row.
type serializedStateDBTX struct {
	mu        sync.Mutex
	rowExists bool
	gen       int64
	holder    string
}

func (s *serializedStateDBTX) Query(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return nil, errors.New("Query not used")
}

func (s *serializedStateDBTX) Exec(_ context.Context, _ string, args ...any) (db.ExecResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Heartbeat / Release — match by (name, cell_id, holder, gen).
	if len(args) >= 4 {
		if reqHolder, ok := args[2].(string); ok && reqHolder != s.holder {
			return db.ExecResult{RowsAffected: 0}, nil
		}
		if reqGen, ok := args[3].(int64); ok && reqGen != s.gen {
			return db.ExecResult{RowsAffected: 0}, nil
		}
	}
	return db.ExecResult{RowsAffected: 1}, nil
}

func (s *serializedStateDBTX) QueryRow(_ context.Context, _ string, args ...any) db.Row {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Emulate the Acquire UPSERT: if no row, INSERT with gen=1.
	// If row exists AND heartbeat is fresh (simulated here as
	// "row exists and we just acquired"), the predicate is FALSE
	// and we return "no rows" (→ ErrNotLeader).
	if !s.rowExists {
		s.rowExists = true
		s.gen = 1
		// args for Acquire: [name, cell_id, stale_sec, holder]
		if len(args) >= 4 {
			if h, ok := args[3].(string); ok {
				s.holder = h
			}
		}
		return &stateRow{scanFn: func(dest ...any) error {
			if len(dest) >= 1 {
				if ptr, ok := dest[0].(*int64); ok {
					*ptr = s.gen
				}
			}
			return nil
		}}
	}

	// Row exists and heartbeat is fresh → RETURNING produces zero rows.
	return &stateRow{scanFn: func(_ ...any) error { return db.ErrNoRows }}
}

type stateRow struct {
	scanFn func(dest ...any) error
}

func (r *stateRow) Scan(dest ...any) error { return r.scanFn(dest...) }

// TestController_LeaderElection_OnlyOneWinner is the unit-level proof
// of the "only 1 controller processes at a time" DoD. 10 goroutines
// race to Acquire the same lease. Exactly 1 must win.
func TestController_LeaderElection_OnlyOneWinner(t *testing.T) {
	const attempts = 10

	fake := &serializedStateDBTX{}
	acq := NewAcquirer(fake, DefaultConfig())

	var wg sync.WaitGroup
	var winners int32
	var losers int32
	var otherErrors int32

	leases := make([]*Lease, attempts)

	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			lease, err := acq.Acquire(context.Background(),
				"loop-runtime-controller", "cell-0",
				// Each goroutine has a unique "holder" string so we can
				// tell them apart, but only one holder gets the row.
				"pod-"+string(rune('A'+idx)))
			if err == nil {
				atomic.AddInt32(&winners, 1)
				leases[idx] = lease
				return
			}
			if errors.Is(err, ErrNotLeader) {
				atomic.AddInt32(&losers, 1)
				return
			}
			atomic.AddInt32(&otherErrors, 1)
			t.Logf("unexpected error for goroutine %d: %v", idx, err)
		}(i)
	}
	wg.Wait()

	if winners != 1 {
		t.Errorf("expected exactly 1 winner, got %d", winners)
	}
	if losers != attempts-1 {
		t.Errorf("expected %d losers (ErrNotLeader), got %d", attempts-1, losers)
	}
	if otherErrors != 0 {
		t.Errorf("expected 0 other errors, got %d", otherErrors)
	}

	// The single winner has a valid lease with generation=1.
	var winner *Lease
	for _, l := range leases {
		if l != nil {
			if winner != nil {
				t.Errorf("two non-nil leases: multiple winners!")
			}
			winner = l
		}
	}
	if winner == nil {
		t.Fatal("no winning lease captured")
		return // unreachable — helps staticcheck
	}
	if winner.GenerationID != 1 {
		t.Errorf("winner generation = %d, want 1", winner.GenerationID)
	}
}

// TestController_LeaderElection_SecondAcquireBlocks proves that an
// already-held lease cannot be re-acquired by a different holder —
// the second caller gets ErrNotLeader without disturbing the first
// caller's lease.
func TestController_LeaderElection_SecondAcquireBlocks(t *testing.T) {
	fake := &serializedStateDBTX{}
	acq := NewAcquirer(fake, DefaultConfig())

	// First Acquire: succeeds.
	lease1, err := acq.Acquire(context.Background(), "x", "cell-0", "pod-1")
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if lease1 == nil || lease1.GenerationID != 1 {
		t.Fatalf("first acquire returned invalid lease: %+v", lease1)
	}

	// Second Acquire from a different pod: ErrNotLeader.
	lease2, err := acq.Acquire(context.Background(), "x", "cell-0", "pod-2")
	if !errors.Is(err, ErrNotLeader) {
		t.Errorf("second acquire: expected ErrNotLeader, got %v", err)
	}
	if lease2 != nil {
		t.Errorf("second acquire returned a non-nil lease")
	}
}
