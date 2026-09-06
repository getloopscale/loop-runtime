// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/placement"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestCrossCellAggregator_QueryAllCells(t *testing.T) {
	router, _ := placement.NewStaticRouter([]placement.CellConfig{
		{CellID: "cell-a"},
		{CellID: "cell-b"},
	})
	querier := &fakeCellQuerier{counts: map[string]int{"cell-a": 10, "cell-b": 5}}
	svc := NewCellService(router, querier, testLogger())
	agg := NewCrossCellAggregator(svc, router, testLogger())

	result, err := agg.QueryAllCells(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Partial {
		t.Error("expected partial=false")
	}
	if len(result.Cells) != 2 {
		t.Errorf("expected 2 cells, got %d", len(result.Cells))
	}
}

func TestCrossCellAggregator_ParallelWithTimeout(t *testing.T) {
	router, _ := placement.NewStaticRouter([]placement.CellConfig{
		{CellID: "cell-a"},
		{CellID: "cell-b"},
		{CellID: "cell-c"},
	})
	querier := &fakeCellQuerier{counts: map[string]int{}}
	svc := NewCellService(router, querier, testLogger())
	agg := NewCrossCellAggregator(svc, router, testLogger())

	// Query function: cell-b times out, others succeed
	queryFn := func(ctx context.Context, cell placement.CellConfig) (*CellStatus, error) {
		if cell.CellID == "cell-b" {
			return nil, fmt.Errorf("connection timeout")
		}
		return &CellStatus{CellID: cell.CellID, TenantCount: 5}, nil
	}

	result, err := agg.QueryAllCellsParallel(context.Background(), queryFn)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !result.Partial {
		t.Error("expected partial=true when cell-b fails")
	}
	if len(result.Cells) != 2 {
		t.Errorf("expected 2 successful cells, got %d", len(result.Cells))
	}
	if len(result.Errors) != 1 {
		t.Errorf("expected 1 error, got %d", len(result.Errors))
	}
}

func TestCrossCellAggregator_AllCellsFail(t *testing.T) {
	router, _ := placement.NewStaticRouter([]placement.CellConfig{
		{CellID: "cell-a"},
		{CellID: "cell-b"},
	})
	querier := &fakeCellQuerier{counts: map[string]int{}}
	svc := NewCellService(router, querier, testLogger())
	agg := NewCrossCellAggregator(svc, router, testLogger())

	queryFn := func(ctx context.Context, cell placement.CellConfig) (*CellStatus, error) {
		return nil, fmt.Errorf("all cells down")
	}

	result, err := agg.QueryAllCellsParallel(context.Background(), queryFn)
	if err != nil {
		t.Fatalf("expected no top-level error (partial results), got %v", err)
	}
	if !result.Partial {
		t.Error("expected partial=true")
	}
	if len(result.Cells) != 0 {
		t.Errorf("expected 0 cells, got %d", len(result.Cells))
	}
	if len(result.Errors) != 2 {
		t.Errorf("expected 2 errors, got %d", len(result.Errors))
	}
}

func TestCrossCellAggregator_ParallelAllSucceed(t *testing.T) {
	router, _ := placement.NewStaticRouter([]placement.CellConfig{
		{CellID: "cell-a"},
		{CellID: "cell-b"},
	})
	querier := &fakeCellQuerier{counts: map[string]int{}}
	svc := NewCellService(router, querier, testLogger())
	agg := NewCrossCellAggregator(svc, router, testLogger())

	queryFn := func(_ context.Context, cell placement.CellConfig) (*CellStatus, error) {
		return &CellStatus{CellID: cell.CellID, TenantCount: 10}, nil
	}

	result, err := agg.QueryAllCellsParallel(context.Background(), queryFn)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result.Partial {
		t.Error("expected partial=false when all succeed")
	}
	if len(result.Cells) != 2 {
		t.Errorf("expected 2 cells, got %d", len(result.Cells))
	}
}

// Suppress unused import warning
var _ = time.Second
