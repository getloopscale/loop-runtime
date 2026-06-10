// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package sse implements the Server-Sent-Events stream that delivers
// build + deploy + rollout progress to CLI/dashboard clients in
// real time. Hub multiplexes per-environment subscriptions; pruner
// reaps stale events from the in-memory ring buffer. SSE events are
// the only `--json` contract observable mid-flight (`build.event`,
// `release.event`, `rollout.step`, `rollout.aborted` etc.).
package sse

import (
	"context"
	"log/slog"
	"time"
)

// EventPruner periodically removes old SSE events to prevent unbounded table growth (C-06).
type EventPruner struct {
	store    EventStore
	logger   *slog.Logger
	maxAge   time.Duration
	interval time.Duration
}

// NewEventPruner creates a new EventPruner.
// maxAge controls how old events must be before pruning (e.g. 24h).
// interval controls how often the pruner runs (e.g. 1h).
func NewEventPruner(store EventStore, logger *slog.Logger, maxAge, interval time.Duration) *EventPruner {
	return &EventPruner{store: store, logger: logger, maxAge: maxAge, interval: interval}
}

// Start runs the pruning loop until the context is cancelled.
func (p *EventPruner) Start(ctx context.Context) {
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	p.logger.Info("event pruner started", "max_age", p.maxAge, "interval", p.interval)
	for {
		select {
		case <-ctx.Done():
			p.logger.Info("event pruner stopped")
			return
		case <-ticker.C:
			deleted, err := p.store.Prune(ctx, p.maxAge)
			if err != nil {
				p.logger.Error("event pruning failed", "error", err)
				continue
			}
			if deleted > 0 {
				p.logger.Info("events pruned", "deleted", deleted)
			}
		}
	}
}
