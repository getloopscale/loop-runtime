// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"log/slog"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// CellMetricsQuerier abstracts the DB query for cell tenant counts.
// Implemented by the DB layer via a simple SQL query.
type CellMetricsQuerier interface {
	// CellTenantCounts returns tenant count per cell_id.
	CellTenantCounts(ctx context.Context) (map[string]int, error)
}

// CellMetricsCollector periodically queries tenant-per-cell counts
// and emits the loop_runtime_cell_tenant_count Prometheus gauge.
// ADR-0008 Phase 0.
type CellMetricsCollector struct {
	querier  CellMetricsQuerier
	logger   *slog.Logger
	interval time.Duration
}

// NewCellMetricsCollector creates a collector that emits cell metrics.
func NewCellMetricsCollector(querier CellMetricsQuerier, logger *slog.Logger, interval time.Duration) *CellMetricsCollector {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &CellMetricsCollector{
		querier:  querier,
		logger:   logger,
		interval: interval,
	}
}

// Start runs the collector until context is cancelled.
func (c *CellMetricsCollector) Start(ctx context.Context) {
	c.logger.Info("cell metrics collector started", "interval", c.interval)

	// Immediate collection on boot
	c.collect(ctx)

	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Info("cell metrics collector stopped")
			return
		case <-ticker.C:
			c.collect(ctx)
		}
	}
}

func (c *CellMetricsCollector) collect(ctx context.Context) {
	counts, err := c.querier.CellTenantCounts(ctx)
	if err != nil {
		c.logger.Error("cell metrics: failed to query tenant counts", "error", err)
		return
	}

	for cellID, count := range counts {
		metrics.CellTenantCount.WithLabelValues(cellID).Set(float64(count))
	}
}
