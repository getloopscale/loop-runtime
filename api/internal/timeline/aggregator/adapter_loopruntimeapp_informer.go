// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package aggregator

import (
	"context"
	"fmt"
	"time"
)

// LoopRuntimeAppPhase is the projection of LoopRuntimeApplication status the
// adapter emits. Decoupled from operators/ CRD types.
type LoopRuntimeAppPhase struct {
	Phase     string // PROGRESSING|SERVING|REACHABLE|DEGRADED|DOWN|CLEANED
	UpdatedAt time.Time
	Message   string
}

// LoopRuntimeAppQuerier abstracts the K8s informer subscription.
type LoopRuntimeAppQuerier interface {
	QueryLoopRuntimeAppPhase(ctx context.Context, deployID string) (*LoopRuntimeAppPhase, error)
}

// LoopRuntimeAppInformerReader implements SourceReader for loopruntimeapp_informer.
type LoopRuntimeAppInformerReader struct {
	Querier      LoopRuntimeAppQuerier
	PollInterval time.Duration
}

// NewLoopRuntimeAppInformerReader constructs the reader.
func NewLoopRuntimeAppInformerReader(q LoopRuntimeAppQuerier) *LoopRuntimeAppInformerReader {
	return &LoopRuntimeAppInformerReader{Querier: q, PollInterval: 2 * time.Second}
}

// ID identifies the source.
func (t *LoopRuntimeAppInformerReader) ID() SourceID { return SourceLoopRuntimeAppInformer }

// Stream polls LoopRuntimeApp phase until REACHABLE OR ctx cancel.
func (t *LoopRuntimeAppInformerReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	if t.PollInterval <= 0 {
		t.PollInterval = 2 * time.Second
	}

	emit := func() (bool, error) {
		st, err := t.Querier.QueryLoopRuntimeAppPhase(ctx, deployID)
		if err != nil {
			return false, fmt.Errorf("loopruntimeapp_informer query: %w", err)
		}
		if st == nil {
			return false, nil
		}
		phase, status, terminal := loopRuntimeAppProjection(st.Phase)
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

func loopRuntimeAppProjection(phase string) (string, string, bool) {
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
