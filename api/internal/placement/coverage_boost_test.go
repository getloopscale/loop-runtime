// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package placement

import (
	"context"
	"errors"
	"testing"
)

// CHG-2026-04-30-DELETE-SINGLE-CELL: this file lost the
// ConstraintEngine.GetCell coverage tests when ConstraintEngine was
// deleted. RoundRobinEngine.GetCell + DynamicRouter.Stop coverage
// retained.

type fakeGetCellQuerier struct {
	cellID   string
	err      error
	countMap map[string]int
}

func (f *fakeGetCellQuerier) GetTenantCellID(_ context.Context, _ string) (string, error) {
	return f.cellID, f.err
}

func (f *fakeGetCellQuerier) CapacityByCell(_ context.Context) (map[string]int, error) {
	return map[string]int{}, nil
}

func (f *fakeGetCellQuerier) CountByCell(_ context.Context) (map[string]int, error) {
	if f.countMap != nil {
		return f.countMap, nil
	}
	return map[string]int{}, nil
}

// =============================================================================
// RoundRobinEngine.GetCell tests
// =============================================================================

func TestRoundRobinEngine_GetCell_Error(t *testing.T) {
	q := &fakeGetCellQuerier{err: errors.New("db down")}
	engine := &RoundRobinEngine{querier: q}

	_, err := engine.GetCell(context.Background(), "tn-test")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestRoundRobinEngine_GetCell_Success(t *testing.T) {
	q := &fakeGetCellQuerier{cellID: "cell-0"}
	engine := &RoundRobinEngine{querier: q}

	cellID, err := engine.GetCell(context.Background(), "tn-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cellID != "cell-0" {
		t.Errorf("expected cell-0, got %q", cellID)
	}
}

// =============================================================================
// DynamicRouter.Stop (previously 0% coverage)
// =============================================================================

func TestDynamicRouter_StopBeforeStart(t *testing.T) {
	dr := &DynamicRouter{stop: make(chan struct{})}
	// Should not panic
	dr.Stop()
	// Verify channel is closed
	select {
	case <-dr.stop:
		// expected
	default:
		t.Error("stop channel should be closed")
	}
}
