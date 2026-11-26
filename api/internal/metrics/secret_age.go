// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"context"
	"log/slog"
	"time"
)

// SecretAgeCollector periodically updates the loopruntime_service_auth_secret_age_seconds
// gauge. If a creation timestamp is provided (via LOOPRUNTIME_SERVICE_AUTH_SECRET_CREATED_AT),
// the age is computed from that. Otherwise it falls back to the process start time,
// which is a lower bound on the real secret age but still useful for alerting on
// long-running processes that never rotated.
type SecretAgeCollector struct {
	createdAt time.Time
	logger    *slog.Logger
	stop      chan struct{}
}

// NewSecretAgeCollector creates a collector that updates the secret age gauge.
// If createdAt is zero, the current time is used (process start fallback).
func NewSecretAgeCollector(createdAt time.Time, logger *slog.Logger) *SecretAgeCollector {
	if createdAt.IsZero() {
		createdAt = time.Now()
		logger.Info("LOOPRUNTIME_SERVICE_AUTH_SECRET_CREATED_AT not set — using process start time for secret age metric")
	} else {
		logger.Info("service auth secret age tracking enabled",
			"created_at", createdAt.Format(time.RFC3339),
			"age", time.Since(createdAt).Round(time.Second).String(),
		)
	}
	return &SecretAgeCollector{
		createdAt: createdAt,
		logger:    logger,
		stop:      make(chan struct{}),
	}
}

// Start begins periodic gauge updates every 60 seconds.
func (c *SecretAgeCollector) Start(ctx context.Context) {
	// Set initial value immediately.
	ServiceAuthSecretAge.Set(time.Since(c.createdAt).Seconds())

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				ServiceAuthSecretAge.Set(time.Since(c.createdAt).Seconds())
			case <-c.stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

// Stop terminates the periodic collection.
func (c *SecretAgeCollector) Stop() {
	close(c.stop)
}
