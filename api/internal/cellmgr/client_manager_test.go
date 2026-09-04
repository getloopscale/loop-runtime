// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cellmgr

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/placement"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// countingRouter wraps a placement.Router and counts Resolve calls per cellID.
type countingRouter struct {
	inner placement.Router
	calls atomic.Int64
}

func (r *countingRouter) Resolve(ctx context.Context, cellID string) (*placement.CellConfig, error) {
	r.calls.Add(1)
	return r.inner.Resolve(ctx, cellID)
}

func (r *countingRouter) ListCells(ctx context.Context) ([]placement.CellConfig, error) {
	return r.inner.ListCells(ctx)
}

// errorRouter always returns an error for Resolve.
type errorRouter struct{}

func (r *errorRouter) Resolve(_ context.Context, cellID string) (*placement.CellConfig, error) {
	return nil, fmt.Errorf("unknown cell: %s", cellID)
}

func (r *errorRouter) ListCells(_ context.Context) ([]placement.CellConfig, error) {
	return nil, nil
}

// fakeFactory returns a ClientFactory that creates fake clientsets.
// Each call returns a new fake.Clientset so identity checks work.
func fakeFactory() ClientFactory {
	return func(kubeconfig, kubecontext string) (kubernetes.Interface, error) {
		return fake.NewClientset(), nil
	}
}

func TestClientManager_ReturnsCachedClient(t *testing.T) {
	// Arrange
	router := &countingRouter{
		inner: placement.NewPassthroughRouter(placement.CellConfig{
			CellID:      "cell-0",
			KubeConfig:  "/tmp/fake-kubeconfig",
			KubeContext: "default",
		}),
	}
	mgr := NewClientManager(router, fakeFactory())
	ctx := context.Background()

	// Act
	client1, err1 := mgr.ClientFor(ctx, "cell-a")
	client2, err2 := mgr.ClientFor(ctx, "cell-a")

	// Assert
	if err1 != nil {
		t.Fatalf("first ClientFor returned error: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("second ClientFor returned error: %v", err2)
	}
	if client1 != client2 {
		t.Error("expected same client instance for same cellID, got different instances")
	}
	if router.calls.Load() != 1 {
		t.Errorf("expected router.Resolve called once, got %d", router.calls.Load())
	}
}

func TestClientManager_DifferentCellsDifferentClients(t *testing.T) {
	// Arrange
	router := placement.NewPassthroughRouter(placement.CellConfig{
		CellID:      "cell-0",
		KubeConfig:  "/tmp/fake-kubeconfig",
		KubeContext: "default",
	})
	mgr := NewClientManager(router, fakeFactory())
	ctx := context.Background()

	// Act
	clientA, errA := mgr.ClientFor(ctx, "cell-a")
	clientB, errB := mgr.ClientFor(ctx, "cell-b")

	// Assert
	if errA != nil {
		t.Fatalf("ClientFor cell-a returned error: %v", errA)
	}
	if errB != nil {
		t.Fatalf("ClientFor cell-b returned error: %v", errB)
	}
	if clientA == clientB {
		t.Error("expected different client instances for different cellIDs, got same instance")
	}
}

func TestClientManager_UnknownCellReturnsError(t *testing.T) {
	// Arrange
	mgr := NewClientManager(&errorRouter{}, fakeFactory())
	ctx := context.Background()

	// Act
	client, err := mgr.ClientFor(ctx, "cell-unknown")

	// Assert
	if err == nil {
		t.Fatal("expected error for unknown cell, got nil")
	}
	if client != nil {
		t.Error("expected nil client when error is returned")
	}
}
