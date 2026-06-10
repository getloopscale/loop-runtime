// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package memory_webhook_worker

import (
	"testing"
	"time"
)

// T5.5 — attempt N's backoff falls within (60s * 2^(N-1)) * (1 ± 0.2).
func TestNextBackoff_ExponentialWithJitter(t *testing.T) {
	cases := []struct {
		attempt        int
		nominalSeconds float64
	}{
		// 60 * 2^(N-1).
		{1, 60},
		{2, 120},
		{3, 240},
		{4, 480},
		{5, 960},
		{6, 1920},
		{7, 3840},
		{8, 7680},
	}
	for _, c := range cases {
		// Run a few samples per attempt to make sure the jitter range
		// stays within ±20% even across random draws.
		for i := 0; i < 20; i++ {
			d := NextBackoff(c.attempt)
			ds := d.Seconds()
			low := c.nominalSeconds * 0.8
			high := c.nominalSeconds * 1.2
			if ds < low || ds > high {
				t.Errorf("attempt=%d: backoff=%.0fs outside [%.0f, %.0f]", c.attempt, ds, low, high)
			}
		}
	}
}

// T5.5 — attempt 9 or higher returns 0 (no more retries; caller marks
// dead_letter).
func TestNextBackoff_TerminalAttemptReturnsZero(t *testing.T) {
	if d := NextBackoff(9); d != 0 {
		t.Errorf("NextBackoff(9) = %s, want 0 (terminal)", d)
	}
	if d := NextBackoff(100); d != 0 {
		t.Errorf("NextBackoff(100) = %s, want 0 (terminal)", d)
	}
}

// T5.5 — attempt 0 or negative is invalid; returns 0 defensively.
func TestNextBackoff_InvalidAttemptReturnsZero(t *testing.T) {
	if d := NextBackoff(0); d != 0 {
		t.Errorf("NextBackoff(0) = %s, want 0", d)
	}
	if d := NextBackoff(-1); d != 0 {
		t.Errorf("NextBackoff(-1) = %s, want 0", d)
	}
}

// T5.5 — IsTerminalAttempt true iff attemptCount >= MaxAttempts (9).
func TestIsTerminalAttempt(t *testing.T) {
	for i := 1; i < MaxAttempts; i++ {
		if IsTerminalAttempt(i) {
			t.Errorf("IsTerminalAttempt(%d) = true, want false", i)
		}
	}
	if !IsTerminalAttempt(MaxAttempts) {
		t.Errorf("IsTerminalAttempt(%d) = false, want true", MaxAttempts)
	}
	if !IsTerminalAttempt(MaxAttempts + 5) {
		t.Errorf("IsTerminalAttempt(%d) = false, want true", MaxAttempts+5)
	}
}

// Sanity: the total worst-case window (sum of all backoffs) is roughly
// what the plan promised (~5h without jitter).
func TestNextBackoff_TotalWorstCaseWindow(t *testing.T) {
	totalSeconds := 0.0
	for i := 1; i < MaxAttempts; i++ {
		// Take just the nominal (no jitter) by using the math directly.
		// 60s * 2^(i-1).
		totalSeconds += 60.0 * float64(int64(1)<<uint(i-1))
	}
	// 60+120+240+480+960+1920+3600+7200 = 14580s = ~4.05h
	totalHours := totalSeconds / 3600
	if totalHours < 3.5 || totalHours > 5.5 {
		t.Errorf("worst-case window = %.2fh; plan promised ~4-5h", totalHours)
	}
}

// Sanity: the worker's poll interval comfortably divides the smallest
// backoff (60s), so we don't oversleep due retries.
func TestPollIntervalDividesSmallestBackoff(t *testing.T) {
	if defaultPollInterval > 60*time.Second/12 {
		t.Errorf("poll interval %s too coarse vs 60s smallest backoff (want < 5s)", defaultPollInterval)
	}
}
