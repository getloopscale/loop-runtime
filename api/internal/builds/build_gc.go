// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"log/slog"
	"time"
)

// BuildGCStore abstracts the database operations needed for garbage collection.
//
// Note: `DeleteOldEvents` (formerly the second method) was removed in
// dogfood-2026-05-08-fixes T1.1 / ADR D2 because it referenced a
// non-existent table `build_events`. The GC scheduler logged ERROR every
// hour. SSE event retention is handled by sse_events table TTL elsewhere;
// webhook delivery dedup uses github_webhook_deliveries (covered below).
type BuildGCStore interface {
	// DeleteOldBuilds removes builds older than maxAge, keeping at least retainPerProject.
	DeleteOldBuilds(ctx context.Context, maxAge time.Duration, retainPerProject int) (int64, error)
	// DeleteOldWebhookDeliveries removes webhook delivery records older than maxAge.
	DeleteOldWebhookDeliveries(ctx context.Context, maxAge time.Duration) (int64, error)
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
	// MEET-044 P2: Class-based retention policy.
	// Builds (metadata): 30 days, retain 10 per project
	// Events (heavy): 14 days (shorter — these are large and only needed for recent debugging)
	// Webhook deliveries: 7 days (dedup window)

	// Clean old builds (30 days, retain 10)
	deleted, err := gc.store.DeleteOldBuilds(ctx, gc.maxAge, gc.retain)
	if err != nil {
		gc.logger.Error("build GC: delete old builds failed", "error", err)
	} else if deleted > 0 {
		gc.logger.Info("build GC: cleaned old builds", "deleted", deleted)
	}

	// (Old `build_events` cleanup removed in dogfood-2026-05-08-fixes T1.1:
	//  the table never existed; cleanup spammed ERROR every tick.)

	// Clean old webhook deliveries (7 days — dedup window)
	webhookMaxAge := 7 * 24 * time.Hour
	webhooksDeleted, err := gc.store.DeleteOldWebhookDeliveries(ctx, webhookMaxAge)
	if err != nil {
		gc.logger.Error("build GC: delete old webhook deliveries failed", "error", err)
	} else if webhooksDeleted > 0 {
		gc.logger.Info("build GC: cleaned old webhook deliveries", "deleted", webhooksDeleted)
	}
}
