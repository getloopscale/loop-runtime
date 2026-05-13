// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package cellmgr manages Kubernetes clients per Cell.
// Clients are lazily initialized and cached for reuse.
// Thread-safe via sync.RWMutex with double-check pattern.
// ADR-0008 Phase 1.
package cellmgr

import (
	"context"
	"fmt"
	"sync"

	"github.com/loopzero/loopzero/api/internal/placement"

	"k8s.io/client-go/kubernetes"
)

// ClientFactory creates a kubernetes.Interface from a kubeconfig path and context name.
// Tests inject a fake; production wires NewK8sClientFromKubeconfig via setup_kube.go.
type ClientFactory func(kubeconfig, kubecontext string) (kubernetes.Interface, error)

// ClientManager manages Kubernetes clients per Cell.
// It lazily initializes clients on first access and caches them.
// Safe for concurrent use.
type ClientManager struct {
	router  placement.Router
	factory ClientFactory

	mu      sync.RWMutex
	clients map[string]kubernetes.Interface
}

// NewClientManager creates a ClientManager that resolves cell configs via the
// given Router and creates Kubernetes clients via the given factory.
func NewClientManager(router placement.Router, factory ClientFactory) *ClientManager {
	return &ClientManager{
		router:  router,
		factory: factory,
		clients: make(map[string]kubernetes.Interface),
	}
}

// ClientFor returns a Kubernetes client for the given cellID.
// On the first call for a cellID, it resolves the cell config via the Router,
// builds a client via the factory, and caches it. Subsequent calls return the
// cached client. Uses double-check locking to avoid redundant client creation
// under concurrent access.
func (m *ClientManager) ClientFor(ctx context.Context, cellID string) (kubernetes.Interface, error) {
	// Fast path: read lock to check cache.
	m.mu.RLock()
	if client, ok := m.clients[cellID]; ok {
		m.mu.RUnlock()
		return client, nil
	}
	m.mu.RUnlock()

	// Slow path: write lock with double-check.
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check: another goroutine may have created the client.
	if client, ok := m.clients[cellID]; ok {
		return client, nil
	}

	cfg, err := m.router.Resolve(ctx, cellID)
	if err != nil {
		return nil, fmt.Errorf("cellmgr: resolve cell %q: %w", cellID, err)
	}

	client, err := m.factory(cfg.KubeConfig, cfg.KubeContext)
	if err != nil {
		return nil, fmt.Errorf("cellmgr: create client for cell %q: %w", cellID, err)
	}

	m.clients[cellID] = client
	return client, nil
}
