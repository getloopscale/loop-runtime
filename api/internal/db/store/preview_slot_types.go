// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// preview_slot_types.go — public types for the preview_slot domain.
// Created 2026-06-07 by plan 2026-06-07-preview-as-category-single-
// slot-plan.md (D2 + D3). Follows the env_target sqlc-migration split
// pattern: types in <name>_types.go + impl in <name>_wrapper.go.
//
// preview_slot is the single-slot source-of-truth for preview
// deployments per tenant. PK=tenant_id enforces "one active preview
// per tenant" at the schema level. Substitution (D3) is atomic via
// ON CONFLICT (tenant_id) DO UPDATE.
//
// A NULL `active_pr_number` means the tenant has a slot row but no
// active preview PR — the dashboard renders "No active branch".

import (
	"context"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

// PreviewSlot represents the current preview deployment state for a
// tenant. When `ActivePRNumber` is nil, there is no active preview
// — the row exists only for audit and project_id continuity. The
// preview_slot_all_or_nothing CHECK constraint enforces that
// ActivePRNumber / ActiveBranch / URL are populated together or all
// NULL.
type PreviewSlot struct {
	TenantID        string    `json:"tenant_id"`
	ProjectID       string    `json:"project_id"`
	ActivePRNumber  *int32    `json:"active_pr_number,omitempty"`
	ActiveBranch    *string   `json:"active_branch,omitempty"`
	ActiveReleaseID *string   `json:"active_release_id,omitempty"`
	URL             *string   `json:"url,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
}

// PreviewSlotStore defines the interface for preview slot operations.
// All tx-accepting methods (Upsert, Clear, EnsureEmpty) run inside an
// existing transaction from withEnvironmentLock — they NEVER start
// their own.
//
// 2026-06-07 per-project model (migration 000028): the slot key is
// (tenant_id, project_id). Each project has its OWN slot. Get/Clear/
// EnsureEmpty require BOTH ids; ListByTenant returns all slots for a
// tenant so the dashboard renders N preview rows.
type PreviewSlotStore interface {
	// Get retrieves the preview slot for a specific (tenant, project).
	// Returns (nil, nil) when the project has no slot row (legacy
	// NoRows contract).
	Get(ctx context.Context, tenantID, projectID string) (*PreviewSlot, error)

	// ListByTenant returns all preview slots for a tenant — one row
	// per project that has had a preview at any time. Used by the
	// dashboard /environments grid to render N preview rows.
	ListByTenant(ctx context.Context, tenantID string) ([]PreviewSlot, error)

	// Upsert atomically inserts or replaces the slot row for a
	// (tenant, project). D3 substitute behavior PER PROJECT: ON
	// CONFLICT (tenant_id, project_id) DO UPDATE replaces only the
	// slot of the SAME project. PR in project B never touches
	// project A's slot.
	Upsert(ctx context.Context, tx db.DBTX, tenantID, projectID string, activePRNumber int32, activeBranch, activeReleaseID, url string) error

	// Clear NULLs the active_* fields of the (tenant, project) slot,
	// preserving the row for audit. Idempotent.
	Clear(ctx context.Context, tx db.DBTX, tenantID, projectID string) error

	// EnsureEmpty inserts an empty slot row if absent for the
	// (tenant, project), else clears active_* fields. Used by
	// project provisioning to guarantee every project has a
	// preview slot row (D1 consequence: preview category is always
	// present).
	EnsureEmpty(ctx context.Context, tx db.DBTX, tenantID, projectID string) error
}

// PostgresPreviewSlotStore implements PreviewSlotStore using
// PostgreSQL via sqlc. Same dual-field design as the env_target
// store: Exec for mutating methods (inside lock tx), Query for reads.
type PostgresPreviewSlotStore struct {
	Exec  db.DBTX
	Query db.DBTX
}

// NewPostgresPreviewSlotStore creates a new PostgresPreviewSlotStore.
// Both args MUST implement SqlcBridge; the prod call site passes the
// same *PgxAdapter for both.
func NewPostgresPreviewSlotStore(exec db.DBTX, query db.DBTX) *PostgresPreviewSlotStore {
	return &PostgresPreviewSlotStore{
		Exec:  exec,
		Query: query,
	}
}

// Compile-time assertion.
var _ PreviewSlotStore = (*PostgresPreviewSlotStore)(nil)
