// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenantpredicate

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRaw_BlocksSensitiveTables — fixture: a raw query at
// api/internal/db/raw/X.go that references the `secrets` table.
// Expected: gate exit 1 with reason "raw-sensitive-table".
//
// T4.1 of plan 2026-05-29-sqlc-migration-plan.md v1.3.
func TestRaw_BlocksSensitiveTables(t *testing.T) {
	repo := t.TempDir()
	rawDir := filepath.Join(repo, "api", "internal", "db", "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Fixture: raw query touching the secrets table (forbidden).
	fixture := `package raw

import "context"

func DeleteSecret(ctx context.Context, dbtx DBTX, name string) (int64, error) {
	res, err := dbtx.Exec(ctx, ` + "`" + `DELETE FROM secrets WHERE name = $1` + "`" + `, name)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}
`
	if err := os.WriteFile(filepath.Join(rawDir, "delete_secret.go"), []byte(fixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	outcome, err := Check(Config{RepoRoot: repo})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected FAIL for raw query on secrets table, got PASS (scanned=%d)", outcome.Scanned)
	}
	if len(outcome.Violations) == 0 {
		t.Fatal("expected at least 1 violation")
	}
	found := false
	for _, v := range outcome.Violations {
		if v.Reason == "raw-sensitive-table" && v.Table == "secrets" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected raw-sensitive-table violation for secrets table, got: %+v", outcome.Violations)
	}
}

// TestRaw_AllowsNonSensitiveTables — fixture: a raw query at
// api/internal/db/raw/X.go that references the `builds` table.
// Expected: gate PASS (builds is not on sensitive list).
//
// T4.1 of plan 2026-05-29-sqlc-migration-plan.md v1.3.
func TestRaw_AllowsNonSensitiveTables(t *testing.T) {
	repo := t.TempDir()
	rawDir := filepath.Join(repo, "api", "internal", "db", "raw")
	if err := os.MkdirAll(rawDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	// Fixture: raw query touching builds (allowed).
	fixture := `package raw

import "context"

func DeleteOldBuilds(ctx context.Context, dbtx DBTX) (int64, error) {
	res, err := dbtx.Exec(ctx, ` + "`" + `DELETE FROM builds WHERE created_at < $1` + "`" + `)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected, nil
}
`
	if err := os.WriteFile(filepath.Join(rawDir, "delete_old_builds.go"), []byte(fixture), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	outcome, err := Check(Config{RepoRoot: repo})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected PASS for raw query on builds, got FAIL: %+v", outcome.Violations)
	}
}

// TestRawSensitiveTablesAlignment — guards that the gate-internal
// rawSensitiveTables list stays in sync with the source-of-truth in
// api/internal/db/raw/sensitive_tables.go.
//
// The gate can't import api/internal/db/raw (it would create a
// cycle with the production code that imports tenantpredicate
// during quality:gates). Instead, this test verifies the SET
// equality by enumerating the canonical 9 entries.
func TestRawSensitiveTablesAlignment(t *testing.T) {
	canonical := []string{
		"secrets",
		"audit_log",
		"users",
		"tenants",
		"tenant_members",
		"subscriptions",
		"tenants_pending_deletion",
		"tenant_tombstones",
		"cells",
	}
	if len(rawSensitiveTables) != len(canonical) {
		t.Fatalf("rawSensitiveTables length: want %d got %d", len(canonical), len(rawSensitiveTables))
	}
	for i, w := range canonical {
		if rawSensitiveTables[i] != w {
			t.Errorf("rawSensitiveTables[%d]: want %q got %q (UPDATE api/internal/db/raw/sensitive_tables.go too!)", i, w, rawSensitiveTables[i])
		}
	}
}
