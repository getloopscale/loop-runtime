// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package sse implements the Server-Sent-Events streams that deliver
// build + deploy + rollout progress to CLI/dashboard clients. Events are
// rows in the Postgres table sse_events (PostgresEventStore); each open
// stream polls that table for rows newer than the last one it sent
// (stream.go holds the shared gate, framing and poll loop), and
// EventPruner deletes rows past their retention. SSE events are the only
// `--json` contract observable mid-flight (`build.event`,
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
