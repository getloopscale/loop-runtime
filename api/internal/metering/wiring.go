// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// wiring.go — the production assembly of the metering Loop from its
// infrastructure collaborators (plan metering-compute-loop T3.2, slice S5). The
// controller's leader constructs the dedicated worker pool + the core-cluster CR
// client + the cell client provider, then calls NewWorkerLoop to wire them into a
// runnable Loop. Kept as a pure assembly (no I/O) so it is unit-testable; the raw
// pgxpool + cell client provider construction lives at the main.go boundary
// (T6.1 LIVE).

import (
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	"github.com/jackc/pgx/v5/pgxpool"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/getloopscale/loop-runtime/api/internal/leader"
)

// SystemClock is the production Clock — the real wall clock.
type SystemClock struct{}

// Now returns the current time.
func (SystemClock) Now() time.Time { return time.Now() }

// LoopConfig bundles the metering Loop's tunables. MaxDelta MUST be shorter than
// one billing period (ADR-8 / SC-7): a stale-cursor catch-up is clamped to
// MaxDelta, and a value ≥ one period could span two boundaries the split writer
// does not handle. Interval drives the ticker; MaxConcurrency bounds the
// per-tenant fan-out (kept ≤ the dedicated pool's MaxConns so the pool is never
// starved, ADR-10).
type LoopConfig struct {
	Interval       time.Duration
	MaxDelta       time.Duration
	MaxConcurrency int
	Lease          leader.Lease
}

// NewWorkerLoop assembles the full metering Loop: a MeteringStore over the
// dedicated worker pool (used as the money-writer Store, the NamespaceLister, and
// the kill-switch), a CRTenantLister over the core-cluster client, a per-cell
// PodListerFactory over the cell client provider, the Prometheus metrics, and the
// system clock — fenced by the leader lease (SC-6). The pool's lifecycle is owned
// by the caller (main.go closes it on shutdown).
func NewWorkerLoop(pool *pgxpool.Pool, crClient client.Client, cellProvider CellClientProvider, cfg LoopConfig) *Loop {
	store := store.NewMeteringStore(pool)
	metrics := NewPrometheusMetrics()
	clock := SystemClock{}
	worker := &Worker{
		Lister:         &CRTenantLister{Client: crClient},
		Store:          store,
		Namespaces:     store,
		PodListers:     NewCellPodListerFactory(cellProvider),
		Clock:          clock,
		Metrics:        metrics,
		Lease:          cfg.Lease,
		MaxDelta:       cfg.MaxDelta,
		MaxConcurrency: cfg.MaxConcurrency,
	}
	return NewLoop(worker, store, metrics, clock, cfg.Interval)
}
