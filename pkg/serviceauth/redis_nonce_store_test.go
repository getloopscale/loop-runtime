// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package serviceauth_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/loopzero/loopzero/pkg/serviceauth"
)

// newTestRedis spins up an in-process miniredis server + go-redis client.
// Returns (client, cleanup). Cleanup closes both. Suitable for unit
// tests; production wiring uses a real Redis client via api/cmd setup.
func newTestRedis(t *testing.T) (*redis.Client, func()) {
	t.Helper()
	srv := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	cleanup := func() {
		_ = c.Close()
	}
	return c, cleanup
}

// TestRedisNonceStore_BlocksReuseWithinTTL — ADR Q2: Redis-backed
// replay protection. Same invariant as the in-memory store: a
// (user_id, nonce) tuple presented twice within the TTL window is
// rejected on the second attempt.
func TestRedisNonceStore_BlocksReuseWithinTTL(t *testing.T) {
	c, cleanup := newTestRedis(t)
	defer cleanup()

	store := serviceauth.NewRedisNonceStore(c, 5*time.Minute)
	ctx := context.Background()

	if err := store.Claim(ctx, "user-1", "nonce-abc"); err != nil {
		t.Fatalf("first claim must succeed: %v", err)
	}
	err := store.Claim(ctx, "user-1", "nonce-abc")
	if !errors.Is(err, serviceauth.ErrNonceReused) {
		t.Fatalf("second claim within TTL must return ErrNonceReused, got %v", err)
	}
}

// TestRedisNonceStore_IsolatesByUser — per-user nonce scope. User A
// claiming "n1" does not block user B from claiming "n1".
// Without per-user isolation, the cache is a DoS vector (cheap to fill
// for one user).
func TestRedisNonceStore_IsolatesByUser(t *testing.T) {
	c, cleanup := newTestRedis(t)
	defer cleanup()

	store := serviceauth.NewRedisNonceStore(c, 5*time.Minute)
	ctx := context.Background()

	if err := store.Claim(ctx, "user-1", "n-shared"); err != nil {
		t.Fatalf("user-1 first: %v", err)
	}
	if err := store.Claim(ctx, "user-2", "n-shared"); err != nil {
		t.Errorf("user-2 same nonce must succeed (per-user scope), got %v", err)
	}
}

// TestRedisNonceStore_ExpiresAfterTTL — once TTL elapses, the nonce
// becomes claimable again. miniredis exposes a FastForward helper to
// advance virtual time without a real sleep.
func TestRedisNonceStore_ExpiresAfterTTL(t *testing.T) {
	srv := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	defer c.Close()

	store := serviceauth.NewRedisNonceStore(c, 30*time.Second)
	ctx := context.Background()

	if err := store.Claim(ctx, "u", "n"); err != nil {
		t.Fatalf("first claim: %v", err)
	}
	srv.FastForward(31 * time.Second)
	if err := store.Claim(ctx, "u", "n"); err != nil {
		t.Errorf("after TTL, same nonce must be reclaimable: %v", err)
	}
}

// TestRedisNonceStore_Concurrent — N goroutines racing on the same
// (user, nonce). Exactly one must succeed. Redis SET NX is atomic
// at the server level, so this test exercises the network-level race
// rather than a process-local mutex.
func TestRedisNonceStore_Concurrent(t *testing.T) {
	c, cleanup := newTestRedis(t)
	defer cleanup()

	store := serviceauth.NewRedisNonceStore(c, 5*time.Minute)
	ctx := context.Background()

	const N = 100
	var wg sync.WaitGroup
	successes := make(chan struct{}, N)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := store.Claim(ctx, "u", "concurrent-nonce"); err == nil {
				successes <- struct{}{}
			}
		}()
	}
	wg.Wait()
	close(successes)

	count := 0
	for range successes {
		count++
	}
	if count != 1 {
		t.Fatalf("exactly 1 goroutine must succeed; got %d", count)
	}
}

// TestRedisNonceStore_RejectsEmptyInput — defense in depth. Empty
// userID or nonce produces ErrInvalidPayloadInput-shaped error so
// the API audit log can grep by 'service-auth' tag.
func TestRedisNonceStore_RejectsEmptyInput(t *testing.T) {
	c, cleanup := newTestRedis(t)
	defer cleanup()

	store := serviceauth.NewRedisNonceStore(c, 5*time.Minute)
	ctx := context.Background()

	if err := store.Claim(ctx, "", "n"); err == nil {
		t.Error("empty userID must fail")
	}
	if err := store.Claim(ctx, "u", ""); err == nil {
		t.Error("empty nonce must fail")
	}
}

// TestRedisNonceStore_PropagatesRedisFailure — when Redis is
// unavailable, Claim returns an error (not nil, NOT ErrNonceReused).
// Caller's fail-closed semantics depend on this: a successful return
// must mean "the claim is recorded"; any error means "we cannot
// commit to single-use, reject the request".
func TestRedisNonceStore_PropagatesRedisFailure(t *testing.T) {
	srv := miniredis.RunT(t)
	c := redis.NewClient(&redis.Options{Addr: srv.Addr()})
	store := serviceauth.NewRedisNonceStore(c, 5*time.Minute)
	ctx := context.Background()

	srv.Close() // simulate Redis outage AFTER store construction

	err := store.Claim(ctx, "u", "n")
	if err == nil {
		t.Fatal("Redis outage must surface as error (not silent success)")
	}
	if errors.Is(err, serviceauth.ErrNonceReused) {
		t.Errorf("outage must NOT be reported as ErrNonceReused: %v", err)
	}
}

// TestRedisNonceStore_KeyPrefixIsolated — confirms the canonical key
// format `loopzero:service-auth:nonce:<userID>:<nonce>` matches the RFC
// + ADR § Nonce store. Test by direct Redis Get after a successful
// Claim. If we ever change the prefix, this test makes the wire
// contract visible.
func TestRedisNonceStore_KeyFormatMatchesADR(t *testing.T) {
	c, cleanup := newTestRedis(t)
	defer cleanup()

	store := serviceauth.NewRedisNonceStore(c, 5*time.Minute)
	ctx := context.Background()

	if err := store.Claim(ctx, "user-canonical", "nonce-canonical"); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// The canonical key (per ADR § Nonce store): loopzero:service-auth:nonce:user:nonce
	got, err := c.Get(ctx, "loopzero:service-auth:nonce:user-canonical:nonce-canonical").Result()
	if err != nil {
		t.Fatalf("expected key not set or read failed: %v", err)
	}
	if got == "" {
		t.Error("expected non-empty value (presence sentinel)")
	}
}
