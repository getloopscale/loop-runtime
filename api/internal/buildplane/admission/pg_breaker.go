// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package admission

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PGPoolBreaker is a circuit breaker that watches the pgx pool's
// saturation ratio and trips when it exceeds a high-water mark. Once
// tripped, every `Allow` call returns false until the saturation drops
// below the low-water mark (hysteresis). This implements roadmap 07
// acceptance criterion #3: the cell must reject new mutations when the
// pool is at 90%, while reads continue working.
//
// Hysteresis prevents flapping: without a separate reset threshold, the
// breaker would toggle on every tick near the trip line as acquire /
// release races move the saturation back and forth across 0.9.
//
// The breaker is a LOCAL instrument — each API / controller process has
// its own. It does not attempt to coordinate across replicas because the
// shared resource it protects (Postgres) already serializes connection
// allocation; a local view is both necessary and sufficient.
//
// Usage pattern: in the handler path, before issuing a mutating SQL
// operation, call breaker.Allow(). If false, return
// loopzeroerrors.ErrPlatformDegraded (HTTP 503) so the user gets a clear
// "come back later" signal. Read-only handlers MUST NOT consult the
// breaker — reads do not worsen the saturation the breaker is trying to
// prevent.
//
// WIRED ON 2026-08-19 (B-054). Until that date this breaker was built, started and stopped on
// shutdown — and `Allow()` had no production caller. It measured saturation, tripped and re-armed
// with hysteresis, logged, and no decision consulted it.
//
// What rejected requests was `api/internal/middleware/pgcircuitbreaker.go`, a percentage gate
// WITHOUT hysteresis. The instrument that solved the flapping was switched off and the one
// deciding had the problem.
//
// Today the middleware consults this breaker through `middleware.PoolGate`, an interface declared
// by the CONSUMER (`rules/architecture.md` § DIP) and satisfied by `Allow()` without this package
// knowing about it. The wiring is `cmd/setup_server.go` → `server.Config.PGPoolGate` →
// `server/router.go`.
//
// Consequence for whoever looks for callers: a scan for IMPORTERS of this package does not find
// the middleware, because it does not import it. What proves the wiring is
// `TestPoolGateIsWiredIntoTheMiddleware`, which follows the assignment instead of the import.
//
// One caveat still holds: "let reads through" is ENFORCED by the middleware, which decides by
// HTTP method; `Allow()` here does not see the request and cannot enforce it alone.
type PGPoolBreaker struct {
	pool *pgxpool.Pool
	cfg  BreakerConfig

	// tripped is an atomic bool exposed via Tripped() and consulted in
	// the hot path. Stored as int32 (0/1) so loads are wait-free.
	tripped int32

	// trippedAt is the monotonic timestamp of the last trip, used for
	// log rate limiting and structured events.
	trippedAt atomic.Int64 // unix-nano
	logger    *slog.Logger

	// stop is a channel used to terminate the collection loop. It is
	// closed by Stop(); the loop selects on it alongside the ticker.
	stop chan struct{}
}

// BreakerConfig tunes the circuit breaker thresholds.
type BreakerConfig struct {
	// HighWaterMark is the saturation ratio (0..1) at or above which the
	// breaker trips. Default 0.90 — matches the roadmap 07 red line.
	HighWaterMark float64
	// LowWaterMark is the saturation ratio below which a tripped breaker
	// resets. MUST be strictly less than HighWaterMark to avoid
	// oscillation. Default 0.75.
	LowWaterMark float64
	// Interval is how often the breaker samples the pool. Default 1s —
	// matches the roadmap 07 "< 1s activation time" target.
	Interval time.Duration
	// MaxConns is the pool's configured upper bound. Must be set; 0
	// disables the breaker entirely (Allow always returns true).
	// Required because pgxpool.Stat does not expose the config'd max.
	MaxConns int32
}

// DefaultBreakerConfig returns the roadmap 07-aligned defaults.
func DefaultBreakerConfig(maxConns int32) BreakerConfig {
	return BreakerConfig{
		HighWaterMark: 0.90,
		LowWaterMark:  0.75,
		Interval:      1 * time.Second,
		MaxConns:      maxConns,
	}
}

// NewPGPoolBreaker constructs a breaker bound to the given pool. The
// returned breaker is inert until Start is called.
func NewPGPoolBreaker(pool *pgxpool.Pool, cfg BreakerConfig, logger *slog.Logger) *PGPoolBreaker {
	if cfg.HighWaterMark <= 0 || cfg.HighWaterMark > 1 {
		cfg.HighWaterMark = 0.90
	}
	if cfg.LowWaterMark <= 0 || cfg.LowWaterMark >= cfg.HighWaterMark {
		cfg.LowWaterMark = 0.75
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 1 * time.Second
	}
	return &PGPoolBreaker{
		pool:   pool,
		cfg:    cfg,
		logger: logger,
		stop:   make(chan struct{}),
	}
}

// Start begins the sampling loop. Non-blocking — returns immediately and
// runs the loop in a goroutine until ctx is done or Stop is called.
func (b *PGPoolBreaker) Start(ctx context.Context) {
	if b.pool == nil || b.cfg.MaxConns <= 0 {
		// Nothing to watch — the breaker stays in the inert state.
		return
	}
	go b.run(ctx)
}

// Stop signals the sampling loop to exit.
func (b *PGPoolBreaker) Stop() {
	select {
	case <-b.stop:
		// already closed
	default:
		close(b.stop)
	}
}

// Allow reports whether a new mutating operation may proceed. When the
// breaker has not been initialized (MaxConns=0) or has never tripped,
// returns true unconditionally. On a trip, returns false until the
// sampling loop observes saturation below LowWaterMark.
func (b *PGPoolBreaker) Allow() bool {
	if b.cfg.MaxConns <= 0 {
		return true
	}
	return atomic.LoadInt32(&b.tripped) == 0
}

// Tripped reports whether the breaker is currently open (blocking writes).
// Useful for metrics endpoints and debug handlers.
func (b *PGPoolBreaker) Tripped() bool {
	return atomic.LoadInt32(&b.tripped) != 0
}

// TrippedSince reports the timestamp of the most recent trip. Zero time
// if the breaker has never tripped.
func (b *PGPoolBreaker) TrippedSince() time.Time {
	ns := b.trippedAt.Load()
	if ns == 0 {
		return time.Time{}
	}
	return time.Unix(0, ns)
}

func (b *PGPoolBreaker) run(ctx context.Context) {
	ticker := time.NewTicker(b.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-b.stop:
			return
		case <-ticker.C:
			b.sample()
		}
	}
}

// sample reads one point-in-time pool saturation from the live pool and
// forwards to samplePoint for the trip-state transition. Called from the
// sampling loop on every tick.
func (b *PGPoolBreaker) sample() {
	stats := b.pool.Stat()
	b.samplePoint(stats.AcquiredConns())
}

// samplePoint is the testable state-transition entry point. It takes an
// observed acquired-connection count and updates the trip state with
// hysteresis. Exposed inside the package so unit tests can drive the
// state machine without instantiating a pgxpool.
//
// Rules:
//   - tripped == false and ratio >= high  → trip
//   - tripped == true  and ratio <  low   → reset
//   - otherwise no change
//
// A MaxConns of 0 means the breaker is disabled; samplePoint exits early.
func (b *PGPoolBreaker) samplePoint(acquired int32) {
	if b.cfg.MaxConns <= 0 {
		return
	}
	ratio := float64(acquired) / float64(b.cfg.MaxConns)

	alreadyTripped := atomic.LoadInt32(&b.tripped) != 0
	switch {
	case !alreadyTripped && ratio >= b.cfg.HighWaterMark:
		atomic.StoreInt32(&b.tripped, 1)
		b.trippedAt.Store(time.Now().UnixNano())
		if b.logger != nil {
			b.logger.Warn("pg pool circuit breaker TRIPPED — rejecting new mutations",
				"acquired", acquired,
				"max", b.cfg.MaxConns,
				"ratio", ratio,
				"high_water", b.cfg.HighWaterMark,
			)
		}
	case alreadyTripped && ratio < b.cfg.LowWaterMark:
		atomic.StoreInt32(&b.tripped, 0)
		if b.logger != nil {
			b.logger.Info("pg pool circuit breaker RESET — accepting mutations",
				"acquired", acquired,
				"max", b.cfg.MaxConns,
				"ratio", ratio,
				"low_water", b.cfg.LowWaterMark,
			)
		}
	}
}
