// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// PostgresTenantDeletionStore implements domain.TenantDeletionStore using PostgreSQL.
// MEET-052: Manages tenants_pending_deletion and tenant_tombstones tables.
type PostgresTenantDeletionStore struct {
	DB DBTX
}

// RequestDeletion inserts a pending deletion record. Idempotent: if the tenant
// already has a pending deletion, returns the existing record without error.
func (s *PostgresTenantDeletionStore) RequestDeletion(ctx context.Context, input domain.DeletionRequest) (*domain.DeletionRecord, error) {
	_, err := s.DB.Exec(ctx,
		`INSERT INTO tenants_pending_deletion (tenant_id, requested_by, reason, grace_until, force)
		 VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (tenant_id) DO NOTHING`,
		input.TenantID, input.RequestedBy, input.Reason, input.GraceUntil, input.Force,
	)
	if err != nil {
		return nil, fmt.Errorf("tenant deletion store: insert pending: %w", err)
	}

	return s.GetPendingDeletion(ctx, input.TenantID)
}

// CancelDeletion removes the pending deletion record for a tenant.
// Idempotent: returns nil if no pending deletion exists.
func (s *PostgresTenantDeletionStore) CancelDeletion(ctx context.Context, tenantID string) error {
	_, err := s.DB.Exec(ctx,
		`DELETE FROM tenants_pending_deletion WHERE tenant_id = $1`,
		tenantID,
	)
	if err != nil {
		return fmt.Errorf("tenant deletion store: cancel: %w", err)
	}
	return nil
}

// GetPendingDeletion returns the pending deletion record for a tenant, or nil
// if no pending deletion exists.
func (s *PostgresTenantDeletionStore) GetPendingDeletion(ctx context.Context, tenantID string) (*domain.DeletionRecord, error) {
	rows, err := s.DB.Query(ctx,
		`SELECT tenant_id, requested_at, requested_by, reason, grace_until, force, attempt_count
		 FROM tenants_pending_deletion
		 WHERE tenant_id = $1`,
		tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("tenant deletion store: get pending: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return nil, fmt.Errorf("tenant deletion store: get pending rows error: %w", err)
		}
		return nil, nil
	}

	var record domain.DeletionRecord
	if err := rows.Scan(
		&record.TenantID,
		&record.RequestedAt,
		&record.RequestedBy,
		&record.Reason,
		&record.GraceUntil,
		&record.Force,
		&record.AttemptCount,
	); err != nil {
		return nil, fmt.Errorf("tenant deletion store: get pending scan: %w", err)
	}

	return &record, nil
}

// WriteTombstone writes a permanent audit record after tenant deletion completes.
func (s *PostgresTenantDeletionStore) WriteTombstone(ctx context.Context, tombstone domain.TombstoneRecord) error {
	cleanedJSON, err := json.Marshal(tombstone.ResourcesCleaned)
	if err != nil {
		cleanedJSON = []byte("{}")
	}
	var failedJSON []byte
	if tombstone.ResourcesFailed != nil {
		failedJSON, err = json.Marshal(tombstone.ResourcesFailed)
		if err != nil {
			failedJSON = nil
		}
	}

	_, err = s.DB.Exec(ctx,
		`INSERT INTO tenant_tombstones (tenant_id, tenant_slug, requested_by, reason, resources_cleaned, resources_failed)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (tenant_id) DO NOTHING`,
		tombstone.TenantID, tombstone.TenantSlug, tombstone.RequestedBy, tombstone.Reason,
		cleanedJSON, failedJSON,
	)
	if err != nil {
		return fmt.Errorf("tenant deletion store: write tombstone: %w", err)
	}

	// Remove from pending deletion queue
	_, err = s.DB.Exec(ctx,
		`DELETE FROM tenants_pending_deletion WHERE tenant_id = $1`,
		tombstone.TenantID,
	)
	if err != nil {
		return fmt.Errorf("tenant deletion store: cleanup pending after tombstone: %w", err)
	}

	return nil
}
