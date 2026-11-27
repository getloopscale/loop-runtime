// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"log/slog"
	"time"
)

// CellStatus represents the status of a single Cell for operator visibility.
// Used by GET /internal/cells and `loopscale status --cells`.
// ADR-0008 Phase 2 (P2-7).
type CellStatus struct {
	CellID        string    `json:"cell_id"`
	Region        string    `json:"region"`
	TenantCount   int       `json:"tenant_count"`
	AppCount      int       `json:"app_count"`      // not measured yet: always 0 (see ListCells)
	ClusterHealth string    `json:"cluster_health"` // "Ready" or "NotReady"
	DeployHealth  string    `json:"deploy_health"`  // "Healthy" or "Degraded" — NOT "ArgoCD" (Rule 3)
	CreatedAt     time.Time `json:"created_at,omitempty"`
}

// CellCountQuerier abstracts the DB queries needed by CellService.
type CellCountQuerier interface {
	// CountByCell returns tenant count per cell_id.
	CountByCell(ctx context.Context) (map[string]int, error)
}

// CellService aggregates Cell status data from multiple sources.
type CellService struct {
	router  Router
	querier CellCountQuerier
	logger  *slog.Logger
}

// NewCellService creates a CellService for operator visibility.
func NewCellService(router Router, querier CellCountQuerier, logger *slog.Logger) *CellService {
	return &CellService{
		router:  router,
		querier: querier,
		logger:  logger,
	}
}

// ListCells returns the status of all known Cells.
func (s *CellService) ListCells(ctx context.Context) ([]CellStatus, error) {
	cells, err := s.router.ListCells(ctx)
	if err != nil {
		return nil, err
	}

	counts, err := s.querier.CountByCell(ctx)
	if err != nil {
		s.logger.Warn("cell service: failed to query tenant counts, using zeros", "error", err)
		counts = make(map[string]int)
	}

	// T6.8 of deep-review-remediation-plan (find_cell_service_static_fields):
	// this loop used to fabricate ClusterHealth="Ready" and
	// DeployHealth="Healthy", which told the operator a disconnected cell was
	// ready. Nothing measures per-cell cluster or deploy health, or apps per
	// cell, today, so health is reported as "Unknown" and app_count stays 0.
	// The fields remain because they are part of the --json contract; when a
	// provider exists it becomes an explicit constructor dependency, not a
	// duck-typed optional interface nothing implements.
	result := make([]CellStatus, 0, len(cells))
	for _, cell := range cells {
		result = append(result, CellStatus{
			CellID:        cell.CellID,
			Region:        ExtractRegion(cell.CellID),
			TenantCount:   counts[cell.CellID],
			ClusterHealth: healthUnknown,
			DeployHealth:  healthUnknown,
		})
	}

	return result, nil
}

// healthUnknown is reported for a health signal nothing measures: an honest
// "we do not know" rather than a fabricated "Ready".
const healthUnknown = "Unknown"

// extractRegion is now in placement.ExtractRegion (shared with ConstraintEngine).
