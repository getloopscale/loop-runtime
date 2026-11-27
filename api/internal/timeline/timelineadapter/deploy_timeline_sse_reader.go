// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package timelineadapter

import (
	"context"

	"github.com/getloopscale/loop-runtime/api/internal/sse"
	"github.com/getloopscale/loop-runtime/api/internal/timeline/aggregator"
)

// DeployTimelineSSEReader adapts sse.EventStore.GetAfter to the
// aggregator.SSEReader contract used by PostgresDeployTimelineStore
// (plan T5.1 wiring).
//
// The aggregator package cannot import sse directly (sse imports db,
// db imports aggregator's adapter siblings — cyclic). This adapter
// lives here so production wiring stays single-direction.
type DeployTimelineSSEReader struct {
	Store sse.EventStore
}

// NewDeployTimelineSSEReader constructs the adapter.
func NewDeployTimelineSSEReader(s sse.EventStore) *DeployTimelineSSEReader {
	return &DeployTimelineSSEReader{Store: s}
}

// GetAfter forwards to the underlying sse.EventStore and maps
// sse.SSEEvent → aggregator.SSEEventRow.
func (a *DeployTimelineSSEReader) GetAfter(ctx context.Context, buildID string, afterID int64) ([]aggregator.SSEEventRow, error) {
	events, err := a.Store.GetAfter(ctx, buildID, afterID)
	if err != nil {
		return nil, err
	}
	out := make([]aggregator.SSEEventRow, len(events))
	for i, e := range events {
		out[i] = aggregator.SSEEventRow{
			ID:        e.ID,
			EventType: e.EventType,
			Data:      e.Data,
		}
	}
	return out, nil
}

// The port this adapter satisfies, asserted at compile time.
//
// B-058: this file did NOT declare, not even in prose, what it adapted — and the package's
// admission criterion was "it is an adapter", which refuses nothing. The port was discovered
// from the consumer, and is now written where the compiler checks it.
var _ aggregator.SSEReader = (*DeployTimelineSSEReader)(nil)
