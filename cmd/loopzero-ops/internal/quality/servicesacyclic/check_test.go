// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package servicesacyclic

import (
	"os"
	"path/filepath"
	"testing"
)

func writeGo(t *testing.T, root, rel, body string) {
	t.Helper()
	full := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// buildImportsRelease is a legal edge (build → release).
const buildImportsRelease = `package build

import (
	"github.com/getloopscale/loopzero/api/internal/services/release"
	"github.com/getloopscale/loopzero/api/internal/services/deploycontracts"
)

var _ = release.X
var _ = deploycontracts.Y
`

// releaseImportsBuild is the forbidden reverse edge (would re-create the cycle).
const releaseImportsBuild = `package release

import "github.com/getloopscale/loopzero/api/internal/services/build"

var _ = build.Z
`

// deploycontractsLeaf must import no sibling.
const deploycontractsLeaf = `package deploycontracts

import "context"

var _ = context.Background
`

// deploycontractsImportsBuild breaks the leaf invariant.
const deploycontractsImportsBuild = `package deploycontracts

import "github.com/getloopscale/loopzero/api/internal/services/build"

var _ = build.Z
`

// placementLeaf is a pure leaf.
const placementLeaf = `package placement

func Do() {}
`

// placementImportsBilling is a forbidden cross-leaf edge.
const placementImportsBilling = `package placement

import "github.com/getloopscale/loopzero/api/internal/services/billing"

var _ = billing.Q
`

func TestCheck_LegalGraphPasses(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/build/b.go", buildImportsRelease)
	writeGo(t, root, "api/internal/services/release/r.go", "package release\n\nvar X = 1\n")
	writeGo(t, root, "api/internal/services/deploycontracts/s.go", deploycontractsLeaf)
	writeGo(t, root, "api/internal/services/placement/p.go", placementLeaf)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("legal DAG must PASS, got violations=%+v unknown=%v", out.Violations, out.Unknown)
	}
}

func TestCheck_ReverseEdgeFails(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/release/r.go", releaseImportsBuild)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("release → build is a forbidden reverse edge; must FAIL")
	}
	if out.Violations[0].From != "release" || out.Violations[0].To != "build" {
		t.Errorf("want release→build violation, got %+v", out.Violations[0])
	}
}

func TestCheck_SharedLeafInvariant(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/deploycontracts/s.go", deploycontractsImportsBuild)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("deploycontracts must import no sibling; deploycontracts → build must FAIL")
	}
}

func TestCheck_CrossLeafEdgeFails(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "api/internal/services/placement/p.go", placementImportsBilling)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("placement → billing is a forbidden cross-leaf edge; must FAIL")
	}
}

func TestCheck_UnknownContextFailsClosed(t *testing.T) {
	root := t.TempDir()
	// a subpackage not in the allow-list must fail-closed (forces registration).
	writeGo(t, root, "api/internal/services/newthing/n.go", "package newthing\n\nfunc F() {}\n")
	writeGo(t, root, "api/internal/services/deploycontracts/s.go", deploycontractsLeaf)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("an unregistered subpackage must fail-closed")
	}
	if len(out.Unknown) != 1 || out.Unknown[0] != "newthing" {
		t.Errorf("want Unknown=[newthing], got %v", out.Unknown)
	}
}

func TestCheck_TestImportEdgeAlsoCounts(t *testing.T) {
	root := t.TempDir()
	// a forbidden edge in a _test.go file is still a violation.
	writeGo(t, root, "api/internal/services/release/r_test.go", "package release\n\nimport \"github.com/getloopscale/loopzero/api/internal/services/build\"\n\nvar _ = build.Z\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a forbidden edge in a test file must FAIL (build-time cycle risk)")
	}
}

func TestCheck_SelfAliasImportIgnored(t *testing.T) {
	root := t.TempDir()
	// tests/unit-style alias of a package to itself is not a sibling edge.
	writeGo(t, root, "api/internal/services/release/r.go", "package release\n\nimport releasesvc \"github.com/getloopscale/loopzero/api/internal/services/release\"\n\nvar _ = releasesvc.X\nvar X = 1\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Errorf("self-import must be ignored, got %+v", out.Violations)
	}
}

func TestCheck_NestedSubpackageEdgeCaught(t *testing.T) {
	root := t.TempDir()
	// a nested subpackage under release/ that imports build must NOT escape the
	// gate by nesting — the parent context's allow-list applies to the subtree.
	writeGo(t, root, "api/internal/services/release/internal/helper/h.go",
		"package helper\n\nimport \"github.com/getloopscale/loopzero/api/internal/services/build\"\n\nvar _ = build.Z\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("a forbidden edge in a nested subpackage must FAIL (no nesting bypass)")
	}
	if out.Violations[0].From != "release" || out.Violations[0].To != "build" {
		t.Errorf("want release→build violation from nested pkg, got %+v", out.Violations[0])
	}
}

func TestCheck_NoServicesDirTriviallyPasses(t *testing.T) {
	root := t.TempDir()
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Error("absent services/ AND absent api/internal/ (partial checkout) must trivially pass")
	}
}

// §7 Step 1 (report finding — false-green nit): the services root being *renamed
// away* while its parent api/internal/ still exists is a real regression the gate
// exists to catch — it MUST fail-closed, not silently pass like a partial
// checkout. Pre-fix, os.Stat(root) error → Pass:true unconditionally (false green).
func TestCheck_ServicesRootRenamedAwayFailsClosed(t *testing.T) {
	root := t.TempDir()
	// api/internal/ exists (a sibling dir proves it), but services/ is gone.
	writeGo(t, root, "api/internal/other/x.go", "package other\n\nfunc F() {}\n")
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("services/ renamed away with api/internal/ present must FAIL (false-green regression)")
	}
}

// §7 Step 1: an *empty* services root (dir exists, zero context subpackages) means
// the whole decomposition vanished — also a regression, also fail-closed.
func TestCheck_EmptyServicesRootFailsClosed(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, servicesRoot), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("empty services/ (all contexts gone) must FAIL, not silently pass")
	}
}

// §7 Step 1 (permission-error case): if the services path is not a directory
// (e.g. clobbered by a file), the gate MUST surface an error — never silently
// pass and never crash.
func TestCheck_ServicesPathNotADirErrors(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "api/internal"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Clobber the services path with a regular file.
	if err := os.WriteFile(filepath.Join(root, servicesRoot), []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, err := Check(Config{RepoRoot: root})
	if err == nil {
		t.Fatalf("services path being a file must return an error, got Pass=%v", out.Pass)
	}
}
