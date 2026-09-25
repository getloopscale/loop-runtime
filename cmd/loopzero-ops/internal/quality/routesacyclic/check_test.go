// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package routesacyclic

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
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
	if !errors.Is(err, subpackagefence.ErrUnregisteredSubpackage) {
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
		"secrets": {routesPkgPath + "/" + "builds"},
		"builds":  nil,
	})
	out, err := Check(Config{RepoRoot: root, Allowed: twoFamilies})
	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
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
		"secrets": {routesPkgPath + "/" + "httpio"},
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
		"secrets": {routesPkgPath + "/" + "httpio/internal/detail"},
	})
	if _, err := Check(Config{RepoRoot: root, Allowed: reg}); err != nil {
		t.Errorf("a nested path under a permitted family is that family; got %v", err)
	}
}

// The floor. A fence that does not know what it permits has verified nothing,
// and reporting green there is the vacuous-pass shape B-040 measured.
func TestCheck_EmptyAllowListIsAnError(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil})
	if _, err := Check(Config{RepoRoot: root, Allowed: map[string]map[string]bool{}}); !errors.Is(err, subpackagefence.ErrEmptyAllowList) {
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
	body := "package secrets\n\nimport _ \"" + routesPkgPath + "/" + "builds\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root, Allowed: twoFamilies}); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Errorf("a test reaching across a family boundary is still an edge; got %v", err)
	}
}

// Against the real tree: one subpackage today (httpio), registered, no edges.
func TestCheck_RealTree(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("the real tree must pass: unregistered=%v edges=%+v", out.Unregistered, out.Edges)
	}
	if len(out.Subpackages) == 0 {
		t.Fatal("routes carries at least the httpio leaf; finding none means the walk is broken, not that the tree is clean")
	}
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
		"alias": "import b \"" + routesPkgPath + "/" + "builds\"\n\nvar _ = b.X",
		"dot":   "import . \"" + routesPkgPath + "/" + "builds\"",
		"blank": "import _ \"" + routesPkgPath + "/" + "builds\"",
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
			if _, err := Check(Config{RepoRoot: root, Allowed: reg}); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
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
	body := "package store\n\nimport _ \"" + routesPkgPath + "/" + "builds\"\n"
	if err := os.WriteFile(filepath.Join(deep, "y.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Check(Config{RepoRoot: root, Allowed: twoFamilies})
	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("a family's allow-list applies to its WHOLE subtree; got %v edges=%+v files=%d", err, out.Edges, out.FilesParsed)
	}
}

// After the split, a family reaching back for a helper left behind in the parent
// package is the coupling the split removes. A prefix with a trailing slash
// would never see it.
func TestCheck_ImportingTheParentPackageIsAnEdge(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "secrets": {routesPkgPath}, "builds": nil})
	out, err := Check(Config{RepoRoot: root, Allowed: twoFamilies})
	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("a family importing routes/ itself must FAIL; got %v", err)
	}
	if len(out.Edges) != 1 || out.Edges[0].To != subpackagefence.ParentNode {
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

// MEASURED blind spot (B-084, raised by the slice-8 review): Check iterated
// SUBDIRECTORIES only, so a file in the PARENT package importing a family passed
// green. Probed on the real tree before the fix:
//
//	routes/zz_parent_probe.go: import _ ".../routes/tenants"
//	→ 4 subpackage(s), 0 unregistered, 0 sibling edge(s)  |  PASS, exit 0
//
// It is the fourth of four directions, and the only one that was open. Fourteen
// families are still to move and would have trusted it.
func TestCheck_ParentImportingAFamilyIsAnEdge(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "tenants": nil})
	body := "package routes\n\nimport _ \"" + routesPkgPath + "/" + "tenants\"\n"
	if err := os.WriteFile(filepath.Join(root, routesDir, "x.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := map[string]map[string]bool{"httpio": {}, "tenants": {}}
	out, err := Check(Config{RepoRoot: root, Allowed: reg})
	if !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("the parent reaching into a family is the same coupling as a sibling edge, on the vertical; got %v edges=%+v", err, out.Edges)
	}
	if len(out.Edges) != 1 || out.Edges[0].From != subpackagefence.ParentNode || out.Edges[0].To != "tenants" {
		t.Errorf("the edge must name the parent and the family; got %+v", out.Edges)
	}
	if out.Edges[0].Line == 0 {
		t.Errorf("the edge must carry a line so the import is findable; got %+v", out.Edges[0])
	}
}

// R1 / ADR D2: the rule is "a leaf yes, a family no", not "the parent imports
// nothing". Measured on the real tree before the fix: the parent holds 29 edges
// and ALL of them go to leaves — 28 to httpio, 1 to routestest. Forbidding every
// parent edge would fail 29 files the day the fence landed, which is an exemption
// at birth.
func TestCheck_ParentImportingALeafIsAllowed(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "tenants": nil})
	body := "package routes\n\nimport _ \"" + routesPkgPath + "/" + "httpio\"\n"
	if err := os.WriteFile(filepath.Join(root, routesDir, "x.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := map[string]map[string]bool{"httpio": {}, "tenants": {}}
	if _, err := Check(Config{RepoRoot: root, Allowed: reg}); err != nil {
		t.Errorf("a leaf exists to be imported by everyone, the parent included; got %v", err)
	}
}

// A nested path under a family still belongs to that family, from the parent too.
func TestCheck_ParentImportingANestedFamilyPathIsAnEdge(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "tenants": nil})
	body := "package routes\n\nimport _ \"" + routesPkgPath + "/" + "tenants/internal/detail\"\n"
	if err := os.WriteFile(filepath.Join(root, routesDir, "x.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := map[string]map[string]bool{"httpio": {}, "tenants": {}}
	if _, err := Check(Config{RepoRoot: root, Allowed: reg}); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Errorf("a nested path resolves to its family from the parent side too; got %v", err)
	}
}

// R2, and the bug this fence shipped with for one iteration: "leaf" was DERIVED
// from the empty allowed set. `version: {}` is a FAMILY that imports no sibling
// and `httpio: {}` is a leaf — identical under that rule, so the parent could
// reach `version` and the gate called it a leaf and passed.
//
// The plan wrote this risk down before the code repeated it, which is the part
// worth remembering: writing a risk is not mitigating it.
func TestCheck_AFamilyWithNoSiblingsIsNotALeaf(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "version": nil})
	body := "package routes\n\nimport _ \"" + routesPkgPath + "/" + "version\"\n"
	if err := os.WriteFile(filepath.Join(root, routesDir, "x.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	// version has an EMPTY allowed set, exactly like httpio — and is still a family.
	cfg := Config{
		RepoRoot: root,
		Allowed:  map[string]map[string]bool{"httpio": {}, "version": {}},
		Leaves:   map[string]bool{"httpio": true},
	}
	if _, err := Check(cfg); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Fatalf("a family that imports no sibling is still a family; deriving leafness from the empty set let the parent reach it; got %v", err)
	}
}

// And the declared leaf list is load-bearing: drop the entry and the 29 real
// parent edges to httpio become findings.
func TestCheck_RemovingALeafDeclarationMakesTheParentEdgesFail(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil})
	body := "package routes\n\nimport _ \"" + routesPkgPath + "/" + "httpio\"\n"
	if err := os.WriteFile(filepath.Join(root, routesDir, "x.go"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		RepoRoot: root,
		Allowed:  map[string]map[string]bool{"httpio": {}},
		Leaves:   map[string]bool{}, // httpio no longer declared a leaf
	}
	if _, err := Check(cfg); !errors.Is(err, subpackagefence.ErrForbiddenEdge) {
		t.Errorf("the leaf declaration must carry weight — without it the parent edge is an edge; got %v", err)
	}
}

// The floor the plan promised and the first implementation omitted. Measured
// before the fix: an existing-but-empty routes/ reported `0 file(s) parsed` and
// PASS, exit 0.
//
// servicesacyclic — which this package's doc calls a deliberate clone — fails
// closed on exactly this shape and documents why in its own source. It is the
// third lesson this lineage has lost from that file; the second was filepath.Walk.
func TestCheck_NothingParsedIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, routesDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root, Allowed: map[string]map[string]bool{"httpio": {}}}); !errors.Is(err, subpackagefence.ErrNothingParsed) {
		t.Errorf("an empty routes/ must FAIL — a fence that read nothing has verified nothing; got %v", err)
	}
}

// HIGH-1: two hand-maintained registries with no cross-check. Declaring a family
// as a leaf made the parent reach it and the gate pass — silently, because the
// lenient side wins. The plan rejected the explicit-leaf design on exactly this
// ground, then the derived design turned out wrong and the risk arrived anyway.
func TestCheck_AFamilyDeclaredAsALeafIsIncoherent(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "tenants": nil})
	cfg := Config{
		RepoRoot: root,
		Allowed:  map[string]map[string]bool{"httpio": {}, "tenants": {"httpio": true}},
		Leaves:   map[string]bool{"httpio": true, "tenants": true}, // contradiction
	}
	if _, err := Check(cfg); !errors.Is(err, subpackagefence.ErrIncoherentRegistry) {
		t.Fatalf("a name declared both a family and a leaf must FAIL before anything is scanned; got %v", err)
	}
}

// A leaf legitimately appears in `allowed` with an EMPTY set — that is how httpio
// and routestest are registered. Only a leaf that ALSO declares siblings is the
// contradiction.
func TestCheck_ALeafWithAnEmptyAllowedEntryIsCoherent(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil})
	cfg := Config{
		RepoRoot: root,
		Allowed:  map[string]map[string]bool{"httpio": {}},
		Leaves:   map[string]bool{"httpio": true},
	}
	if _, err := Check(cfg); err != nil {
		t.Errorf("that is exactly how the real leaves are registered; got %v", err)
	}
}

// The real tree must be READ, not merely walked. Without this the count is free
// to collapse — skipping _test.go drops 97 to 49 and every other test stays green.
func TestCheck_RealTreeParsesBothTheParentAndTheSubpackages(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.Root(t)})
	if err != nil {
		t.Fatalf("real tree: %v", err)
	}
	// Reconciled against an INDEPENDENT count, not against a lower bound. The
	// first version asserted `parsed > len(subpackages)` and a mutation that
	// skipped _test.go — dropping the real read from 97 to 49 — sailed through it,
	// because 49 is still greater than 4. A floor that any plausible regression
	// clears is not a floor.
	want := countGoFiles(t, qtestutil.Root(t))
	if out.FilesParsed != want {
		t.Fatalf("the walk parsed %d Go file(s) but api/internal/routes holds %d — a confident verdict over a miscount is worth less than no verdict",
			out.FilesParsed, want)
	}
}

// countGoFiles counts every .go file under routes/, at any depth, skipping the
// directories the Go toolchain ignores (testdata, dot- and underscore-prefixed).
// Deliberately naive and independent of Check's walk: two implementations of the
// same count only agree when both are right. It descends at any depth because a
// package nested inside a family (bootstrap/bootstraptest) is compiled and read
// like any other; a one-level count reported it as a miscount by the walk.
func countGoFiles(t *testing.T, repo string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(filepath.Join(repo, routesDir), func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			if path != filepath.Join(repo, routesDir) &&
				(name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".go") {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// MEDIUM-3: a fixture under testdata/ is never compiled by Go, so an import
// there is not an edge. The top-level walk already skipped those directories and
// the family-side walk did not — which would have made the first family carrying
// a fixture into an exemption.
func TestCheck_ToolchainIgnoredDirsUnderAFamilyAreNotEdges(t *testing.T) {
	root := tree(t, map[string][]string{"httpio": nil, "tenants": nil, "version": nil})
	for _, d := range []string{"testdata", "_scratch"} {
		dir := filepath.Join(root, routesDir, "tenants", d)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := "package fixture\n\nimport _ \"" + routesPkgPath + "/" + "version\"\n"
		if err := os.WriteFile(filepath.Join(dir, "f.go"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	reg := map[string]map[string]bool{"httpio": {}, "tenants": {}, "version": {}}
	if _, err := Check(Config{RepoRoot: root, Allowed: reg}); err != nil {
		t.Errorf("a fixture Go never compiles is not an edge; got %v", err)
	}
}

// MEDIUM-1: the operator-facing message must describe the rule that actually
// fired. The parent is not a sibling and not a subpackage, and the fix for it is
// the third bullet — move the shared helper to a leaf — which the previous text
// did not offer at all.
func TestActionableFix_NamesAllFourRules(t *testing.T) {
	fix := ActionableFix()
	for _, want := range []string{"nobody registered", "one family reached for another", "PARENT package", "leaf", "test-only leaf", "_test.go"} {
		if !strings.Contains(fix, want) {
			t.Errorf("the fix text must cover %q — a gate is worth its actionability, and the parent case is the one this slice added; got:\n%s", want, fix)
		}
	}
	// The count in the opening line is part of the message: saying "three"
	// while listing four is the text drifting from the rules it describes,
	// which is precisely what this gate exists to stop elsewhere.
	if !strings.Contains(fix, "One of four things") {
		t.Errorf("the opening count must match the number of rules listed; got:\n%s", fix)
	}
}

// writeFile puts one file with a chosen NAME inside a subpackage, which `tree`
// cannot do: it always writes x.go. The distinction between x.go and x_test.go
// is the whole subject of the two tests below.
func writeFile(t *testing.T, root, pkg, name string, imports ...string) {
	t.Helper()
	dir := filepath.Join(root, routesDir, pkg)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "package " + pkg + "\n"
	if strings.HasSuffix(name, "_test.go") {
		body = "package " + pkg + "_test\n"
	}
	if len(imports) > 0 {
		body += "\nimport (\n"
		for _, i := range imports {
			body += "\t_ \"" + i + "\"\n"
		}
		body += ")\n"
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// Rule 5: a TEST-ONLY leaf may be reached only from a _test.go.
//
// routestest exists to be imported by tests, and `permitted` could not tell the
// two kinds of file apart: scanPackage HAD info.Name() and never consulted it.
// So declaring `routestest: true` — which every family with a test must do —
// also licensed PRODUCTION code to import it.
//
// What is behind that door is not stylistic. routestest.DefaultUserContext
// returns an owner-role identity for a fixed tenant; a production path reaching
// it is auth-shaped, and this fence is the mechanism meant to catch that class.
//
// Measured by the review of slice-19: adding the import to production
// repositories.go left the real tree GREEN at "0 sibling edge(s)".
func TestCheck_ProductionFileImportingATestOnlyLeafIsAnEdge(t *testing.T) {
	reg := map[string]map[string]bool{
		"routestest": {},
		"secrets":    {"routestest": true}, // declared, as every family with tests must
	}
	root := t.TempDir()
	writeFile(t, root, "routestest", "helpers.go")
	writeFile(t, root, "secrets", "secrets.go", routesPkgPath+"/"+"routestest")

	out, err := Check(Config{RepoRoot: root, Allowed: reg})
	if err == nil {
		t.Fatalf("production code reaching the test-only leaf must fail even when the family declares it; got PASS: %+v", out)
	}

	if !errors.Is(err, subpackagefence.ErrTestOnlyLeafFromProduction) {
		t.Errorf("the message must name the ACTUAL rule: subpackagefence.ErrForbiddenEdge says \"families are peers\",\n"+
			"which would send the reader hunting a coupling that is not there — the target IS a\n"+
			"declared leaf and the family DID declare the edge; got %v", err)
	}
	// The file:line lives on the Outcome, which is what the CLI prints. An
	// operator cannot act on a rule name alone.
	if len(out.Edges) != 1 || !out.Edges[0].TestOnly || !strings.HasSuffix(out.Edges[0].File, "secrets.go") {
		t.Errorf("the edge must name the production file and carry its kind; got %+v", out.Edges)
	}
}

// The other half: the permission is real for the file kind it was declared for.
// Without this, the fix above could be "forbid routestest entirely", which would
// break every family that moved out and lost its shared helpers.
func TestCheck_TestFileImportingATestOnlyLeafIsAllowed(t *testing.T) {
	reg := map[string]map[string]bool{
		"routestest": {},
		"secrets":    {"routestest": true},
	}
	root := t.TempDir()
	writeFile(t, root, "routestest", "helpers.go")
	writeFile(t, root, "secrets", "secrets.go")
	writeFile(t, root, "secrets", "secrets_test.go", routesPkgPath+"/"+"routestest")

	if _, err := Check(Config{RepoRoot: root, Allowed: reg}); err != nil {
		t.Errorf("a _test.go reaching the test-only leaf is precisely why the leaf exists; got %v", err)
	}
}

// The parent obeys the same rule, and it matters more there than anywhere: the
// parent is the LARGEST body of production code under this fence — 40 files at
// slice-19 against a family's one or two. A rule that covered the families and
// exempted the parent would exempt the bulk of what it guards.
//
// It passes today only by accident of history: the package doc records that the
// parent's single routestest edge happens to live in a _test.go. Accident is not
// a guarantee, and the next helper someone reaches for from a production file
// would have been silent.
func TestCheck_ParentProductionFileImportingATestOnlyLeafIsAnEdge(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "routestest", "helpers.go")
	dir := filepath.Join(root, routesDir)
	if err := os.WriteFile(filepath.Join(dir, "projects.go"),
		[]byte("package routes\n\nimport (\n\t_ \""+routesPkgPath+"/"+"routestest\"\n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Check(Config{RepoRoot: root, Allowed: map[string]map[string]bool{"routestest": {}}})
	if !errors.Is(err, subpackagefence.ErrTestOnlyLeafFromProduction) {
		t.Fatalf("the parent's production files are as barred as a family's; got %v (%+v)", err, out.Edges)
	}
	if len(out.Edges) != 1 || out.Edges[0].From != subpackagefence.ParentNode || !out.Edges[0].TestOnly {
		t.Errorf("the edge must be attributed to the parent and carry its kind; got %+v", out.Edges)
	}
}

// And the parent keeps the permission it actually uses — the real tree's one
// routestest import. Without this the fix could be "bar the parent outright",
// which would fail the tree the day it landed.
func TestCheck_ParentTestFileImportingATestOnlyLeafIsAllowed(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "routestest", "helpers.go")
	dir := filepath.Join(root, routesDir)
	if err := os.WriteFile(filepath.Join(dir, "projects_test.go"),
		[]byte("package routes_test\n\nimport (\n\t_ \""+routesPkgPath+"/"+"routestest\"\n)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root, Allowed: map[string]map[string]bool{"routestest": {}}}); err != nil {
		t.Errorf("the parent's _test.go is exactly the edge the real tree carries; got %v", err)
	}
}

func TestRegistryValidates(t *testing.T) {
	if err := subpackagefence.ValidateRegistry(Config{}.Fence()); err != nil {
		t.Fatalf("the committed registry must be coherent and acyclic: %v", err)
	}
}
