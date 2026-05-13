// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package serviceauth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/loopzero/loopzero/pkg/serviceauth"
)

// TestReplayWindow_AcceptsCurrentTimestamp — happy path: signature
// emitted within the configured tolerance is allowed.
func TestReplayWindow_AcceptsCurrentTimestamp(t *testing.T) {
	now := time.Unix(1715616000, 0)
	rw := serviceauth.NewReplayWindow(60 * time.Second)
	if err := rw.Validate(time.Unix(now.Unix(), 0), now); err != nil {
		t.Fatalf("current ts rejected: %v", err)
	}
}

// TestReplayWindow_RejectsStaleTimestamp — RFC says ±60s; this test
// pins both directions.
func TestReplayWindow_RejectsStaleTimestamp(t *testing.T) {
	now := time.Unix(1715616000, 0)
	rw := serviceauth.NewReplayWindow(60 * time.Second)

	// 61s in the past — outside window
	err := rw.Validate(time.Unix(now.Unix()-61, 0), now)
	if err == nil || !errors.Is(err, serviceauth.ErrTimestampOutsideWindow) {
		t.Errorf("61s past must be rejected with ErrTimestampOutsideWindow, got %v", err)
	}

	// 61s in the future — outside window (catches clock-skew + caller bug)
	err = rw.Validate(time.Unix(now.Unix()+61, 0), now)
	if err == nil || !errors.Is(err, serviceauth.ErrTimestampOutsideWindow) {
		t.Errorf("61s future must be rejected with ErrTimestampOutsideWindow, got %v", err)
	}

	// exactly 60s past — INSIDE window (inclusive boundary)
	err = rw.Validate(time.Unix(now.Unix()-60, 0), now)
	if err != nil {
		t.Errorf("60s past (boundary) must be accepted, got %v", err)
	}
}

// TestNonceStore_BlocksReuseWithinTTL — RFC § Replay protection:
// nonce must be unique per (user_id, nonce) tuple for the TTL window.
func TestNonceStore_BlocksReuseWithinTTL(t *testing.T) {
	ctx := context.Background()
	store := serviceauth.NewInMemoryNonceStore(5 * time.Minute)

	// First use: accepted
	if err := store.Claim(ctx, "user-1", "nonce-abc"); err != nil {
		t.Fatalf("first claim must succeed: %v", err)
	}

	// Second use within TTL: rejected
	err := store.Claim(ctx, "user-1", "nonce-abc")
	if !errors.Is(err, serviceauth.ErrNonceReused) {
		t.Errorf("nonce reuse within TTL must return ErrNonceReused, got %v", err)
	}
}

// TestNonceStore_IsolatesByUser — nonces are scoped per-user. User A
// using nonce "n1" does NOT block User B from using "n1". Without
// per-user scoping, the cache would conflate independent requests and
// become a DoS vector (cheap to fill).
func TestNonceStore_IsolatesByUser(t *testing.T) {
	ctx := context.Background()
	store := serviceauth.NewInMemoryNonceStore(5 * time.Minute)

	if err := store.Claim(ctx, "user-1", "nonce-shared"); err != nil {
		t.Fatalf("user-1 first claim: %v", err)
	}
	if err := store.Claim(ctx, "user-2", "nonce-shared"); err != nil {
		t.Errorf("user-2 same nonce string must be accepted (per-user scope), got %v", err)
	}
}

// TestNonceStore_ExpiresAfterTTL — once the TTL elapses, the nonce
// is collectible again. This is mandatory for steady-state memory
// boundedness — an in-memory store that never forgets explodes.
func TestNonceStore_ExpiresAfterTTL(t *testing.T) {
	ctx := context.Background()
	store := serviceauth.NewInMemoryNonceStore(10 * time.Millisecond)

	if err := store.Claim(ctx, "u", "n"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if err := store.Claim(ctx, "u", "n"); err != nil {
		t.Errorf("after TTL, same nonce must be accepted: %v", err)
	}
}

// TestNonceStore_Concurrent — claims from many goroutines simultaneously
// must yield exactly one success and N-1 ErrNonceReused, never two
// successes (race condition would mean replay protection is broken).
func TestNonceStore_Concurrent(t *testing.T) {
	ctx := context.Background()
	store := serviceauth.NewInMemoryNonceStore(5 * time.Minute)

	const N = 100
	var wg sync.WaitGroup
	successes := make(chan struct{}, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Claim(ctx, "u", "concurrent-nonce"); err == nil {
				successes <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(successes)

	count := 0
	for range successes {
		count++
	}
	if count != 1 {
		t.Fatalf("exactly 1 goroutine must succeed; got %d (race in nonce store)", count)
	}
}
