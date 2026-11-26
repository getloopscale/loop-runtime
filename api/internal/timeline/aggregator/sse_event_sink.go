// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

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

// SSEOnceAppender is a store that can write an event only when its stream holds
// none of that type yet, as one decision.
type SSEOnceAppender interface {
	AppendOnce(ctx context.Context, buildID string, eventType string, data json.RawMessage) (id int64, written bool, err error)
}

// AppendDeployTimelineEvent satisfies the EventSink interface.
//
// A phase event is written once per deploy when the store can promise that. Two
// aggregators walk the same deploy while a restart is being recovered from (the
// one the submit started and the one that resumed the timeline), and both reach
// the same phases. A degraded event is an occurrence and may repeat.
func (s *SSEEventSink) AppendDeployTimelineEvent(ctx context.Context, deployID, eventType string, data json.RawMessage) (int64, error) {
	once, ok := s.Appender.(SSEOnceAppender)
	if !ok || eventType == AggregatorDegradedEventType {
		return s.Appender.Append(ctx, deployID, eventType, data)
	}
	id, _, err := once.AppendOnce(ctx, deployID, eventType, data)
	return id, err
}
