// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package cellmgr

import (
	"context"
	"fmt"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/placement"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

// =============================================================================
// ClientFactory error handling
// =============================================================================

func TestClientManager_FactoryError_PropagatesError(t *testing.T) {
	router := placement.NewPassthroughRouter(placement.CellConfig{
		CellID:      "cell-default",
		KubeConfig:  "/tmp/bad",
		KubeContext: "bad-ctx",
	})
	errFactory := func(kubeconfig, kubecontext string) (kubernetes.Interface, error) {
		return nil, fmt.Errorf("factory error: bad kubeconfig")
	}
	mgr := NewClientManager(router, errFactory)

	client, err := mgr.ClientFor(context.Background(), "cell-a")
	if err == nil {
		t.Fatal("expected error from factory")
	}
	if client != nil {
		t.Error("expected nil client when factory errors")
	}
}

// =============================================================================
// Concurrent access - double-check locking
// =============================================================================

func TestClientManager_ConcurrentAccess_SameCell(t *testing.T) {
	router := placement.NewPassthroughRouter(placement.CellConfig{
		CellID:      "cell-default",
		KubeConfig:  "/tmp/kc",
		KubeContext: "ctx",
	})
	factory := func(kubeconfig, kubecontext string) (kubernetes.Interface, error) {
		return fake.NewClientset(), nil
	}
	mgr := NewClientManager(router, factory)

	errs := make(chan error, 10)
	clients := make(chan kubernetes.Interface, 10)
	for i := 0; i < 10; i++ {
		go func() {
			c, err := mgr.ClientFor(context.Background(), "cell-a")
			errs <- err
			clients <- c
		}()
	}

	var first kubernetes.Interface
	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		c := <-clients
		if first == nil {
			first = c
		} else if c != first {
			t.Error("expected same client for all concurrent callers (double-check locking)")
		}
	}
}
