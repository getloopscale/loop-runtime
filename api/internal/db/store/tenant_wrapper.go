// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// tenant_wrapper.go — thin Go wrappers around sqlc-generated Queries
// for the tenant lifecycle domain. Covers BOTH legacy stores:
//
//   - TenantStoreSqlc replaces PostgresTenantStore (admin bulk
//     operations: ListTenantIDsByPattern + DeleteTenantsFKOrdered 8-step
//     cascade)
//   - TenantDeletionStoreSqlc replaces PostgresTenantDeletionStore
//     (soft-delete lifecycle per MEET-052: RequestDeletion +
//     CancelDeletion + GetPendingDeletion + WriteTombstone)
//
// T1.4 of plan 2026-05-29-sqlc-migration-plan v1.3
// (Phase 1 pilot — multi-tenant SoT + admin bulk + soft-delete
// lifecycle).
//
// Bridge pattern same as T1.1-T1.3: db.DBTX → generated.DBTX via
// SqlcBridge. Admin queries are tenantpredicate:exempt by design
// (caller authenticated via LOOPRUNTIME_ADMIN_API gate; cross-tenant intent).
// Lifecycle queries are tenant-keyed (tenants_pending_deletion +
// tenant_tombstones tables added to TenantOwnedTables allowlist in
// T1.4).

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/getloopscale/loop-runtime/api/internal/db"

	"github.com/getloopscale/loop-runtime/api/internal/db/generated"
)

// =========================================================================
// TenantStoreSqlc — replaces legacy PostgresTenantStore (admin path)
// =========================================================================

// TenantStoreSqlc is the sqlc-backed admin tenant store.
type TenantStoreSqlc struct {
	q    *generated.Queries
	pool db.DBTX
}

// NewTenantStoreSqlc constructs a sqlc-backed admin tenant store.
// `pool` MUST implement SqlcBridge (PgxAdapter does); test fakes that
// don't fail each query with db.ErrNotSqlcBridge.
func NewTenantStoreSqlc(pool db.DBTX) *TenantStoreSqlc {
	return &TenantStoreSqlc{
		q:    generated.New(),
		pool: pool,
	}
}

func (s *TenantStoreSqlc) gen(tx db.DBTX) generated.DBTX {
	target := tx
	if target == nil {
		target = s.pool
	}
	return db.Generated(target)
}

// ListTenantIDsByPattern returns tenant IDs matching the LIKE pattern.
// Caller validates the pattern is safe BEFORE invoking — this method
// trusts the handler layer (see admin_evict.go validation). Results
// sorted ASC; LIMITED to 10_000 rows (pathological pattern guard).
func (s *TenantStoreSqlc) ListTenantIDsByPattern(ctx context.Context, pattern string) ([]string, error) {
	ids, err := s.q.ListTenantIDsByPattern(ctx, s.gen(nil), pattern)
	if err != nil {
		return nil, fmt.Errorf("tenant store: list by pattern: %w", err)
	}
	// Match legacy contract: always non-nil slice (callers iterate
	// without nil-checks).
	if ids == nil {
		return []string{}, nil
	}
	return ids, nil
}

// DeleteTenantsFKOrdered runs the 8-step FK-safe cascade for the given
// tenant IDs, plus a 9th SC-5 (ADR-13) step that GCs the compute-metering
// cursors (no FK to tenants; order-free). The whole cascade runs in ONE
// transaction the store opens on its pool and commits once: either every row
// for every tenant in the list is gone, or none of them are. A failure at
// step k rolls back steps 1..k-1 and returns zero counts. No-op (zero
// counts) for empty input.
//
// FK order (validated by tools/admin-evict-tenants.sh step 5):
//
//	environment_targets → releases → build_apps → builds → projects →
//	subscriptions → tenant_members → tenants
//
// Each child table is purged before its parent. Releases/build_apps
// reference builds (not tenants directly), so they use a sub-SELECT.
// Audit log rows are retained intentionally: eviction is itself
// auditable.
func (s *TenantStoreSqlc) DeleteTenantsFKOrdered(ctx context.Context, tenantIDs []string) (TenantDeletionCounts, error) {
	var counts TenantDeletionCounts
	if len(tenantIDs) == 0 {
		return counts, nil
	}

	adapter, ok := s.pool.(*db.PgxAdapter)
	if !ok || adapter.Pool == nil {
		return counts, fmt.Errorf("tenant store: eviction needs a pgx pool to run atomically, got DBTX %T", s.pool)
	}
	pgtx, err := adapter.Pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return counts, fmt.Errorf("tenant store: begin eviction tx: %w", err)
	}
	defer func() { _ = pgtx.Rollback(ctx) }() // no-op after a successful Commit
	dbtx := s.gen(&db.PgxTxAdapter{Tx: pgtx})

	type step struct {
		name  string
		fn    func() (int64, error)
		tally *int64
	}
	steps := []step{
		{"environment_targets", func() (int64, error) { return s.q.AdminEvictEnvironmentTargets(ctx, dbtx, tenantIDs) }, &counts.EnvironmentTargets},
		{"releases", func() (int64, error) { return s.q.AdminEvictReleases(ctx, dbtx, tenantIDs) }, &counts.Releases},
		{"build_apps", func() (int64, error) { return s.q.AdminEvictBuildApps(ctx, dbtx, tenantIDs) }, &counts.BuildApps},
		{"builds", func() (int64, error) { return s.q.AdminEvictBuilds(ctx, dbtx, tenantIDs) }, &counts.Builds},
		{"projects", func() (int64, error) { return s.q.AdminEvictProjects(ctx, dbtx, tenantIDs) }, &counts.Projects},
		{"subscriptions", func() (int64, error) { return s.q.AdminEvictSubscriptions(ctx, dbtx, tenantIDs) }, &counts.Subscriptions},
		{"tenant_members", func() (int64, error) { return s.q.AdminEvictTenantMembers(ctx, dbtx, tenantIDs) }, &counts.TenantMembers},
		{"tenants", func() (int64, error) { return s.q.AdminEvictTenants(ctx, dbtx, tenantIDs) }, &counts.Tenants},
		// SC-5 (ADR-13) — GC the compute-metering cursors so a reused tenant_id
		// cannot bill a stale window. No FK to tenants, so order is free.
		{"metering_cursor", func() (int64, error) { return s.q.AdminEvictMeteringCursors(ctx, dbtx, tenantIDs) }, &counts.MeteringCursors},
	}

	for i, st := range steps {
		rows, err := st.fn()
		if err != nil {
			return TenantDeletionCounts{}, fmt.Errorf("tenant store: step %d (%s), eviction rolled back: %w", i+1, st.name, err)
		}
		*st.tally = rows
	}
	if err := pgtx.Commit(ctx); err != nil {
		return TenantDeletionCounts{}, fmt.Errorf("tenant store: commit eviction: %w", err)
	}
	return counts, nil
}

// =========================================================================
// TenantDeletionStoreSqlc — replaces legacy PostgresTenantDeletionStore
// (MEET-052 soft-delete lifecycle)
// =========================================================================

// TenantDeletionStoreSqlc is the sqlc-backed soft-delete lifecycle
// store.

// lifecycle store. `pool` MUST implement SqlcBridge.

// RequestDeletion inserts a pending deletion record. Idempotent: if
// the tenant already has a pending deletion, returns the EXISTING
// record without error (matches legacy behavior — ON CONFLICT DO
// NOTHING preserves the original requested_at/requested_by).

// CancelDeletion removes the pending deletion record for a tenant.
// Idempotent: returns nil if no pending deletion exists.

// GetPendingDeletion returns the pending deletion record for a
// tenant, or nil if no pending deletion exists.

// WriteTombstone writes the permanent audit record AFTER tenant
// deletion completes, and removes the pending deletion entry. The two
// statements ideally run in a single transaction (caller's
// responsibility — this store keeps the legacy 2-statement contract;
// in practice both are idempotent so a partial failure is recoverable
// on retry).

// NOTE: conversion helpers (timePtrToTimestamptz, etc.) live in
// release_wrapper.go and are shared across all sqlc-backed stores in
// this package.
