// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

// audit_wrapper.go — sqlc-backed implementation of WriteAuditLog +
// PostgresAuditStore.ListAuditEntries.
//
// T1.5 of plan 2026-05-29-sqlc-migration-plan v1.3
// (Phase 1 pilot — SENSITIVE store; M-3 audit-callsite-diff +
// M-4 pair-review per MEET-2026-05-29-001).
//
// The 6 prod callsites that write or list audit entries (api/cmd/
// setup_server.go × 3, api/cmd/setup_orchestrator.go × 1,
// api/cmd/setup_auth.go × 1, api/internal/adapters/audit_adapter.go ×
// 1) continue to use the SAME public symbols (WriteAuditLog,
// AuditLogger, PostgresAuditStore) declared in audit_types.go — ONLY
// the implementation changes. The M-3 audit-callsite-diff passes
// structurally because no callsite shape changes:
//
//   - 2 sites `&db.PostgresAuditStore{DB: dbAdapter}` compile
//     unchanged — PostgresAuditStore struct has the same `DB`
//     field (widened to db.DBTX so the sqlc dispatch works); the
//     struct-literal compiles with the same syntax
//   - 1 site `&db.AuditLogger{DB: dbAdapter}` compile unchanged —
//     AuditLogger.DB widened similarly
//   - 3 sites `db.WriteAuditLog(ctx, dbAdapter, entry)` compile
//     unchanged — function signature widened from ExecQueryExecutor
//     to db.DBTX (dbAdapter satisfies both)
//
// Invariants preserved:
//   - Single INSERT shape (audit_log columns unchanged in
//     queries/audit.sql)
//   - Multi-tenant: tenant_id always present (tenantpredicate gate
//     enforces; audit_log is in TenantOwnedTables)
//   - achado #26 fix: COALESCE(correlation_id, '') preserved in
//     ListAuditLogForTenant so NULL correlation_id scans as empty
//     string into ports.AuditEntryResult.CorrelationID
//   - fail-loud row-scan: legacy `return err` instead of silent
//     `continue` preserved verbatim
//   - nil-pool fallback: PostgresAuditStore with nil DB returns
//     empty + zero, matching legacy `if s.DB == nil` guard

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"

	"github.com/getloopscale/loopzero/api/internal/db/dbconv"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/getloopscale/loopzero/api/internal/db/generated"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// ListAuditEntries returns paginated audit entries for a tenant with
// optional filters. Method on PostgresAuditStore (declared in
// audit_types.go) — implementation here uses sqlc-generated
// CountAuditLogForTenant + ListAuditLogForTenant.
//
// Nil-pool fallback (legacy: `if s.DB == nil { return nil, 0, nil }`)
// preserved.
func (s *PostgresAuditStore) ListAuditEntries(ctx context.Context, tenantID string, filter ports.AuditFilter) ([]ports.AuditEntryResult, int, error) {
	if s.DB == nil {
		return nil, 0, nil
	}

	gdb := db.Generated(s.DB)
	dbtx := gdb
	q := generated.New()

	// Nullable args via sqlc.narg form: nil/Valid=false → predicate
	// short-circuits to TRUE (no filter applied).
	actionArg := dbconv.StringEmptyToNilPtr(filter.Action)
	resourceTypeArg := dbconv.StringEmptyToNilPtr(filter.ResourceType)
	sinceArg := optionalTimestamptz(filter.Since)

	total, err := q.CountAuditLogForTenant(ctx, dbtx, generated.CountAuditLogForTenantParams{
		TenantID:     tenantID,
		Action:       actionArg,
		ResourceType: resourceTypeArg,
		Since:        sinceArg,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("audit: count: %w", err)
	}

	rows, err := q.ListAuditLogForTenant(ctx, dbtx, generated.ListAuditLogForTenantParams{
		TenantID:     tenantID,
		Action:       actionArg,
		ResourceType: resourceTypeArg,
		Since:        sinceArg,
		Lim:          int32(filter.Limit),
		Off:          int32(filter.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("audit: query: %w", err)
	}

	entries := make([]ports.AuditEntryResult, 0, len(rows))
	for _, r := range rows {
		e := ports.AuditEntryResult{
			Action:        r.Action,
			ResourceType:  r.ResourceType,
			ResourceID:    r.ResourceID,
			Actor:         r.Actor,
			CorrelationID: r.CorrelationID, // already COALESCE'd to '' in SQL
		}
		if r.CreatedAt.Valid {
			e.CreatedAt = r.CreatedAt.Time
		}
		if len(r.Metadata) > 0 {
			// Metadata is written as a JSON object. One that is not is a
			// corrupt audit record, reported with the row it belongs to.
			if err := json.Unmarshal(r.Metadata, &e.Metadata); err != nil {
				return nil, 0, fmt.Errorf("audit: %s on %s/%s: metadata is not a JSON object: %w",
					r.Action, r.ResourceType, r.ResourceID, err)
			}
		}
		entries = append(entries, e)
	}

	return entries, int(total), nil
}

// WriteAuditLog inserts an audit entry into the audit_log table.
// Application-level audit trail (MEET-2026-03-31-024 A.13). Database-
// level triggers provide defense-in-depth (migration 000009).
//
// Signature: takes db.DBTX so the body can dispatch through
// SqlcBridge to the sqlc-generated InsertAuditLog. All 6 prod call
// sites pass *PgxAdapter (which implements both ExecQueryExecutor
// AND db.DBTX AND SqlcBridge), so the signature widening from the
// legacy ExecQueryExecutor → DBTX is invisible at the seam.
//
// Per M-3 (audit-callsite-diff) of MEET-2026-05-29-001, the SQL
// shape (table columns, parameter order, INSERT semantic) is
// identical to the legacy raw INSERT — verified by sqlc-emitting
// the same VALUES tuple from queries/audit.sql InsertAuditLog.
func WriteAuditLog(ctx context.Context, dbtx db.DBTX, entry AuditEntry) error {
	var metadataJSON []byte
	if entry.Metadata != nil {
		var err error
		metadataJSON, err = json.Marshal(entry.Metadata)
		if err != nil {
			return fmt.Errorf("audit: marshal metadata: %w", err)
		}
	}

	gdb := db.Generated(dbtx)

	q := generated.New()
	if err := q.InsertAuditLog(ctx, gdb, generated.InsertAuditLogParams{
		TenantID:      entry.TenantID,
		Action:        entry.Action,
		Actor:         entry.Actor,
		ResourceType:  entry.ResourceType,
		ResourceID:    entry.ResourceID,
		CorrelationID: dbconv.StringEmptyToNilPtr(entry.CorrelationID),
		Metadata:      metadataJSON,
	}); err != nil {
		return fmt.Errorf("audit: insert: %w", err)
	}
	return nil
}

// =========================================================================
// Conversion helpers (audit-specific; shared with other wrappers via
// release_wrapper.go for stringEmptyToNilPtr + timePtrToTimestamptz)
// =========================================================================

// optionalTimestamptz maps a zero time.Time to Valid=false (NULL
// semantics for the nullable sqlc.narg('since')::timestamptz predicate).
func optionalTimestamptz(t time.Time) pgtype.Timestamptz {
	if t.IsZero() {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: t, Valid: true}
}
