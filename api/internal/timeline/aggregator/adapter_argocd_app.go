// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package aggregator

import (
	"context"
	"fmt"
	"time"
)

// ArgoCDAppStatus is the projection of ArgoCD Application status the
// adapter needs. Decoupled from the K8s client (operators/internal/...).
type ArgoCDAppStatus struct {
	SyncStatus   string // Synced | OutOfSync | Unknown
	HealthStatus string // Healthy | Suspended | Progressing | Missing | Degraded | Unknown
	UpdatedAt    time.Time
	Message      string
}

// ArgoCDAppQuerier abstracts the K8s API watch. Production wires
// operators/internal/argocd.Watcher; tests use a memory mock.
type ArgoCDAppQuerier interface {
	QueryAppStatus(ctx context.Context, deployID string) (*ArgoCDAppStatus, error)
}

// ArgoCDAppReader implements SourceReader for the argocd_app source.
// Polls ArgoCD Application sync+health every PollInterval until
// Healthy=true OR ctx cancel.
type ArgoCDAppReader struct {
	Querier      ArgoCDAppQuerier
	PollInterval time.Duration
}

// NewArgoCDAppReader constructs the reader.
func NewArgoCDAppReader(q ArgoCDAppQuerier) *ArgoCDAppReader {
	return &ArgoCDAppReader{Querier: q, PollInterval: 2 * time.Second}
}

// ID identifies the source.
func (a *ArgoCDAppReader) ID() SourceID { return SourceArgoCDApp }

// Stream polls Application status until Healthy or failure.
func (a *ArgoCDAppReader) Stream(ctx context.Context, deployID string, out chan<- PhaseEvent) error {
	if a.PollInterval <= 0 {
		a.PollInterval = 2 * time.Second
	}

	emit := func() (bool, error) {
		st, err := a.Querier.QueryAppStatus(ctx, deployID)
		if err != nil {
			return false, fmt.Errorf("argocd_app query: %w", err)
		}
		if st == nil {
			return false, nil
		}
		// worst-health priority aggregation (per blueprint):
		// Healthy > Suspended > Progressing > Missing > Degraded > Unknown
		phase := "deploy"
		status := healthToStatus(st.HealthStatus, st.SyncStatus)
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
		// Terminal-from-this-source: Healthy && Synced.
		if st.HealthStatus == "Healthy" && st.SyncStatus == "Synced" {
			return true, nil
		}
		return false, nil
	}

	return pollUntilTerminal(ctx, a.PollInterval, emit)
}

func healthToStatus(health, sync string) string {
	switch health {
	case "Healthy":
		if sync == "Synced" {
			return "completed"
		}
		return "in_progress"
	case "Degraded":
		return "failed"
	case "Suspended", "Progressing", "Missing":
		return "in_progress"
	}
	return "in_progress"
}
