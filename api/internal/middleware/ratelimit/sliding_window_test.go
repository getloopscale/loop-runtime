// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ratelimit

import (
	"testing"
	"time"
)

// SD-04.3 (infrastructure system-design audit, 2026-09-07).
//
// A fixed-window counter resets the count at each window boundary, so a client
// can spend its whole allowance at the end of one window and the whole allowance
// again at the start of the next — twice the configured limit delivered inside a
// span shorter than one window. The card's stated inadequate case is burst
// protection, which is precisely where a fixed window fails.
//
// It compounds with SD-04.7: the state is per process, so with 2 replicas the
// worst case is 4x the configured allowance in a boundary span.
//
// The fix is local and needs no shared store: weight the previous window's count
// by how much of it still overlaps the trailing window.
func TestRateLimiter_BoundaryBurstIsNotDoubleTheLimit(t *testing.T) {
	const rate = 10
	const window = time.Second

	rl := NewRateLimiter(rate, window)

	// Spend the whole allowance at the very end of a window.
	allowedFirst := 0
	for i := 0; i < rate; i++ {
		if rl.Allow("tenant-a") {
			allowedFirst++
		}
	}
	if allowedFirst != rate {
		t.Fatalf("first window allowed %d of %d — the limiter is not admitting its own rate", allowedFirst, rate)
	}

	// Cross the boundary. Under a fixed window this resets the count to zero and
	// the next `rate` requests all pass.
	time.Sleep(window + 20*time.Millisecond)

	allowedSecond := 0
	for i := 0; i < rate; i++ {
		if rl.Allow("tenant-a") {
			allowedSecond++
		}
	}

	total := allowedFirst + allowedSecond
	// Immediately after the boundary the previous window still overlaps almost
	// entirely, so a sliding window must admit far fewer than a second full
	// allowance. Anything at or near 2x rate means the window is still fixed.
	if total >= 2*rate {
		t.Errorf("boundary burst admitted %d requests across two adjacent windows (limit %d per window); "+
			"a fixed window lets a client spend its allowance twice in under one window, "+
			"and with 2 replicas that is 4x the configured limit",
			total, rate)
	}
}

// The limiter must still admit its full rate in steady state — a sliding window
// that under-admits is a different defect, not a fix.
func TestRateLimiter_AdmitsFullRateWithinAWindow(t *testing.T) {
	const rate = 5
	rl := NewRateLimiter(rate, time.Second)

	for i := 0; i < rate; i++ {
		if !rl.Allow("tenant-b") {
			t.Fatalf("request %d of %d rejected inside a single window", i+1, rate)
		}
	}
	if rl.Allow("tenant-b") {
		t.Error("limiter admitted one more than its configured rate inside a single window")
	}
}

// A fully elapsed window must recover the whole allowance: once no part of the
// previous window overlaps, the client starts fresh.
func TestRateLimiter_RecoversAfterAFullWindow(t *testing.T) {
	const rate = 3
	const window = 200 * time.Millisecond

	rl := NewRateLimiter(rate, window)

	for i := 0; i < rate; i++ {
		rl.Allow("tenant-c")
	}
	if rl.Allow("tenant-c") {
		t.Fatal("limiter did not stop at its rate")
	}

	// Two full windows: nothing of the spending window overlaps any more.
	time.Sleep(2*window + 20*time.Millisecond)

	for i := 0; i < rate; i++ {
		if !rl.Allow("tenant-c") {
			t.Errorf("request %d rejected after two full windows elapsed; the allowance never recovers", i+1)
		}
	}
}
