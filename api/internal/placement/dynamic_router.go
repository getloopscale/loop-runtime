// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
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

	r.mu.Lock()
	r.cells = newCells
	r.mu.Unlock()

	r.logger.Info("cell list refreshed", "count", len(newCells))
	return nil
}

// Resolve returns the CellConfig for the given cell_id.
func (r *DynamicRouter) Resolve(_ context.Context, cellID string) (*CellConfig, error) {
	if cellID == "" {
		return nil, fmt.Errorf("cell router: empty cell_id")
	}

	r.mu.RLock()
	cfg, ok := r.cells[cellID]
	r.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("cell router: unknown cell_id %q (known: %d cells)", cellID, len(r.cells))
	}
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
