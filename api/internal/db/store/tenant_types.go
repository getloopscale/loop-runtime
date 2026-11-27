// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// tenant_types.go — public types for the tenant lifecycle domain that
// survive the T1.4 deletion of the legacy hand-rolled `tenant_store.go`
// + `tenant_deletion_store.go`. Per plan
// 2026-05-29-sqlc-migration-plan v1.3 § T1.4, this file
// carries:
//
//   - TenantDeletionCounts struct (per-table tally returned by the
//     admin bulk-eviction cascade)
//   - AdminTenantStore interface (minimal contract consumed by
//     api/internal/placement/placementadapter/tenant_adapter.go — widened from concrete
//     in T1.4 so the sqlc-backed wrapper drops in without churn)
//
// The sqlc-backed implementations live in tenant_wrapper.go:
//   - TenantStoreSqlc      implements AdminTenantStore 1:1
//   - TenantDeletionStoreSqlc implements domain.TenantDeletionStore 1:1
//     (the lifecycle interface lives in api/internal/domain/types.go;
//     no separate db.* interface required)

import "context"

// TenantDeletionCounts reports how many rows were removed per table
// during a single call to DeleteTenantsFKOrdered. Exposed for auditing
// and for the AdminEvictHandler response.
type TenantDeletionCounts struct {
	EnvironmentTargets int64 `json:"environment_targets"`
	Releases           int64 `json:"releases"`
	BuildApps          int64 `json:"build_apps"`
	Builds             int64 `json:"builds"`
	Projects           int64 `json:"projects"`
	Subscriptions      int64 `json:"subscriptions"`
	TenantMembers      int64 `json:"tenant_members"`
	Tenants            int64 `json:"tenants"`
	MeteringCursors    int64 `json:"metering_cursors"` // SC-5 (ADR-13) — compute-metering cursor GC
}

// AdminTenantStore is the minimal contract consumed by the admin
// eviction adapter (placementadapter.TenantDBDeleterAdapter) +
// the LOOP_RUNTIME_ADMIN_API handler. Queries here intentionally bypass tenant
// scoping — the caller is authenticated as an operator and needs to
// read/write across tenants.
//
// Implemented by *TenantStoreSqlc (tenant_wrapper.go) as of T1.4 of
// plan 2026-05-29-sqlc-migration v1.3. Previously implemented by
// *PostgresTenantStore in tenant_store.go — that file was deleted in
// T1.4 after the 2 prod call sites (api/cmd/adapters.go +
// api/internal/adapters/tenant_adapter.go) switched to
// NewTenantStoreSqlc.
// Co-located with the adapter in db/store/ per ADR 2026-07-03-m9-db-store-
// subpackage-topology D2 (negligible external port footprint; not a smell).
type AdminTenantStore interface {
	// ListTenantIDsByPattern returns tenant IDs matching a SQL LIKE
	// pattern (ESCAPE '\' applied; ORDER BY id ASC; LIMIT 10_000).
	// Caller validates the pattern is safe BEFORE invoking.
	ListTenantIDsByPattern(ctx context.Context, pattern string) ([]string, error)
	// DeleteTenantsFKOrdered runs the 8-step FK-safe cascade for the
	// given tenant IDs (environment_targets → releases → build_apps →
	// builds → projects → subscriptions → tenant_members → tenants).
	// No-op for empty input.
	DeleteTenantsFKOrdered(ctx context.Context, tenantIDs []string) (TenantDeletionCounts, error)
}
