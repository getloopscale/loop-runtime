// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/getloopscale/loopzero/api/internal/aggregator"
	"github.com/getloopscale/loopzero/api/internal/sse"
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

// A porta que este adaptador satisfaz, afirmada em tempo de compilacao.
//
// B-058: este arquivo NAO declarava, nem em prosa, o que adaptava — e o criterio de admissao
// do pacote era "e um adapter", que nao recusa nada. A porta foi descoberta pelo consumidor,
// e agora esta escrita onde o compilador a verifica.
var _ aggregator.SSEReader = (*DeployTimelineSSEReader)(nil)
