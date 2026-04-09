// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"log/slog"
	"time"
)

// BuildGCStore abstracts the database operations needed for garbage collection.
type BuildGCStore interface {
	// DeleteOldBuilds removes builds older than maxAge, keeping at least retainPerProject.
	// Returns number of rows deleted.
	DeleteOldBuilds(ctx context.Context, maxAge time.Duration, retainPerProject int) (int64, error)
	// DeleteOldEvents removes SSE events older than maxAge.
	DeleteOldEvents(ctx context.Context, maxAge time.Duration) (int64, error)
}

// BuildGC periodically cleans up old builds, releases, and events.
// Prevents unbounded PostgreSQL growth from historical deploy data.
// MEET-2026-04-09-042 P1: Production readiness.
type BuildGC struct {
	store    BuildGCStore
	logger   *slog.Logger
	interval time.Duration
	maxAge   time.Duration
	retain   int
}

// NewBuildGC creates a garbage collector with configurable retention.
func NewBuildGC(store BuildGCStore, logger *slog.Logger, interval, maxAge time.Duration, retainPerProject int) *BuildGC {
	if interval <= 0 {
		interval = 1 * time.Hour
	}
	if maxAge <= 0 {
		maxAge = 30 * 24 * time.Hour // 30 days
	}
	if retainPerProject <= 0 {
		retainPerProject = 10
	}
	return &BuildGC{
		store:    store,
		logger:   logger,
		interval: interval,
		maxAge:   maxAge,
		retain:   retainPerProject,
	}
}

// Start runs the GC loop until context is cancelled.
func (gc *BuildGC) Start(ctx context.Context) {
	gc.logger.Info("build GC started", "interval", gc.interval, "max_age", gc.maxAge, "retain_per_project", gc.retain)
	ticker := time.NewTicker(gc.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			gc.logger.Info("build GC stopped")
			return
		case <-ticker.C:
			gc.collect(ctx)
		}
	}
}

func (gc *BuildGC) collect(ctx context.Context) {
	// Clean old builds
	deleted, err := gc.store.DeleteOldBuilds(ctx, gc.maxAge, gc.retain)
	if err != nil {
		gc.logger.Error("build GC: delete old builds failed", "error", err)
	} else if deleted > 0 {
		gc.logger.Info("build GC: cleaned old builds", "deleted", deleted)
	}

	// Clean old events
	eventsDeleted, err := gc.store.DeleteOldEvents(ctx, gc.maxAge)
	if err != nil {
		gc.logger.Error("build GC: delete old events failed", "error", err)
	} else if eventsDeleted > 0 {
		gc.logger.Info("build GC: cleaned old events", "deleted", eventsDeleted)
	}
}
