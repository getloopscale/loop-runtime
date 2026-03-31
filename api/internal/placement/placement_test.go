// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package placement

import (
	"context"
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

// --- Interface compliance ---

func TestDefaultEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = (*DefaultEngine)(nil)
}

func TestPassthroughRouter_ImplementsRouter(t *testing.T) {
	var _ Router = (*PassthroughRouter)(nil)
}
