// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewVaultRateLimiter_AppliesDefaults(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{})
	if rl.perSecond != 50 {
		t.Errorf("default perSecond = %v, want 50", rl.perSecond)
	}
	if rl.burst != 100 {
		t.Errorf("default burst = %d, want 100", rl.burst)
	}
	if rl.maxCells != 100 {
		t.Errorf("default maxCells = %d, want 100", rl.maxCells)
	}
}

func TestNewVaultRateLimiter_CustomConfig(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 10,
		Burst:     20,
		MaxCells:  5,
	})
	if rl.perSecond != 10 || rl.burst != 20 || rl.maxCells != 5 {
		t.Errorf("custom config mismatch: %+v", rl)
	}
}

func TestAllow_EmptyCellID_Denied(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{})
	if rl.Allow("") {
		t.Error("expected empty cellID denied")
	}
}

func TestAllow_BurstThenReject(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 1, // 1 req/s sustained
		Burst:     3, // burst 3
	})
	// First 3 should pass (burst).
	for i := 0; i < 3; i++ {
		if !rl.Allow("cell-a") {
			t.Errorf("iter %d: expected allowed (burst)", i)
		}
	}
	// 4th should be rejected (bucket empty, refill rate too slow).
	if rl.Allow("cell-a") {
		t.Error("4th request should be rate-limited")
	}
}

func TestAllow_PerCellIndependent(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 1,
		Burst:     2,
	})
	// Exhaust cell-a
	rl.Allow("cell-a")
	rl.Allow("cell-a")
	if rl.Allow("cell-a") {
		t.Error("cell-a should be exhausted")
	}
	// cell-b has its own bucket; first 2 still pass.
	if !rl.Allow("cell-b") {
		t.Error("cell-b first request should pass")
	}
	if !rl.Allow("cell-b") {
		t.Error("cell-b second request should pass (burst)")
	}
}

func TestAllow_MaxCellsCap_RejectsNewCells(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 100,
		Burst:     100,
		MaxCells:  2,
	})
	if !rl.Allow("cell-0") {
		t.Error("cell-0 should pass")
	}
	if !rl.Allow("cell-1") {
		t.Error("cell-1 should pass")
	}
	if rl.Allow("cell-3") {
		t.Error("cell-3 must be rejected (MaxCells cap)")
	}
	if rl.CellCount() != 2 {
		t.Errorf("CellCount = %d, want 2", rl.CellCount())
	}
}

func TestAllow_RefillsAfterTime(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 100, // 100 req/s = 10ms per token refill
		Burst:     1,
	})
	if !rl.Allow("cell-a") {
		t.Fatal("first should pass")
	}
	if rl.Allow("cell-a") {
		t.Fatal("second should be rejected (burst=1, no refill yet)")
	}
	time.Sleep(15 * time.Millisecond) // > 10ms refill window
	if !rl.Allow("cell-a") {
		t.Error("after refill window, request should pass")
	}
}

func TestMiddleware_HappyPath_Passes(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 100, Burst: 100,
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(rl.Middleware(next))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL, nil)
	req.Header.Set("X-Cell-ID", "cell-0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestMiddleware_RateLimited_Returns429(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 1,
		Burst:     1,
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(rl.Middleware(next))
	defer srv.Close()

	req1, _ := http.NewRequest("POST", srv.URL, nil)
	req1.Header.Set("X-Cell-ID", "cell-x")
	resp1, _ := http.DefaultClient.Do(req1)
	resp1.Body.Close()

	req2, _ := http.NewRequest("POST", srv.URL, nil)
	req2.Header.Set("X-Cell-ID", "cell-x")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusTooManyRequests {
		t.Errorf("want 429, got %d", resp2.StatusCode)
	}
	if got := resp2.Header.Get("Retry-After"); got == "" {
		t.Error("expected Retry-After header set")
	}
}

func TestMiddleware_OnRateLimitedCallback(t *testing.T) {
	var called atomic.Int32
	var capturedCell, capturedActor string
	var mu sync.Mutex
	rl := NewVaultRateLimiter(VaultRateLimitConfig{
		PerSecond: 1,
		Burst:     1,
		OnRateLimited: func(cellID, actor string) {
			called.Add(1)
			mu.Lock()
			capturedCell = cellID
			capturedActor = actor
			mu.Unlock()
		},
	})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(rl.Middleware(next))
	defer srv.Close()

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", srv.URL, nil)
		req.Header.Set("X-Cell-ID", "cell-noisy")
		resp, _ := http.DefaultClient.Do(req)
		resp.Body.Close()
	}
	if called.Load() != 1 {
		t.Errorf("OnRateLimited called %d times, want 1", called.Load())
	}
	mu.Lock()
	defer mu.Unlock()
	if capturedCell != "cell-noisy" {
		t.Errorf("captured cellID = %s", capturedCell)
	}
	// No middleware identity in this test → actor empty (still valid).
	if capturedActor != "" {
		t.Errorf("captured actor = %q, want empty (no identity wired)", capturedActor)
	}
}

func TestMiddleware_MissingCellID_Returns429(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Fatal("next should NOT be called when X-Cell-ID missing")
	})
	srv := httptest.NewServer(rl.Middleware(next))
	defer srv.Close()

	req, _ := http.NewRequest("POST", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("http.DefaultClient.Do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("want 429, got %d", resp.StatusCode)
	}
}

func TestMiddleware_429BodyContainsCode(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{PerSecond: 1, Burst: 1})
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	srv := httptest.NewServer(rl.Middleware(next))
	defer srv.Close()

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequest("POST", srv.URL, nil)
		req.Header.Set("X-Cell-ID", "cell-q")
		resp, _ := http.DefaultClient.Do(req)
		if i == 1 {
			buf := make([]byte, 4096)
			n, _ := resp.Body.Read(buf)
			body := string(buf[:n])
			if !strings.Contains(body, "RATE_LIMITED") {
				t.Errorf("expected RATE_LIMITED in body, got: %s", body)
			}
		}
		resp.Body.Close()
	}
}

func TestReset_ClearsCells(t *testing.T) {
	rl := NewVaultRateLimiter(VaultRateLimitConfig{MaxCells: 3})
	rl.Allow("cell-a")
	rl.Allow("cell-b")
	if rl.CellCount() != 2 {
		t.Fatalf("CellCount = %d, want 2", rl.CellCount())
	}
	rl.Reset()
	if rl.CellCount() != 0 {
		t.Errorf("after Reset, CellCount = %d", rl.CellCount())
	}
}
