// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package admission implements the build admission gate described in
// roadmap/07-backpressure-and-admission. It answers a single question
// before the API accepts a new build: "Can this cell+tenant actually
// make progress on this work right now?"
//
// The gate checks two budgets sourced from the cell capacity document
// (cell-budget):
//
//  1. **Per-cell concurrent build ceiling.** Reflects the real BuildKit
//     solve capacity of the build cluster plus the reconcile loop's
//     MaxConcurrent cap. When exceeded, the gate returns
//     `ErrCellAtCapacity` (HTTP 429) with a Retry-After hint.
//
//  2. **Per-tenant concurrent build quota.** Enforces fairness so one
//     tenant submitting 50 builds cannot starve 49 other tenants on the
//     same cell. When exceeded, the gate returns `ErrTenantBuildQuota`
//     (HTTP 429) and the caller should serialize its own work.
//
// Both limits are advisory — the gate's decision is based on a point-in-
// time snapshot, not a transaction. That is acceptable because:
//
//   - The reconcile loop is already bounded by its own MaxConcurrent and
//     MaxBuildsPerTenant semaphores.
//   - Builds queue safely in the QUEUED state; rejecting at the front
//     door just gives the user a clear signal instead of a silent wait.
//   - A stale count causes at most one extra admission; the next tick
//     will reject the subsequent one.
//
// The gate is configured via `LOOP_RUNTIME_ADMISSION_CONTROL=true`. When the
// flag is false (the default today), `Check` always returns `Admit` so
// the existing dev stack keeps behaving exactly as before. This is the
// rollback path documented in roadmap 07.
package admission

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Decision is the result of an admission check.
type Decision int

const (
	// Admit — the build can proceed. Callers should submit the build.
	Admit Decision = iota
	// RejectCellAtCapacity — the cell has no free concurrent-build slot.
	// Callers must return HTTP 429 with ErrCellAtCapacity and a
	// Retry-After header derived from Result.RetryAfter.
	RejectCellAtCapacity
	// RejectTenantQuotaExceeded — the cell has free slots but this
	// tenant already owns the maximum allowed concurrent builds.
	// Callers must return HTTP 429 with ErrTenantBuildQuota.
	RejectTenantQuotaExceeded
)

// String reports a stable debug label for each decision. Useful in logs
// and metrics labels.
func (d Decision) String() string {
	switch d {
	case Admit:
		return "admit"
	case RejectCellAtCapacity:
		return "reject_cell_at_capacity"
	case RejectTenantQuotaExceeded:
		return "reject_tenant_quota_exceeded"
	default:
		return "unknown"
	}
}

// Result is the full answer from a Check call. Always non-nil.
type Result struct {
	Decision Decision
	// CellActiveBuilds is the count of non-terminal builds the gate saw
	// for this cell at check time. Useful for metrics + debug.
	CellActiveBuilds int
	// TenantActiveBuilds is the count of non-terminal builds the gate
	// saw for this tenant at check time.
	TenantActiveBuilds int
	// RetryAfter is a suggested backoff duration for 429 responses.
	// Computed based on how saturated the cell is — at exactly the cap
	// it suggests a short wait; well above the cap it suggests a longer
	// wait. Callers render this into the HTTP `Retry-After` header.
	RetryAfter time.Duration
	// Reason is a human-readable explanation suitable for the error
	// payload. Deliberately does not leak internal terms (Rule 3).
	Reason string
}

// ActiveBuildsSnapshot returns the point-in-time active-build picture the
// gate needs to make a decision. Typical implementation: call the build
// store's FindActiveBuilds(ctx, cellID) and bucket by tenant.
//
// The snapshot MUST be cell-scoped — the gate does not know about
// builds in other cells, and the per-cell budget would be meaningless
// if the count mixed cells.
type ActiveBuildsSnapshot struct {
	// Total is the cell-scoped count of builds in any non-terminal state
	// (QUEUED, RUNNING, RELEASING).
	Total int
	// ByTenant is a map from tenant_id to that tenant's non-terminal
	// build count within the same cell. Entries for tenants with zero
	// active builds may be absent.
	ByTenant map[string]int
}

// ActiveBuildsQuerier returns a snapshot the gate uses to check capacity.
// Implementations are typically thin wrappers over the build store.
type ActiveBuildsQuerier interface {
	ActiveBuilds(ctx context.Context, cellID string) (ActiveBuildsSnapshot, error)
}

// Config tunes the gate thresholds. Zero values are allowed and mean
// "no limit" which effectively disables the corresponding check; the
// gate only enforces positive caps.
type Config struct {
	// MaxConcurrentPerCell is the hard ceiling for non-terminal builds
	// within a single cell. Matches the `loop_runtime_build_queue_depth` red
	// line in cell-budget (20 by default).
	MaxConcurrentPerCell int
	// MaxConcurrentPerTenant is the per-tenant fairness cap within a
	// cell. Set from the same config.MaxConcurrentBuildsPerTenant as the
	// BuildReconcileLoop's per-tenant backpressure, so a rejection here
	// mirrors the backpressure the loop would apply anyway.
	MaxConcurrentPerTenant int
	// BaseRetryAfter is the minimum Retry-After hint returned on 429.
	// Default 5s.
	BaseRetryAfter time.Duration
	// MaxRetryAfter caps the Retry-After hint even when the cell is
	// severely saturated. Default 60s.
	MaxRetryAfter time.Duration
}

// DefaultConfig returns thresholds aligned with the roadmap 04 budget
// and the current BuildReconcileLoop defaults.
func DefaultConfig() Config {
	return Config{
		MaxConcurrentPerCell:   20,
		MaxConcurrentPerTenant: 3,
		BaseRetryAfter:         5 * time.Second,
		MaxRetryAfter:          60 * time.Second,
	}
}

// BuildGate is the admission gate instance. Construct one per API
// process via NewBuildGate and pass it to the build handler.
type BuildGate struct {
	querier ActiveBuildsQuerier
	cfg     Config
	enabled bool
}

// NewBuildGate builds a gate backed by the given querier. When enabled
// is false (the `LOOP_RUNTIME_ADMISSION_CONTROL` flag unset), every Check call
// returns Admit without touching the querier. This is the rollback
// path and also the current production default during the dev iteration.
func NewBuildGate(querier ActiveBuildsQuerier, cfg Config, enabled bool) *BuildGate {
	if cfg.BaseRetryAfter <= 0 {
		cfg.BaseRetryAfter = 5 * time.Second
	}
	if cfg.MaxRetryAfter <= 0 || cfg.MaxRetryAfter < cfg.BaseRetryAfter {
		cfg.MaxRetryAfter = 60 * time.Second
	}
	return &BuildGate{querier: querier, cfg: cfg, enabled: enabled}
}

// Check runs the admission logic for one incoming build submission.
//
// Never returns a nil Result — on error, the Result still carries a
// Decision=Admit so callers that treat errors as fatal do not double-
// penalize users by both 500-ing and rejecting. Errors are returned
// alongside for logging.
func (g *BuildGate) Check(ctx context.Context, cellID, tenantID string) (*Result, error) {
	if !g.enabled {
		return &Result{Decision: Admit, Reason: "admission_disabled"}, nil
	}

	snapshot, err := g.querier.ActiveBuilds(ctx, cellID)
	if err != nil {
		// Fail-open: we do not know the state, so we let the build
		// through and rely on downstream loops to cope. Log the error
		// in the caller.
		return &Result{Decision: Admit, Reason: "querier_error"},
			fmt.Errorf("admission: query active builds: %w", err)
	}

	// Cell-wide cap first — if the cell is full, tenant fairness
	// is moot because nobody can make progress.
	if g.cfg.MaxConcurrentPerCell > 0 && snapshot.Total >= g.cfg.MaxConcurrentPerCell {
		return &Result{
			Decision:           RejectCellAtCapacity,
			CellActiveBuilds:   snapshot.Total,
			TenantActiveBuilds: snapshot.ByTenant[tenantID],
			RetryAfter:         g.retryAfter(snapshot.Total, g.cfg.MaxConcurrentPerCell),
			Reason: fmt.Sprintf("cell has %d / %d concurrent builds",
				snapshot.Total, g.cfg.MaxConcurrentPerCell),
		}, nil
	}

	// Per-tenant cap — the cell has room, but this tenant does not.
	if g.cfg.MaxConcurrentPerTenant > 0 {
		tenantCount := snapshot.ByTenant[tenantID]
		if tenantCount >= g.cfg.MaxConcurrentPerTenant {
			return &Result{
				Decision:           RejectTenantQuotaExceeded,
				CellActiveBuilds:   snapshot.Total,
				TenantActiveBuilds: tenantCount,
				RetryAfter:         g.retryAfter(tenantCount, g.cfg.MaxConcurrentPerTenant),
				Reason: fmt.Sprintf("tenant has %d / %d concurrent builds",
					tenantCount, g.cfg.MaxConcurrentPerTenant),
			}, nil
		}
	}

	return &Result{
		Decision:           Admit,
		CellActiveBuilds:   snapshot.Total,
		TenantActiveBuilds: snapshot.ByTenant[tenantID],
		RetryAfter:         0,
		Reason:             "admit",
	}, nil
}

// retryAfter computes a simple linear backoff: at exactly the cap, return
// BaseRetryAfter; each extra build above the cap adds BaseRetryAfter up
// to MaxRetryAfter. This gives clients a useful gradient without
// requiring them to do their own backoff math.
func (g *BuildGate) retryAfter(current, cap int) time.Duration {
	if current < cap {
		return g.cfg.BaseRetryAfter
	}
	over := current - cap + 1
	d := time.Duration(over) * g.cfg.BaseRetryAfter
	if d > g.cfg.MaxRetryAfter {
		return g.cfg.MaxRetryAfter
	}
	return d
}

// ErrGateFailed is a sentinel returned when Check's querier path errors.
// Callers can errors.Is check it to decide whether to log or ignore.
var ErrGateFailed = errors.New("admission: gate query failed")
