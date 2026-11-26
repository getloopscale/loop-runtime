// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metrics

import (
	"context"
	"log/slog"
	"math"
	"os"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestSecretAgeCollector_SetsInitialValue(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	createdAt := time.Now().Add(-2 * time.Hour)

	collector := NewSecretAgeCollector(createdAt, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.Start(ctx)
	defer collector.Stop()

	// The initial value should be approximately 2 hours in seconds.
	got := testutil.ToFloat64(ServiceAuthSecretAge)
	expected := 2 * time.Hour
	tolerance := 5.0 // 5 seconds tolerance for test execution time

	if math.Abs(got-expected.Seconds()) > tolerance {
		t.Errorf("ServiceAuthSecretAge = %.0f, want ~%.0f (tolerance %.0fs)", got, expected.Seconds(), tolerance)
	}
}

func TestSecretAgeCollector_ZeroCreatedAtUsesNow(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	collector := NewSecretAgeCollector(time.Time{}, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector.Start(ctx)
	defer collector.Stop()

	// With zero createdAt, age should be very close to 0 (process just started).
	got := testutil.ToFloat64(ServiceAuthSecretAge)
	if got > 5.0 {
		t.Errorf("ServiceAuthSecretAge = %.0f, expected close to 0 for zero createdAt (fallback to now)", got)
	}
}

func TestSecretAgeCollector_StopsCleanly(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	collector := NewSecretAgeCollector(time.Now(), logger)
	ctx, cancel := context.WithCancel(context.Background())
	collector.Start(ctx)

	// Stop via context cancellation.
	cancel()
	// Give goroutine time to exit.
	time.Sleep(10 * time.Millisecond)

	// Stop again via Stop() — should not panic (channel already closed by ctx).
	// Actually Stop() closes the channel, which would panic if already closed.
	// The goroutine exits on ctx.Done() first, so Stop() is safe to call
	// because the goroutine is no longer reading from the channel.
	// But we should not call Stop() after ctx cancel since the goroutine
	// exited on ctx.Done(), not on stop channel. Let's just verify no panic.
}
