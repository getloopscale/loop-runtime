// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mockDB struct {
	getCalls    int
	renewCalls  int
	revokeCalls int
	credToReturn *DatabaseCredential
	getErr      error
	renewErr    error
	revokeErr   error
}

func (m *mockDB) GetCredentials(_ context.Context, role string) (*DatabaseCredential, error) {
	m.getCalls++
	if m.getErr != nil {
		return nil, m.getErr
	}
	if m.credToReturn != nil {
		// Return a fresh copy each call so cache stores distinct objects per role.
		c := *m.credToReturn
		c.IssuedAt = time.Now()
		return &c, nil
	}
	return &DatabaseCredential{
		Username:  "v-k8s-" + role + "-abc",
		Password:  "secret-pw",
		LeaseID:   "lease-" + role + "-001",
		LeaseDur:  1 * time.Hour,
		IssuedAt:  time.Now(),
		Renewable: true,
	}, nil
}

func (m *mockDB) RenewCredential(_ context.Context, _ string, _ int) error {
	m.renewCalls++
	return m.renewErr
}

func (m *mockDB) RevokeCredential(_ context.Context, _ string) error {
	m.revokeCalls++
	return m.revokeErr
}

func TestDatabaseCredential_Expired(t *testing.T) {
	c := DatabaseCredential{
		IssuedAt: time.Now().Add(-2 * time.Hour),
		LeaseDur: 1 * time.Hour,
	}
	if !c.Expired(time.Now()) {
		t.Error("expected expired (issued 2h ago, lease 1h)")
	}
}

func TestDatabaseCredential_NotExpired(t *testing.T) {
	c := DatabaseCredential{
		IssuedAt: time.Now(),
		LeaseDur: 1 * time.Hour,
	}
	if c.Expired(time.Now()) {
		t.Error("expected NOT expired (just issued)")
	}
}

func TestCachingDatabaseClient_EmptyRole_Errors(t *testing.T) {
	c := NewCachingDatabaseClient(&mockDB{}, 30*time.Minute)
	_, err := c.GetCredentials(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty role")
	}
}

func TestCachingDatabaseClient_GetCredentials_CacheHit(t *testing.T) {
	m := &mockDB{}
	c := NewCachingDatabaseClient(m, 30*time.Minute)

	cred1, err := c.GetCredentials(context.Background(), "loopzero-api")
	if err != nil {
		t.Fatalf("get 1: %v", err)
	}
	cred2, err := c.GetCredentials(context.Background(), "loopzero-api")
	if err != nil {
		t.Fatalf("get 2: %v", err)
	}
	if cred1 != cred2 {
		t.Error("expected cache hit — second call should return same instance")
	}
	if m.getCalls != 1 {
		t.Errorf("expected 1 underlying call, got %d", m.getCalls)
	}
}

func TestCachingDatabaseClient_RefreshWhenNearExpiry(t *testing.T) {
	m := &mockDB{
		credToReturn: &DatabaseCredential{
			Username:  "u",
			Password:  "p",
			LeaseID:   "l",
			LeaseDur:  20 * time.Minute, // < 30m refresh window
			Renewable: true,
		},
	}
	c := NewCachingDatabaseClient(m, 30*time.Minute)

	_, _ = c.GetCredentials(context.Background(), "loopzero-api")
	_, _ = c.GetCredentials(context.Background(), "loopzero-api")

	if m.getCalls != 2 {
		t.Errorf("expected 2 underlying calls (refresh because near expiry), got %d", m.getCalls)
	}
}

func TestCachingDatabaseClient_GetError_Propagated(t *testing.T) {
	m := &mockDB{getErr: errors.New("boom")}
	c := NewCachingDatabaseClient(m, 30*time.Minute)
	_, err := c.GetCredentials(context.Background(), "loopzero-api")
	if err == nil {
		t.Fatal("expected error to propagate")
	}
}

func TestCachingDatabaseClient_Revoke_EvictsCache(t *testing.T) {
	m := &mockDB{}
	c := NewCachingDatabaseClient(m, 30*time.Minute)

	cred1, err := c.GetCredentials(context.Background(), "loopzero-api")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if err := c.RevokeCredential(context.Background(), cred1.LeaseID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := c.cache["loopzero-api"]; ok {
		t.Error("expected cache entry to be evicted after revoke")
	}
	if m.revokeCalls != 1 {
		t.Errorf("expected 1 revoke call, got %d", m.revokeCalls)
	}
}

func TestCachingDatabaseClient_DefaultRefreshWindow(t *testing.T) {
	c := NewCachingDatabaseClient(&mockDB{}, 0)
	if c.refreshWin != 30*time.Minute {
		t.Errorf("expected default 30m, got %v", c.refreshWin)
	}
}

func TestCachingDatabaseClient_Renew_ProxiesToInner(t *testing.T) {
	m := &mockDB{}
	c := NewCachingDatabaseClient(m, 30*time.Minute)
	if err := c.RenewCredential(context.Background(), "lease-x", 3600); err != nil {
		t.Fatalf("renew: %v", err)
	}
	if m.renewCalls != 1 {
		t.Errorf("expected 1 renew call, got %d", m.renewCalls)
	}
}

func TestCachingDatabaseClient_MultiRole_IndependentCache(t *testing.T) {
	m := &mockDB{}
	c := NewCachingDatabaseClient(m, 30*time.Minute)

	_, _ = c.GetCredentials(context.Background(), "loopzero-api")
	_, _ = c.GetCredentials(context.Background(), "loopzero-build")

	if m.getCalls != 2 {
		t.Errorf("expected 2 underlying calls (distinct roles), got %d", m.getCalls)
	}
	if len(c.cache) != 2 {
		t.Errorf("expected 2 cache entries, got %d", len(c.cache))
	}
}
