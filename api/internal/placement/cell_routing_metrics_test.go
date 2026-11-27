// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors
//
// Phase 5 T5.4 — observability. Tests that DynamicRouter.Resolve()
// emits loop_runtime_cell_routing_decisions_total with the right labels.
// Closes ADR D6 mandate (observability BEFORE feature ships) and
// MEET-INFRA-NORMALIZE-T3-multi-cell condition C19 (deterministic
// routing decisions; cell-unavailable handled).

package placement

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/getloopscale/loop-runtime/api/internal/metrics"
)

func TestCellRoutingDecisions_AssignedOnSuccess(t *testing.T) {
	store := &fakeCellStore{cells: []CellConfig{{CellID: "cell-x"}}}
	router := NewDynamicRouter(store, time.Minute, slog.Default())
	if err := router.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	before := testutil.ToFloat64(metrics.CellRoutingDecisions.WithLabelValues("cell-x", "assigned"))
	cfg, err := router.Resolve(context.Background(), "cell-x")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.CellID != "cell-x" {
		t.Fatalf("unexpected cell: %q", cfg.CellID)
	}
	after := testutil.ToFloat64(metrics.CellRoutingDecisions.WithLabelValues("cell-x", "assigned"))
	if after != before+1 {
		t.Errorf("assigned counter: before=%v after=%v (delta should be 1)", before, after)
	}
}

func TestCellRoutingDecisions_ErrorOnUnknownCell(t *testing.T) {
	store := &fakeCellStore{cells: []CellConfig{{CellID: "cell-known"}}}
	router := NewDynamicRouter(store, time.Minute, slog.Default())
	if err := router.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	before := testutil.ToFloat64(metrics.CellRoutingDecisions.WithLabelValues("cell-ghost", "error"))
	if _, err := router.Resolve(context.Background(), "cell-ghost"); err == nil {
		t.Fatal("expected error for unknown cell")
	}
	after := testutil.ToFloat64(metrics.CellRoutingDecisions.WithLabelValues("cell-ghost", "error"))
	if after != before+1 {
		t.Errorf("error counter: before=%v after=%v (delta should be 1)", before, after)
	}
}

func TestCellRoutingDecisions_ErrorOnEmptyCellID(t *testing.T) {
	store := &fakeCellStore{cells: []CellConfig{{CellID: "cell-x"}}}
	router := NewDynamicRouter(store, time.Minute, slog.Default())
	if err := router.refresh(context.Background()); err != nil {
		t.Fatalf("refresh: %v", err)
	}

	// Empty-string cell_id has its own labelled counter ("", "error").
	before := testutil.ToFloat64(metrics.CellRoutingDecisions.WithLabelValues("", "error"))
	if _, err := router.Resolve(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty cell_id")
	}
	after := testutil.ToFloat64(metrics.CellRoutingDecisions.WithLabelValues("", "error"))
	if after != before+1 {
		t.Errorf("empty-cell error counter: before=%v after=%v (delta should be 1)", before, after)
	}
}
