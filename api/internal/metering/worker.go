// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package metering

// worker.go — the compute-metering worker (plan metering-compute-loop T3.1). This
// is the pillar-(a) production caller that finally wires the three tested
// collaborators — TenantLister (enumeration), collectNamespaces (Σ reserved
// request), MeteringStore (the money-writer) — into one leader-gated path per
// active tenant. Every collaborator is injected behind a narrow interface (DIP)
// so meterTenant is unit-testable with fakes: no live cluster, no live Postgres.

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/store"

	"github.com/getloopscale/contracts/plan"

	"github.com/getloopscale/loop-runtime/api/internal/leader"
)

// defaultMeterConcurrency bounds the per-tenant fan-out of a RunOnce pass when
// the Worker does not set MaxConcurrency. Small by design: the writes go through
// the dedicated bounded worker pool (ADR-10), and a high fan-out would starve it.
const defaultMeterConcurrency = 8

// meteringStore is the narrow money-writer surface the worker depends on
// (satisfied by *store.MeteringStore). Defined here, in the consumer, per DIP — the
// worker owns the contract; db provides the concrete.
type meteringStore interface {
	GetCellForTenant(ctx context.Context, tenantID string) (cell string, found bool, err error)
	SeedMeteringCursor(ctx context.Context, tenantID, metric, cellID string) error
	GetMeteringCursor(ctx context.Context, tenantID, metric, cellID string) (cursor time.Time, found bool, err error)
	GetPlanTierForTenant(ctx context.Context, tenantID string) (string, error)
	MeterCompute(ctx context.Context, in store.MeterInput) (advanced bool, delta int64, err error)
	MeterComputeSplit(ctx context.Context, in store.MeterInput) (advanced bool, delta int64, err error)
}

// NamespaceLister resolves a tenant's (project, env) namespaces on its cell from
// canonical PG (ListProjectsByTenant × envs → naming.Namespace). The worker meters
// the Σ request across ALL of them (v1.1's single-namespace bug → under-billing).
type NamespaceLister interface {
	NamespacesForTenant(ctx context.Context, tenantID string) ([]string, error)
}

// PodListerFactory yields a PodLister bound to a specific cell's read-scoped
// client (over SecretCellClientProvider.ClientFor). Per-cell so a tenant's pods
// are listed on the cell PG says it lives on (INV I2), never a mutable CR mirror.
type PodListerFactory interface {
	PodListerForCell(ctx context.Context, cellID string) (PodLister, error)
}

// Clock is the injectable time source for the same-period dispatch decision (a
// real clock in production; a fixed clock in tests — determinism, no flake).
type Clock interface {
	Now() time.Time
}

// Metrics is the thin observability seam the worker records against. Only the
// methods with a call site in meterTenant exist today; the concrete Prometheus
// implementation + the remaining counters land in T3.2 (DIP keeps the worker
// unit-testable with a counting fake).
type Metrics interface {
	CellError()                        // a cell/collect read failed — the cursor was HELD (SC-3)
	Advanced(delta int64)              // the cursor advanced; delta cpu_core_seconds were billed
	Held()                             // the write HELD (lost CAS / backward clock / straddle / stale fence)
	Panic()                            // a metering pass panicked and was recovered (ADR-1 — never crash)
	TenantMetered()                    // one tenant was metered this pass (vs active count — subset-miss)
	PassDuration(seconds float64)      // wall-clock of one RunOnce pass (informer-ceiling signal)
	SecondsSinceLastSuccess(s float64) // freshness — seconds elapsed since the last completed pass
}

// Worker meters every active tenant once per RunOnce pass, gated by the leader
// lease it was constructed with. All fields are interfaces (DIP) except the lease
// (the fence identity) and MaxDelta (the ADR-8 catch-up clamp).
type Worker struct {
	Lister     TenantLister
	Store      meteringStore
	Namespaces NamespaceLister
	PodListers PodListerFactory
	Clock      Clock
	Metrics    Metrics
	Lease      leader.Lease
	MaxDelta   time.Duration
	// MaxConcurrency bounds the per-tenant fan-out of a RunOnce pass (0 →
	// defaultMeterConcurrency). Kept small so the dedicated worker pool (ADR-10)
	// is never starved by too many in-flight metering transactions.
	MaxConcurrency int
}

// RunOnce meters every active tenant once, fanning out with bounded concurrency
// (plan T3.1). It is the production caller that wires the enumeration
// (TenantLister) to the per-tenant metering path (meterTenant). A single tenant's
// failure is logged + collected into the aggregate error but NEVER aborts the
// pass — one unreachable cell or a stale row must not stop the fleet from being
// metered (SC-3 per-tenant HOLD isolation). The returned error is non-nil iff at
// least one tenant failed, so the caller (the T3.2 leader loop) can observe a
// degraded pass. The pass is already leader-gated by the lease the Worker holds;
// no in-pass single-flight is needed (the loop guarantees no overlap).
func (w *Worker) RunOnce(ctx context.Context) error {
	tenants, err := w.Lister.ListActiveTenants(ctx)
	if err != nil {
		return fmt.Errorf("metering: list active tenants: %w", err)
	}

	limit := w.MaxConcurrency
	if limit <= 0 {
		limit = defaultMeterConcurrency
	}

	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var errs []error

	for _, tenantID := range tenants {
		if ctx.Err() != nil {
			// Pass cancelled (shutdown / deadline) — stop spawning. The cursors of
			// the un-metered tenants simply HOLD and are retried next tick.
			break
		}
		wg.Add(1)
		sem <- struct{}{} // acquire (blocks past the concurrency bound)
		go func(tenantID string) {
			defer wg.Done()
			defer func() { <-sem }() // release
			if err := w.meterTenant(ctx, tenantID); err != nil {
				// One tenant's failure never aborts the fleet pass.
				slog.WarnContext(ctx, "metering: tenant pass failed",
					"tenant_id", tenantID, "err", err)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(tenantID)
	}
	wg.Wait()

	if len(errs) == 0 {
		return nil
	}
	return &PassError{Failed: len(errs), Total: len(tenants), Err: errors.Join(errs...)}
}

// PassError reports a pass in which some tenants could not be metered. Total
// is the number of tenants listed for the pass, so Failed < Total means the
// pass still metered someone.
type PassError struct {
	Failed, Total int
	Err           error
}

func (e *PassError) Error() string {
	return fmt.Sprintf("metering: %d of %d tenants failed: %v", e.Failed, e.Total, e.Err)
}

func (e *PassError) Unwrap() error { return e.Err }

// meterTenant meters ONE active tenant for the cpu_core_seconds metric. Order
// (SEPA-locked): resolve cell (skip if unplaced — cheapest guard) → enumerate
// namespaces → list pods on the cell → Σ request. SC-3: a collect/list failure
// short-circuits with a typed error BEFORE any seed or meter (the cursor is
// HELD, retried next tick — a read failure is NEVER swallowed to Σreq=0). Then
// seed-if-absent (SC-5) → read the cursor (the CAS target) → resolve plan tier →
// dispatch same-period (MeterCompute) vs straddle (MeterComputeSplit) on a Go
// month guess (the DB =/<> predicate is the safety net: a wrong guess is a 0-row
// HOLD, never a mis-bill).
func (w *Worker) meterTenant(ctx context.Context, tenantID string) error {
	cell, found, err := w.Store.GetCellForTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("metering: get cell for tenant %s: %w", tenantID, err)
	}
	if !found {
		// No canonical placement (a CR Active whose PG tenants row was deleted
		// mid-race has no cell + no projects) — nothing to meter. Harmless skip,
		// not an error, and NOT a cell error (nothing failed).
		return nil
	}

	namespaces, err := w.Namespaces.NamespacesForTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("metering: namespaces for tenant %s: %w", tenantID, err)
	}

	podLister, err := w.PodListers.PodListerForCell(ctx, cell)
	if err != nil {
		w.Metrics.CellError()
		return fmt.Errorf("metering: pod lister for cell %s: %w", cell, err)
	}

	cpuMillicores, _, err := collectNamespaces(ctx, podLister, namespaces)
	if err != nil {
		// SC-3: read failure HOLDS the cursor — short-circuit BEFORE seed/meter.
		w.Metrics.CellError()
		return fmt.Errorf("metering: collect for tenant %s on cell %s: %w", tenantID, cell, err)
	}

	metric := string(plan.MetricCPUCoreSeconds)
	if err := w.Store.SeedMeteringCursor(ctx, tenantID, metric, cell); err != nil {
		return fmt.Errorf("metering: seed cursor for tenant %s: %w", tenantID, err)
	}
	expected, found, err := w.Store.GetMeteringCursor(ctx, tenantID, metric, cell)
	if err != nil {
		return fmt.Errorf("metering: read cursor for tenant %s: %w", tenantID, err)
	}
	if !found {
		// Seeded immediately above → must exist; absence is a real anomaly, not a
		// tick to skip silently (fail loud, Rule 8).
		return fmt.Errorf("metering: cursor absent after seed for tenant %s", tenantID)
	}

	tier, err := w.Store.GetPlanTierForTenant(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("metering: plan tier for tenant %s: %w", tenantID, err)
	}

	in := store.MeterInput{
		TenantID:     tenantID,
		Metric:       metric,
		CellID:       cell,
		Expected:     expected,
		MaxDelta:     w.MaxDelta,
		ReqMillicore: cpuMillicores,
		Gen:          w.Lease.GenerationID,
		LoopName:     w.Lease.Name,
		LeaderCell:   w.Lease.CellID,
		PlanTier:     tier,
	}

	var advanced bool
	var delta int64
	if sameMonth(expected.UTC(), w.Clock.Now().UTC()) {
		advanced, delta, err = w.Store.MeterCompute(ctx, in)
	} else {
		advanced, delta, err = w.Store.MeterComputeSplit(ctx, in)
	}
	if err != nil {
		return fmt.Errorf("metering: meter tenant %s: %w", tenantID, err)
	}
	if advanced {
		w.Metrics.Advanced(delta)
	} else {
		w.Metrics.Held()
	}
	// The tenant completed a clean pass (advanced OR held without error). Count it
	// so tenants_metered_total can be compared against the active-tenant gauge — a
	// persistent shortfall is the subset-miss alarm (a slice silently skipped). A
	// tenant whose collect/meter ERRORED returned above BEFORE this line, so it is
	// correctly excluded from the metered count.
	w.Metrics.TenantMetered()
	return nil
}

// sameMonth reports whether two instants fall in the same UTC calendar month —
// the Go mirror of the SQL metering_period_of (date_trunc('month', ts UTC)).
// Year is compared too: Dec 2026 and Dec 2027 are different periods.
func sameMonth(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month()
}
