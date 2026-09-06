// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// DatabaseCredential is a dynamic PG credential issued by Vault database engine.
// Username + password are ephemeral (TTL bounded by Vault role); LeaseID tracks
// the lease for renewal/revocation.
type DatabaseCredential struct {
	Username  string
	Password  string
	LeaseID   string
	LeaseDur  time.Duration
	IssuedAt  time.Time
	Renewable bool
}

// Expired reports whether the credential is past its lease duration.
func (c *DatabaseCredential) Expired(now time.Time) bool {
	return now.After(c.IssuedAt.Add(c.LeaseDur))
}

// DatabaseClient is the minimal contract for fetching dynamic PG credentials
// from Vault database engine (T6 of plan 2026-06-02-openbao-adoption v1.3).
//
// Implementations MUST:
//   - Authenticate via K8s auth (delegated to underlying SDK; see Client)
//   - GetCredentials issues a fresh credential via `database/creds/<role>`
//   - RenewCredential extends lease before TTL expiry (avoid mid-query reconnect)
//   - RevokeCredential explicitly when no longer needed (defense in depth)
type DatabaseClient interface {
	GetCredentials(ctx context.Context, role string) (*DatabaseCredential, error)
	RenewCredential(ctx context.Context, leaseID string, incrementSeconds int) error
	RevokeCredential(ctx context.Context, leaseID string) error
}

// CachingDatabaseClient wraps a DatabaseClient with a per-role credential
// cache. Credentials are renewed when ~30 min before expiry (per T6.4).
//
// Concurrency: cache reads + writes use sync.Mutex (low contention; one
// goroutine per role).
//
// Race safety: GetCredentials() returns the cached cred if still valid
// (>30 min remaining). Otherwise it locks, re-checks (DCL), and refreshes.
// Stale credential window: maximum 30 min before refresh kicks in.
type CachingDatabaseClient struct {
	inner      DatabaseClient
	mu         sync.Mutex
	cache      map[string]*DatabaseCredential
	refreshWin time.Duration // refresh when remaining < this (default 30m)
}

// NewCachingDatabaseClient wraps a base client with caching semantics.
func NewCachingDatabaseClient(inner DatabaseClient, refreshWindow time.Duration) *CachingDatabaseClient {
	if refreshWindow <= 0 {
		refreshWindow = 30 * time.Minute
	}
	return &CachingDatabaseClient{
		inner:      inner,
		cache:      make(map[string]*DatabaseCredential),
		refreshWin: refreshWindow,
	}
}

// GetCredentials returns a non-expired credential for the role, refreshing
// from Vault when the cached one is within refreshWin of expiry.
func (c *CachingDatabaseClient) GetCredentials(ctx context.Context, role string) (*DatabaseCredential, error) {
	if role == "" {
		return nil, fmt.Errorf("vault.CachingDatabaseClient: empty role")
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	if cred, ok := c.cache[role]; ok {
		remaining := time.Until(cred.IssuedAt.Add(cred.LeaseDur))
		if remaining > c.refreshWin {
			return cred, nil
		}
	}

	fresh, err := c.inner.GetCredentials(ctx, role)
	if err != nil {
		return nil, fmt.Errorf("vault.CachingDatabaseClient: get %s: %w", role, err)
	}
	c.cache[role] = fresh
	return fresh, nil
}

// RenewCredential proxies to inner; no cache mutation since renew extends
// the existing LeaseID in place semantically.
func (c *CachingDatabaseClient) RenewCredential(ctx context.Context, leaseID string, incrementSeconds int) error {
	return c.inner.RenewCredential(ctx, leaseID, incrementSeconds)
}

// RevokeCredential proxies to inner + evicts the cached entry on success.
func (c *CachingDatabaseClient) RevokeCredential(ctx context.Context, leaseID string) error {
	if err := c.inner.RevokeCredential(ctx, leaseID); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for role, cred := range c.cache {
		if cred.LeaseID == leaseID {
			delete(c.cache, role)
		}
	}
	return nil
}

// ErrDatabaseEngineDisabled is returned when the database engine is not mounted.
var ErrDatabaseEngineDisabled = errors.New("vault: database engine not mounted at expected path")
