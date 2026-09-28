// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/getloopscale/loopzero/api/internal/config"
	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/leader"
	"github.com/getloopscale/loopzero/api/internal/metrics"
	"github.com/getloopscale/loopzero/pkg/domain"
)

// leaseAcquirer is the part of leader.Acquirer the API's election uses.
type leaseAcquirer interface {
	Acquire(ctx context.Context, name, cellID, holder string) (*leader.Lease, error)
	Heartbeat(ctx context.Context, lease *leader.Lease) error
	Release(ctx context.Context, lease *leader.Lease) error
}

// loopsElection runs the background loops on at most one process at a time,
// using the same fenced generation lease (leader_generations) as the
// controller binary, under the same name — so the API and the controller can
// never both run the loops.
//
// It replaces a session-scoped pg_try_advisory_lock held on a pooled
// connection. Session locks are re-entrant, so the heartbeat that re-ran
// pg_try_advisory_lock on the holder's connection answered true for as long
// as that connection lived: it could not detect a lost lock, only a dead
// connection (and project Rule 7 forbids session-scoped advisory locks).
type loopsElection struct {
	acquirer leaseAcquirer
	name     string
	cellID   string
	holder   string
	// tick paces both the acquire retries and the heartbeats; production
	// passes a ticker, tests drive it by hand.
	tick   <-chan time.Time
	logger *slog.Logger
}

// run acquires the lease, starts the loops with a context that is cancelled
// when the lease is lost, keeps it alive, and re-enters the election after a
// fence. It returns when ctx is done, releasing a held lease on the way out.
func (e *loopsElection) run(ctx context.Context, startLoops func(ctx context.Context)) {
	for {
		lease, ok := e.acquire(ctx)
		if !ok {
			return
		}
		e.logger.Info("background loops lease acquired", "name", lease.Name, "generation", lease.GenerationID)
		metrics.LeaderStatus.Set(1)
		loopsCtx, stopLoops := context.WithCancel(ctx)
		startLoops(loopsCtx)
		fenced := e.hold(ctx, lease)
		stopLoops()
		metrics.LeaderStatus.Set(0)
		if !fenced {
			e.release(lease)
			return
		}
		e.logger.Error("background loops lease fenced — loops stopped, re-entering the election",
			"name", lease.Name, "generation", lease.GenerationID)
	}
}

// acquire retries until the lease is held (true) or ctx is done (false).
func (e *loopsElection) acquire(ctx context.Context) (*leader.Lease, bool) {
	for {
		lease, err := e.acquirer.Acquire(ctx, e.name, e.cellID, e.holder)
		if err == nil {
			return lease, true
		}
		if !errors.Is(err, leader.ErrNotLeader) {
			e.logger.Warn("background loops lease: acquire failed — will retry", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-e.tick:
		}
	}
}

// hold heartbeats the lease until it is fenced (true) or ctx is done (false).
// A transient heartbeat error is retried: only a fence means another process
// owns the loops.
func (e *loopsElection) hold(ctx context.Context, lease *leader.Lease) bool {
	for {
		select {
		case <-ctx.Done():
			return false
		case <-e.tick:
		}
		err := e.acquirer.Heartbeat(ctx, lease)
		if err == nil {
			continue
		}
		if errors.Is(err, leader.ErrFenced) {
			return true
		}
		e.logger.Warn("background loops lease: heartbeat failed — will retry", "error", err)
	}
}

func (e *loopsElection) release(lease *leader.Lease) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := e.acquirer.Release(ctx, lease); err != nil && !errors.Is(err, leader.ErrFenced) {
		e.logger.Warn("background loops lease: release failed", "error", err)
	}
}

// startLoopsElection runs the loops election in a goroutine, so the HTTP
// server starts at once and standby replicas serve traffic while they wait.
// The lease is the fenced generation lease the controller binary takes, under
// the same name, so the two binaries never run the loops together. With
// LOOPZERO_DISABLE_LEGACY_LOOPS=true the API does not compete: the controller
// owns the loops.
func startLoopsElection(ctx context.Context, bgWg *sync.WaitGroup, cfg config.Config,
	bgAdapter *db.PgxAdapter, logger *slog.Logger, startLoops func(context.Context)) {
	if cfg.DisableLegacyLoops {
		logger.Info("LOOPZERO_DISABLE_LEGACY_LOOPS=true — loops will not run here; controller binary owns them")
		metrics.LeaderStatus.Set(0)
		return
	}
	leaderCfg := leader.DefaultConfig()
	ticker := time.NewTicker(leaderCfg.HeartbeatInterval)
	election := &loopsElection{
		acquirer: leader.NewAcquirer(bgAdapter, leaderCfg),
		name:     leader.BackgroundLoopsLeaseName,
		cellID:   domain.DefaultCellID,
		holder:   leaseHolder(),
		tick:     ticker.C,
		logger:   logger,
	}
	bgWg.Add(1)
	go func() {
		defer bgWg.Done()
		defer ticker.Stop()
		election.run(ctx, startLoops)
	}()
}
