// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/getloopscale/contracts/serviceauth"
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
// When empty, it is the in-memory store. When set but unreachable, it is an
// error: the boot fails instead of running every replica without shared
// replay protection while the operator believes it is on.
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
func setupServiceAuthNonceStore(logger *slog.Logger) (serviceauth.NonceStore, error) {
	addr := os.Getenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR")
	if addr == "" {
		logger.Info("service-auth nonce store: in-memory (set LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR for multi-replica Redis)")
		return serviceauth.NewInMemoryNonceStore(5 * time.Minute), nil
	}

	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: os.Getenv("LOOPZERO_SERVICE_AUTH_NONCE_REDIS_PASSWORD"),
		DB:       0,
	})

	// PING to confirm reachability before installing. Configured but
	// unreachable is a boot failure, the same class as a missing database
	// URL; the orchestrator restarts the pod until Redis answers.
	ctx, cancel := context.WithTimeout(context.Background(), nonceStoreBootstrapTimeout)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		if cerr := client.Close(); cerr != nil {
			logger.Warn("service-auth nonce store: closing the unreachable Redis client failed", "error", cerr)
		}
		return nil, fmt.Errorf("service-auth nonce store: LOOPZERO_SERVICE_AUTH_NONCE_REDIS_ADDR=%s is set but Redis did not answer within %s: %w",
			addr, nonceStoreBootstrapTimeout, err)
	}

	logger.Info("service-auth nonce store: Redis", "addr", addr)
	return serviceauth.NewRedisNonceStore(client, 5*time.Minute), nil
}
