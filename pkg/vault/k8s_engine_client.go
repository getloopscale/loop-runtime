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

// SAToken is a dynamic K8s ServiceAccount token issued by Vault kubernetes
// secrets engine (T7 of plan 2026-06-02-openbao-adoption v1.3).
//
// Replaces the legacy `type: kubernetes.io/service-account-token` Secret
// pattern (long-lived static token) with short-lived (TTL bounded) JWTs
// signed by the K8s cluster's TokenReview issuer.
type SAToken struct {
	Token         string
	ServiceAccount string
	Namespace      string
	LeaseID        string
	LeaseDur       time.Duration
	IssuedAt       time.Time
}

// Expired reports whether the token is past its lease duration.
func (t *SAToken) Expired(now time.Time) bool {
	return now.After(t.IssuedAt.Add(t.LeaseDur))
}

// K8sEngineClient is the minimal contract for fetching dynamic SA tokens
// from Vault kubernetes engine. Implementations write to
// kubernetes/creds/<role> with optional kubernetes_namespace override.
type K8sEngineClient interface {
	GenerateSAToken(ctx context.Context, role, namespace string) (*SAToken, error)
	RevokeSAToken(ctx context.Context, leaseID string) error
}

// CachingK8sEngineClient wraps a K8sEngineClient with per-(role, namespace)
// token cache. Refresh when ~15 min before expiry (shorter than DB engine
// because SA tokens are typically used for short-lived RPC bursts).
type CachingK8sEngineClient struct {
	inner      K8sEngineClient
	mu         sync.Mutex
	cache      map[string]*SAToken // key = role + "|" + namespace
	refreshWin time.Duration
}

// NewCachingK8sEngineClient wraps a base client with caching.
func NewCachingK8sEngineClient(inner K8sEngineClient, refreshWindow time.Duration) *CachingK8sEngineClient {
	if refreshWindow <= 0 {
		refreshWindow = 15 * time.Minute
	}
	return &CachingK8sEngineClient{
		inner:      inner,
		cache:      make(map[string]*SAToken),
		refreshWin: refreshWindow,
	}
}

// GenerateSAToken returns a cached non-expired token or fetches a fresh one.
func (c *CachingK8sEngineClient) GenerateSAToken(ctx context.Context, role, namespace string) (*SAToken, error) {
	if role == "" {
		return nil, fmt.Errorf("vault.CachingK8sEngineClient: empty role")
	}
	if namespace == "" {
		return nil, fmt.Errorf("vault.CachingK8sEngineClient: empty namespace")
	}
	key := role + "|" + namespace
	c.mu.Lock()
	defer c.mu.Unlock()

	if tok, ok := c.cache[key]; ok {
		remaining := tok.IssuedAt.Add(tok.LeaseDur).Sub(time.Now())
		if remaining > c.refreshWin {
			return tok, nil
		}
	}
	fresh, err := c.inner.GenerateSAToken(ctx, role, namespace)
	if err != nil {
		return nil, fmt.Errorf("vault.CachingK8sEngineClient: generate %s/%s: %w", role, namespace, err)
	}
	c.cache[key] = fresh
	return fresh, nil
}

// RevokeSAToken proxies + evicts cache.
func (c *CachingK8sEngineClient) RevokeSAToken(ctx context.Context, leaseID string) error {
	if err := c.inner.RevokeSAToken(ctx, leaseID); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, tok := range c.cache {
		if tok.LeaseID == leaseID {
			delete(c.cache, key)
		}
	}
	return nil
}

// ErrK8sEngineDisabled is returned when the kubernetes engine is not mounted.
var ErrK8sEngineDisabled = errors.New("vault: kubernetes engine not mounted at expected path")
