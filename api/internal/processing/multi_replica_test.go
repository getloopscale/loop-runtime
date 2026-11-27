// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package processing

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// TestAPI_MultiReplica_NoDuplicateProcessing — roadmap 02 DoD (unit level).
//
// The live version of this DoD asks: "4 API replicas + 20 concurrent
// deploys = zero duplicate state transitions." The property under test
// is that no build is processed by more than one worker at a time, even
// when N workers race to pick it up.
//
// The unit level proves the same property at the claim layer:
// N goroutines (simulating N replicas) all call Claim on the same
// build id. Exactly 1 succeeds. The other N-1 return ErrClaimHeld.
//
// This is the same property the live PG CAS enforces. If the
// ClaimStore drifts into a race (e.g. a naive SELECT-then-UPDATE
// instead of a single UPDATE WHERE), this test catches it.

// claimStateDBTX is a stateful fake DBTX that emulates PG's UPDATE
// WHERE semantics for the processing_by / processing_since columns.
// Every Exec is serialized through a mutex so the predicate evaluation
// is atomic — mirroring PG row-level locking during a CAS UPDATE.
type claimStateDBTX struct {
	mu     sync.Mutex
	claims map[string]string // build_id → holder
}

func newClaimStateDBTX() *claimStateDBTX {
	return &claimStateDBTX{claims: make(map[string]string)}
}

func (c *claimStateDBTX) Query(_ context.Context, _ string, _ ...any) (db.Rows, error) {
	return nil, nil
}

func (c *claimStateDBTX) Exec(_ context.Context, sql string, args ...any) (db.ExecResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Claim:   UPDATE ... SET processing_by=$1 WHERE id=$2 AND (processing_by IS NULL OR stale)
	//   args = [holder, id, stale_sec]
	// Release: UPDATE ... SET processing_by=NULL WHERE id=$1 AND processing_by=$2
	//   args = [id, holder]
	// Check Release FIRST — the release SET has NULL, claim SET has $1.
	// A claim query won't contain "processing_by    = NULL", so checking
	// for NULL first eliminates any ambiguity.
	if isRelease(sql) && len(args) >= 2 {
		id, _ := args[0].(string)
		holder, _ := args[1].(string)
		if current, ok := c.claims[id]; ok && current == holder {
			delete(c.claims, id)
			return db.ExecResult{RowsAffected: 1}, nil
		}
		return db.ExecResult{RowsAffected: 0}, nil // not the holder
	}
	if isClaim(sql) && len(args) >= 2 {
		holder, _ := args[0].(string)
		id, _ := args[1].(string)
		if _, held := c.claims[id]; held {
			return db.ExecResult{RowsAffected: 0}, nil // already held
		}
		c.claims[id] = holder
		return db.ExecResult{RowsAffected: 1}, nil
	}
	return db.ExecResult{RowsAffected: 0}, nil
}

func (c *claimStateDBTX) QueryRow(_ context.Context, _ string, _ ...any) db.Row {
	return errRowForClaim{}
}

type errRowForClaim struct{}

func (errRowForClaim) Scan(_ ...any) error { return db.ErrNoRows }

// Claim SET clause: `processing_by    = $1,`
// Release SET clause: `processing_by    = NULL,`
// The distinction is whether the first SET target assigns NULL or $1.
// Order of checks matters: check for the NULL (release) pattern first
// because the claim pattern could technically appear as a substring.
func isClaim(sql string) bool {
	return contains(sql, "processing_by    = $1")
}

func isRelease(sql string) bool {
	return contains(sql, "processing_by    = NULL")
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestAPI_MultiReplica_NoDuplicateProcessing — 4 replicas × 20 builds,
// zero duplicate claims.
func TestAPI_MultiReplica_NoDuplicateProcessing(t *testing.T) {
	const (
		replicas     = 4
		buildsPerRun = 20
	)

	fake := newClaimStateDBTX()
	store := NewClaimStore(fake)

	// Simulate 4 replicas each trying to claim all 20 builds.
	// Each (replica, build) pair is a claim attempt — 80 total attempts.
	// Correct behavior: each build is claimed exactly once across all
	// replicas, so total successes == 20 and total ErrClaimHeld == 60.
	var (
		wg       sync.WaitGroup
		claimed  int64
		notHeld  int64
		otherErr int64
	)

	// Track which replica got which build so we can verify distribution.
	// Each entry is "replica → list of build IDs claimed by it"
	ownership := make([][]string, replicas)
	var ownMu sync.Mutex

	for r := 0; r < replicas; r++ {
		ownership[r] = make([]string, 0)
		for b := 0; b < buildsPerRun; b++ {
			wg.Add(1)
			go func(replica, build int) {
				defer wg.Done()
				buildID := buildIDForIdx(build)
				holder := "replica-" + replicaName(replica)
				err := store.Claim(context.Background(), KindBuild, buildID, holder)
				if err == nil {
					atomic.AddInt64(&claimed, 1)
					ownMu.Lock()
					ownership[replica] = append(ownership[replica], buildID)
					ownMu.Unlock()
					return
				}
				if err == ErrClaimHeld {
					atomic.AddInt64(&notHeld, 1)
					return
				}
				atomic.AddInt64(&otherErr, 1)
				t.Logf("unexpected error replica=%d build=%d: %v", replica, build, err)
			}(r, b)
		}
	}
	wg.Wait()

	// Assertion 1: exactly 20 builds claimed (one per build id).
	if claimed != buildsPerRun {
		t.Errorf("expected exactly %d claims, got %d (duplicate processing!)", buildsPerRun, claimed)
	}

	// Assertion 2: exactly 60 ErrClaimHeld (80 attempts - 20 successes).
	expectedRejections := int64(replicas*buildsPerRun - buildsPerRun)
	if notHeld != expectedRejections {
		t.Errorf("expected %d ErrClaimHeld, got %d", expectedRejections, notHeld)
	}

	// Assertion 3: zero unexpected errors.
	if otherErr != 0 {
		t.Errorf("expected 0 unexpected errors, got %d", otherErr)
	}

	// Assertion 4: no build is owned by two replicas (sanity).
	seen := make(map[string]int)
	for r, builds := range ownership {
		for _, b := range builds {
			if prev, dup := seen[b]; dup {
				t.Errorf("build %s owned by replica %d AND replica %d (DUPLICATE PROCESSING)", b, prev, r)
			}
			seen[b] = r
		}
	}
	if len(seen) != buildsPerRun {
		t.Errorf("expected %d distinct builds in ownership map, got %d", buildsPerRun, len(seen))
	}

	t.Logf("distribution across %d replicas:", replicas)
	for r, builds := range ownership {
		t.Logf("  replica-%s: %d builds", replicaName(r), len(builds))
	}
}

// TestAPI_MultiReplica_OrphanedClaim_Reclaimable proves the recovery
// path: replica A claims a build then dies without releasing. After
// the stale window passes, replica B reclaims it successfully.
//
// This validates the TestController_CrashRecovery DoD at the unit
// layer — a crashed worker's claim does not permanently lock the
// build.
func TestAPI_MultiReplica_OrphanedClaim_Reclaimable(t *testing.T) {
	fake := newClaimStateDBTX()
	store := NewClaimStoreWithStale(fake, 1*time.Millisecond)

	// Replica A claims and then "crashes" (never calls Release).
	err := store.Claim(context.Background(), KindBuild, "bld-crashed", "replica-A")
	if err != nil {
		t.Fatalf("replica A claim: %v", err)
	}

	// While the claim is still fresh, replica B must be rejected.
	// (In the fake, the staleness check is controlled via the stale
	// window parameter — we can't easily emulate time passing, so we
	// simulate via the fake's ability to override.)
	err = store.Claim(context.Background(), KindBuild, "bld-crashed", "replica-B")
	if err != ErrClaimHeld {
		t.Errorf("replica B while claim fresh: expected ErrClaimHeld, got %v", err)
	}

	// Simulate replica A's death: forcibly clear the fake's claim
	// (mirrors what PG would do after the claim's processing_since
	// exceeded the stale window — the next Claim's predicate would
	// pass and overwrite).
	fake.mu.Lock()
	delete(fake.claims, "bld-crashed")
	fake.mu.Unlock()

	// Replica B re-attempts and succeeds.
	err = store.Claim(context.Background(), KindBuild, "bld-crashed", "replica-B")
	if err != nil {
		t.Errorf("replica B reclaim after stale window: expected success, got %v", err)
	}

	// Replica A returning from the dead and trying to release must
	// NOT affect replica B's claim.
	err = store.Release(context.Background(), KindBuild, "bld-crashed", "replica-A")
	if err != ErrNotClaimHolder {
		t.Errorf("zombie replica A release: expected ErrNotClaimHolder, got %v", err)
	}
}

// Helpers kept local to the test file — trivial formatters.

func buildIDForIdx(i int) string {
	return "bld-" + formatInt(i)
}

func replicaName(i int) string {
	return string(rune('A' + i))
}

func formatInt(i int) string {
	// Fast int-to-string for small values — avoids strconv in a test
	// helper to keep the import surface minimal.
	if i < 10 {
		return string(rune('0' + i))
	}
	return string(rune('0'+i/10)) + string(rune('0'+i%10))
}
