// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// LokiMarker represents one ::loopzero:: marker log line surfaced by the
// Loki query (workload+build clusters). The adapter is decoupled from
// the HTTP client behind LokiQuerier.
type LokiMarker struct {
	Phase     string // build|sign|release|deploy|live
	Step      string // free-form per producer
	Level     string // info|warn|error
	Message   string
	Timestamp time.Time
}

// LokiQuerier abstracts Loki HTTP queries. Production wires
// api/internal/logs/loki.Client; tests use a memory mock.
type LokiQuerier interface {
	QueryMarkersAfter(ctx context.Context, deployID string, afterTS time.Time) ([]LokiMarker, error)
}

// LokiMarkersReader implements SourceReader for the loki_markers source.
type LokiMarkersReader struct {
	Querier      LokiQuerier
	PollInterval time.Duration
}

// NewLokiMarkersReader constructs the reader (default 3s poll).
func NewLokiMarkersReader(q LokiQuerier) *LokiMarkersReader {
	return &LokiMarkersReader{Querier: q, PollInterval: 3 * time.Second}
}

// ID identifies the source.
func (l *LokiMarkersReader) ID() SourceID { return SourceLokiMarkers }

// Stream polls Loki and emits one PhaseEvent per marker until the
// "live" phase produces a terminal marker (info final) OR ctx cancel.
func (l *LokiMarkersReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	if l.PollInterval <= 0 {
		l.PollInterval = 3 * time.Second
	}
	last := time.Time{}

	emit := func() (bool, error) {
		markers, err := l.Querier.QueryMarkersAfter(ctx, deployID, last)
		if err != nil {
			return false, fmt.Errorf("loki query: %w", err)
		}
		terminal := false
		for _, m := range markers {
			if m.Timestamp.After(last) {
				last = m.Timestamp
			}
			ev := PhaseEvent{
				Phase:           m.Phase,
				Status:          markerLevelToStatus(m.Level, m.Step),
				SubstateMessage: m.Message,
				Timestamp:       m.Timestamp,
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return false, ctx.Err()
			}
			if m.Phase == "live" && strings.EqualFold(m.Step, "ready") {
				terminal = true
			}
		}
		return terminal, nil
	}

	return pollUntilTerminal(ctx, l.PollInterval, emit)
}

func markerLevelToStatus(level, step string) string {
	switch strings.ToLower(level) {
	case "error":
		return "failed"
	case "warn":
		return "degraded"
	}
	if strings.EqualFold(step, "completed") || strings.EqualFold(step, "ready") {
		return "completed"
	}
	return "in_progress"
}
