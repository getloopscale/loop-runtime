// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package auth

// token_cache.go — the bounded cache that keeps the token validator off the
// hot path, split out of auth.go by B-059.
//
// The split is not cosmetic. AC-T3.1.5 of the B-059 plan (NFR-006) budgets
// every non-test file under api/internal/middleware/ at 500 lines, and after
// the five-package refactor auth.go stood at 503. Caching is the one concern
// in that file that nothing else in it reads: AuthWithTTL calls
// validateWithCache and never touches the map again, so the seam was already
// there. cache_eviction_test.go, which the refactor filed under auth/ because
// the cache it evicts is the token cache, tests exactly what now lives here.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// maxTokenCacheSize is the maximum number of entries in the token validation cache.
// When exceeded, the cache is cleared to prevent unbounded memory growth.
// The TTL ensures entries expire naturally; this limit is a safety net.
const maxTokenCacheSize = 10000

// cachedValidation holds a cached token validation result.
type cachedValidation struct {
	result    *TokenValidationResult
	createdAt time.Time
	expiresAt time.Time
}

// tokenCacheKey returns a SHA256 hash of the token for use as cache key.
// SEC-001 fix: raw tokens as map keys means a heap dump exposes all active sessions.
// Using a hash prevents token recovery from memory dumps.
func tokenCacheKey(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

func validateWithCache(ctx context.Context, validator TokenValidator, cache *sync.Map, cacheSize *atomic.Int64, token string, ttl time.Duration) (*TokenValidationResult, error) {
	cacheKey := tokenCacheKey(token)

	// Check cache
	if cached, ok := cache.Load(cacheKey); ok {
		cv := cached.(*cachedValidation)
		if time.Now().Before(cv.expiresAt) {
			return cv.result, nil
		}
		cache.Delete(cacheKey)
		cacheSize.Add(-1)
	}

	result, err := validator.ValidateToken(ctx, token)
	if err != nil {
		return nil, err
	}

	// Bound cache size: evict entries when the cache exceeds the limit.
	// First pass: delete expired entries. Second pass (if still over 80%):
	// delete oldest entries until under the target.
	now := time.Now()
	if cacheSize.Add(1) > maxTokenCacheSize {
		evictCache(cache, cacheSize, now)
	}

	cache.Store(cacheKey, &cachedValidation{
		result:    result,
		createdAt: now,
		expiresAt: now.Add(ttl),
	})

	return result, nil
}

// evictCache removes entries from the cache to bring it under the size limit.
// First pass: remove expired entries. If still over 80% capacity, remove the
// oldest entries until the target is reached. All operations use sync.Map
// methods — the struct is never replaced.
func evictCache(cache *sync.Map, cacheSize *atomic.Int64, now time.Time) {
	target := int64(float64(maxTokenCacheSize) * 0.8)

	// First pass: delete expired entries.
	// Use LoadAndDelete to avoid double-decrement when concurrent evictions
	// race on the same key — Delete is idempotent but cacheSize.Add(-1) is not.
	cache.Range(func(key, value any) bool {
		cv := value.(*cachedValidation)
		if now.After(cv.expiresAt) {
			if _, loaded := cache.LoadAndDelete(key); loaded {
				cacheSize.Add(-1)
			}
		}
		return true
	})

	if cacheSize.Load() <= target {
		return
	}

	// Second pass: find the oldest entries and delete them until under target.
	// Collect keys with their creation times, then delete the oldest.
	type entry struct {
		key       any
		createdAt time.Time
	}
	var entries []entry
	cache.Range(func(key, value any) bool {
		cv := value.(*cachedValidation)
		entries = append(entries, entry{key: key, createdAt: cv.createdAt})
		return true
	})

	// Sort oldest first.
	slices.SortFunc(entries, func(a, b entry) int { return a.createdAt.Compare(b.createdAt) })

	// Delete oldest until under target.
	// Use LoadAndDelete to prevent double-decrement from concurrent evictions.
	for _, e := range entries {
		if cacheSize.Load() <= target {
			break
		}
		if _, loaded := cache.LoadAndDelete(e.key); loaded {
			cacheSize.Add(-1)
		}
	}
}
