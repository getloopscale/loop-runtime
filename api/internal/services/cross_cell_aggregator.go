// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/loopzero/loopzero/api/internal/placement"
)

// CellQueryResult holds the result of querying a single Cell.
type CellQueryResult struct {
	CellID string      `json:"cell_id"`
	Data   *CellStatus `json:"data,omitempty"`
	Error  string      `json:"error,omitempty"`
}

// AggregatedCellResult holds the combined results from all Cells.
type AggregatedCellResult struct {
	Cells   []CellStatus `json:"cells"`
	Partial bool         `json:"partial"` // true if some Cells failed/timed out
	Errors  []string     `json:"errors,omitempty"`
}

// CrossCellAggregator queries multiple Cells in parallel and aggregates results.
// ADR-0008 Phase 3 (P3-6).
type CrossCellAggregator struct {
	cellService *CellService
	router      placement.Router
	timeout     time.Duration
	logger      *slog.Logger
}

// NewCrossCellAggregator creates an aggregator for cross-cell queries.
func NewCrossCellAggregator(cellService *CellService, router placement.Router, logger *slog.Logger) *CrossCellAggregator {
	return &CrossCellAggregator{
		cellService: cellService,
		router:      router,
		timeout:     5 * time.Second,
		logger:      logger,
	}
}

// QueryAllCells fans out to all Cells and aggregates status data.
// Returns partial results if some Cells fail or timeout.
func (a *CrossCellAggregator) QueryAllCells(ctx context.Context) (*AggregatedCellResult, error) {
	// For Phase 3, CellService already aggregates from the shared PG.
	// In a future where Cells have local state, this would fan-out HTTP requests.
	// For now, we use the existing CellService which queries PG + router.
	cells, err := a.cellService.ListCells(ctx)
	if err != nil {
		return nil, err
	}

	return &AggregatedCellResult{
		Cells:   cells,
		Partial: false,
	}, nil
}

// QueryAllCellsParallel fans out queries to N Cells in parallel with timeout.
// Each Cell is queried independently. Partial results returned if some fail.
// This is the full implementation for when Cells have local state/APIs.
func (a *CrossCellAggregator) QueryAllCellsParallel(ctx context.Context, queryFn func(ctx context.Context, cell placement.CellConfig) (*CellStatus, error)) (*AggregatedCellResult, error) {
	cells, err := a.router.ListCells(ctx)
	if err != nil {
		return nil, err
	}

	type result struct {
		status *CellStatus
		err    error
		cellID string
	}

	results := make(chan result, len(cells))
	var wg sync.WaitGroup

	for _, cell := range cells {
		wg.Add(1)
		go func(c placement.CellConfig) {
			defer wg.Done()
			queryCtx, cancel := context.WithTimeout(ctx, a.timeout)
			defer cancel()

			status, err := queryFn(queryCtx, c)
			results <- result{status: status, err: err, cellID: c.CellID}
		}(cell)
	}

	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	aggResult := &AggregatedCellResult{
		Cells: make([]CellStatus, 0, len(cells)),
	}

	for r := range results {
		if r.err != nil {
			aggResult.Partial = true
			aggResult.Errors = append(aggResult.Errors, r.cellID+": "+r.err.Error())
			a.logger.Warn("cross-cell query failed", "cell_id", r.cellID, "error", r.err)
			continue
		}
		if r.status != nil {
			aggResult.Cells = append(aggResult.Cells, *r.status)
		}
	}

	return aggResult, nil
}
