// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package payloadlog is loopzero's async, opt-in request/response payload logger
// (M14 gap #6, plan ADR D3). It ports KServe's dispatcher→worker→store shape
// (blueprint Q3) but diverges consciously on two traps the kserve tests left
// uncovered (blueprint Q5):
//
//  1. Drop-with-metric, never block. KServe's producer does `WorkQueue <- req`
//     (pkg/logger/worker.go:61) which BLOCKS when the buffer is full — for a
//     multi-tenant control plane with an SLO, blocking the request path on
//     optional observability is unacceptable. Enqueue here uses select+default
//     and counts drops in loopzero_payload_logger_dropped_total.
//  2. Scrub before enqueue. PII is redacted BEFORE a record enters the buffer,
//     so raw sensitive data never sits in memory in the queue.
//
// Status: this package ships the mechanism + tests. Real wiring into a request
// path is DORMANT and gated on a compliance requirement (plan Unresolved Q2 —
// same posture as the conversion webhook D5). The tests exercise the dispatcher
// so it is never dead code.
package payloadlog

import (
	"github.com/prometheus/client_golang/prometheus"
)

// Record is one payload-log entry. Kept minimal; extend when a real consumer
// lands (YAGNI).
type Record struct {
	// Body is the (already-scrubbed, post-Enqueue) payload text.
	Body string
	// Tenant scopes the record; opt-in is per-tenant.
	Tenant string
}

// Config configures a Dispatcher.
type Config struct {
	// QueueSize is the buffered-channel capacity. Full queue → drop-with-metric.
	QueueSize int
	// Scrub redacts PII BEFORE the record is enqueued. nil means identity
	// (no-op) — callers that log potentially-sensitive bodies MUST supply one.
	Scrub func(Record) Record
}

// Dispatcher is the non-blocking front of the payload logger. Enqueue is safe
// for concurrent use and never blocks the caller.
type Dispatcher struct {
	queue   chan Record
	dropped prometheus.Counter
	scrub   func(Record) Record
}

// New builds a Dispatcher. The dropped counter is owned by this dispatcher (not
// the global registry) so it is unit-testable without re-registration panics.
func New(cfg Config) *Dispatcher {
	size := cfg.QueueSize
	if size < 1 {
		size = 1
	}
	scrub := cfg.Scrub
	if scrub == nil {
		scrub = func(r Record) Record { return r } // identity
	}
	return &Dispatcher{
		queue: make(chan Record, size),
		dropped: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "loopzero_payload_logger_dropped_total",
			Help: "Payload-log records dropped because the async queue was full (D3 drop-with-metric).",
		}),
		scrub: scrub,
	}
}

// Enqueue scrubs the record and tries to buffer it. On a full queue it drops
// the record and increments the counter — it NEVER blocks the caller (D3).
func (d *Dispatcher) Enqueue(r Record) {
	r = d.scrub(r)
	select {
	case d.queue <- r:
		// buffered
	default:
		d.dropped.Inc() // full → drop counted, request path never blocks
	}
}

// Collector exposes the drop counter so a real wiring can register it with the
// api's Prometheus registry when the logger leaves dormant status.
func (d *Dispatcher) Collector() prometheus.Collector { return d.dropped }
