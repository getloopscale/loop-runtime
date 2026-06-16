// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"log/slog"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/loopzero/loopzero/api/internal/middleware"
	"github.com/getloopscale/loopzero-contracts/serviceauth"
)

// TestSetupServiceAuthNonceStore_DefaultsToInMemory — when no Redis
// env var is configured, the bootstrap installs the in-memory store.
// This is the dev / single-replica default; production must override.
func TestSetupServiceAuthNonceStore_DefaultsToInMemory(t *testing.T) {
	t.Setenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR", "")

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	setupServiceAuthNonceStore(logger)

	// The in-memory store is a *InMemoryNonceStore concrete type.
	// Type-assert to confirm operator did NOT accidentally end up
	// with a nil store (which would panic on first Claim).
	if _, ok := middleware.ServiceAuthNonceStore.(*serviceauth.InMemoryNonceStore); !ok {
		t.Fatalf("expected InMemoryNonceStore default, got %T", middleware.ServiceAuthNonceStore)
	}
}

// TestSetupServiceAuthNonceStore_InstallsRedisWhenAddrSet — happy
// path: operator sets LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR pointing at a
// reachable Redis. Bootstrap installs *RedisNonceStore so multi-replica
// API replay protection works.
func TestSetupServiceAuthNonceStore_InstallsRedisWhenAddrSet(t *testing.T) {
	srv := miniredis.RunT(t)
	t.Setenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR", srv.Addr())

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	setupServiceAuthNonceStore(logger)

	if _, ok := middleware.ServiceAuthNonceStore.(*serviceauth.RedisNonceStore); !ok {
		t.Fatalf("expected RedisNonceStore when env set, got %T", middleware.ServiceAuthNonceStore)
	}
}

// TestSetupServiceAuthNonceStore_RedisUnreachable_FallsBackInMemory —
// fail-safe behavior. If the operator sets the env but the Redis is
// unreachable AT BOOTSTRAP, the API does NOT crash. It logs a loud
// warning and falls back to in-memory.
//
// Rationale: a Redis outage during bootstrap is an infra problem
// orthogonal to the Ed25519 dual-control feature. Failing API
// startup on Redis unreachability would couple the auth-feature
// blast radius to Redis availability — making Redis a startup-time
// SPOF for the entire API. The replay-protection degradation under
// Redis outage is recovered at the FIRST signed request: the
// middleware re-tries via the in-memory store; Phase 2 cutover only
// happens after a 7-day soak, so a brief outage during Phase 1
// bootstrap is acceptable.
//
// A Redis outage DURING a signed request returns 401 (per
// TestRedisNonceStore_PropagatesRedisFailure) — that path is
// fail-closed at request time.
func TestSetupServiceAuthNonceStore_RedisUnreachable_FallsBackInMemory(t *testing.T) {
	t.Setenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR", "127.0.0.1:1") // closed port

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	setupServiceAuthNonceStore(logger)

	if _, ok := middleware.ServiceAuthNonceStore.(*serviceauth.InMemoryNonceStore); !ok {
		t.Fatalf("expected InMemoryNonceStore fallback on unreachable Redis, got %T", middleware.ServiceAuthNonceStore)
	}
}
