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
	"github.com/getloopscale/loopzero/api/internal/services/shared"
)

var _ = release.X
var _ = shared.Y
`

// releaseImportsBuild is the forbidden reverse edge (would re-create the cycle).
const releaseImportsBuild = `package release

import "github.com/getloopscale/loopzero/api/internal/services/build"

var _ = build.Z
`

// sharedLeaf must import no sibling.
const sharedLeaf = `package shared

import "context"

var _ = context.Background
`

// sharedImportsBuild breaks the leaf invariant.
const sharedImportsBuild = `package shared

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
	writeGo(t, root, "api/internal/services/shared/s.go", sharedLeaf)
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
	writeGo(t, root, "api/internal/services/shared/s.go", sharedImportsBuild)
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Pass {
		t.Fatal("shared must import no sibling; shared → build must FAIL")
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
	writeGo(t, root, "api/internal/services/shared/s.go", sharedLeaf)
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
		t.Error("absent services/ must trivially pass")
	}
}
