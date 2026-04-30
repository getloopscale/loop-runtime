// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/loopzero/loopzero/api/internal/domain"
)

// PostgresAuditStore implements domain.AuditStore using PostgreSQL.
type PostgresAuditStore struct {
	DB DBTX
}

// ListAuditEntries returns paginated audit entries for a tenant with optional filters.
func (s *PostgresAuditStore) ListAuditEntries(ctx context.Context, tenantID string, filter domain.AuditFilter) ([]domain.AuditEntryResult, int, error) {
	if s.DB == nil {
		return nil, 0, nil
	}

	// Build query dynamically based on filters.
	// COALESCE on correlation_id: column is nullable; AuditEntryResult.CorrelationID
	// is plain string (not *string). Without COALESCE, scanning NULL fails and the
	// per-row `continue` silently skips every entry — list returned `[]` with
	// total>0. Achado #26 (CHG-2026-04-29-DOGFOOD iter 13).
	query := `SELECT action, resource_type, resource_id, actor, COALESCE(correlation_id, ''), metadata, created_at
	          FROM audit_log WHERE tenant_id = $1`
	countQuery := `SELECT COUNT(*) FROM audit_log WHERE tenant_id = $1`
	args := []any{tenantID}
	argIdx := 2

	if filter.Action != "" {
		query += fmt.Sprintf(" AND action = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND action = $%d", argIdx)
		args = append(args, filter.Action)
		argIdx++
	}
	if filter.ResourceType != "" {
		query += fmt.Sprintf(" AND resource_type = $%d", argIdx)
		countQuery += fmt.Sprintf(" AND resource_type = $%d", argIdx)
		args = append(args, filter.ResourceType)
		argIdx++
	}
	if !filter.Since.IsZero() {
		query += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		countQuery += fmt.Sprintf(" AND created_at >= $%d", argIdx)
		args = append(args, filter.Since)
		argIdx++
	}

	// Count total
	var total int
	if err := s.DB.QueryRow(ctx, countQuery, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("audit: count: %w", err)
	}

	// Fetch entries
	query += " ORDER BY created_at DESC"
	query += fmt.Sprintf(" LIMIT $%d OFFSET $%d", argIdx, argIdx+1)
	fetchArgs := append(args, filter.Limit, filter.Offset)

	rows, err := s.DB.Query(ctx, query, fetchArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("audit: query: %w", err)
	}
	defer rows.Close()

	var entries []domain.AuditEntryResult
	for rows.Next() {
		var e domain.AuditEntryResult
		var metadataJSON []byte
		var createdAt time.Time
		if err := rows.Scan(&e.Action, &e.ResourceType, &e.ResourceID, &e.Actor, &e.CorrelationID, &metadataJSON, &createdAt); err != nil {
			// Fail-loud — silent `continue` previously masked the NULL-scan bug
			// (Achado #26): every row failed and the list endpoint returned [].
			return nil, 0, fmt.Errorf("audit: scan row: %w", err)
		}
		e.CreatedAt = createdAt
		if len(metadataJSON) > 0 {
			_ = json.Unmarshal(metadataJSON, &e.Metadata)
		}
		entries = append(entries, e)
	}

	if entries == nil {
		entries = []domain.AuditEntryResult{}
	}
	return entries, total, nil
}
