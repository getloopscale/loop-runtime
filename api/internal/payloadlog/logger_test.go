// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package payloadlog

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// TestDispatcher_FullQueueDropsCounted: cap=1, no worker draining, 3 enqueues →
// 1 buffered + 2 dropped, counter == 2 (M14 gap #6, ADR D3 drop-with-metric).
func TestDispatcher_FullQueueDropsCounted(t *testing.T) {
	d := New(Config{QueueSize: 1, Scrub: nil}) // no worker started
	for i := 0; i < 3; i++ {
		d.Enqueue(Record{Body: "x"})
	}
	if got := testutil.ToFloat64(d.dropped); got != 2 {
		t.Errorf("dropped counter = %v, want 2 (cap=1, 3 enqueues → 2 dropped)", got)
	}
}

// TestDispatcher_ScrubAppliedBeforeQueue: the scrub hook runs BEFORE the record
// enters the queue — raw PII never sits in the buffer.
func TestDispatcher_ScrubAppliedBeforeQueue(t *testing.T) {
	scrub := func(r Record) Record { r.Body = "REDACTED"; return r }
	d := New(Config{QueueSize: 4, Scrub: scrub})
	d.Enqueue(Record{Body: "secret-pii"})
	got := <-d.queue
	if got.Body != "REDACTED" {
		t.Errorf("queued body = %q, want REDACTED (scrub must run before enqueue)", got.Body)
	}
}

// TestDispatcher_ScrubNoPIINoOp (EC-5): a scrub on an already-clean record is a
// no-op — no corruption, no drop.
func TestDispatcher_ScrubNoPIINoOp(t *testing.T) {
	scrub := func(r Record) Record { return r } // identity
	d := New(Config{QueueSize: 4, Scrub: scrub})
	d.Enqueue(Record{Body: "clean"})
	got := <-d.queue
	if got.Body != "clean" {
		t.Errorf("queued body = %q, want clean (no-op scrub must not corrupt)", got.Body)
	}
	if v := testutil.ToFloat64(d.dropped); v != 0 {
		t.Errorf("dropped = %v, want 0 (clean record must not drop)", v)
	}
}

// TestDispatcher_NeverBlocks: Enqueue on a full queue returns within a tight
// timeout — the request path never blocks (D3 vs kserve's blocking producer).
func TestDispatcher_NeverBlocks(t *testing.T) {
	d := New(Config{QueueSize: 1})
	d.Enqueue(Record{Body: "fill"}) // buffer now full
	done := make(chan struct{})
	go func() {
		d.Enqueue(Record{Body: "overflow"}) // must NOT block
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Enqueue blocked on a full queue — request path must never block (D3)")
	}
}

// TestDispatcher_AtomicCounterInvariant (EC-6): N concurrent producers on a
// cap=1 queue; every enqueue either buffers or drops, never lost. Run under
// -race. processed(≤1 buffered) + dropped == N.
func TestDispatcher_AtomicCounterInvariant(t *testing.T) {
	const n = 200
	d := New(Config{QueueSize: 1})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); d.Enqueue(Record{Body: "x"}) }()
	}
	wg.Wait()
	buffered := float64(len(d.queue))
	dropped := testutil.ToFloat64(d.dropped)
	if buffered+dropped != n {
		t.Errorf("buffered(%v) + dropped(%v) = %v, want %d (no lost update)", buffered, dropped, buffered+dropped, n)
	}
}
