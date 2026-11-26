// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package slidingwindow counts requests per key over a sliding window and
// refuses the ones above a limit.
//
// It is the one implementation behind every in-process request limiter in
// the API — per tenant, per client IP, per (tenant, domain) re-verification,
// and the auth-failure audit throttle. They differ only in what they key on,
// so they share the arithmetic and, crucially, the eviction: a limiter keyed
// by something a client controls (an IP, a /64) is a map an attacker can grow,
// and a map without eviction is a memory leak with a public entry point.
//
// The algorithm is the standard sliding-window counter: the count of the
// previous window is weighted by how much of it still overlaps the trailing
// window. A fixed window lets a client spend its allowance at the end of one
// window and again at the start of the next — twice the limit in less than
// one window (SD-04.3, audit 2026-09-07). golang.org/x/time/rate is a token
// bucket, which admits a configured burst by design; the limiters here promise
// "N per window", so the counter is kept rather than the semantics changed.
package slidingwindow

import (
	"math"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Counter is a keyed sliding-window request counter, safe for concurrent use.
//
// Keys idle for two whole windows no longer weigh on any decision and are
// evicted. Eviction is amortised into Allow — at most one sweep per window —
// so a Counter owns no goroutine and needs no Stop.
type Counter struct {
	mu        sync.Mutex
	counters  map[string]*windowCount
	limit     int
	window    time.Duration
	now       func() time.Time
	lastSweep time.Time
}

type windowCount struct {
	count       int
	prevCount   int
	windowStart time.Time
}

// New returns a Counter admitting limit requests per window for each key.
// now is the clock; nil means time.Now.
func New(limit int, window time.Duration, now func() time.Time) *Counter {
	if now == nil {
		now = time.Now
	}
	return &Counter{
		counters:  make(map[string]*windowCount),
		limit:     limit,
		window:    window,
		now:       now,
		lastSweep: now(),
	}
}

// Allow registers one request against key. It returns whether the request is
// admitted and, when it is not, how long until the current window ends — the
// earliest moment the caller's allowance starts to recover.
func (c *Counter) Allow(key string) (bool, time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := c.now()
	c.sweepLocked(now)

	wc, ok := c.counters[key]
	if !ok {
		wc = &windowCount{windowStart: now}
		c.counters[key] = wc
	}

	// Roll the window forward. One window elapsed keeps the previous count as
	// the overlapping tail; two or more means nothing overlaps any more.
	switch elapsed := now.Sub(wc.windowStart); {
	case elapsed >= 2*c.window:
		wc.prevCount, wc.count, wc.windowStart = 0, 0, now
	case elapsed >= c.window:
		wc.prevCount, wc.count, wc.windowStart = wc.count, 0, wc.windowStart.Add(c.window)
	}

	intoWindow := now.Sub(wc.windowStart)
	overlap := math.Max(0, 1-intoWindow.Seconds()/c.window.Seconds())
	weighted := float64(wc.prevCount)*overlap + float64(wc.count)
	if weighted >= float64(c.limit) {
		return false, c.window - intoWindow
	}
	wc.count++
	return true, 0
}

// Len reports how many keys are currently tracked.
func (c *Counter) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.counters)
}

// sweepLocked drops the keys whose last window started two or more windows
// ago: their weighted count is zero, so a fresh counter is equivalent.
func (c *Counter) sweepLocked(now time.Time) {
	if now.Sub(c.lastSweep) < c.window {
		return
	}
	c.lastSweep = now
	for key, wc := range c.counters {
		if now.Sub(wc.windowStart) >= 2*c.window {
			delete(c.counters, key)
		}
	}
}

// RetryAfterSeconds converts d to an RFC 9110 delay-seconds value: a whole
// number of seconds, rounded up so a client never retries early, and at least
// 1 so a refused client is never told to retry immediately.
func RetryAfterSeconds(d time.Duration) int {
	return max(1, int(math.Ceil(d.Seconds())))
}

// SetRetryAfter writes d as the Retry-After header of w, in delay-seconds.
func SetRetryAfter(w http.ResponseWriter, d time.Duration) {
	w.Header().Set("Retry-After", strconv.Itoa(RetryAfterSeconds(d)))
}
