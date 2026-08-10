// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package vault

import (
	"context"
	"errors"
	"testing"
	"time"
)

type mockK8s struct {
	genCalls    int
	revokeCalls int
	tokenToReturn *SAToken
	genErr      error
	revokeErr   error
}

func (m *mockK8s) GenerateSAToken(_ context.Context, role, namespace string) (*SAToken, error) {
	m.genCalls++
	if m.genErr != nil {
		return nil, m.genErr
	}
	if m.tokenToReturn != nil {
		t := *m.tokenToReturn
		t.IssuedAt = time.Now()
		return &t, nil
	}
	return &SAToken{
		Token:          "eyJhbGciOi...",
		ServiceAccount: role,
		Namespace:      namespace,
		LeaseID:        "lease-" + role + "-001",
		LeaseDur:       1 * time.Hour,
		IssuedAt:       time.Now(),
	}, nil
}

func (m *mockK8s) RevokeSAToken(_ context.Context, _ string) error {
	m.revokeCalls++
	return m.revokeErr
}

func TestSAToken_Expired(t *testing.T) {
	tok := SAToken{
		IssuedAt: time.Now().Add(-2 * time.Hour),
		LeaseDur: 1 * time.Hour,
	}
	if !tok.Expired(time.Now()) {
		t.Error("expected expired")
	}
}

func TestCachingK8sEngineClient_EmptyRole_Errors(t *testing.T) {
	c := NewCachingK8sEngineClient(&mockK8s{}, 15*time.Minute)
	_, err := c.GenerateSAToken(context.Background(), "", "loopzero-system")
	if err == nil {
		t.Fatal("expected error for empty role")
	}
}

func TestCachingK8sEngineClient_EmptyNamespace_Errors(t *testing.T) {
	c := NewCachingK8sEngineClient(&mockK8s{}, 15*time.Minute)
	_, err := c.GenerateSAToken(context.Background(), "loopzero-cell-0", "")
	if err == nil {
		t.Fatal("expected error for empty namespace")
	}
}

func TestCachingK8sEngineClient_CacheHit(t *testing.T) {
	m := &mockK8s{}
	c := NewCachingK8sEngineClient(m, 15*time.Minute)

	tok1, _ := c.GenerateSAToken(context.Background(), "loopzero-cell-0", "loopzero-system")
	tok2, _ := c.GenerateSAToken(context.Background(), "loopzero-cell-0", "loopzero-system")
	if tok1 != tok2 {
		t.Error("expected cache hit")
	}
	if m.genCalls != 1 {
		t.Errorf("expected 1 underlying call, got %d", m.genCalls)
	}
}

func TestCachingK8sEngineClient_PerNamespaceCache(t *testing.T) {
	m := &mockK8s{}
	c := NewCachingK8sEngineClient(m, 15*time.Minute)

	_, _ = c.GenerateSAToken(context.Background(), "loopzero-cell-0", "loopzero-system")
	_, _ = c.GenerateSAToken(context.Background(), "loopzero-cell-0", "argo")

	if m.genCalls != 2 {
		t.Errorf("expected 2 underlying calls (distinct namespaces), got %d", m.genCalls)
	}
}

func TestCachingK8sEngineClient_RefreshWhenNearExpiry(t *testing.T) {
	m := &mockK8s{
		tokenToReturn: &SAToken{
			LeaseDur: 5 * time.Minute, // < 15m refresh window
		},
	}
	c := NewCachingK8sEngineClient(m, 15*time.Minute)
	_, _ = c.GenerateSAToken(context.Background(), "r", "ns")
	_, _ = c.GenerateSAToken(context.Background(), "r", "ns")
	if m.genCalls != 2 {
		t.Errorf("expected 2 calls (refresh), got %d", m.genCalls)
	}
}

func TestCachingK8sEngineClient_GenError_Propagated(t *testing.T) {
	m := &mockK8s{genErr: errors.New("network down")}
	c := NewCachingK8sEngineClient(m, 15*time.Minute)
	_, err := c.GenerateSAToken(context.Background(), "r", "ns")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCachingK8sEngineClient_Revoke_EvictsCache(t *testing.T) {
	m := &mockK8s{}
	c := NewCachingK8sEngineClient(m, 15*time.Minute)

	tok, _ := c.GenerateSAToken(context.Background(), "r", "ns")
	if err := c.RevokeSAToken(context.Background(), tok.LeaseID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, ok := c.cache["r|ns"]; ok {
		t.Error("expected cache entry evicted")
	}
}

func TestCachingK8sEngineClient_DefaultRefreshWindow(t *testing.T) {
	c := NewCachingK8sEngineClient(&mockK8s{}, 0)
	if c.refreshWin != 15*time.Minute {
		t.Errorf("expected default 15m, got %v", c.refreshWin)
	}
}
