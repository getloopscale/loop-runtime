// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
	"fmt"
	"testing"
)

// --- DefaultEngine tests ---

func TestDefaultEngine_AssignCell(t *testing.T) {
	engine := NewDefaultEngine()
	cellID, err := engine.AssignCell(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != DefaultCellID {
		t.Errorf("expected %q, got %q", DefaultCellID, cellID)
	}
}

func TestDefaultEngine_GetCell(t *testing.T) {
	engine := NewDefaultEngine()
	cellID, err := engine.GetCell(context.Background(), "tn-acme")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != DefaultCellID {
		t.Errorf("expected %q, got %q", DefaultCellID, cellID)
	}
}

func TestDefaultEngine_GetCell_AnyTenantReturnsSameCell(t *testing.T) {
	engine := NewDefaultEngine()
	tenants := []string{"tn-acme", "tn-globex", "tn-initech"}
	for _, tenant := range tenants {
		cellID, err := engine.GetCell(context.Background(), tenant)
		if err != nil {
			t.Fatalf("tenant %s: expected no error, got %v", tenant, err)
		}
		if cellID != DefaultCellID {
			t.Errorf("tenant %s: expected %q, got %q", tenant, DefaultCellID, cellID)
		}
	}
}

// --- PassthroughRouter tests ---

func TestPassthroughRouter_Resolve(t *testing.T) {
	router := NewPassthroughRouter(CellConfig{
		CellID:        DefaultCellID,
		KubeConfig:    "/path/to/kubeconfig",
		KubeContext:   "k3d-loopzero-workload",
		ArgocdURL:     "http://argocd:80",
		ArgocdToken:   "test-token",
		IngressDomain: "loopzero.local",
	})

	cfg, err := router.Resolve(context.Background(), "cell-default")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.KubeConfig != "/path/to/kubeconfig" {
		t.Errorf("expected kubeconfig path, got %q", cfg.KubeConfig)
	}
	if cfg.ArgocdURL != "http://argocd:80" {
		t.Errorf("expected ArgoCD URL, got %q", cfg.ArgocdURL)
	}
}

func TestPassthroughRouter_Resolve_AnyCellID(t *testing.T) {
	router := NewPassthroughRouter(CellConfig{
		CellID:      DefaultCellID,
		KubeContext: "default-context",
	})

	// Any cell_id should resolve to the same config (passthrough)
	cfg, err := router.Resolve(context.Background(), "cell-sfo3-02")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.CellID != "cell-sfo3-02" {
		t.Errorf("expected cell_id to be passed through, got %q", cfg.CellID)
	}
	if cfg.KubeContext != "default-context" {
		t.Errorf("expected default context, got %q", cfg.KubeContext)
	}
}

func TestPassthroughRouter_Resolve_EmptyCellID(t *testing.T) {
	router := NewPassthroughRouter(CellConfig{CellID: DefaultCellID})

	_, err := router.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty cell_id, got nil")
	}
}

func TestPassthroughRouter_ListCells(t *testing.T) {
	router := NewPassthroughRouter(CellConfig{
		CellID:      DefaultCellID,
		KubeContext: "k3d-loopzero-workload",
	})

	cells, err := router.ListCells(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}
	if cells[0].CellID != DefaultCellID {
		t.Errorf("expected %q, got %q", DefaultCellID, cells[0].CellID)
	}
}

// --- RoundRobinEngine tests ---

type fakeQuerier struct {
	counts  map[string]int
	cellIDs map[string]string
}

func (f *fakeQuerier) CountByCell(_ context.Context) (map[string]int, error) {
	return f.counts, nil
}

func (f *fakeQuerier) GetTenantCellID(_ context.Context, tenantID string) (string, error) {
	cellID, ok := f.cellIDs[tenantID]
	if !ok {
		return "", fmt.Errorf("tenant not found")
	}
	return cellID, nil
}

func newMultiCellRouter(cells ...string) *multiCellRouter {
	var configs []CellConfig
	for _, c := range cells {
		configs = append(configs, CellConfig{CellID: c})
	}
	return &multiCellRouter{cells: configs}
}

type multiCellRouter struct {
	cells []CellConfig
}

func (r *multiCellRouter) Resolve(_ context.Context, cellID string) (*CellConfig, error) {
	for _, c := range r.cells {
		if c.CellID == cellID {
			return &c, nil
		}
	}
	return nil, fmt.Errorf("cell %q not found", cellID)
}

func (r *multiCellRouter) ListCells(_ context.Context) ([]CellConfig, error) {
	return r.cells, nil
}

func TestRoundRobinEngine_AssignsToLeastLoaded(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-a": 10, "cell-b": 5},
	}
	engine := NewRoundRobinEngine(router, querier, 500)

	cellID, err := engine.AssignCell(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != "cell-b" {
		t.Errorf("expected cell-b (fewer tenants), got %q", cellID)
	}
}

func TestRoundRobinEngine_AssignsToEmptyCell(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-a": 10}, // cell-b has 0 tenants
	}
	engine := NewRoundRobinEngine(router, querier, 500)

	cellID, err := engine.AssignCell(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != "cell-b" {
		t.Errorf("expected cell-b (zero tenants), got %q", cellID)
	}
}

func TestRoundRobinEngine_SkipsFullCell(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-a": 500, "cell-b": 100},
	}
	engine := NewRoundRobinEngine(router, querier, 500)

	cellID, err := engine.AssignCell(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != "cell-b" {
		t.Errorf("expected cell-b (cell-a at capacity), got %q", cellID)
	}
}

func TestRoundRobinEngine_AllCellsFullReturnsError(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{
		counts: map[string]int{"cell-a": 500, "cell-b": 500},
	}
	engine := NewRoundRobinEngine(router, querier, 500)

	_, err := engine.AssignCell(context.Background())
	if err == nil {
		t.Fatal("expected error when all cells at capacity")
	}
}

func TestRoundRobinEngine_GetCellReturnsSticky(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	querier := &fakeQuerier{
		cellIDs: map[string]string{"tn-acme": "cell-a"},
	}
	engine := NewRoundRobinEngine(router, querier, 500)

	cellID, err := engine.GetCell(context.Background(), "tn-acme")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cellID != "cell-a" {
		t.Errorf("expected sticky cell-a, got %q", cellID)
	}
}

func TestRoundRobinEngine_BalancedDistribution(t *testing.T) {
	router := newMultiCellRouter("cell-a", "cell-b")
	counts := map[string]int{"cell-a": 0, "cell-b": 0}
	querier := &fakeQuerier{counts: counts}
	engine := NewRoundRobinEngine(router, querier, 500)

	// Simulate 10 assignments by incrementing counts
	for i := 0; i < 10; i++ {
		cellID, err := engine.AssignCell(context.Background())
		if err != nil {
			t.Fatalf("assignment %d: expected no error, got %v", i, err)
		}
		counts[cellID]++
	}

	// Both cells should have 5 tenants (balanced)
	if counts["cell-a"] != 5 || counts["cell-b"] != 5 {
		t.Errorf("expected balanced 5/5, got cell-a=%d cell-b=%d",
			counts["cell-a"], counts["cell-b"])
	}
}

// --- Interface compliance ---

func TestDefaultEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*DefaultEngine)(nil)
}

func TestRoundRobinEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*RoundRobinEngine)(nil)
}

func TestPassthroughRouter_ImplementsRouter(t *testing.T) {
	var _ Router = (*PassthroughRouter)(nil)
}

func TestStaticRouter_ImplementsRouter(t *testing.T) {
	var _ Router = (*StaticRouter)(nil)
}

// --- StaticRouter tests ---

func TestStaticRouter_ResolvesCorrectCell(t *testing.T) {
	router, err := NewStaticRouter([]CellConfig{
		{CellID: "cell-a", ArgocdURL: "http://argocd-a:80"},
		{CellID: "cell-b", ArgocdURL: "http://argocd-b:80"},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	cfg, err := router.Resolve(context.Background(), "cell-b")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if cfg.ArgocdURL != "http://argocd-b:80" {
		t.Errorf("expected argocd-b URL, got %q", cfg.ArgocdURL)
	}
}

func TestStaticRouter_UnknownCellReturnsError(t *testing.T) {
	router, _ := NewStaticRouter([]CellConfig{{CellID: "cell-a"}})

	_, err := router.Resolve(context.Background(), "cell-unknown")
	if err == nil {
		t.Fatal("expected error for unknown cell_id")
	}
}

func TestStaticRouter_EmptyCellIDReturnsError(t *testing.T) {
	router, _ := NewStaticRouter([]CellConfig{{CellID: "cell-a"}})

	_, err := router.Resolve(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty cell_id")
	}
}

func TestStaticRouter_ListCellsReturnsAll(t *testing.T) {
	router, _ := NewStaticRouter([]CellConfig{
		{CellID: "cell-a"},
		{CellID: "cell-b"},
		{CellID: "cell-c"},
	})

	cells, err := router.ListCells(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cells) != 3 {
		t.Errorf("expected 3 cells, got %d", len(cells))
	}
}

func TestStaticRouter_RejectsDuplicateCellIDs(t *testing.T) {
	_, err := NewStaticRouter([]CellConfig{
		{CellID: "cell-a"},
		{CellID: "cell-a"}, // duplicate
	})
	if err == nil {
		t.Fatal("expected error for duplicate cell_id")
	}
}

func TestStaticRouter_RejectsEmptyList(t *testing.T) {
	_, err := NewStaticRouter([]CellConfig{})
	if err == nil {
		t.Fatal("expected error for empty cell list")
	}
}
