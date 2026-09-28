// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"
)

// --- Test doubles for OrphanDetector ---

type fakeOrphanCellLister struct {
	cellIDs []string
	err     error
}

func (f *fakeOrphanCellLister) ListCellIDs(_ context.Context) ([]string, error) {
	return f.cellIDs, f.err
}

type fakeOrphanQuerier struct {
	counts map[string]int
	err    error
}

func (f *fakeOrphanQuerier) CountByCell(_ context.Context) (map[string]int, error) {
	return f.counts, f.err
}

// --- Tests ---

func TestOrphanDetector_NoOrphans(t *testing.T) {
	lister := &fakeOrphanCellLister{cellIDs: []string{"cell-0", "cell-1"}}
	querier := &fakeOrphanQuerier{
		counts: map[string]int{"cell-0": 5, "cell-1": 3},
	}

	detector := NewOrphanDetector(lister, querier, slog.Default(), time.Minute)
	// Should not panic, no warnings expected
	detector.check(context.Background())
}

func TestOrphanDetector_OrphansDetected(t *testing.T) {
	lister := &fakeOrphanCellLister{cellIDs: []string{"cell-0"}}
	querier := &fakeOrphanQuerier{
		counts: map[string]int{
			"cell-0":       5,
			"cell-removed": 3, // this cell is not in the lister
		},
	}

	detector := NewOrphanDetector(lister, querier, slog.Default(), time.Minute)
	// Should not panic; orphaned tenants metric should be set (not assertable here, but ensures code path runs)
	detector.check(context.Background())
}

func TestOrphanDetector_CellListerError_ReturnsEarly(t *testing.T) {
	lister := &fakeOrphanCellLister{err: fmt.Errorf("k8s API unreachable")}
	querier := &fakeOrphanQuerier{counts: map[string]int{"cell-0": 5}}

	detector := NewOrphanDetector(lister, querier, slog.Default(), time.Minute)
	detector.check(context.Background())
	// Should not panic, should return early
}

func TestOrphanDetector_QuerierError_ReturnsEarly(t *testing.T) {
	lister := &fakeOrphanCellLister{cellIDs: []string{"cell-0"}}
	querier := &fakeOrphanQuerier{err: fmt.Errorf("pg timeout")}

	detector := NewOrphanDetector(lister, querier, slog.Default(), time.Minute)
	detector.check(context.Background())
	// Should not panic
}

func TestNewOrphanDetector_DefaultInterval(t *testing.T) {
	detector := NewOrphanDetector(nil, nil, slog.Default(), 0)
	if detector.interval != 60*time.Second {
		t.Errorf("expected default interval 60s, got %v", detector.interval)
	}
}

func TestNewOrphanDetector_CustomInterval(t *testing.T) {
	detector := NewOrphanDetector(nil, nil, slog.Default(), 5*time.Minute)
	if detector.interval != 5*time.Minute {
		t.Errorf("expected interval 5m, got %v", detector.interval)
	}
}

func TestOrphanDetector_EmptyCounts_NoOrphans(t *testing.T) {
	lister := &fakeOrphanCellLister{cellIDs: []string{"cell-0"}}
	querier := &fakeOrphanQuerier{counts: map[string]int{}}

	detector := NewOrphanDetector(lister, querier, slog.Default(), time.Minute)
	detector.check(context.Background())
	// Should not panic
}

func TestOrphanDetector_NegativeInterval_UsesDefault(t *testing.T) {
	detector := NewOrphanDetector(nil, nil, slog.Default(), -1)
	if detector.interval != 60*time.Second {
		t.Errorf("expected default interval for negative input, got %v", detector.interval)
	}
}
