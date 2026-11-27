// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package argocd

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/placement"
)

// SyncClientInterface abstracts ArgoCD sync operations.
// This matches the shared.SyncClient interface — both are satisfied
// by *SyncClient and test doubles. Defined here so the factory can
// reference it without importing the services package (avoids a cycle).
type SyncClientInterface interface {
	TriggerSync(ctx context.Context, appName string) error
	WaitForSync(ctx context.Context, appName string, timeout time.Duration, deployedApps []string) error
}

// SyncClientBuilder creates a SyncClientInterface from a CellConfig.
// In production, this wraps NewSyncClient with the appropriate config.
type SyncClientBuilder func(cfg *placement.CellConfig) (SyncClientInterface, error)

// SyncClientFactory creates and caches SyncClient instances per Cell.
// Each Cell has its own ArgoCD at a different URL.
// Thread-safe with sync.RWMutex + double-check pattern.
// ADR-0008 Phase 1.
type SyncClientFactory struct {
	router  placement.Router
	builder SyncClientBuilder

	mu      sync.RWMutex
	clients map[string]SyncClientInterface
}

// NewSyncClientFactory creates a factory that lazily creates and caches
// SyncClient instances, one per Cell.
func NewSyncClientFactory(router placement.Router, builder SyncClientBuilder) *SyncClientFactory {
	return &SyncClientFactory{
		router:  router,
		builder: builder,
		clients: make(map[string]SyncClientInterface),
	}
}

// ClientFor returns a cached SyncClientInterface for the given cellID,
// creating one on first access. Thread-safe via double-check locking:
// fast-path read lock, slow-path write lock with re-check.
func (f *SyncClientFactory) ClientFor(ctx context.Context, cellID string) (SyncClientInterface, error) {
	// Fast path: read lock check.
	f.mu.RLock()
	if client, ok := f.clients[cellID]; ok {
		f.mu.RUnlock()
		return client, nil
	}
	f.mu.RUnlock()

	// Slow path: write lock, then resolve + build.
	// R-09 fix: Resolve() inside write lock for consistency with ClientManager.
	f.mu.Lock()
	defer f.mu.Unlock()

	// Double-check: another goroutine may have created it while we waited.
	if client, ok := f.clients[cellID]; ok {
		return client, nil
	}

	cfg, err := f.router.Resolve(ctx, cellID)
	if err != nil {
		return nil, fmt.Errorf("sync client factory: resolve cell %q: %w", cellID, err)
	}

	client, err := f.builder(cfg)
	if err != nil {
		return nil, fmt.Errorf("sync client factory: build client for cell %q: %w", cellID, err)
	}

	f.clients[cellID] = client
	return client, nil
}
