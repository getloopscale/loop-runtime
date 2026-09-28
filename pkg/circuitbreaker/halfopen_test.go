// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package circuitbreaker

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeClock is a clock that only moves when the test moves it.
type fakeClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = c.at.Add(d)
}

// openBreaker returns a breaker that has just opened, on a fake clock.
func openBreaker(t *testing.T, closeThreshold int, onChange func(string, State, State)) (*Breaker, *fakeClock) {
	t.Helper()
	clock := &fakeClock{at: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)}
	cb := New(Config{Name: "dep", OpenThreshold: 1, CloseThreshold: closeThreshold, RetryInterval: time.Minute}, onChange)
	cb.now = clock.now
	cb.RecordResult(errTest)
	if cb.State() != Open {
		t.Fatalf("setup: want Open, got %v", cb.State())
	}
	return cb, clock
}

// Half-open exists to send ONE probe to a recovering dependency. Every caller
// used to pass once the retry interval elapsed, so a burst hit the dependency
// with full traffic the moment the breaker "half-opened".
func TestBreaker_HalfOpenLetsExactlyOneConcurrentProbeThrough(t *testing.T) {
	cb, clock := openBreaker(t, 1, nil)
	clock.advance(time.Minute)

	const callers = 50
	var wg sync.WaitGroup
	var mu sync.Mutex
	admitted := 0
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if cb.PreCheck() == nil {
				mu.Lock()
				admitted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if admitted != 1 {
		t.Fatalf("half-open admitted %d concurrent probes, want exactly 1", admitted)
	}
}

func TestBreaker_HalfOpenAdmitsTheNextProbeOnceTheFirstReports(t *testing.T) {
	cb, clock := openBreaker(t, 2, nil)
	clock.advance(time.Minute)

	if err := cb.PreCheck(); err != nil {
		t.Fatalf("first probe must pass: %v", err)
	}
	if err := cb.PreCheck(); err == nil {
		t.Fatal("a second probe must wait while the first is in flight")
	}
	cb.RecordResult(nil) // 1 of 2 successes needed to close

	if err := cb.PreCheck(); err != nil {
		t.Fatalf("the next probe must pass once the first reported: %v", err)
	}
	cb.RecordResult(nil)
	if cb.State() != Closed {
		t.Fatalf("two successful probes must close the breaker, got %v", cb.State())
	}
}

// An excluded error (the caller's own cancellation) reports nothing about the
// dependency, but it must still free the probe slot, or the breaker would
// stay half-open refusing every call.
func TestExecute_ExcludedErrorFreesTheHalfOpenProbe(t *testing.T) {
	cb, clock := openBreaker(t, 1, nil)
	clock.advance(time.Minute)

	_, err := Execute(context.Background(), cb, func(context.Context) (int, error) { return 0, context.Canceled })
	if err == nil {
		t.Fatal("the excluded error must reach the caller")
	}

	if err := cb.PreCheck(); err != nil {
		t.Fatalf("the probe slot must be free after an excluded error: %v", err)
	}
}

// A probe that never reports would otherwise wedge the breaker half-open; after
// another retry interval a new probe is admitted.
func TestBreaker_AbandonedProbeIsReplacedAfterTheRetryInterval(t *testing.T) {
	cb, clock := openBreaker(t, 1, nil)
	clock.advance(time.Minute)
	if err := cb.PreCheck(); err != nil {
		t.Fatalf("first probe must pass: %v", err)
	}

	clock.advance(time.Minute)

	if err := cb.PreCheck(); err != nil {
		t.Fatalf("a probe unreported for a full retry interval must be replaced: %v", err)
	}
}

// The state-change callback runs outside the breaker's lock, so a callback
// that reads the breaker (for a metric label, say) does not deadlock.
func TestBreaker_StateChangeCallbackMayReadTheBreaker(t *testing.T) {
	seen := make(chan State, 4)
	var cb *Breaker
	cb = New(Config{Name: "dep", OpenThreshold: 1, RetryInterval: time.Minute},
		func(_ string, _, _ State) { seen <- cb.State() })
	finished := make(chan struct{})

	go func() {
		defer close(finished)
		cb.RecordResult(errTest)
	}()

	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("state-change callback deadlocked reading the breaker")
	}
	if got := <-seen; got != Open {
		t.Fatalf("callback saw %v, want Open", got)
	}
}
