// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"errors"
	"testing"
)

// =============================================================================
// ConstraintEngine.GetCell tests (previously 0% coverage)
// =============================================================================

type fakeGetCellQuerier struct {
	cellID   string
	err      error
	countMap map[string]int
}

func (f *fakeGetCellQuerier) GetTenantCellID(_ context.Context, _ string) (string, error) {
	return f.cellID, f.err
}

func (f *fakeGetCellQuerier) CountByCell(_ context.Context) (map[string]int, error) {
	if f.countMap != nil {
		return f.countMap, nil
	}
	return map[string]int{}, nil
}

func TestConstraintEngine_GetCell_Success(t *testing.T) {
	q := &fakeGetCellQuerier{cellID: "cell-us-east-1"}
	engine := NewConstraintEngine(nil, q, nil, 100)

	cellID, err := engine.GetCell(context.Background(), "tn-acme")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cellID != "cell-us-east-1" {
		t.Errorf("expected cell-us-east-1, got %q", cellID)
	}
}

func TestConstraintEngine_GetCell_Error(t *testing.T) {
	q := &fakeGetCellQuerier{err: errors.New("not found")}
	engine := NewConstraintEngine(nil, q, nil, 100)

	_, err := engine.GetCell(context.Background(), "tn-unknown")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNewConstraintEngine_DefaultMaxPerCell(t *testing.T) {
	engine := NewConstraintEngine(nil, nil, nil, 0) // 0 should default to 500
	if engine.maxPerCell != 500 {
		t.Errorf("expected default 500, got %d", engine.maxPerCell)
	}
}

// =============================================================================
// RoundRobinEngine.GetCell tests (previously 75% → cover error path)
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
	q := &fakeGetCellQuerier{cellID: "cell-default"}
	engine := &RoundRobinEngine{querier: q}

	cellID, err := engine.GetCell(context.Background(), "tn-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cellID != "cell-default" {
		t.Errorf("expected cell-default, got %q", cellID)
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
