// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routesacyclic

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// tree writes subpackages under api/internal/routes/. Each entry maps a
// subpackage name to the import paths its single file declares.
func tree(t *testing.T, pkgs map[string][]string) string {
	t.Helper()
	root := t.TempDir()
	for name, imports := range pkgs {
		dir := filepath.Join(root, routesDir, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "package " + name + "\n\nimport (\n"
		for _, i := range imports {
			body += "\t_ \"" + i + "\"\n"
		}
		body += ")\n"
		if len(imports) == 0 {
			body = "package " + name + "\n"
		}
		if err := os.WriteFile(filepath.Join(dir, "x.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

var twoFamilies = map[string]map[string]bool{
	"httpio":  {},
	"secrets": {},
	"builds":  {},
}

// Rule 1, deny by default: a directory nobody registered is a handler family
// nobody decided on.
func TestCheck_UnregisteredSubpackageFails(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "inventado": nil})
	out, err := Check(Config{RepoRoot: root, Allowed: twoFamilies})
	if !errors.Is(err, ErrUnregisteredSubpackage) {
		t.Fatalf("an unregistered subpackage must FAIL; got %v", err)
	}
	if len(out.Unregistered) != 1 || out.Unregistered[0] != "inventado" {
		t.Errorf("the failure must NAME the directory; got %+v", out.Unregistered)
	}
}

// Rule 2: families are peers.
func TestCheck_SiblingEdgeFails(t *testing.T) {
	root := tree(t, map[string][]string{
		"httpio":  nil,
		"secrets": {routesImportPrefix + "builds"},
		"builds":  nil,
	})
	out, err := Check(Config{RepoRoot: root, Allowed: twoFamilies})
	if !errors.Is(err, ErrSiblingEdge) {
		t.Fatalf("one family importing another must FAIL; got %v", err)
	}
	if len(out.Edges) != 1 || out.Edges[0].From != "secrets" || out.Edges[0].To != "builds" {
		t.Errorf("the failure must name BOTH ends and where; got %+v", out.Edges)
	}
	if out.Edges[0].Line == 0 {
		t.Errorf("the edge must carry a line so the import is findable; got %+v", out.Edges[0])
	}
}

// The leaf everyone may import: httpio holds the response writers that used to
// live inside the projects family, and depending on it is not a sibling edge.
func TestCheck_ImportingTheLeafIsAllowed(t *testing.T) {
	reg := map[string]map[string]bool{
		"httpio":  {},
		"secrets": {"httpio": true},
	}
	root := tree(t, map[string][]string{
		"httpio":  nil,
		"secrets": {routesImportPrefix + "httpio"},
	})
	if _, err := Check(Config{RepoRoot: root, Allowed: reg}); err != nil {
		t.Errorf("a permitted edge to the shared leaf is the whole point of having one; got %v", err)
	}
}

// A nested package under a family still belongs to that family.
func TestCheck_NestedPathResolvesToItsFamily(t *testing.T) {
	reg := map[string]map[string]bool{"httpio": {}, "secrets": {"httpio": true}}
	root := tree(t, map[string][]string{
		"httpio":  nil,
		"secrets": {routesImportPrefix + "httpio/internal/detail"},
	})
	if _, err := Check(Config{RepoRoot: root, Allowed: reg}); err != nil {
		t.Errorf("a nested path under a permitted family is that family; got %v", err)
	}
}

// The floor. A fence that does not know what it permits has verified nothing,
// and reporting green there is the vacuous-pass shape B-040 measured.
func TestCheck_EmptyAllowListIsAnError(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil})
	if _, err := Check(Config{RepoRoot: root, Allowed: map[string]map[string]bool{}}); !errors.Is(err, ErrEmptyAllowList) {
		t.Errorf("an empty registry must FAIL, never pass green; got %v", err)
	}
}

// The routes package being renamed away must be LOUD. A fence whose subject
// vanished silently is a fence that stopped existing without telling anyone.
func TestCheck_MissingRoutesRootIsAnError(t *testing.T) {
	if _, err := Check(Config{RepoRoot: t.TempDir()}); err == nil {
		t.Error("an absent routes/ must fail — the fence cannot vanish with its subject and stay green")
	}
}

// Test files are scanned too: excluding them would leave the cheapest place to
// author a bypass unwatched, and both sibling gates include them on purpose.
func TestCheck_TestFilesAreScanned(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "secrets": nil, "builds": nil})
	p := filepath.Join(root, routesDir, "secrets", "x_test.go")
	body := "package secrets\n\nimport _ \"" + routesImportPrefix + "builds\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root, Allowed: twoFamilies}); !errors.Is(err, ErrSiblingEdge) {
		t.Errorf("a test reaching across a family boundary is still an edge; got %v", err)
	}
}

// Against the real tree: one subpackage today (httpio), registered, no edges.
func TestCheck_RealTree(t *testing.T) {
	out, err := Check(Config{RepoRoot: repoRoot(t)})
	if err != nil {
		t.Fatalf("the real tree must pass: unregistered=%v edges=%+v", out.Unregistered, out.Edges)
	}
	if len(out.Subpackages) == 0 {
		t.Fatal("routes carries at least the httpio leaf; finding none means the walk is broken, not that the tree is clean")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	start := dir
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.work")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatalf("go.work not found within 8 levels above %s — the real-tree assertion cannot run, and skipping it would report green while measuring nothing", start)
	return ""
}

// Matching by import PATH rather than by local name is what makes this fence
// immune to the three bypasses that slice-4's `domainsinkpurity` had to learn
// the hard way: an alias, a dot-import and a blank import all carry the same
// path, and none of them changes what the walker sees.
//
// Probed rather than assumed, because "it should be immune by construction" is
// the reasoning that let the alias bypass ship one slice ago.
func TestCheck_EdgeIsCaughtRegardlessOfImportForm(t *testing.T) {
	reg := map[string]map[string]bool{"httpio": {}, "secrets": {}, "builds": {}}
	forms := map[string]string{
		"alias": "import b \"" + routesImportPrefix + "builds\"\n\nvar _ = b.X",
		"dot":   "import . \"" + routesImportPrefix + "builds\"",
		"blank": "import _ \"" + routesImportPrefix + "builds\"",
	}
	for name, imp := range forms {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			for _, p := range []string{"httpio", "builds"} {
				d := filepath.Join(root, routesDir, p)
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(d, "x.go"), []byte("package "+p+"\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			d := filepath.Join(root, routesDir, "secrets")
			if err := os.MkdirAll(d, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(d, "x.go"), []byte("package secrets\n\n"+imp+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := Check(Config{RepoRoot: root, Allowed: reg}); !errors.Is(err, ErrSiblingEdge) {
				t.Errorf("an %s import carries the same path and must be caught; got %v", name, err)
			}
		})
	}
}

// MEASURED bypass: with a non-recursive ReadDir, an edge authored ONE LEVEL
// DOWN passed green. Fixture `secrets/store/y.go` importing `routes/builds`
// reported `Edges=[] err=<nil>`.
//
// The asymmetry is what made the hole look covered: the TARGET side already
// resolved nested paths (TestCheck_NestedPathResolvesToItsFamily passed), so a
// nesting test existed — for the wrong side. servicesacyclic, which this
// package's doc calls a deliberate clone, carries the lesson in its own source:
// "else the gate is trivially bypassable by nesting". The clone had dropped it.
func TestCheck_EdgeNestedBelowAFamilyIsStillCaught(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "secrets": nil, "builds": nil})
	deep := filepath.Join(root, routesDir, "secrets", "store")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package store\n\nimport _ \"" + routesImportPrefix + "builds\"\n"
	if err := os.WriteFile(filepath.Join(deep, "y.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Check(Config{RepoRoot: root, Allowed: twoFamilies})
	if !errors.Is(err, ErrSiblingEdge) {
		t.Fatalf("a family's allow-list applies to its WHOLE subtree; got %v edges=%+v files=%d", err, out.Edges, out.FilesParsed)
	}
}

// After the split, a family reaching back for a helper left behind in the parent
// package is the coupling the split removes. A prefix with a trailing slash
// would never see it.
func TestCheck_ImportingTheParentPackageIsAnEdge(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "secrets": {routesPkgPath}, "builds": nil})
	out, err := Check(Config{RepoRoot: root, Allowed: twoFamilies})
	if !errors.Is(err, ErrSiblingEdge) {
		t.Fatalf("a family importing routes/ itself must FAIL; got %v", err)
	}
	if len(out.Edges) != 1 || out.Edges[0].To != "<routes-parent>" {
		t.Errorf("the edge must name the parent so the reader knows what to move; got %+v", out.Edges)
	}
}

// Directories the Go toolchain ignores are not subpackages. Reporting testdata/
// as an unregistered family would be a finding about a convention, not the code.
func TestCheck_ToolchainIgnoredDirsAreNotSubpackages(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil})
	for _, d := range []string{"testdata", ".idea", "_scratch"} {
		if err := os.MkdirAll(filepath.Join(root, routesDir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	out, err := Check(Config{RepoRoot: root, Allowed: map[string]map[string]bool{"httpio": {}}})
	if err != nil {
		t.Fatalf("testdata/ is not a handler family; got %v unregistered=%+v", err, out.Unregistered)
	}
}
