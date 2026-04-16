// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package db

import (
	"context"
	"encoding/json"
	"fmt"
)

// AuditEntry represents a single audit log entry.
type AuditEntry struct {
	TenantID      string
	Action        string
	Actor         string
	ResourceType  string
	ResourceID    string
	CorrelationID string
	Metadata      map[string]any
}

// WriteAuditLog inserts an audit entry into the audit_log table.
// This is the application-level audit trail (MEET-2026-03-31-024 A.13).
// Database-level triggers provide defense-in-depth (migration 000009).
func WriteAuditLog(ctx context.Context, db ExecQueryExecutor, entry AuditEntry) error {
	var metadataJSON []byte
	if entry.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("audit: marshal metadata: %w", err)
		}
	}

	_, err := db.Exec(ctx,
		`INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, correlation_id, metadata)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		entry.TenantID, entry.Action, entry.Actor, entry.ResourceType,
		entry.ResourceID, entry.CorrelationID, metadataJSON,
	)
	if err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}
