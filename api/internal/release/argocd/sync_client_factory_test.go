// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argocd

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/placement"
)

// noopSyncClient is a minimal SyncClient for factory tests.
type noopSyncClient struct {
	id string // tracks which builder call created this client
}

func (n *noopSyncClient) TriggerSync(_ context.Context, _ string) error { return nil }
func (n *noopSyncClient) WaitForSync(_ context.Context, _ string, _ time.Duration, _ []string) error {
	return nil
}

func TestSyncClientFactory_CachesSameCell(t *testing.T) {
	router := placement.NewPassthroughRouter(placement.CellConfig{
		CellID:    "cell-0",
		ArgocdURL: "https://argocd.cell-0.svc",
	})

	callCount := 0
	builder := func(cfg *placement.CellConfig) (SyncClientInterface, error) {
		callCount++
		return &noopSyncClient{id: cfg.CellID}, nil
	}

	factory := NewSyncClientFactory(router, builder)
	ctx := context.Background()

	c1, err := factory.ClientFor(ctx, "cell-0")
	if err != nil {
		t.Fatalf("first ClientFor: %v", err)
	}

	c2, err := factory.ClientFor(ctx, "cell-0")
	if err != nil {
		t.Fatalf("second ClientFor: %v", err)
	}

	if c1 != c2 {
		t.Errorf("expected same instance for same cell, got different pointers")
	}
	if callCount != 1 {
		t.Errorf("expected builder called once, got %d", callCount)
	}
}

func TestSyncClientFactory_DifferentCellsDifferentClients(t *testing.T) {
	// Use a custom router that returns different configs per cell.
	router := &multiCellRouter{
		cells: map[string]*placement.CellConfig{
			"cell-a": {CellID: "cell-a", ArgocdURL: "https://argocd.cell-a.svc"},
			"cell-b": {CellID: "cell-b", ArgocdURL: "https://argocd.cell-b.svc"},
		},
	}

	builder := func(cfg *placement.CellConfig) (SyncClientInterface, error) {
		return &noopSyncClient{id: cfg.CellID}, nil
	}

	factory := NewSyncClientFactory(router, builder)
	ctx := context.Background()

	ca, err := factory.ClientFor(ctx, "cell-a")
	if err != nil {
		t.Fatalf("ClientFor cell-a: %v", err)
	}
	cb, err := factory.ClientFor(ctx, "cell-b")
	if err != nil {
		t.Fatalf("ClientFor cell-b: %v", err)
	}

	if ca == cb {
		t.Errorf("expected different instances for different cells, got same pointer")
	}

	// Verify each got the right cell config
	if ca.(*noopSyncClient).id != "cell-a" {
		t.Errorf("expected cell-a client, got %s", ca.(*noopSyncClient).id)
	}
	if cb.(*noopSyncClient).id != "cell-b" {
		t.Errorf("expected cell-b client, got %s", cb.(*noopSyncClient).id)
	}
}

func TestSyncClientFactory_UnknownCellError(t *testing.T) {
	router := &multiCellRouter{
		cells: map[string]*placement.CellConfig{}, // no cells
	}

	builder := func(cfg *placement.CellConfig) (SyncClientInterface, error) {
		return &noopSyncClient{}, nil
	}

	factory := NewSyncClientFactory(router, builder)
	ctx := context.Background()

	_, err := factory.ClientFor(ctx, "cell-unknown")
	if err == nil {
		t.Fatal("expected error for unknown cell, got nil")
	}
}

// multiCellRouter is a test helper that resolves from a map of known cells.
type multiCellRouter struct {
	cells map[string]*placement.CellConfig
}

func (r *multiCellRouter) Resolve(_ context.Context, cellID string) (*placement.CellConfig, error) {
	cfg, ok := r.cells[cellID]
	if !ok {
		return nil, fmt.Errorf("unknown cell: %s", cellID)
	}
	return cfg, nil
}

func (r *multiCellRouter) ListCells(_ context.Context) ([]placement.CellConfig, error) {
	out := make([]placement.CellConfig, 0, len(r.cells))
	for _, cfg := range r.cells {
		out = append(out, *cfg)
	}
	return out, nil
}
