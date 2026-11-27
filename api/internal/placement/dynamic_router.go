// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

// CellStore is the interface for loading cell configurations.
// Implementations may read from K8s API (ClusterCredential CRDs), database, or config files.
type CellStore interface {
	// LoadCells returns all active cells with their connection details.
	LoadCells(ctx context.Context) ([]CellConfig, error)
}

// DynamicRouter resolves cell_id from a periodically refreshed cell list.
// This is the Phase 2 implementation — cells are discovered at runtime from
// ClusterCredential CRDs (via the operator) or a database table.
// ADR-0008 Phase 2.
//
// Thread-safe: reads use a RWMutex, refresh runs in background.
type DynamicRouter struct {
	store    CellStore
	logger   *slog.Logger
	interval time.Duration

	mu    sync.RWMutex
	cells map[string]CellConfig
	stop  chan struct{}
}

// NewDynamicRouter creates a router that refreshes its cell list periodically.
// Call Start() to begin background refresh.
func NewDynamicRouter(store CellStore, interval time.Duration, logger *slog.Logger) *DynamicRouter {
	return &DynamicRouter{
		store:    store,
		logger:   logger.With("component", "dynamic-cell-router"),
		interval: interval,
		cells:    make(map[string]CellConfig),
		stop:     make(chan struct{}),
	}
}

// Start begins periodic cell list refresh. Blocks until context is cancelled or Stop is called.
func (r *DynamicRouter) Start(ctx context.Context) {
	// Initial load
	if err := r.refresh(ctx); err != nil {
		r.logger.Error("initial cell refresh failed", "error", err)
	}

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-r.stop:
			return
		case <-ticker.C:
			if err := r.refresh(ctx); err != nil {
				r.logger.Error("cell refresh failed", "error", err)
			}
		}
	}
}

// Stop terminates the background refresh.
func (r *DynamicRouter) Stop() {
	close(r.stop)
}

func (r *DynamicRouter) refresh(ctx context.Context) error {
	configs, err := r.store.LoadCells(ctx)
	if err != nil {
		return fmt.Errorf("load cells: %w", err)
	}

	newCells := make(map[string]CellConfig, len(configs))
	for _, cfg := range configs {
		if cfg.CellID == "" {
			continue
		}
		newCells[cfg.CellID] = cfg
	}

	// MEET-053 A-03: Stale-data protection.
	// Never replace a non-empty cell map with an empty set from a poll.
	// A K8s API hiccup returning 0 CRDs would otherwise wipe all cells
	// and orphan every tenant until the next successful refresh (60s).
	r.mu.Lock()
	previousCount := len(r.cells)
	if len(newCells) == 0 && previousCount > 0 {
		r.mu.Unlock()
		r.logger.Error("cell refresh returned 0 cells but router has active cells — refusing to clear (stale-data protection)",
			"previous_count", previousCount)
		metrics.CellDiscoveryCount.Set(float64(previousCount))
		return fmt.Errorf("cell refresh: 0 cells returned, refusing to clear %d known cells", previousCount)
	}
	if len(newCells) < previousCount {
		r.logger.Warn("cell count decreased", "previous", previousCount, "current", len(newCells))
	}
	r.cells = newCells
	r.mu.Unlock()

	// MEET-053 A-09: Emit discovery count metric for alerting.
	metrics.CellDiscoveryCount.Set(float64(len(newCells)))
	r.logger.Info("cell list refreshed", "count", len(newCells))
	return nil
}

// Resolve returns the CellConfig for the given cell_id.
//
// Emits loop_runtime_cell_routing_decisions_total{cell, decision} per
// MEET-INFRA-NORMALIZE-T3-multi-cell C19 + ADR D6.
//
// Decisions:
//
//	"assigned" — cell found, returned normally
//	"error"    — cell_id empty OR unknown; caller gets a typed error
//
// "fallback" decision is reserved for a future enhancement (graceful
// degradation to default-cell when primary unavailable). Today the
// router does not fall back — unknown cells are errors, by design,
// because silent fallback would mask routing drift. Plan v2 D3 Stage
// 5b reconsiders if/when stage 4 surfaces real demand.
//
// INVARIANT (cell-1-activation-plan ADR C2): Resolve is deterministic
// — for a fixed cell_id, every call between two refreshes returns a
// CellConfig that DeepEquals the previous call. The 60s refresh window
// is the ONLY source of routing-result variability under steady state.
// Routing decisions are pure-function lookups under RWMutex; concurrent
// Resolve calls from N goroutines must agree byte-for-byte. Tests:
// TestDynamicRouter_Resolve_DeterministicAcrossCalls (sequential) +
// TestDynamicRouter_Resolve_DeterministicConcurrent (parallel + race).
// Drift on this invariant breaks distributed-systems veto criterion in
// MEET-INFRA-NORMALIZE-T3-multi-cell.
func (r *DynamicRouter) Resolve(_ context.Context, cellID string) (*CellConfig, error) {
	if cellID == "" {
		metrics.CellRoutingDecisions.WithLabelValues("", "error").Inc()
		return nil, fmt.Errorf("cell router: empty cell_id")
	}

	r.mu.RLock()
	cfg, ok := r.cells[cellID]
	known := len(r.cells)
	r.mu.RUnlock()

	if !ok {
		metrics.CellRoutingDecisions.WithLabelValues(cellID, "error").Inc()
		return nil, fmt.Errorf("cell router: unknown cell_id %q (known: %d cells)", cellID, known)
	}
	metrics.CellRoutingDecisions.WithLabelValues(cellID, "assigned").Inc()
	return &cfg, nil
}

// ListCells returns all currently known cells.
func (r *DynamicRouter) ListCells(_ context.Context) ([]CellConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cells := make([]CellConfig, 0, len(r.cells))
	for _, cfg := range r.cells {
		cells = append(cells, cfg)
	}
	return cells, nil
}

// DiscoveredCellCount returns the number of cells currently in the router.
// MEET-053 A-04: Implements routes.CellDiscoveryChecker.
func (r *DynamicRouter) DiscoveredCellCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.cells)
}

// IsDynamic returns true — DynamicRouter is always dynamic.
func (r *DynamicRouter) IsDynamic() bool { return true }

// ListCellIDs returns all known cell IDs (no connection details).
// MEET-053 A-07: Implements shared.CellLister for BuildReconcileLoop.
func (r *DynamicRouter) ListCellIDs(_ context.Context) ([]string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.cells))
	for id := range r.cells {
		ids = append(ids, id)
	}
	return ids, nil
}
