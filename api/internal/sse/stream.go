// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sse

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// This file is the one server-sent-events primitive every stream endpoint
// composes: a concurrency gate, the response headers and framing, the
// resume anchor, and a poll loop with heartbeats and bounded retry. Each
// handler supplies only its query and its terminal condition, so the
// guarantees — heartbeat, connection cap, bounded retry — are the same on
// every stream instead of re-implemented five times with five answers.

// ErrStreamingUnsupported reports a ResponseWriter that cannot flush, which
// an SSE response requires (some proxies wrap the writer).
var ErrStreamingUnsupported = errors.New("sse: response writer does not support flushing")

// ErrClientGone wraps a failed write to the client: the connection is gone,
// so the stream ends at once and nothing is retried.
var ErrClientGone = errors.New("sse: client connection gone")

// ErrPollRetriesExhausted is returned when the poll failed on
// MaxConsecutiveErrors consecutive ticks; it wraps the last failure.
var ErrPollRetriesExhausted = errors.New("sse: poll failed repeatedly")

// Gate caps the number of simultaneous streams an endpoint serves.
type Gate struct {
	max    int64
	active atomic.Int64
}

// NewGate returns a gate admitting at most max streams; max <= 0 selects
// defaultMaxConcurrentStreams.
func NewGate(max int64) *Gate {
	if max <= 0 {
		max = defaultMaxConcurrentStreams
	}
	return &Gate{max: max}
}

// Enter admits one stream. When ok, leave must be called when the stream
// ends; the active-connections gauge follows the admitted count.
func (g *Gate) Enter() (leave func(), ok bool) {
	if g.active.Add(1) > g.max {
		g.active.Add(-1)
		return nil, false
	}
	metrics.SSEConnectionsActive.Inc()
	return func() {
		metrics.SSEConnectionsActive.Dec()
		g.active.Add(-1)
	}, true
}

// Active reports the number of admitted streams.
func (g *Gate) Active() int64 { return g.active.Load() }

// Max reports the admission limit.
func (g *Gate) Max() int64 { return g.max }

// WriteTooManyStreams answers a request the gate refused.
func WriteTooManyStreams(w http.ResponseWriter) {
	http.Error(w, `{"code":"TOO_MANY_STREAMS","message":"Too many active streams. Try again shortly."}`,
		http.StatusServiceUnavailable)
}

// Stream is an open text/event-stream response.
type Stream struct {
	w http.ResponseWriter
	f http.Flusher
}

// Open writes the SSE response headers and flushes them, so the client
// knows the connection is established before the first event.
func Open(w http.ResponseWriter) (*Stream, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, ErrStreamingUnsupported
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // prevent proxy buffering (Nginx, CF)
	w.WriteHeader(http.StatusOK)
	f.Flush()
	return &Stream{w: w, f: f}, nil
}

// Send writes one named event. An empty id omits the id line (a control
// event the client must not resume from).
func (s *Stream) Send(id, eventType string, data []byte) error {
	var err error
	if id == "" {
		_, err = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", eventType, data)
	} else {
		_, err = fmt.Fprintf(s.w, "id: %s\nevent: %s\ndata: %s\n\n", id, eventType, data)
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrClientGone, err)
	}
	s.f.Flush()
	metrics.SSEEventsEmitted.WithLabelValues(eventType).Inc()
	return nil
}

// SendEvent writes a stored event with its sequence id.
func (s *Stream) SendEvent(ev SSEEvent) error {
	return s.Send(strconv.FormatInt(ev.ID, 10), ev.EventType, ev.Data)
}

// Heartbeat writes an SSE comment, which keeps idle proxies from closing
// the connection and lets the server notice a client that went away.
func (s *Stream) Heartbeat() error {
	if _, err := fmt.Fprint(s.w, ":heartbeat\n\n"); err != nil {
		return fmt.Errorf("%w: %w", ErrClientGone, err)
	}
	s.f.Flush()
	return nil
}

// LastEventID is the resume anchor a reconnecting EventSource sends; 0
// when absent or unparseable.
func LastEventID(r *http.Request) int64 {
	if v := r.Header.Get("Last-Event-ID"); v != "" {
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return id
		}
	}
	return 0
}

// PollFunc performs one poll: it sends whatever is new and reports whether
// the stream is finished. A returned error is a failure of the SOURCE and is
// retried; ErrClientGone (from a Send) ends the stream immediately.
type PollFunc func(ctx context.Context) (done bool, err error)

// Loop drives a stream: a poll every Interval, a heartbeat every Heartbeat
// (none when zero), until the context ends, the poll reports done, the
// client is gone, or the poll fails MaxConsecutiveErrors ticks in a row.
type Loop struct {
	Interval             time.Duration
	Heartbeat            time.Duration
	MaxConsecutiveErrors int
}

// Run returns nil when the stream ended normally (context over or poll
// done), an ErrClientGone error when the client went away, and
// ErrPollRetriesExhausted when the source kept failing.
func (l Loop) Run(ctx context.Context, s *Stream, poll PollFunc) error {
	maxErrors := l.MaxConsecutiveErrors
	if maxErrors <= 0 {
		maxErrors = maxPollRetries
	}
	pollTicker := time.NewTicker(l.Interval)
	defer pollTicker.Stop()
	var heartbeat <-chan time.Time
	if l.Heartbeat > 0 {
		t := time.NewTicker(l.Heartbeat)
		defer t.Stop()
		heartbeat = t.C
	}

	failures := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-heartbeat:
			if err := s.Heartbeat(); err != nil {
				return err
			}
		case <-pollTicker.C:
			done, err := poll(ctx)
			switch {
			case err == nil:
				failures = 0
				if done {
					return nil
				}
			case errors.Is(err, ErrClientGone):
				return err
			case ctx.Err() != nil:
				return nil
			default:
				failures++
				if failures >= maxErrors {
					return fmt.Errorf("%w after %d attempts: %w", ErrPollRetriesExhausted, failures, err)
				}
			}
		}
	}
}
