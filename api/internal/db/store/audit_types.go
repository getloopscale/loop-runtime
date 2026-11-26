// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

// audit_types.go — public types for the audit_log domain that survive
// the T1.5 deletion of the legacy hand-rolled `audit.go` +
// `audit_store.go`. Per plan
// 2026-05-29-sqlc-migration-plan v1.3 § T1.5 (sensitive
// store — M-3 audit-callsite-diff + M-4 pair-review), this file carries:
//
//   - AuditEntry struct (row-shape consumed by WriteAuditLog + the 6
//     prod callsites that emit audit entries)
//   - AuditLogger struct (adapter wrapping WriteAuditLog so callers
//     can depend on domain.AuditLogger interface, DIP)
//   - PostgresAuditStore type alias (kept for backward-compat at the
//     2 struct-literal call sites in api/cmd/setup_server.go; the
//     alias points at AuditStoreSqlc so the SQL implementation is
//     sqlc-backed while the public symbol shape is unchanged)
//
// The sqlc-backed implementation (AuditStoreSqlc) + the migrated
// package-level WriteAuditLog live in audit_wrapper.go.

import (
	"context"
	"fmt"

	"github.com/getloopscale/loop-runtime/api/internal/db"
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

// AuditLogger is an adapter wrapping WriteAuditLog so callers can
// depend on the domain.AuditLogger interface (DIP). Used by services
// that need to emit audit entries (T1.2 cosign sign audit, secrets
// handler, etc).
//
// T1.5 (plan 2026-05-29-sqlc-migration v1.3): field type widened from
// ExecQueryExecutor to DBTX so the body can dispatch through the
// sqlc-generated InsertAuditLog. The 1 prod call site
// (setup_orchestrator.go) passes *PgxAdapter which satisfies both.
type AuditLogger struct {
	DB db.DBTX
}

// LogAudit implements domain.AuditLogger.
//
// Single-source-of-truth wrapper — delegates to WriteAuditLog so the
// SQL remains in one place. Callers in `services/` and `routes/` use
// this adapter instead of importing WriteAuditLog directly, keeping
// the dependency direction clean (services → domain.AuditLogger ←
// adapter).
func (a *AuditLogger) LogAudit(ctx context.Context, tenantID, action, actor, resourceType, resourceID, correlationID string, metadata map[string]any) error {
	if a == nil || a.DB == nil {
		// Nothing can be written without a database. Reporting it lets the
		// caller log a lost audit row instead of believing it was recorded.
		return fmt.Errorf("audit: no database wired, %s on %s/%s by %s was not recorded",
			action, resourceType, resourceID, actor)
	}
	return WriteAuditLog(ctx, a.DB, AuditEntry{
		TenantID:      tenantID,
		Action:        action,
		Actor:         actor,
		ResourceType:  resourceType,
		ResourceID:    resourceID,
		CorrelationID: correlationID,
		Metadata:      metadata,
	})
}

// PostgresAuditStore is a type alias preserving the legacy public
// symbol shape at the 2 struct-literal call sites in
// api/cmd/setup_server.go (`&db.PostgresAuditStore{DB: dbAdapter}`).
// The implementation is the sqlc-backed AuditStoreSqlc — only the
// name + field layout matches the legacy struct.
//
// T1.5 (plan 2026-05-29-sqlc-migration v1.3): kept as a struct (not
// an alias) because the struct-literal `{DB: dbAdapter}` syntax at
// the call sites depends on the DB field name. The AuditStoreSqlc
// internals are reused via a sqlc-backed ListAuditEntries that the
// legacy method shape forwards to.
type PostgresAuditStore struct {
	DB db.DBTX
}
