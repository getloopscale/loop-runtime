// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"fmt"
	"time"
)

// StatusURLProbeResult is the HTTP probe outcome.
type StatusURLProbeResult struct {
	StatusCode int
	Reachable  bool
	Latency    time.Duration
	Reason     string
}

// StatusURLProber abstracts the HTTP GET (allows hermetic tests).
// Production wires net/http.Client with budget per ADR D6.
type StatusURLProber interface {
	ProbeStatusURL(ctx context.Context, deployID string) (*StatusURLProbeResult, error)
}

// StatusURLProbeReader implements SourceReader for status_url_probe.
type StatusURLProbeReader struct {
	Prober       StatusURLProber
	PollInterval time.Duration
	// MaxAttempts caps the probe budget — after this many failures the
	// adapter exits via PhaseEvent{Phase:live, Status:failed}.
	MaxAttempts int
}

// NewStatusURLProbeReader constructs the reader.
func NewStatusURLProbeReader(p StatusURLProber) *StatusURLProbeReader {
	return &StatusURLProbeReader{Prober: p, PollInterval: 5 * time.Second, MaxAttempts: 60}
}

// ID identifies the source.
func (s *StatusURLProbeReader) ID() SourceID { return SourceStatusURLProbe }

// Stream probes the live URL until 2xx OR attempts exhausted.
func (s *StatusURLProbeReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	if s.PollInterval <= 0 {
		s.PollInterval = 5 * time.Second
	}
	if s.MaxAttempts <= 0 {
		s.MaxAttempts = 60
	}
	attempts := 0

	emit := func() (bool, error) {
		attempts++
		res, err := s.Prober.ProbeStatusURL(ctx, deployID)
		if err != nil {
			return false, fmt.Errorf("status_url_probe: %w", err)
		}
		if res == nil {
			return false, nil
		}
		ev := PhaseEvent{
			Phase:           "live",
			Timestamp:       time.Now().UTC(),
			SubstateMessage: res.Reason,
		}
		if res.Reachable && res.StatusCode >= 200 && res.StatusCode < 400 {
			ev.Status = "completed"
		} else if attempts >= s.MaxAttempts {
			ev.Status = "failed"
			ev.DegradedReason = fmt.Sprintf("live URL unreachable after %d attempts", attempts)
		} else {
			ev.Status = "in_progress"
		}
		select {
		case out <- ev:
		case <-ctx.Done():
			return false, ctx.Err()
		}
		return ev.Status == "completed" || ev.Status == "failed", nil
	}

	return pollUntilTerminal(ctx, s.PollInterval, emit)
}
