// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package build

import (
	"context"
	"errors"
	"sync"

	"github.com/getloopscale/loopzero/api/internal/metrics"
)

// ErrCellAtCapacity is returned when the cell has reached its maximum
// concurrent build count. The handler should map this to HTTP 429.
var ErrCellAtCapacity = errors.New("cell at build capacity")

// ErrTenantAtCapacity is returned when a specific tenant has reached its
// per-tenant concurrent build limit. The handler should map this to HTTP 429.
var ErrTenantAtCapacity = errors.New("tenant at build capacity")

// BuildAdmissionConfig controls the admission gate thresholds.
type BuildAdmissionConfig struct {
	// MaxConcurrentPerCell is the maximum number of concurrent builds
	// allowed across all tenants in a single cell.
	MaxConcurrentPerCell int

	// MaxConcurrentPerTenant is the maximum number of concurrent builds
	// allowed for a single tenant within a cell.
	MaxConcurrentPerTenant int
}

// BuildAdmissionGate enforces cell-level and tenant-level build capacity.
// It is a lightweight in-process counter — the authoritative check is the
// pg_advisory_xact_lock in the build orchestrator. This gate provides
// fast-fail rejection before touching the database.
type BuildAdmissionGate struct {
	cfg BuildAdmissionConfig

	mu           sync.Mutex
	cellCounts   map[string]int // cell_id → active count
	tenantCounts map[string]int // "cell:tenant" → active count
}

// NewBuildAdmissionGate creates an admission gate with the given config.
func NewBuildAdmissionGate(cfg BuildAdmissionConfig) *BuildAdmissionGate {
	return &BuildAdmissionGate{
		cfg:          cfg,
		cellCounts:   make(map[string]int),
		tenantCounts: make(map[string]int),
	}
}

// Admit checks if a new build can be admitted for the given tenant in the
// given cell. Returns nil if admitted, ErrCellAtCapacity or
// ErrTenantAtCapacity if rejected. The caller MUST call Release when the
// build completes (succeeds, fails, or is cancelled).
func (g *BuildAdmissionGate) Admit(_ context.Context, tenantID, cellID string) error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check cell-level capacity.
	if g.cellCounts[cellID] >= g.cfg.MaxConcurrentPerCell {
		metrics.BuildAdmissionRejected.WithLabelValues(cellID, "cell_capacity").Inc()
		return ErrCellAtCapacity
	}

	// Check per-tenant capacity.
	tenantKey := cellID + ":" + tenantID
	if g.tenantCounts[tenantKey] >= g.cfg.MaxConcurrentPerTenant {
		metrics.BuildAdmissionRejected.WithLabelValues(cellID, "tenant_capacity").Inc()
		return ErrTenantAtCapacity
	}

	g.cellCounts[cellID]++
	g.tenantCounts[tenantKey]++
	metrics.BuildAdmissionAdmitted.WithLabelValues(cellID).Inc()
	return nil
}

// Release decrements the counters for a completed build. Must be called
// exactly once per successful Admit call.
func (g *BuildAdmissionGate) Release(cellID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cellCounts[cellID] > 0 {
		g.cellCounts[cellID]--
	}
}

// ReleaseTenant decrements both cell and tenant counters.
func (g *BuildAdmissionGate) ReleaseTenant(tenantID, cellID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cellCounts[cellID] > 0 {
		g.cellCounts[cellID]--
	}
	tenantKey := cellID + ":" + tenantID
	if g.tenantCounts[tenantKey] > 0 {
		g.tenantCounts[tenantKey]--
	}
}

// CellUtilization returns the current build count for a cell.
func (g *BuildAdmissionGate) CellUtilization(cellID string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cellCounts[cellID]
}
