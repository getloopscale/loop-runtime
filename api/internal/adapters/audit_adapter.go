// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package adapters

import (
	"context"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/routes"
)

// AuditAdapter implements routes.AuditLogger backed by PostgreSQL.
// MEET-2026-03-31-024: Application-level audit for sensitive operations.
type AuditAdapter struct {
	DB db.ExecQueryExecutor
}

// Compile-time check.
var _ routes.AuditLogger = (*AuditAdapter)(nil)

// LogAudit writes an audit entry to the audit_log table.
func (a *AuditAdapter) LogAudit(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error {
	return db.WriteAuditLog(ctx, a.DB, db.AuditEntry{
		TenantID:      tenantID,
		Action:        action,
		Actor:         actor,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		CorrelationID: correlationID,
		Metadata:      metadata,
	})
}
