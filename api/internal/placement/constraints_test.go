// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"testing"
)

type fakeLabelQuerier struct {
	labels map[string]map[string]string // cellID → labels
}

func (f *fakeLabelQuerier) GetCellLabels(_ context.Context, cellID string) (map[string]string, error) {
	if labels, ok := f.labels[cellID]; ok {
		return labels, nil
	}
	return map[string]string{}, nil
}

func TestConstraintEngine_RegionAffinity(t *testing.T) {
	router := newMultiCellRouter("cell-sfo3-01", "cell-nyc3-01")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-sfo3-01": 10, "cell-nyc3-01": 10},
	}
	labels := &fakeLabelQuerier{labels: map[string]map[string]string{}}
	engine := NewConstraintEngine(router, querier, labels, 500)

	cellID, err := engine.AssignCellWithOpts(context.Background(), PlacementOpts{
		PreferredRegion: "sfo3",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != "cell-sfo3-01" {
		t.Errorf("expected cell-sfo3-01 (region affinity), got %q", cellID)
	}
}

func TestConstraintEngine_RegionAffinityWithCapacity(t *testing.T) {
	router := newMultiCellRouter("cell-sfo3-01", "cell-nyc3-01")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-sfo3-01": 500, "cell-nyc3-01": 10}, // sfo3 full
	}
	labels := &fakeLabelQuerier{labels: map[string]map[string]string{}}
	engine := NewConstraintEngine(router, querier, labels, 500)

	cellID, err := engine.AssignCellWithOpts(context.Background(), PlacementOpts{
		PreferredRegion: "sfo3",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// sfo3 is at capacity, falls back to nyc3
	if cellID != "cell-nyc3-01" {
		t.Errorf("expected cell-nyc3-01 (sfo3 full), got %q", cellID)
	}
}

func TestConstraintEngine_RequiredLabels(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b", "cell-c")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-a": 0, "cell-b": 0, "cell-c": 0},
	}
	labels := &fakeLabelQuerier{
		labels: map[string]map[string]string{
			"cell-a": {"tier": "starter"},
			"cell-b": {"tier": "premium"},
			"cell-c": {"tier": "premium"},
		},
	}
	engine := NewConstraintEngine(router, querier, labels, 500)

	cellID, err := engine.AssignCellWithOpts(context.Background(), PlacementOpts{
		RequiredLabels: map[string]string{"tier": "premium"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	// Only cell-b and cell-c are premium, both have 0 tenants
	if cellID != "cell-b" && cellID != "cell-c" {
		t.Errorf("expected cell-b or cell-c (premium), got %q", cellID)
	}
}

func TestConstraintEngine_NoMatchingLabels(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{counts: map[string]int{}}
	labels := &fakeLabelQuerier{
		labels: map[string]map[string]string{
			"cell-a": {"tier": "starter"},
			"cell-b": {"tier": "starter"},
		},
	}
	engine := NewConstraintEngine(router, querier, labels, 500)

	_, err := engine.AssignCellWithOpts(context.Background(), PlacementOpts{
		RequiredLabels: map[string]string{"tier": "enterprise"},
	})
	if err == nil {
		t.Fatal("expected error when no cells match labels")
	}
}

func TestConstraintEngine_AllAtCapacity(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-a": 500, "cell-b": 500},
	}
	labels := &fakeLabelQuerier{labels: map[string]map[string]string{}}
	engine := NewConstraintEngine(router, querier, labels, 500)

	_, err := engine.AssignCellWithOpts(context.Background(), PlacementOpts{})
	if err == nil {
		t.Fatal("expected error when all cells at capacity")
	}
}

func TestConstraintEngine_DefaultAssignCell(t *testing.T) {
	// AssignCell (no opts) uses empty PlacementOpts — should pick least-loaded
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-a": 100, "cell-b": 50},
	}
	labels := &fakeLabelQuerier{labels: map[string]map[string]string{}}
	engine := NewConstraintEngine(router, querier, labels, 500)

	cellID, err := engine.AssignCell(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != "cell-b" {
		t.Errorf("expected cell-b (least loaded), got %q", cellID)
	}
}

func TestConstraintEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*ConstraintEngine)(nil)
}
