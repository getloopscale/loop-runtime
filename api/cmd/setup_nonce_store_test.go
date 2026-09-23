// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"log/slog"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/getloopscale/loopzero-contracts/serviceauth"
)

// TestSetupServiceAuthNonceStore_DefaultsToInMemory — when no Redis
// env var is configured, the bootstrap installs the in-memory store.
// This is the dev / single-replica default; production must override.
func TestSetupServiceAuthNonceStore_DefaultsToInMemory(t *testing.T) {
	t.Setenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR", "")

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	store, err := setupServiceAuthNonceStore(logger)
	if err != nil {
		t.Fatalf("setupServiceAuthNonceStore: %v", err)
	}

	// The in-memory store is a *InMemoryNonceStore concrete type.
	// Type-assert to confirm operator did NOT accidentally end up
	// with a nil store (which would panic on first Claim).
	if _, ok := store.(*serviceauth.InMemoryNonceStore); !ok {
		t.Fatalf("expected InMemoryNonceStore default, got %T", store)
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
	store, err := setupServiceAuthNonceStore(logger)
	if err != nil {
		t.Fatalf("setupServiceAuthNonceStore: %v", err)
	}

	if _, ok := store.(*serviceauth.RedisNonceStore); !ok {
		t.Fatalf("expected RedisNonceStore when env set, got %T", store)
	}
}

// Falling back to a per-process store when Redis is unreachable at boot would
// run every replica for its whole lifetime without that protection while the
// operator believes it is on. Configured-but-unreachable is a boot failure, the
// same class as ErrDatabaseURLNotSet; the orchestrator restarts the pod until
// Redis answers. A Redis outage during a request already fails closed.
func TestSetupServiceAuthNonceStore_RedisUnreachableRefusesToBoot(t *testing.T) {
	t.Setenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR", "127.0.0.1:1") // closed port

	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if store, err := setupServiceAuthNonceStore(logger); err == nil {
		t.Fatalf("a configured but unreachable Redis must fail the boot, got %T", store)
	}
}
