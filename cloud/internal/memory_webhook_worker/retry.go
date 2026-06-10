// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package memory_webhook_worker

import (
	"math/rand"
	"time"
)

// MaxAttempts is the total number of delivery attempts (1 initial + 8
// retries). After this many failures, the row transitions to
// `status='dead_letter'` per D2.
const MaxAttempts = 9

// jitterFrac is the fractional bound for backoff jitter: each computed
// delay is multiplied by (1 + rand[-jitterFrac, +jitterFrac]). Prevents
// thundering-herd retries when a fleet of webhooks fails simultaneously.
const jitterFrac = 0.2

// baseBackoff is the base delay before the first retry. Each subsequent
// retry doubles the base: 60s, 120s, 240s, 480s, 960s, 1920s, 3600s, 7200s.
const baseBackoff = 60 * time.Second

// NextBackoff returns the delay before the (attemptCount+1)-th attempt
// after a failure on the attemptCount-th attempt (1-indexed).
//
// attemptCount=1 (1st attempt just failed) → ~60s before retry 2
// attemptCount=2 → ~120s before retry 3
// ...
// attemptCount=8 → ~7200s before retry 9 (the final attempt)
// attemptCount>=9 → 0 (no more retries; caller marks dead_letter)
//
// Uses a non-deterministic RNG; the jitter is ±20% by default.
func NextBackoff(attemptCount int) time.Duration {
	if attemptCount < 1 || attemptCount >= MaxAttempts {
		return 0
	}
	// 60s * 2^(attemptCount-1)
	exp := baseBackoff << (attemptCount - 1)
	// Jitter: ±20% of exp.
	jitterRange := float64(exp) * jitterFrac
	// rand.Float64 returns [0.0, 1.0); shift to [-1.0, +1.0).
	delta := (rand.Float64()*2 - 1) * jitterRange // #nosec G404 — jitter is a load-spreading heuristic, not a security primitive
	withJitter := float64(exp) + delta
	if withJitter < 0 {
		return 0
	}
	return time.Duration(withJitter)
}

// IsTerminalAttempt reports whether attemptCount represents the final
// allowed attempt (after which no retry happens; dead_letter follows).
func IsTerminalAttempt(attemptCount int) bool {
	return attemptCount >= MaxAttempts
}
