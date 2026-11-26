// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenantpredicate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestActionableFix verifies the message format.
func TestActionableFix(t *testing.T) {
	msg := ActionableFix()
	if !strings.Contains(msg, "tenant_id = $N") {
		t.Fatalf("ActionableFix must mention tenant_id = $N; got: %q", msg)
	}
	if !strings.Contains(msg, exemptionMarker) {
		t.Fatalf("ActionableFix must reference the exemption marker; got: %q", msg)
	}
}

// TestTenantTablesList verifies the canonical tenant-owned list contains
// the load-bearing tables.
func TestTenantTablesList(t *testing.T) {
	mustHave := []string{
		"tenants", // self-table
		"projects",
		"builds",
		"releases",
		"release_app_attestations", // M07 attestation persistence
		"audit_log",                // CR-9 audit trail
		"secrets",                  // tenant isolation CVSS 9.x
		"environment_targets",
		"memberships",
	}
	for _, table := range mustHave {
		if !IsTenantOwned(table) {
			t.Errorf("expected %s in TenantOwnedTables", table)
		}
	}
}

// TestIsTenantSelfTable verifies the tenants table is recognized as self.
func TestIsTenantSelfTable(t *testing.T) {
	if !IsTenantSelfTable("tenants") {
		t.Fatal("tenants must be the self-table")
	}
	if !IsTenantSelfTable("Tenants") {
		t.Fatal("case-insensitive match required")
	}
	if IsTenantSelfTable("projects") {
		t.Fatal("projects is not the self-table")
	}
}

// TestCheckEmptyQueriesDir ensures empty queries/ → PASS (Phase 0 baseline).
func TestCheckEmptyQueriesDir(t *testing.T) {
	root := t.TempDir()
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS on empty dir; got %d violations", len(outcome.Violations))
	}
}

// TestCheckDetectsMissingPredicateOnSelect detects a SELECT query on a
// tenant-owned table without `tenant_id`.
func TestCheckDetectsMissingPredicateOnSelect(t *testing.T) {
	root := t.TempDir()
	queriesDir := filepath.Join(root, defaultQueriesDir)
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1;
`
	if err := os.WriteFile(filepath.Join(queriesDir, "projects.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected FAIL on missing tenant_id; got PASS")
	}
	if len(outcome.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(outcome.Violations), outcome.Violations)
	}
	v := outcome.Violations[0]
	if v.Query != "GetProjectByID" {
		t.Errorf("expected Query=GetProjectByID, got %q", v.Query)
	}
	if v.Table != "projects" {
		t.Errorf("expected Table=projects, got %q", v.Table)
	}
	if v.Reason != "missing-where-predicate" {
		t.Errorf("expected Reason=missing-where-predicate, got %q", v.Reason)
	}
}

// TestCheckAcceptsTenantIDPredicate verifies that a SELECT with tenant_id
// is accepted.
func TestCheckAcceptsTenantIDPredicate(t *testing.T) {
	root := t.TempDir()
	queriesDir := filepath.Join(root, defaultQueriesDir)
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `-- name: GetProjectByID :one
SELECT * FROM projects WHERE id = $1 AND tenant_id = $2;
`
	if err := os.WriteFile(filepath.Join(queriesDir, "projects.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS with tenant_id; got: %+v", outcome.Violations)
	}
}

// TestCheckExemptsTenantsSelfTable verifies that queries on the `tenants`
// table do not require tenant_id (they use id = $N).
func TestCheckExemptsTenantsSelfTable(t *testing.T) {
	root := t.TempDir()
	queriesDir := filepath.Join(root, defaultQueriesDir)
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = $1;
`
	if err := os.WriteFile(filepath.Join(queriesDir, "tenants.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS on tenants self-table; got: %+v", outcome.Violations)
	}
}

// TestCheckHonorsExemptionMarker verifies that the
// `-- tenantpredicate:exempt` comment allowlists a query.
func TestCheckHonorsExemptionMarker(t *testing.T) {
	root := t.TempDir()
	queriesDir := filepath.Join(root, defaultQueriesDir)
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `-- tenantpredicate:exempt
-- name: AdminListAllProjects :many
SELECT * FROM projects;
`
	if err := os.WriteFile(filepath.Join(queriesDir, "admin.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS with exempt marker; got: %+v", outcome.Violations)
	}
}

// TestCheckDetectsMissingInsertColumn verifies that INSERT INTO without
// tenant_id column triggers a violation.
func TestCheckDetectsMissingInsertColumn(t *testing.T) {
	root := t.TempDir()
	queriesDir := filepath.Join(root, defaultQueriesDir)
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `-- name: InsertProject :one
INSERT INTO projects (id, name) VALUES ($1, $2) RETURNING *;
`
	if err := os.WriteFile(filepath.Join(queriesDir, "projects.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Fatalf("expected FAIL on INSERT without tenant_id column")
	}
	if len(outcome.Violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(outcome.Violations))
	}
	if outcome.Violations[0].Reason != "missing-insert-column" {
		t.Errorf("expected Reason=missing-insert-column, got %q", outcome.Violations[0].Reason)
	}
}

// TestCheckAcceptsInsertWithTenantIDColumn verifies INSERT with tenant_id
// column is accepted.
func TestCheckAcceptsInsertWithTenantIDColumn(t *testing.T) {
	root := t.TempDir()
	queriesDir := filepath.Join(root, defaultQueriesDir)
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `-- name: InsertProject :one
INSERT INTO projects (id, tenant_id, name) VALUES ($1, $2, $3) RETURNING *;
`
	if err := os.WriteFile(filepath.Join(queriesDir, "projects.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS with tenant_id INSERT column; got: %+v", outcome.Violations)
	}
}

// TestCheckIgnoresNonTenantTables verifies that queries on non-tenant-owned
// tables don't produce violations.
func TestCheckIgnoresNonTenantTables(t *testing.T) {
	root := t.TempDir()
	queriesDir := filepath.Join(root, defaultQueriesDir)
	if err := os.MkdirAll(queriesDir, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `-- name: GetCellByID :one
SELECT * FROM cells WHERE id = $1;
`
	if err := os.WriteFile(filepath.Join(queriesDir, "cells.sql"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("expected PASS on non-tenant-owned cells table; got: %+v", outcome.Violations)
	}
}
