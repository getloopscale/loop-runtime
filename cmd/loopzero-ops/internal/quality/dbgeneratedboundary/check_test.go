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
	writeGo(t, root, "api/internal/x/xhttp/route.go", cleanServiceFile)
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
	if out.Violations[0].File == "" {
		t.Error("Violation.File must be populated")
	}
}

const dotImportServiceFile = `package services

import (
	"context"

	. "github.com/getloopscale/loopzero/api/internal/db/generated"
)

func Do(ctx context.Context) { _ = Queries{} }
`

// A dot-import of the generated package (path unchanged, Name==".") must still
// be flagged — the AST matches on imp.Path.Value, so the alias/dot form cannot
// evade the fence. Locks the robustness the reviewer verified by reasoning.
func TestCheck_DotImportStillFlagged(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/svc.go", dotImportServiceFile)

	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a dot-import of db/generated must FAIL Gate A")
	}
}

// The aliased-import form (`gen "…/generated"`) is exercised by
// violatingServiceFile itself (it uses `gen`). Assert it explicitly so a future
// "simplify to grep" cannot silently regress the alias case.
func TestCheck_AliasedImportStillFlagged(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/svc.go", violatingServiceFile) // uses `gen` alias
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("an aliased import of db/generated must FAIL Gate A")
	}
}

// The api/cmd composition root is fenced too (the highest-leverage bypass site).
func TestCheck_CmdCompositionRootFenced(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/cmd/setup_stores.go", violatingServiceFile)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("api/cmd importing db/generated must FAIL Gate A (composition root)")
	}
}

// A forbidden dir absent from the tree is recorded in MissingDirs so a silent
// fence-drop (dir rename) is never invisible.
func TestCheck_MissingDirsRecorded(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/svc.go", cleanServiceFile) // only services present
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Fatalf("clean single-dir tree must PASS, got: %+v", out.Violations)
	}
	// server, cmd are absent → recorded.
	if len(out.MissingDirs) != 2 {
		t.Errorf("MissingDirs = %v, want 2 absent (server, cmd)", out.MissingDirs)
	}
}

func TestCheck_TestFileIncludedInScan(t *testing.T) {
	// A *_test.go under an outer layer that imports generated is still a breach.
	root := t.TempDir()
	writeGo(t, root, "api/internal/x/xhttp/route_test.go", violatingServiceFile)

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

// mkGo writes one Go file importing the given path, under the given dir.
func mkGo(t *testing.T, root, dir, imported string) {
	t.Helper()
	full := filepath.Join(root, dir)
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package x\n\nimport _ \"" + imported + "\"\n"
	if err := os.WriteFile(filepath.Join(full, "x.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const handWritten = "github.com/getloopscale/loopzero/api/internal/db"

// slice-4 T1.3: the server layer was measured at ZERO importers of the
// hand-written persistence package, so the fence lands there green.
func TestCheck_ServerImportingHandWrittenDBFails(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/server", handWritten)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Errorf("the server layer must not import the hand-written persistence package; got %+v", out.Violations)
	}
	if len(out.Violations) != 1 || out.Violations[0].Import != handWritten {
		t.Errorf("the violation must NAME the hand-written path, not the generated one; got %+v", out.Violations)
	}
}

// The negative control the ADR demands. `services` carries 28 importers today;
// fencing it would fail 43 files on arrival — an exemption at birth, which is
// the shape slice-1 spent itself removing. This test locks the DELIBERATE
// narrowness so a later widening is a conscious act with its own measurement,
// not an accident.
func TestCheck_ServicesImportingHandWrittenDBStillPasses(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/services", handWritten)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("the fence over the hand-written package is deliberately narrower than the generated one — widening it to services is B-079, measured at 28 files; got %+v", out.Violations)
	}
}

// And the generated fence is unchanged everywhere.
func TestCheck_ServicesImportingGeneratedStillFails(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/services", handWritten+"/generated")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Error("the generated fence must still hold in every forbidden dir — narrowing the new one must not narrow the old one")
	}
}

// slice-6 T1.3: routes reached zero importers of the hand-written package when
// db.DBTX moved to the dbtx leaf, so the fence landed there green; every HTTP
// package inherits it.
func TestCheck_HTTPPackageImportingHandWrittenDBFails(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/x/xhttp", handWritten)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Errorf("the interface layer must not import the hand-written persistence package; got %+v", out.Violations)
	}
}

// R2, and the reason the fence matches by EXACT path rather than by prefix:
// `api/internal/db/dbtx` lives UNDER `api/internal/db`, and a prefix match would
// have forbidden the very leaf that made this fence possible — the fix would
// have failed the gate it enables.
func TestCheck_ImportingTheDbtxLeafIsAllowed(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/x/xhttp", handWritten+"/dbtx")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("the SQL access port is not the entity package; forbidding it by prefix would forbid the correction itself; got %+v", out.Violations)
	}
}

// A feature's HTTP package inherits the fences routes/ carried: it is found by
// its `<name>http` suffix, so a handler family that left routes/ keeps both.
func TestCheck_FeatureHTTPPackageImportingHandWrittenDBFails(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/build/buildhttp", handWritten)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Errorf("an HTTP package must not import the hand-written persistence package; got %+v", out.Violations)
	}
}

func TestCheck_FeatureHTTPPackageImportingGeneratedFails(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/build/buildhttp", handWritten+"/generated")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Errorf("an HTTP package must not import the generated package; got %+v", out.Violations)
	}
}

// A feature root holds what services/ held and keeps its fence against the
// generated package; the feature's adapter package, like adapters/ before it,
// is where persistence rows are translated and is not fenced.
func TestCheck_FeatureRootImportingGeneratedFails(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/placement", handWritten+"/generated") // any registered feature
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Error("a feature's own code must not import the generated package")
	}
}

func TestCheck_FeatureAdapterImportingGeneratedPasses(t *testing.T) {
	root := t.TempDir()
	mkGo(t, root, "api/internal/placement/placementadapter", handWritten+"/generated")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("a feature's adapter package translates persistence rows, as adapters/ did; got %+v", out.Violations)
	}
}
