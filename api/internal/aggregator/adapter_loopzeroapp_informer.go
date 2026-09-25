// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"fmt"
	"time"
)

// LoopZeroAppPhase is the projection of LoopZeroApplication status the
// adapter emits. Decoupled from operators/ CRD types.
type LoopZeroAppPhase struct {
	Phase     string // PROGRESSING|SERVING|REACHABLE|DEGRADED|DOWN|CLEANED
	UpdatedAt time.Time
	Message   string
}

// LoopZeroAppQuerier abstracts the K8s informer subscription.
type LoopZeroAppQuerier interface {
	QueryLoopZeroAppPhase(ctx context.Context, deployID string) (*LoopZeroAppPhase, error)
}

// LoopZeroAppInformerReader implements SourceReader for loopzeroapp_informer.
type LoopZeroAppInformerReader struct {
	Querier      LoopZeroAppQuerier
	PollInterval time.Duration
}

// NewLoopZeroAppInformerReader constructs the reader.
func NewLoopZeroAppInformerReader(q LoopZeroAppQuerier) *LoopZeroAppInformerReader {
	return &LoopZeroAppInformerReader{Querier: q, PollInterval: 2 * time.Second}
}

// ID identifies the source.
func (t *LoopZeroAppInformerReader) ID() SourceID { return SourceLoopZeroAppInformer }

// Stream polls LoopZeroApp phase until REACHABLE OR ctx cancel.
func (t *LoopZeroAppInformerReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	if t.PollInterval <= 0 {
		t.PollInterval = 2 * time.Second
	}

	emit := func() (bool, error) {
		st, err := t.Querier.QueryLoopZeroAppPhase(ctx, deployID)
		if err != nil {
			return false, fmt.Errorf("loopzeroapp_informer query: %w", err)
		}
		if st == nil {
			return false, nil
		}
		phase, status, terminal := loopzeroAppProjection(st.Phase)
		ev := PhaseEvent{
			Phase:           phase,
			Status:          status,
			SubstateMessage: st.Message,
			Timestamp:       st.UpdatedAt,
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		return terminal, nil
	}

	return pollUntilTerminal(ctx, t.PollInterval, emit)
}

func loopzeroAppProjection(phase string) (string, string, bool) {
	switch phase {
	case "PROGRESSING":
		return "deploy", "in_progress", false
	case "SERVING":
		return "deploy", "completed", false
	case "REACHABLE":
		return "live", "in_progress", false
	case "DEGRADED":
		return "deploy", "degraded", false
	case "DOWN":
		return "deploy", "failed", true
	case "CLEANED":
		return "deploy", "canceled", true
	}
	return "deploy", "in_progress", false
}
