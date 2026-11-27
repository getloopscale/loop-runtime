// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// M23 T3.1 (F20): PollUntil generic helper — canonical value-return closure
// shape per blueprint D1 (matches operators↔tests boundary analysis:
// operators/internal/*/*_test.go uses subtest-style `Eventually(func(g Gomega)`;
// infra/tests/e2e/journey_*_test.go uses value-returning closure with explicit
// timeout+interval — this file extracts the latter into a reusable helper).
//
// Semantics (per plan v1.1 T3.1 Deep Dives EC-3 absorption):
//   - Polls checkFn every `interval` until checkFn returns nil error → success
//     (returns T, nil regardless of T value — zero-value T is a valid success
//     signal, e.g., "wait until count==0 remaining").
//   - If checkFn returns non-nil error, polling continues until `timeout` elapses
//     OR ctx.Done() fires.
//   - interval=0 → default 500ms; timeout=0 → default 30s; ctx=nil → default
//     context.Background().
//   - No panic recovery per rules/error-handling.md § 5 (callers are trusted).

package e2e

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

const (
	defaultPollInterval = 500 * time.Millisecond
	defaultPollTimeout  = 30 * time.Second
)

// PollUntil polls checkFn every `interval` until it returns nil error (success)
// OR `timeout` elapses OR ctx is cancelled. Returns (T, nil) on success or
// (zero-T, error) on timeout/cancel. See file header for full semantics.
func PollUntil[T any](ctx context.Context, timeout, interval time.Duration, checkFn func() (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		ctx = context.Background()
	}
	if timeout <= 0 {
		timeout = defaultPollTimeout
	}
	if interval <= 0 {
		interval = defaultPollInterval
	}

	deadline := time.Now().Add(timeout)
	pollCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// First poll immediately (not after first interval)
	var lastErr error
	for {
		val, err := checkFn()
		if err == nil {
			return val, nil
		}
		lastErr = err

		select {
		case <-pollCtx.Done():
			if lastErr != nil {
				return zero, fmt.Errorf("PollUntil: %w (last error: %v)", pollCtx.Err(), lastErr)
			}
			return zero, pollCtx.Err()
		case <-ticker.C:
			// continue polling
		}
	}
}

// -----------------------------------------------------------------------------
// Hermetic tests (6 cases per plan v1.1 T3.1 TDD section)
// -----------------------------------------------------------------------------

func TestPollUntil_success(t *testing.T) {
	t.Parallel()
	attempts := 0
	val, err := PollUntil(context.Background(), 1*time.Second, 10*time.Millisecond, func() (string, error) {
		attempts++
		if attempts < 3 {
			return "", errors.New("not yet")
		}
		return "ok", nil
	})
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if val != "ok" {
		t.Errorf("expected val=ok, got %q", val)
	}
	if attempts < 3 {
		t.Errorf("expected ≥3 attempts, got %d", attempts)
	}
}

func TestPollUntil_timeout(t *testing.T) {
	t.Parallel()
	_, err := PollUntil(context.Background(), 50*time.Millisecond, 10*time.Millisecond, func() (int, error) {
		return 0, errors.New("always fails")
	})
	if err == nil {
		t.Fatal("expected error after timeout, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.DeadlineExceeded, got %v", err)
	}
}

func TestPollUntil_ctxCancel(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// intentional-sleep: test-scaffolding — trigger ctx.Cancel mid-poll to validate PollUntil cancellation semantics
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	_, err := PollUntil(ctx, 1*time.Second, 10*time.Millisecond, func() (bool, error) {
		return false, errors.New("still checking")
	})
	if err == nil {
		t.Fatal("expected error after ctx cancel, got nil")
	}
	if !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("expected context.Canceled or DeadlineExceeded, got %v", err)
	}
}

func TestPollUntil_zeroIntervalDefault(t *testing.T) {
	t.Parallel()
	start := time.Now()
	// interval=0 → default 500ms; with timeout=100ms, only 1 poll attempted
	attempts := 0
	_, _ = PollUntil(context.Background(), 100*time.Millisecond, 0, func() (int, error) {
		attempts++
		return 0, errors.New("nope")
	})
	elapsed := time.Since(start)
	if attempts > 2 {
		t.Errorf("with 500ms default interval + 100ms timeout, expected ≤2 attempts, got %d", attempts)
	}
	if elapsed > 200*time.Millisecond {
		t.Errorf("expected timeout ~100ms, got %v", elapsed)
	}
}

// EC-3 absorption: zero-value T is a valid success signal.
func TestPollUntil_zeroValueTIsSuccess(t *testing.T) {
	t.Parallel()
	val, err := PollUntil(context.Background(), 1*time.Second, 10*time.Millisecond, func() (int, error) {
		return 0, nil // zero-value int is valid success
	})
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if val != 0 {
		t.Errorf("expected val=0 (zero-value success), got %d", val)
	}
}

// EC-5 absorption: interval > timeout returns DeadlineExceeded after ~timeout.
func TestPollUntil_intervalExceedsTimeout(t *testing.T) {
	t.Parallel()
	start := time.Now()
	_, err := PollUntil(context.Background(), 50*time.Millisecond, 5*time.Second, func() (string, error) {
		return "", errors.New("always fails")
	})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// First poll runs immediately; second poll waits for interval (5s) but timeout (50ms) wins.
	if elapsed > 200*time.Millisecond {
		t.Errorf("interval-exceeds-timeout should terminate near timeout ~50ms, got %v", elapsed)
	}
}
