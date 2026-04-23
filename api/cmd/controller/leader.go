// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/loopzero/loopzero/api/internal/leader"
	"github.com/loopzero/loopzero/api/internal/metrics"
)

// leaderLoopName is the name the controller binary uses in the
// leader_generations table. Paired with cell_id from config.
const leaderLoopName = "loopzero-controller"

// runHeartbeat refreshes the leader lease every HeartbeatInterval. On
// ErrFenced it sets LeaderStatus=0 and invokes cancelRoot() which cascades
// to all loop contexts. The process then exits via the normal shutdown
// path and Kubernetes restarts it; the next Acquire will either succeed
// (we become the new leader) or block until the peer dies.
func runHeartbeat(
	ctx context.Context,
	wg *sync.WaitGroup,
	acquirer *leader.Acquirer,
	lease *leader.Lease,
	logger *slog.Logger,
	cancelRoot context.CancelFunc,
) {
	defer wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hbCtx, hbCancel := context.WithTimeout(context.Background(), 10*time.Second)
			err := acquirer.Heartbeat(hbCtx, lease)
			hbCancel()
			if err == nil {
				metrics.LeaderStatus.Set(1)
				continue
			}
			if errors.Is(err, leader.ErrFenced) {
				logger.Error("leader heartbeat fenced — another controller has taken over, exiting")
				metrics.LeaderStatus.Set(0)
				cancelRoot()
				return
			}
			// Transient error — log and retry on the next tick.
			logger.Warn("leader heartbeat transient error — will retry", "error", err)
		}
	}
}
