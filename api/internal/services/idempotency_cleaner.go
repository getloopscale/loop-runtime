// Package services implements business logic orchestration.
package services

import (
	"context"
	"log/slog"
	"time"
)

// IdempotencyStore defines the cleanup contract for expired idempotency keys.
// Implemented by db.PostgresIdempotencyStore.
type IdempotencyStore interface {
	Cleanup(ctx context.Context) (int64, error)
}

// IdempotencyCleaner periodically removes expired idempotency keys (A-08).
// Without this, the idempotency_keys table grows unbounded.
type IdempotencyCleaner struct {
	store    IdempotencyStore
	logger   *slog.Logger
	interval time.Duration
}

// NewIdempotencyCleaner creates a new IdempotencyCleaner.
func NewIdempotencyCleaner(store IdempotencyStore, logger *slog.Logger, interval time.Duration) *IdempotencyCleaner {
	return &IdempotencyCleaner{store: store, logger: logger, interval: interval}
}

// Start runs the cleanup loop until the context is cancelled.
func (c *IdempotencyCleaner) Start(ctx context.Context) {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	c.logger.Info("idempotency cleaner started", "interval", c.interval)
	for {
		select {
		case <-ctx.Done():
			c.logger.Info("idempotency cleaner stopped")
			return
		case <-ticker.C:
			deleted, err := c.store.Cleanup(ctx)
			if err != nil {
				c.logger.Error("idempotency cleanup failed", "error", err)
				continue
			}
			if deleted > 0 {
				c.logger.Info("idempotency cleanup completed", "deleted", deleted)
			}
		}
	}
}
