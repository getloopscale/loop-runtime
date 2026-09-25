// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package bootstraptest holds the in-memory bootstrap store the bootstrap
// route's tests run against. It lives outside package bootstrap so production
// code carries no test double; tests in any package may import it.
package bootstraptest

import (
	"context"
	"fmt"
	"sync"

	"github.com/getloopscale/loopzero/api/internal/bootstrap/bootstraphttp"
)

// InMemoryStore is an in-memory bootstrap.BootstrapStore for tests.
// It tracks created records in memory to simulate ON CONFLICT DO NOTHING.
type InMemoryStore struct {
	mu      sync.Mutex
	users   map[string]bool   // keyed by github_id
	tenants map[string]bool   // keyed by tenant_id
	members map[string]bool   // keyed by "tenant_id:user_id"
	cellIDs map[string]string // keyed by tenant_id → cell_id (ADR-0008)
}

// NewInMemoryStore creates a new InMemoryStore.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		users:   make(map[string]bool),
		tenants: make(map[string]bool),
		members: make(map[string]bool),
		cellIDs: make(map[string]string),
	}
}

// Bootstrap creates records in memory with idempotency.
func (s *InMemoryStore) Bootstrap(_ context.Context, req bootstraphttp.BootstrapRequest) (string, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	userID := fmt.Sprintf("github-%s", req.AdminLogin)
	tenantID := fmt.Sprintf("tn-%s", req.TenantSlug)
	memberKey := fmt.Sprintf("%s:%s", tenantID, userID)

	created := false

	if !s.users[req.AdminGitHubID] {
		s.users[req.AdminGitHubID] = true
		created = true
	}
	if !s.tenants[tenantID] {
		s.tenants[tenantID] = true
		s.cellIDs[tenantID] = req.CellID // ADR-0008: track cell assignment
		created = true
	}
	if !s.members[memberKey] {
		s.members[memberKey] = true
		created = true
	}

	return userID, tenantID, created, nil
}

// GetCellID returns the stored cell_id for a tenant (test helper).
func (s *InMemoryStore) GetCellID(tenantID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cellIDs[tenantID]
}
