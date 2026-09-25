// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package admission

import (
	"testing"
	"time"
)

// The pg_breaker tests exercise the state machine directly without a real
// pgxpool — the production `sample()` path reads from the pool, so here
// we use an alternate entry point `trip` / `reset` to drive transitions
// and assert the hysteresis contract.
//
// We intentionally do NOT use a fake pgxpool: creating one is heavy and
// the logic under test is the state machine, not the pgx integration.
// Live validation comes from roadmap 07 load tests against real PG.

// A thin helper to drive the breaker's state machine via saturation
// ratios, bypassing the sample() reading of the live pool.
func drive(b *PGPoolBreaker, ratio float64) {
	// Emulate the sample() update without touching a real pool.
	// Ratio must be in [0, 1]; tests that pass values outside that
	// range are exercising boundary conditions.
	acquired := int32(float64(b.cfg.MaxConns) * ratio)
	b.samplePoint(acquired)
}

func TestPGPoolBreaker_TripsAtHighWater(t *testing.T) {
	b := NewPGPoolBreaker(nil, BreakerConfig{
		HighWaterMark: 0.90,
		LowWaterMark:  0.75,
		Interval:      time.Second,
		MaxConns:      20,
	}, nil)

	// 80% — below high water, should NOT trip.
	drive(b, 0.80)
	if b.Tripped() {
		t.Error("breaker tripped at 80% (below 90% high water)")
	}

	// 90% — at high water, MUST trip.
	drive(b, 0.90)
	if !b.Tripped() {
		t.Error("breaker did not trip at 90%")
	}
	if b.TrippedSince().IsZero() {
		t.Error("TrippedSince must be set after trip")
	}
}

// TestPGCircuitBreaker_RejectAt90Percent — the roadmap 07 DoD-named test.
// 18 / 20 acquired = 90% ⇒ breaker trips ⇒ Allow returns false.
func TestPGCircuitBreaker_RejectAt90Percent(t *testing.T) {
	b := NewPGPoolBreaker(nil, BreakerConfig{
		HighWaterMark: 0.90,
		LowWaterMark:  0.75,
		Interval:      time.Second,
		MaxConns:      20,
	}, nil)

	b.samplePoint(18) // exactly 90%
	if !b.Tripped() {
		t.Fatal("breaker must trip at 90% saturation")
	}
	if b.Allow() {
		t.Error("Allow must return false after trip")
	}
}

func TestPGPoolBreaker_Hysteresis_HoldsUntilLowWater(t *testing.T) {
	b := NewPGPoolBreaker(nil, BreakerConfig{
		HighWaterMark: 0.90,
		LowWaterMark:  0.75,
		Interval:      time.Second,
		MaxConns:      20,
	}, nil)

	// Trip first.
	drive(b, 0.91)
	if !b.Tripped() {
		t.Fatal("breaker should be tripped")
	}

	// Drop to 80% — still above low water, stays tripped.
	drive(b, 0.80)
	if !b.Tripped() {
		t.Error("breaker must stay tripped above low water (80% > 75%)")
	}

	// Drop to 76% — still above low water, stays tripped.
	drive(b, 0.76)
	if !b.Tripped() {
		t.Error("breaker must stay tripped above low water (76% > 75%)")
	}

	// Drop to 70% — below low water, resets.
	drive(b, 0.70)
	if b.Tripped() {
		t.Error("breaker must reset below low water (70% < 75%)")
	}
	if !b.Allow() {
		t.Error("Allow must return true after reset")
	}
}

func TestPGPoolBreaker_MaxConnsZero_AlwaysAllows(t *testing.T) {
	b := NewPGPoolBreaker(nil, BreakerConfig{
		HighWaterMark: 0.90,
		LowWaterMark:  0.75,
		Interval:      time.Second,
		MaxConns:      0, // disabled
	}, nil)

	// Even calling samplePoint directly has no effect when MaxConns=0
	// because the breaker never evaluates the ratio.
	if !b.Allow() {
		t.Error("Allow must be true when MaxConns=0 (breaker disabled)")
	}
}

func TestPGPoolBreaker_Config_NormalizesInvalid(t *testing.T) {
	// HighWaterMark > 1 and LowWaterMark >= HighWaterMark must be
	// normalized to defaults rather than trip the breaker immediately.
	b := NewPGPoolBreaker(nil, BreakerConfig{
		HighWaterMark: 1.5, // invalid — must become 0.90
		LowWaterMark:  2.0, // invalid — must become 0.75
		MaxConns:      20,
	}, nil)
	if b.cfg.HighWaterMark != 0.90 {
		t.Errorf("HighWaterMark not normalized, got %v", b.cfg.HighWaterMark)
	}
	if b.cfg.LowWaterMark != 0.75 {
		t.Errorf("LowWaterMark not normalized, got %v", b.cfg.LowWaterMark)
	}
}

func TestDefaultBreakerConfig(t *testing.T) {
	c := DefaultBreakerConfig(50)
	if c.HighWaterMark != 0.90 {
		t.Errorf("HighWaterMark = %v, want 0.90", c.HighWaterMark)
	}
	if c.LowWaterMark != 0.75 {
		t.Errorf("LowWaterMark = %v, want 0.75", c.LowWaterMark)
	}
	if c.MaxConns != 50 {
		t.Errorf("MaxConns = %d, want 50", c.MaxConns)
	}
	if c.Interval <= 0 {
		t.Error("Interval must default to > 0 for the sampling loop to run")
	}
}
