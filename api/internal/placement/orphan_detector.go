// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"log/slog"
	"time"

	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/metrics"
)

// OrphanedTenantQuerier returns the set of cell_ids that have tenants assigned
// but are NOT in the provided set of known cells.
type OrphanedTenantQuerier interface {
	// CountByCell returns tenant count per cell_id.
	CountByCell(ctx context.Context) (map[string]int, error)
}

// OrphanDetector periodically checks for tenants assigned to cells that
// no longer exist in the router. When a ClusterCredential is removed,
// tenants referencing that cell become orphaned — their deploys, rollbacks,
// and status operations will fail.
//
// MEET-053 D-04: Emits loopzero_orphaned_tenants gauge and logs warnings.
// Manual remediation is acceptable for Phase 1; automatic migration is Phase 2.
type OrphanDetector struct {
	cellLister deploycontracts.CellLister
	querier    OrphanedTenantQuerier
	logger     *slog.Logger
	interval   time.Duration
}

// NewOrphanDetector creates a detector that runs every interval.
func NewOrphanDetector(
	cellLister deploycontracts.CellLister,
	querier OrphanedTenantQuerier,
	logger *slog.Logger,
	interval time.Duration,
) *OrphanDetector {
	if interval <= 0 {
		interval = 60 * time.Second
	}
	return &OrphanDetector{
		cellLister: cellLister,
		querier:    querier,
		logger:     logger.With("component", "orphan-detector"),
		interval:   interval,
	}
}

// Start runs the orphan detection loop until ctx is cancelled.
func (d *OrphanDetector) Start(ctx context.Context) {
	d.logger.Info("orphan detector started", "interval", d.interval)

	// Immediate check on boot.
	d.check(ctx)

	ticker := time.NewTicker(d.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.check(ctx)
		}
	}
}

func (d *OrphanDetector) check(ctx context.Context) {
	// Get known cells from the router.
	knownIDs, err := d.cellLister.ListCellIDs(ctx)
	if err != nil {
		d.logger.Warn("orphan detector: failed to list cells", "error", err)
		return
	}
	knownSet := make(map[string]bool, len(knownIDs))
	for _, id := range knownIDs {
		knownSet[id] = true
	}

	// Get tenant counts per cell from the database.
	counts, err := d.querier.CountByCell(ctx)
	if err != nil {
		d.logger.Warn("orphan detector: failed to query tenant counts", "error", err)
		return
	}

	// Find orphaned cells (in DB but not in router).
	orphanedTotal := 0
	for cellID, count := range counts {
		if !knownSet[cellID] {
			orphanedTotal += count
			d.logger.Warn("orphaned tenants detected",
				"cell_id", cellID,
				"tenant_count", count,
				"known_cells", len(knownIDs),
			)
		}
	}

	metrics.OrphanedTenants.Set(float64(orphanedTotal))
}
