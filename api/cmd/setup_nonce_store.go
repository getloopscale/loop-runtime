// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/getloopscale/loopzero-contracts/serviceauth"
)

// nonceStoreBootstrapTimeout caps how long bootstrap waits for the
// initial Redis PING. Aggressive default: a healthy Redis answers in
// single-digit milliseconds; 2s is plenty for slow networks while
// preventing a hung Redis from blocking API startup indefinitely.
//
// On bootstrap timeout we fall back to in-memory rather than crashing
// — see setup_nonce_store_test.go § TestSetupServiceAuthNonceStore_RedisUnreachable_FallsBackInMemory
// for the rationale.
const nonceStoreBootstrapTimeout = 2 * time.Second

// setupServiceAuthNonceStore returns the service-auth replay-protection nonce store.
//
// Env: LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR (e.g., "redis.loopzero-system:6379").
// When empty OR unreachable, falls back to *InMemoryNonceStore.
//
// Per ADR Q2: Redis is the production target — multi-replica API
// requires shared state for replay protection. In-memory is the
// single-replica / dev / fallback path.
//
// Note: this function MUST be called BEFORE the API starts accepting
// traffic. The middleware singleton has a default in-memory store set
// in pkg/middleware init(), but that default is process-local and
// won't share state across replicas. This function replaces the
// default with the Redis-backed implementation when env-configured.
func setupServiceAuthNonceStore(logger *slog.Logger) serviceauth.NonceStore {
	addr := os.Getenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR")
	if addr == "" {
		logger.Info("service-auth nonce store: in-memory (set LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR for multi-replica Redis)")
		return serviceauth.NewInMemoryNonceStore(5 * time.Minute)
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_PASSWORD"),
		DB:       0,
	})

	// PING to confirm reachability before installing. If Redis is
	// unreachable at bootstrap, fall back to in-memory rather than
	// blocking startup — see test rationale.
	ctx, cancel := context.WithTimeout(context.Background(), nonceStoreBootstrapTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		logger.Warn("service-auth nonce store: Redis ping failed at bootstrap; falling back to in-memory",
			"addr", addr, "error", err)
		return serviceauth.NewInMemoryNonceStore(5 * time.Minute)
	}

	logger.Info("service-auth nonce store: Redis", "addr", addr)
	return serviceauth.NewRedisNonceStore(client, 5*time.Minute)
}
