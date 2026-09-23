// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package sqlcsync

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestActionableFix verifies the operator-facing fix message format. Stable
// contract — CI failure messages quote this; future updates require operator
// communication.
func TestActionableFix(t *testing.T) {
	msg := ActionableFix()
	if !strings.Contains(msg, "task db:generate") {
		t.Fatalf("ActionableFix() must mention `task db:generate`; got: %q", msg)
	}
	if !strings.Contains(msg, "git add") {
		t.Fatalf("ActionableFix() must mention `git add`; got: %q", msg)
	}
	if !strings.Contains(msg, "api/internal/db/generated") {
		t.Fatalf("ActionableFix() must mention `api/internal/db/generated`; got: %q", msg)
	}
}

// TestDiffDirsEmptyDirs ensures the diff logic handles two empty directories
// as PASS (zero violations).
func TestDiffDirsEmptyDirs(t *testing.T) {
	committed := t.TempDir()
	fresh := t.TempDir()

	violations, err := diffDirs(committed, fresh)
	if err != nil {
		t.Fatalf("diffDirs returned unexpected error: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations on empty dirs, got %d: %+v", len(violations), violations)
	}
}

// TestDiffDirsMissingInCommitted detects fresh-only files (sqlc emitted a
// file that's not committed).
func TestDiffDirsMissingInCommitted(t *testing.T) {
	committed := t.TempDir()
	fresh := t.TempDir()
	writeTempGo(t, fresh, "models.go", "package generated\n")

	violations, err := diffDirs(committed, fresh)
	if err != nil {
		t.Fatalf("diffDirs: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d: %+v", len(violations), violations)
	}
	if violations[0].Reason != "missing" {
		t.Fatalf("expected Reason=missing, got %q", violations[0].Reason)
	}
	if violations[0].File != "models.go" {
		t.Fatalf("expected File=models.go, got %q", violations[0].File)
	}
}

// TestDiffDirsExtraInCommitted detects committed-only files (sqlc stopped
// emitting a file that's still committed — orphan from a deleted query).
func TestDiffDirsExtraInCommitted(t *testing.T) {
	committed := t.TempDir()
	fresh := t.TempDir()
	writeTempGo(t, committed, "orphan.go", "package generated\n")

	violations, err := diffDirs(committed, fresh)
	if err != nil {
		t.Fatalf("diffDirs: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Reason != "extra" {
		t.Fatalf("expected Reason=extra, got %q", violations[0].Reason)
	}
}

// TestDiffDirsContentDiff detects byte-level drift between committed and
// fresh output for the same filename — the canonical "schema changed but
// generated not regenerated" failure mode.
func TestDiffDirsContentDiff(t *testing.T) {
	committed := t.TempDir()
	fresh := t.TempDir()
	writeTempGo(t, committed, "models.go", "package generated\n\ntype A struct { ID string }\n")
	writeTempGo(t, fresh, "models.go", "package generated\n\ntype A struct { ID string; Name string }\n")

	violations, err := diffDirs(committed, fresh)
	if err != nil {
		t.Fatalf("diffDirs: %v", err)
	}
	if len(violations) != 1 {
		t.Fatalf("expected 1 violation, got %d", len(violations))
	}
	if violations[0].Reason != "content-diff" {
		t.Fatalf("expected Reason=content-diff, got %q", violations[0].Reason)
	}
	if !strings.Contains(violations[0].Detail, "fresh:") {
		t.Fatalf("expected Detail to include `fresh:`; got: %q", violations[0].Detail)
	}
}

// TestDiffDirsIgnoresNonGoFiles ensures README.md / sqlc.yaml / etc. don't
// produce false violations.
func TestDiffDirsIgnoresNonGoFiles(t *testing.T) {
	committed := t.TempDir()
	fresh := t.TempDir()
	if err := os.WriteFile(filepath.Join(committed, "README.md"), []byte("different content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fresh, "README.md"), []byte("also different"), 0o644); err != nil {
		t.Fatal(err)
	}

	violations, err := diffDirs(committed, fresh)
	if err != nil {
		t.Fatalf("diffDirs: %v", err)
	}
	if len(violations) != 0 {
		t.Fatalf("expected 0 violations (non-.go files ignored), got %d", len(violations))
	}
}

// TestTruncate ensures the helper truncates long lines for diff previews.
func TestTruncate(t *testing.T) {
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("short string should not be truncated; got %q", got)
	}
	long := strings.Repeat("a", 200)
	got := truncate(long, 100)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("truncated string should end with `...`; got tail: %q", got[len(got)-5:])
	}
	if len(got) != 103 {
		t.Fatalf("truncated string should be 100 + 3 dots = 103 chars; got %d", len(got))
	}
}

// writeTempGo is the canonical test helper for committing .go file fixtures.
func writeTempGo(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writeTempGo %s/%s: %v", dir, name, err)
	}
}

// fakeSqlc writes an executable that answers `version` like the pinned sqlc,
// so the pre-flight passes without the real binary.
func fakeSqlc(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "sqlc")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\necho "+expectedSqlcVersion+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin
}

func sqlcRepo(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		defaultSqlcYAML:                    "version: \"2\"\n",
		defaultSchemaFile:                  "CREATE TABLE t (id int);\n",
		defaultGeneratedDir + "/models.go": "package generated\n",
		defaultQueriesDir + "/.gitkeep":    "",
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

// The Phase-0 branch passed whenever queries/ held no .sql file — and on any
// error reading it. Queries exist now, so an empty or unreadable queries dir is
// a comparison of nothing, not a tree in sync.
func TestCheck_EmptyQueriesDirIsAnError(t *testing.T) {
	_, err := Check(Config{RepoRoot: sqlcRepo(t), SqlcBinary: fakeSqlc(t)})

	if !errors.Is(err, ErrNoQueries) {
		t.Fatalf("err = %v, want ErrNoQueries", err)
	}
}
