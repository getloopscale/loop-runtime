// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
)

func TestAuditStore_ListAuditEntries_NilDB(t *testing.T) {
	store := &PostgresAuditStore{DB: nil}

	entries, total, err := store.ListAuditEntries(context.Background(), "t1", ports.AuditFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 total, got: %d", total)
	}
	if entries != nil {
		t.Error("expected nil entries for nil DB")
	}
}

// An audit logger with no database cannot record the action it is handed. That
// is a lost audit row, and the caller must learn about it rather than proceed
// as if the row was written.
func TestAuditLogger_WithoutADatabaseReportsTheLostWrite(t *testing.T) {
	var unwired *AuditLogger
	if err := unwired.LogAudit(context.Background(), "t1", "SECRET_CREATED", "u1", "secret", "DB_URL", "", nil); err == nil {
		t.Fatal("a nil logger must report that nothing was written")
	}
	if err := (&AuditLogger{}).LogAudit(context.Background(), "t1", "SECRET_CREATED", "u1", "secret", "DB_URL", "", nil); err == nil {
		t.Fatal("a logger with no database must report that nothing was written")
	}
}

// Audit metadata is written as a JSON object. A row whose metadata is not an
// object is a corrupt audit record, and listing it must say so instead of
// returning the entry with its metadata silently emptied.
func TestAuditStore_ListAuditEntries_ReportsMetadataThatIsNotAnObject(t *testing.T) {
	ctx, pool := meteringPool(t)
	const tenantID = "tn-audit-meta"
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM audit_log WHERE tenant_id=$1`, tenantID)
	}
	cleanup()
	t.Cleanup(cleanup)
	if _, err := pool.Exec(ctx, `INSERT INTO audit_log (tenant_id, action, actor, resource_type, resource_id, metadata)
	                             VALUES ($1,'SECRET_CREATED','u1','secret','DB_URL','[1,2]'::jsonb)`, tenantID); err != nil {
		t.Fatalf("seed audit row: %v", err)
	}

	s := &PostgresAuditStore{DB: &db.PgxAdapter{Pool: pool}}
	if _, _, err := s.ListAuditEntries(ctx, tenantID, ports.AuditFilter{Limit: 10}); err == nil {
		t.Fatal("metadata that is not a JSON object must be reported")
	}
}
