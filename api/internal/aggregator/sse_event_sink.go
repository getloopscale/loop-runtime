// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"encoding/json"
)

// SSEAppender is the minimal subset of sse.EventStore.Append the
// aggregator needs. Decoupled here so we don't import the sse package
// directly (avoids cyclic imports through db).
type SSEAppender interface {
	Append(ctx context.Context, buildID string, eventType string, data json.RawMessage) (int64, error)
}

// SSEEventSink adapts an SSE event store to the aggregator's EventSink.
// The aggregator's deployID is forwarded as the underlying store's
// buildID — this reuses the same sse_events table without a migration.
//
// Plan T4.1: the canonical write-path adapter. Production wires
// store.PostgresEventStore here; tests use SSEAppender stubs.
//
// Per ADR D8 the EventStore enforces tenant_id at SQL level via a join
// on the builds table — same defense applies here because deploy IDs
// are validated upstream by the BuildOrchestrator before spawn.
type SSEEventSink struct {
	Appender SSEAppender
}

// NewSSEEventSink constructs the sink.
func NewSSEEventSink(a SSEAppender) *SSEEventSink {
	return &SSEEventSink{Appender: a}
}

// AppendDeployTimelineEvent satisfies the EventSink interface.
func (s *SSEEventSink) AppendDeployTimelineEvent(ctx context.Context, deployID, eventType string, data json.RawMessage) (int64, error) {
	return s.Appender.Append(ctx, deployID, eventType, data)
}
