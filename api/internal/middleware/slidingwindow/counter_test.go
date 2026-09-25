// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package slidingwindow

import (
	"fmt"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 9, 25, 12, 0, 0, 0, time.UTC)} }

func TestCounter_AdmitsExactlyTheLimitWithinOneWindow(t *testing.T) {
	clock := newClock()
	c := New(3, time.Minute, clock.now)

	for i := 0; i < 3; i++ {
		if ok, _ := c.Allow("k"); !ok {
			t.Fatalf("request %d of 3 refused inside one window", i+1)
		}
	}
	if ok, _ := c.Allow("k"); ok {
		t.Fatal("fourth request admitted; the limit is 3")
	}
}

func TestCounter_BoundaryBurstIsWeightedByThePreviousWindow(t *testing.T) {
	clock := newClock()
	c := New(10, time.Minute, clock.now)
	for i := 0; i < 10; i++ {
		c.Allow("k")
	}

	clock.advance(time.Minute + time.Second)

	admitted := 0
	for i := 0; i < 10; i++ {
		if ok, _ := c.Allow("k"); ok {
			admitted++
		}
	}
	if admitted > 1 {
		t.Errorf("admitted %d right after the boundary; the previous window still overlaps ~98%%", admitted)
	}
}

func TestCounter_RecoversTheWholeAllowanceAfterTwoWindows(t *testing.T) {
	clock := newClock()
	c := New(2, time.Minute, clock.now)
	c.Allow("k")
	c.Allow("k")

	clock.advance(2 * time.Minute)

	for i := 0; i < 2; i++ {
		if ok, _ := c.Allow("k"); !ok {
			t.Fatalf("request %d refused after two idle windows", i+1)
		}
	}
}

func TestCounter_RetryAfterIsTheRemainderOfTheCurrentWindow(t *testing.T) {
	clock := newClock()
	c := New(1, time.Minute, clock.now)
	c.Allow("k")
	clock.advance(15 * time.Second)

	ok, retryAfter := c.Allow("k")
	if ok {
		t.Fatal("second request admitted with a limit of 1")
	}
	if retryAfter != 45*time.Second {
		t.Errorf("retryAfter = %s, want 45s", retryAfter)
	}
}

func TestCounter_EvictsKeysIdleForTwoWindows(t *testing.T) {
	clock := newClock()
	c := New(5, time.Minute, clock.now)
	for i := 0; i < 1000; i++ {
		c.Allow(fmt.Sprintf("rotating-%d", i))
	}
	if got := c.Len(); got != 1000 {
		t.Fatalf("Len = %d before any window elapsed, want 1000", got)
	}

	clock.advance(2*time.Minute + time.Second)
	c.Allow("fresh")

	if got := c.Len(); got != 1 {
		t.Errorf("Len = %d after the rotating keys sat idle for two windows, want 1 (only the fresh key)", got)
	}
}

func TestCounter_KeepsKeysThatStillOverlapTheTrailingWindow(t *testing.T) {
	clock := newClock()
	c := New(5, time.Minute, clock.now)
	c.Allow("recent")

	clock.advance(90 * time.Second)
	c.Allow("other")

	if got := c.Len(); got != 2 {
		t.Errorf("Len = %d, want 2: a key last seen 90s ago still weighs on a 60s sliding window", got)
	}
}

func TestRetryAfterSeconds_RoundsUpToWholeSeconds(t *testing.T) {
	cases := map[time.Duration]int{
		45300 * time.Millisecond: 46,
		30 * time.Second:         30,
		0:                        1,
		-time.Second:             1,
		200 * time.Millisecond:   1,
	}
	for in, want := range cases {
		if got := RetryAfterSeconds(in); got != want {
			t.Errorf("RetryAfterSeconds(%s) = %d, want %d", in, got, want)
		}
	}
}

func TestSetRetryAfter_WritesIntegerDelaySeconds(t *testing.T) {
	rec := httptest.NewRecorder()
	SetRetryAfter(rec, 45300*time.Millisecond)
	if got := rec.Header().Get("Retry-After"); got != "46" {
		t.Errorf("Retry-After = %q, want \"46\" (RFC 9110 delay-seconds is an integer)", got)
	}
}
