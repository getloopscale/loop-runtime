// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package dbgeneratedboundary

import (
	"os"
	"path/filepath"
	"testing"
)

// writeGo writes a .go file at repoRoot/rel, creating parent dirs.
func writeGo(t *testing.T, repoRoot, rel, body string) {
	t.Helper()
	full := filepath.Join(repoRoot, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", full, err)
	}
}

const cleanServiceFile = `package services

import "context"

func Do(ctx context.Context) {}
`

const violatingServiceFile = `package services

import (
	"context"

	gen "github.com/getloopscale/loopzero/api/internal/db/generated"
)

func Do(ctx context.Context) { _ = gen.Queries{} }
`

func TestCheck_CleanBoundaryPasses(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/svc.go", cleanServiceFile)
	writeGo(t, root, "api/internal/routes/route.go", cleanServiceFile)
	writeGo(t, root, "api/internal/server/srv.go", cleanServiceFile)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("clean boundary must PASS, got violations: %+v", out.Violations)
	}
	if out.Scanned != 3 {
		t.Errorf("Scanned = %d, want 3", out.Scanned)
	}
}

func TestCheck_ServiceImportingGeneratedFails(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/svc.go", violatingServiceFile)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a service importing db/generated must FAIL Gate A")
	}
	if len(out.Violations) != 1 {
		t.Fatalf("want exactly 1 violation, got %d: %+v", len(out.Violations), out.Violations)
	}
	if got := out.Violations[0].Import; got != generatedImportPath {
		t.Errorf("Violation.Import = %q, want %q", got, generatedImportPath)
	}
	if !filepath.IsAbs(out.Violations[0].File) && out.Violations[0].File == "" {
		t.Error("Violation.File must be populated")
	}
}

func TestCheck_TestFileIncludedInScan(t *testing.T) {
	// A *_test.go under an outer layer that imports generated is still a breach.
	root := t.TempDir()
	writeGo(t, root, "api/internal/routes/route_test.go", violatingServiceFile)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a *_test.go in an outer layer importing db/generated must FAIL Gate A")
	}
}

func TestCheck_AbsentDirsTolerated(t *testing.T) {
	// Empty repo root: none of the forbidden dirs exist → trivially passes.
	root := t.TempDir()

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("absent dirs must PASS, got: %+v", out.Violations)
	}
	if out.Scanned != 0 {
		t.Errorf("Scanned = %d, want 0", out.Scanned)
	}
}

func TestCheck_StoreLayerNotForbidden(t *testing.T) {
	// The db/store/ adapter package IS allowed to import generated — it is the
	// ONLY sanctioned importer. Gate A must not flag it (it is outside the
	// forbidden dir set).
	root := t.TempDir()
	writeGo(t, root, "api/internal/db/store/release_wrapper.go", violatingServiceFile)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("db/store importing generated is sanctioned; Gate A must not flag it, got: %+v", out.Violations)
	}
}
