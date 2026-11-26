// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/metrics"
	"github.com/getloopscale/loop-runtime/pkg/domain"
)

// reconcile checks all non-terminal builds and reconciles their state
// against the Argo Workflow CRDs in the build cluster.
//
// BLOCKER-3 fix: Builds are processed concurrently using a semaphore pattern
// (bounded to MaxConcurrent). Previously, serial iteration meant build #20
// could wait ~40 minutes while earlier builds blocked on ArgoCD WaitForSync.
//
// REVIEW-MOD-2: Fire-and-forget — reconcile dispatches goroutines and returns
// immediately. The next ticker tick can dispatch new builds even if previous
// ones are still waiting for ArgoCD sync. The semaphore (r.sem) bounds total
// concurrent processing across ticks. The r.active map prevents duplicate
// processing of the same build across ticks.
//
// MOD-6 fix: Each build is tracked in r.active while being processed.
// BuildReaper checks this to avoid competing on the same build.
func (r *BuildReconcileLoop) reconcile(ctx context.Context) {
	// MEET-053 A-07: Iterate over all known cells when CellLister is set.
	// This ensures builds on non-default cells are processed.
	cellIDs := []string{r.cfg.CellID}
	if r.cellLister != nil {
		if ids, err := r.cellLister.ListCellIDs(ctx); err == nil && len(ids) > 0 {
			cellIDs = ids
		} else if err != nil {
			r.logger.Warn("reconcile loop: cell lister failed, using configured cell",
				"error", err, "fallback_cell", r.cfg.CellID)
		}
	}

	var builds []db.Build
	for _, cellID := range cellIDs {
		cid := cellID
		if cid == "" {
			cid = domain.DefaultCellID
		}
		cellBuilds, err := r.buildStore.FindActiveBuilds(ctx, cid)
		if err != nil {
			r.logger.Error("reconcile loop: failed to find active builds",
				"error", err, "cell_id", cid)
			continue
		}
		builds = append(builds, cellBuilds...)
	}
	if len(builds) == 0 {
		return // avoid nil dereference below when builds is empty
	}

	// MEET-2026-03-31-023 T1-8: Record successful tick timestamp.
	metrics.ReconcileLoopLastSuccess.WithLabelValues("build").SetToCurrentTime()

	// Count active builds per tenant for backpressure.
	tenantActive := make(map[string]int)
	r.active.Range(func(_, value interface{}) bool {
		if tid, ok := value.(string); ok {
			tenantActive[tid]++
		}
		return true
	})

	for _, build := range builds {
		// Skip builds already being processed (MOD-6: avoid reaper competition)
		if _, loaded := r.active.LoadOrStore(build.ID, build.TenantID); loaded {
			continue
		}

		// Phase 0.2 (scale-50k-plan): cross-replica dedup via PG claims.
		// The sync.Map check above is the fast path (in-process). The PG
		// claim adds the cross-replica guarantee: if another controller
		// replica claimed this build, TryClaim returns false and we skip it.
		if r.pgDedup != nil {
			claimed, err := r.pgDedup.TryClaim(ctx, "builds", build.ID)
			if err != nil {
				r.logger.Warn("reconcile loop: build claim failed; retrying next tick",
					"build_id", build.ID, "error", err)
			}
			if !claimed {
				r.active.Delete(build.ID) // undo the local LoadOrStore
				continue
			}
		}

		// Per-tenant backpressure: skip if this tenant has too many active builds.
		if tenantActive[build.TenantID] >= r.cfg.MaxBuildsPerTenant {
			r.active.Delete(build.ID)
			metrics.BuildTenantBackpressure.WithLabelValues(build.TenantID).Inc()
			r.logger.Debug("reconcile loop: tenant at build limit, deferring",
				"build_id", build.ID, "tenant", build.TenantID,
				"active", tenantActive[build.TenantID], "limit", r.cfg.MaxBuildsPerTenant)
			continue
		}
		tenantActive[build.TenantID]++

		b := build // capture loop variable

		// Try to acquire semaphore slot without blocking the dispatch loop.
		// If all slots are full, skip this build — it will be retried next tick.
		select {
		case r.sem <- struct{}{}:
			go func() {
				defer func() { <-r.sem }()
				defer r.active.Delete(b.ID)
				defer func() {
					if r.pgDedup != nil {
						r.pgDedup.Release(ctx, "builds", b.ID)
					}
				}()
				// BRC-004 fix: recover from panics in reconcileBuild. The call chain
				// reaches deep into BuildOrchestrator → DeliveryActivator → ActivationKernel.Activate.
				// State machine panics on invalid transitions (by design). Without recovery,
				// a single corrupted build record crashes the entire API process.
				defer func() {
					if p := recover(); p != nil {
						r.logger.Error("reconcile loop: panic in reconcileBuild — recovered",
							"build_id", b.ID, "panic", fmt.Sprintf("%v", p),
							"stack", string(debug.Stack()))
					}
				}()
				r.reconcileBuild(ctx, &b)
			}()
		default:
			// All semaphore slots occupied — release active tracking so the
			// build can be retried on the next tick.
			r.active.Delete(b.ID)
			r.logger.Debug("reconcile loop: all slots busy, deferring build",
				"build_id", b.ID, "max_concurrent", r.cfg.MaxConcurrent)
		}
	}
}
