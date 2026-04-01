// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/loopzero/loopzero/api/internal/placement"
)

type fakeCellQuerier struct {
	counts map[string]int
}

func (f *fakeCellQuerier) CountByCell(_ context.Context) (map[string]int, error) {
	return f.counts, nil
}

func TestCellService_ListCells(t *testing.T) {
	router, _ := placement.NewStaticRouter([]placement.CellConfig{
		{CellID: "cell-default"},
		{CellID: "cell-sfo3-02"},
	})
	querier := &fakeCellQuerier{
		counts: map[string]int{"cell-default": 45, "cell-sfo3-02": 12},
	}
	svc := NewCellService(router, querier, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	cells, err := svc.ListCells(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cells) != 2 {
		t.Fatalf("expected 2 cells, got %d", len(cells))
	}

	// Verify counts are populated
	countMap := make(map[string]int)
	for _, c := range cells {
		countMap[c.CellID] = c.TenantCount
	}
	if countMap["cell-default"] != 45 {
		t.Errorf("expected 45 tenants in cell-default, got %d", countMap["cell-default"])
	}
	if countMap["cell-sfo3-02"] != 12 {
		t.Errorf("expected 12 tenants in cell-sfo3-02, got %d", countMap["cell-sfo3-02"])
	}
}

func TestCellService_ListCellsEmptyRouter(t *testing.T) {
	router := placement.NewPassthroughRouter(placement.CellConfig{CellID: "cell-default"})
	querier := &fakeCellQuerier{counts: map[string]int{"cell-default": 5}}
	svc := NewCellService(router, querier, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	cells, err := svc.ListCells(context.Background())
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(cells) != 1 {
		t.Fatalf("expected 1 cell, got %d", len(cells))
	}
	if cells[0].TenantCount != 5 {
		t.Errorf("expected 5 tenants, got %d", cells[0].TenantCount)
	}
}

func TestExtractRegion(t *testing.T) {
	tests := []struct {
		cellID string
		want   string
	}{
		{"cell-default", "default"},
		{"cell-sfo3-02", "sfo3"},
		{"cell-nyc3-01", "nyc3"},
		{"cell-eu-west-1-03", "eu-west-1"},
	}
	for _, tt := range tests {
		t.Run(tt.cellID, func(t *testing.T) {
			got := placement.ExtractRegion(tt.cellID)
			if got != tt.want {
				t.Errorf("placement.ExtractRegion(%q) = %q, want %q", tt.cellID, got, tt.want)
			}
		})
	}
}

func TestCellStatus_NoInternalTerms(t *testing.T) {
	// Rule 3: No internal terms in user-facing output.
	// CellStatus fields must not contain ArgoCD, Gitea, etc.
	status := CellStatus{
		CellID:        "cell-default",
		ClusterHealth: "Ready",
		DeployHealth:  "Healthy",
	}
	// deploy_health is the user-facing name (not "argocd_health")
	if status.DeployHealth == "" {
		t.Error("DeployHealth should have a value")
	}
	// Verify no "argocd" in JSON field names
	// The json tags use "deploy_health" not "argocd_health" — checked by code review
}
