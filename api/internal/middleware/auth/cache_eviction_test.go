// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// concurrentTokenValidator is a thread-safe token validator for concurrency tests.
// It implements the real TokenValidator interface — no mocking framework used.
type concurrentTokenValidator struct {
	mu    sync.Mutex
	calls int
}

func (v *concurrentTokenValidator) ValidateToken(_ context.Context, token string) (*TokenValidationResult, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.calls++
	return &TokenValidationResult{
		UserID: "user-" + token,
		Email:  token + "@test.dev",
	}, nil
}

func (v *concurrentTokenValidator) callCount() int {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.calls
}

// concurrentMembershipStore is a thread-safe membership store for concurrency tests.
type concurrentMembershipStore struct{}

func (s *concurrentMembershipStore) GetMemberships(_ context.Context, _ string) ([]TenantMembership, error) {
	return []TenantMembership{{TenantID: "test-tenant", Role: "owner"}}, nil
}

func TestEvictCache_DeletesExpiredEntries(t *testing.T) {
	cache := &sync.Map{}
	var cacheSize atomic.Int64

	now := time.Now()

	// Add entries: half expired, half valid.
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("token-%d", i)
		var expiresAt time.Time
		if i%2 == 0 {
			expiresAt = now.Add(-1 * time.Minute) // expired
		} else {
			expiresAt = now.Add(5 * time.Minute) // valid
		}
		cache.Store(key, &cachedValidation{
			result:    &TokenValidationResult{UserID: key},
			createdAt: now.Add(-time.Duration(100-i) * time.Second),
			expiresAt: expiresAt,
		})
		cacheSize.Add(1)
	}

	if cacheSize.Load() != 100 {
		t.Fatalf("expected 100 entries before eviction, got %d", cacheSize.Load())
	}

	evictCache(cache, &cacheSize, now)

	// 50 expired entries should be deleted; 50 valid remain.
	if cacheSize.Load() != 50 {
		t.Fatalf("expected 50 entries after evicting expired, got %d", cacheSize.Load())
	}
}

func TestEvictCache_DeletesOldestWhenStillOverTarget(t *testing.T) {
	cache := &sync.Map{}
	var cacheSize atomic.Int64

	now := time.Now()

	// Fill cache to maxTokenCacheSize+1 with all-valid entries (none expired).
	count := maxTokenCacheSize + 1
	for i := 0; i < count; i++ {
		key := fmt.Sprintf("token-%d", i)
		cache.Store(key, &cachedValidation{
			result:    &TokenValidationResult{UserID: key},
			createdAt: now.Add(-time.Duration(count-i) * time.Millisecond),
			expiresAt: now.Add(5 * time.Minute),
		})
		cacheSize.Add(1)
	}

	evictCache(cache, &cacheSize, now)

	target := int64(float64(maxTokenCacheSize) * 0.8)
	if cacheSize.Load() > target {
		t.Fatalf("expected cache size <= %d after eviction, got %d", target, cacheSize.Load())
	}
	if _, ok := cache.Load("token-0"); ok {
		t.Error("oldest entry token-0 survived eviction; eviction must remove oldest first")
	}
	if _, ok := cache.Load(fmt.Sprintf("token-%d", count-1)); !ok {
		t.Error("newest entry was evicted; eviction must keep the most recent entries")
	}
}

func TestEvictCache_ConcurrentAccess_NoDataRace(t *testing.T) {
	// This test is designed to be run with -race to detect data races.
	// It exercises concurrent reads, writes, and evictions on the same cache.
	cache := &sync.Map{}
	var cacheSize atomic.Int64

	now := time.Now()

	// Pre-fill with maxTokenCacheSize entries so eviction triggers immediately.
	for i := 0; i < maxTokenCacheSize; i++ {
		key := fmt.Sprintf("token-%d", i)
		var expiresAt time.Time
		if i%3 == 0 {
			expiresAt = now.Add(-1 * time.Minute) // expired
		} else {
			expiresAt = now.Add(5 * time.Minute)
		}
		cache.Store(key, &cachedValidation{
			result:    &TokenValidationResult{UserID: key},
			createdAt: now.Add(-time.Duration(maxTokenCacheSize-i) * time.Millisecond),
			expiresAt: expiresAt,
		})
		cacheSize.Add(1)
	}

	const goroutines = 50
	var wg sync.WaitGroup
	wg.Add(goroutines * 3)

	// Concurrent evictions.
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			evictCache(cache, &cacheSize, time.Now())
		}()
	}

	// Concurrent reads.
	for i := 0; i < goroutines; i++ {
		// Go 1.22+: per-iteration loop variable.
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("token-%d", i)
			cache.Load(key)
		}()
	}

	// Concurrent writes.
	for i := 0; i < goroutines; i++ {
		// Go 1.22+: per-iteration loop variable.
		go func() {
			defer wg.Done()
			key := fmt.Sprintf("new-token-%d", i)
			cache.Store(key, &cachedValidation{
				result:    &TokenValidationResult{UserID: key},
				createdAt: time.Now(),
				expiresAt: time.Now().Add(5 * time.Minute),
			})
			cacheSize.Add(1)
		}()
	}

	wg.Wait()

	// No assertion on exact count — the point is no panic and no data race.
	// The cache size should be reasonable (not negative, not wildly large).
	size := cacheSize.Load()
	if size < 0 {
		t.Fatalf("cache size went negative: %d", size)
	}
}

func TestValidateWithCache_ConcurrentRequests_NoDataRace(t *testing.T) {
	// Exercises the full validateWithCache path under concurrent access.
	// Run with -race to verify no data races in the eviction path.
	validator := &concurrentTokenValidator{}
	store := &concurrentMembershipStore{}

	authn := NewAuthenticator(Config{})
	mw := authn.MiddlewareWithTTL(validator, store, 50*time.Millisecond)

	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw(okHandler)

	const goroutines = 200
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		// Go 1.22+: per-iteration loop variable.
		go func() {
			defer wg.Done()
			// Use unique tokens to fill the cache, and some repeated tokens for cache hits.
			token := fmt.Sprintf("token-%d", i%50)
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Errorf("goroutine %d: expected 200, got %d", i, rec.Code)
			}
		}()
	}

	wg.Wait()

	// Validator should have been called (at least once per unique token on first miss).
	calls := validator.callCount()
	if calls == 0 {
		t.Fatal("expected at least one validator call")
	}
	if calls > goroutines {
		t.Fatalf("expected at most %d validator calls, got %d", goroutines, calls)
	}
}
