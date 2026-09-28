// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package advisorylock

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scanresult"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestCheck_XactLock_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/internal/lock/lock.go", "func L() { db.Exec(\"SELECT pg_advisory_xact_lock($1)\", k) }\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass() {
		t.Fatalf("xact-scoped lock must PASS: %+v", out)
	}
}

func TestCheck_SessionLock_Fails(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/internal/lock/lock.go", "func L() { db.Exec(\"SELECT pg_advisory_lock($1)\", k) }\n")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass() || len(out.Findings) != 1 {
		t.Fatalf("session-scoped pg_advisory_lock( must FAIL: %+v", out)
	}
	if !strings.Contains(out.Findings[0], "lock.go") {
		t.Fatalf("violation must name the file: %v", out.Findings)
	}
}

func TestCheck_SessionLockInComment_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/internal/lock/lock.go", "// never use pg_advisory_lock( — use the xact variant\nfunc L() {}\n")
	out, _ := Check(Config{RepoRoot: root})
	if !out.Pass() {
		t.Fatalf("session lock only in a comment must PASS: %+v", out)
	}
}

func TestCheck_TestFileIgnored_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/internal/lock/lock.go", "package lock\n")
	write(t, root, "api/internal/lock/lock_test.go", "db.Exec(\"pg_advisory_lock($1)\")\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Scanned != 1 || !out.Pass() {
		t.Fatalf("_test file must be ignored: %+v", out)
	}
}

func TestCheck_ScansAllConfiguredDirs(t *testing.T) {
	root := t.TempDir()
	write(t, root, "infra/scripts/migrate.sh", "psql -c \"SELECT pg_advisory_lock(1)\"\n")
	out, _ := Check(Config{RepoRoot: root})
	if out.Pass() {
		t.Fatalf("session lock under infra/scripts/ must FAIL: %+v", out)
	}
}

func TestCheck_NoLocks_Pass(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !out.Pass() {
		t.Fatalf("no advisory locks → PASS: %+v", out)
	}
}

// A root where none of the production trees exists is a misconfigured run, not a
// clean one: zero files scanned must never print PASS.
func TestCheck_NothingScannedIsAnError(t *testing.T) {
	_, err := Check(Config{RepoRoot: t.TempDir()})
	if !errors.Is(err, scanresult.ErrNothingScanned) {
		t.Fatalf("want ErrNothingScanned, got %v", err)
	}
}

func TestCheck_ReportsHowManyFilesItScanned(t *testing.T) {
	root := t.TempDir()
	write(t, root, "api/main.go", "package main\n")
	write(t, root, "infra/scripts/run.sh", "echo ok\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Scanned != 2 {
		t.Fatalf("want 2 scanned files, got %d", out.Scanned)
	}
}
