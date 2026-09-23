// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"errors"
	"testing"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"golang.org/x/time/rate"
)

// Burst capacity must be asserted without depending on the wall clock.
//
// B-014, measured 2026-08-10: `TestTransitClient_RateLimitBurst` failed with
// `allowed: got 11 want 10`. It made 15 real HTTP calls and counted how many
// passed; with `RateLimitPerSec=10` the bucket refills at 10/s, so the 15 calls
// only had to take longer than 100ms for an extra token to arrive and change the
// count. It failed on timing, not behaviour — `rules/testing.md § 3` calls that a
// bug.
//
// Here time is a parameter. The limiter is the production one; what changes is
// that it no longer reads the machine clock.
func TestAcquireRateToken_BurstIsExactAtAFrozenInstant(t *testing.T) {
	c, _ := newRateLimitedClientAtFixedTime(t, 10)

	var allowed, limited int
	for i := 0; i < 15; i++ {
		err := c.acquireRateToken("tn-burst")
		switch {
		case err == nil:
			allowed++
		case errors.Is(err, ErrRateLimited):
			limited++
		default:
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if allowed != 10 {
		t.Errorf("allowed: %d, want 10 (the burst capacity)", allowed)
	}
	if limited != 5 {
		t.Errorf("refused: %d, want 5 (15 minus the burst)", limited)
	}
}

// Refill is behaviour too, and it is asserted without really waiting: advancing
// the clock by a tenth of a second returns exactly one token at 10/s.
func TestAcquireRateToken_RefillIsDrivenByTheInjectedClock(t *testing.T) {
	c, clock := newRateLimitedClientAtFixedTime(t, 10)

	for i := 0; i < 10; i++ {
		if err := c.acquireRateToken("tn-refill"); err != nil {
			t.Fatalf("the initial burst should pass, failed at %d: %v", i, err)
		}
	}
	if err := c.acquireRateToken("tn-refill"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("an empty bucket should refuse, got %v", err)
	}

	clock.at = clock.at.Add(100 * time.Millisecond)

	if err := c.acquireRateToken("tn-refill"); err != nil {
		t.Fatalf("a tenth of a second at 10/s returns one token; got %v", err)
	}
	if err := c.acquireRateToken("tn-refill"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("it returned ONE token, not two; got %v", err)
	}
}

// Per-tenant isolation is what the burst protects, and it must not depend on
// the clock either: an exhausted tenant cannot consume another's capacity.
func TestAcquireRateToken_IsolationHoldsAtAFrozenInstant(t *testing.T) {
	c, _ := newRateLimitedClientAtFixedTime(t, 5)

	for i := 0; i < 5; i++ {
		if err := c.acquireRateToken("tn-noisy"); err != nil {
			t.Fatalf("the noisy neighbour's burst should pass at %d: %v", i, err)
		}
	}
	if err := c.acquireRateToken("tn-noisy"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("the noisy neighbour should be exhausted, got %v", err)
	}
	for i := 0; i < 5; i++ {
		if err := c.acquireRateToken("tn-quiet"); err != nil {
			t.Fatalf("the quiet tenant has its own bucket, failed at %d: %v", i, err)
		}
	}
}

// frozenClock is a clock that only moves when a test moves it.
type frozenClock struct{ at time.Time }

func (f *frozenClock) now() time.Time { return f.at }

// newRateLimitedClientAtFixedTime builds the client directly, with no HTTP
// server: acquireRateToken does no I/O, and going through the network to
// exercise a token bucket is exactly what made the old test time-dependent.
func newRateLimitedClientAtFixedTime(t *testing.T, perSec int) (*TransitClient, *frozenClock) {
	t.Helper()
	cache, err := lru.New[string, *rate.Limiter](64)
	if err != nil {
		t.Fatalf("limiter cache: %v", err)
	}
	// An arbitrary fixed instant: what matters is that it does not advance on its own.
	clock := &frozenClock{at: time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)}
	return &TransitClient{
		rateLimiters: cache,
		rateLimit:    rate.Limit(perSec),
		rateBurst:    perSec,
		now:          clock.now,
	}, clock
}
