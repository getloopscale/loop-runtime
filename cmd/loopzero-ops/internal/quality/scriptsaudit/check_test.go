// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scriptsaudit

import (
	"bytes"
	"encoding/csv"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// fixtureRepo materializes a synthetic repo in t.TempDir with the minimal
// structure needed to exercise ScanRepo + DetectCallers + ProposeDisposition.
// Returns the root path; cleanup is automatic via t.TempDir().
func fixtureRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for relPath, content := range files {
		full := filepath.Join(root, relPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", full, err)
		}
	}
	return root
}

// TestScanRepo_FindsAllShellScripts — baseline assertion: ScanRepo returns
// every .sh in the repo, with Path + LOC + LastMod populated.
func TestScanRepo_FindsAllShellScripts(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/a.sh":            "#!/bin/bash\necho a\n",
		"infra/scripts/b.sh":            "#!/bin/bash\necho b\necho line3\n",
		"infra/tests/scripts/c_test.sh": "#!/bin/bash\necho c\n",
		"README.md":                     "# not a shell script",
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if len(scripts) != 3 {
		t.Errorf("expected 3 scripts, got %d", len(scripts))
	}
	for _, s := range scripts {
		if !strings.HasSuffix(s.Path, ".sh") {
			t.Errorf("Path must end in .sh, got %s", s.Path)
		}
		if s.LOC <= 0 {
			t.Errorf("LOC must be > 0, got %d for %s", s.LOC, s.Path)
		}
		if s.LastMod.IsZero() {
			t.Errorf("LastMod must be set for %s", s.Path)
		}
	}
}

// TestScanRepo_ExcludesNodeModules — vendor/, node_modules/, .git/ must be
// excluded from the scan.
func TestScanRepo_ExcludesNodeModules(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/real.sh":        "#!/bin/bash\necho real\n",
		"node_modules/foo/bin/foo.sh":  "#!/bin/bash\necho dep\n",
		"vendor/x/script.sh":           "#!/bin/bash\necho vendor\n",
		".git/hooks/pre-commit.sample": "#!/bin/sh\necho git\n",
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if len(scripts) != 1 {
		t.Errorf("expected 1 script (excluding node_modules/vendor/.git), got %d", len(scripts))
		for _, s := range scripts {
			t.Logf("  found: %s", s.Path)
		}
	}
}

// TestScanRepo_HandlesSymlinks_NoCycle — EC-07: symlinks are deduplicated
// (the real script is listed ONCE) and do not cause a cycle.
func TestScanRepo_HandlesSymlinks_NoCycle(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/real.sh": "#!/bin/bash\necho real\n",
	})
	// Create a symlink pointing to real.sh
	linkPath := filepath.Join(root, "infra/scripts/alias.sh")
	if err := os.Symlink(filepath.Join(root, "infra/scripts/real.sh"), linkPath); err != nil {
		t.Skipf("symlink not supported on this filesystem: %v", err)
	}

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	// The contract: listed ONCE (not twice). We accept either the real one or the
	// alias as the canonical path — but NEVER both. The implementation resolves via
	// EvalSymlinks.
	if len(scripts) > 1 {
		t.Errorf("expected 1 unique script after symlink dedup, got %d", len(scripts))
		for _, s := range scripts {
			t.Logf("  found: %s", s.Path)
		}
	}
	if len(scripts) == 0 {
		t.Errorf("expected at least 1 script, got 0")
	}
}

// TestDetectCallers_FindsTaskfileRefs — a script invoked by Taskfile.yml or
// taskfiles/*.yml must show up with the corresponding caller.
func TestDetectCallers_FindsTaskfileRefs(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/target.sh": "#!/bin/bash\necho target\n",
		"Taskfile.yml": `
version: '3'
tasks:
  build:
    cmds:
      - bash infra/scripts/target.sh
`,
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var target *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "target.sh") {
			target = &scripts[i]
			break
		}
	}
	if target == nil {
		t.Fatal("target.sh not found")
	}
	if len(target.Callers) == 0 {
		t.Errorf("expected caller for target.sh, got 0 callers")
	}
	foundTaskfile := false
	for _, c := range target.Callers {
		if strings.Contains(c, "Taskfile.yml") {
			foundTaskfile = true
		}
	}
	if !foundTaskfile {
		t.Errorf("expected Taskfile.yml as caller, got %v", target.Callers)
	}
}

// TestDetectCallers_FindsCIYamlRefs — a script referenced in
// .github/workflows/*.yml must show up with the corresponding caller.
func TestDetectCallers_FindsCIYamlRefs(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/ci-tool.sh": "#!/bin/bash\necho ci\n",
		".github/workflows/test.yml": `
jobs:
  test:
    steps:
      - run: bash infra/scripts/ci-tool.sh
`,
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var target *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "ci-tool.sh") {
			target = &scripts[i]
			break
		}
	}
	if target == nil {
		t.Fatal("ci-tool.sh not found")
	}
	foundCI := false
	for _, c := range target.Callers {
		if strings.Contains(c, "test.yml") {
			foundCI = true
		}
	}
	if !foundCI {
		t.Errorf("expected workflows/test.yml as caller, got %v", target.Callers)
	}
}

// TestDetectCallers_FindsCrossScriptRefs — a script A.sh that invokes B.sh
// must create caller A on B.
func TestDetectCallers_FindsCrossScriptRefs(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/scripts/parent.sh": "#!/bin/bash\nbash infra/scripts/child.sh\n",
		"infra/scripts/child.sh":  "#!/bin/bash\necho child\n",
	})

	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var child *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "child.sh") {
			child = &scripts[i]
			break
		}
	}
	if child == nil {
		t.Fatal("child.sh not found")
	}
	foundParent := false
	for _, c := range child.Callers {
		if strings.Contains(c, "parent.sh") {
			foundParent = true
		}
	}
	if !foundParent {
		t.Errorf("expected parent.sh as caller of child.sh, got %v", child.Callers)
	}
}

// TestProposeDisposition_DeadCode — a script with 0 callers + last-mod > 90d
// is classified as DELETE-DEAD.
func TestProposeDisposition_DeadCode(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	s := Script{
		Path:    "infra/scripts/old-dead.sh",
		LOC:     50,
		LastMod: old,
		Callers: nil,
	}
	disp := ProposeDisposition(s, time.Now())
	if disp != "DELETE-DEAD" {
		t.Errorf("expected DELETE-DEAD, got %s", disp)
	}
}

// TestProposeDisposition_HermeticTest — X_test.sh is classified as AUTO-DIE
// (it dies with its parent X.sh).
func TestProposeDisposition_HermeticTest(t *testing.T) {
	s := Script{
		Path:    "infra/tests/scripts/foo_test.sh",
		LOC:     30,
		LastMod: time.Now(),
	}
	disp := ProposeDisposition(s, time.Now())
	if disp != "AUTO-DIE" {
		t.Errorf("expected AUTO-DIE for hermetic test pattern, got %s", disp)
	}
}

// TestProposeDisposition_BareHelmWrapper — a script whose content is `helm upgrade`
// is classified as DELEGATE-HELMFILE (per D2).
func TestProposeDisposition_BareHelmWrapper(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "install.sh")
	body := "#!/bin/bash\nhelm upgrade --install foo charts/foo --namespace foo\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{
		Path:    path,
		Body:    body,
		LOC:     2,
		LastMod: time.Now(),
	}
	disp := ProposeDisposition(s, time.Now())
	if disp != "DELEGATE-HELMFILE" {
		t.Errorf("expected DELEGATE-HELMFILE, got %s", disp)
	}
}

// TestScriptHasRealHelmInvocation — T0.1 false-positive-awareness: `helm
// install`/`upgrade` inside a comment, an echo string, or a die() error message
// is NOT a real invocation and must not trigger DELEGATE-HELMFILE.
func TestScriptHasRealHelmInvocation(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"real-command", "#!/bin/bash\nhelm upgrade --install foo charts/foo\n", true},
		{"real-indented", "#!/bin/bash\nif true; then\n    helm install foo charts/foo\nfi\n", true},
		{"comment", "#!/bin/bash\n# install: helm upgrade --install chaos-mesh ...\n", false},
		{"echo-string", "#!/bin/bash\necho \"    helm install chaos-mesh ...\"\n", false},
		{"die-message", "#!/bin/bash\nkubectl get crd x || die \"install: helm upgrade --install ...\"\n", false},
		{"none", "#!/bin/bash\nkubectl apply -f x.yaml\n", false},
	}
	for _, c := range cases {
		if got := hasRealHelmInvocation(c.body); got != c.want {
			t.Errorf("%s: hasRealHelmInvocation = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestProposeDisposition_PureLogicGate — a script matching `check-*.sh` in
// infra/scripts/ is a MIGRATE-GO-PUREFN candidate.
func TestProposeDisposition_PureLogicGate(t *testing.T) {
	s := Script{
		Path:    "infra/scripts/check-prod-image-tags.sh",
		LOC:     92,
		LastMod: time.Now(),
		Callers: []string{".github/workflows/ci.yml"},
	}
	disp := ProposeDisposition(s, time.Now())
	if disp != "MIGRATE-GO-PUREFN" {
		t.Errorf("expected MIGRATE-GO-PUREFN, got %s", disp)
	}
}

// TestProposeDisposition_CheckScriptThatShellsOut_NotPureFn — T0.1
// false-positive-awareness: a `check-*.sh`/`validate-*.sh` that invokes an
// external CLI (kubectl/helm/cosign/doctl/pulumi/argocd) is NOT pure-logic;
// migrating it to Go would be a forbidden shell-wrap (parent D5). It must NOT
// be classified MIGRATE-GO-PUREFN.
func TestProposeDisposition_CheckScriptThatShellsOut_NotPureFn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "check-prod-vap-readiness.sh")
	body := "#!/bin/bash\nkubectl get validatingadmissionpolicy\ncosign verify $IMG\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, Body: body, LOC: 3, LastMod: time.Now(), Callers: []string{".github/workflows/x.yml"}}
	if disp := ProposeDisposition(s, time.Now()); disp == "MIGRATE-GO-PUREFN" {
		t.Errorf("check-* that shells out must NOT be MIGRATE-GO-PUREFN, got %s", disp)
	}
}

// TestProposeDisposition_PureCheckScript_StaysPureFn — converse: a check-*.sh
// with NO external CLI invocation remains a MIGRATE-GO-PUREFN candidate.
func TestProposeDisposition_PureCheckScript_StaysPureFn(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "check-prod-image-tags.sh")
	body := "#!/bin/bash\ngrep -r ':latest' manifests/ && exit 1\nexit 0\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, Body: body, LOC: 3, LastMod: time.Now(), Callers: []string{".github/workflows/x.yml"}}
	if disp := ProposeDisposition(s, time.Now()); disp != "MIGRATE-GO-PUREFN" {
		t.Errorf("pure check-* must stay MIGRATE-GO-PUREFN, got %s", disp)
	}
}

// TestProposeDisposition_GeneratorThatShellsOut_NotPureGenerator — T0.1
// false-positive-awareness: a `generate-*.sh` that injects secrets via kubectl
// is a bootstrap injector, NOT a pure file generator. It belongs to the
// helmfile-delegation slice, not a hermetic go:generate migration.
func TestProposeDisposition_GeneratorThatShellsOut_NotPureGenerator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "generate-secrets.sh")
	body := "#!/bin/bash\nPW=$(openssl rand -hex 16)\nkubectl create secret generic foo --from-literal=pw=$PW\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, Body: body, LOC: 3, LastMod: time.Now(), Callers: []string{"Taskfile.yml"}}
	if disp := ProposeDisposition(s, time.Now()); disp == "MIGRATE-GENERATOR" {
		t.Errorf("generate-* that shells out must NOT be MIGRATE-GENERATOR, got %s", disp)
	}
}

// TestProposeDisposition_PureGenerator_StaysGenerator — converse: a generate-*
// with no external CLI (writes files only) remains MIGRATE-GENERATOR.
func TestProposeDisposition_PureGenerator_StaysGenerator(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "gen-state-machine-dots.sh")
	body := "#!/bin/bash\nfor m in build release; do echo \"digraph $m {}\" > docs/$m.dot; done\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, Body: body, LOC: 2, LastMod: time.Now(), Callers: []string{"Taskfile.yml"}}
	if disp := ProposeDisposition(s, time.Now()); disp != "MIGRATE-GENERATOR" {
		t.Errorf("pure generate-* must stay MIGRATE-GENERATOR, got %s", disp)
	}
}

// TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead — EC-03:
// When a script is referenced through dynamic interpolation (`bash "$VAR"`),
// caller_count stays at zero (the regex does not catch it), but the heuristic must
// NOT mark it DELETE-DEAD in those cases. That validation happens in T1.1 via a
// dynamic grep; here we validate that CallerCount > 0 rules out DELETE-DEAD.
func TestProposeDisposition_ScriptWithDynamicCaller_NotMarkedDead(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	s := Script{
		Path:    "infra/scripts/lib/dispatcher.sh",
		LOC:     50,
		LastMod: old,
		Callers: []string{"infra/scripts/main.sh"}, // has a documented caller
	}
	disp := ProposeDisposition(s, time.Now())
	if disp == "DELETE-DEAD" {
		t.Errorf("script with caller should not be DELETE-DEAD, got %s", disp)
	}
}

// TestEmitCSV_StableSchema — the CSV emits a header and rows with the canonical schema:
// path, loc, last_mod_utc, caller_count, disposition, callers
func TestEmitCSV_StableSchema(t *testing.T) {
	scripts := []Script{
		{
			Path:        "a.sh",
			LOC:         10,
			LastMod:     time.Date(2026, 5, 22, 12, 0, 0, 0, time.UTC),
			Callers:     []string{"Taskfile.yml"},
			Disposition: "KEEP-OPERATOR",
		},
	}
	var buf bytes.Buffer
	if err := EmitCSV(scripts, &buf); err != nil {
		t.Fatalf("EmitCSV: %v", err)
	}
	r := csv.NewReader(&buf)
	header, err := r.Read()
	if err != nil {
		t.Fatalf("read header: %v", err)
	}
	expected := []string{"path", "loc", "last_mod_utc", "caller_count", "disposition", "callers"}
	if len(header) != len(expected) {
		t.Errorf("header columns mismatch: got %v, want %v", header, expected)
	}
	for i, col := range expected {
		if i >= len(header) || header[i] != col {
			t.Errorf("header[%d]: got %q, want %q", i, header[i], col)
		}
	}
	// Read the data row
	row, err := r.Read()
	if err == io.EOF {
		t.Fatal("expected 1 data row, got EOF")
	}
	if err != nil {
		t.Fatalf("read row: %v", err)
	}
	if row[0] != "a.sh" {
		t.Errorf("path: got %q, want a.sh", row[0])
	}
	if row[1] != "10" {
		t.Errorf("loc: got %q, want 10", row[1])
	}
	if row[3] != "1" {
		t.Errorf("caller_count: got %q, want 1", row[3])
	}
	if row[4] != "KEEP-OPERATOR" {
		t.Errorf("disposition: got %q, want KEEP-OPERATOR", row[4])
	}
}

// TestEmitCSV_DeterministicOrder — output sorted by path (stable in a diff).
func TestEmitCSV_DeterministicOrder(t *testing.T) {
	scripts := []Script{
		{Path: "zeta.sh", LOC: 1, LastMod: time.Now(), Disposition: "X"},
		{Path: "alpha.sh", LOC: 1, LastMod: time.Now(), Disposition: "Y"},
		{Path: "mid.sh", LOC: 1, LastMod: time.Now(), Disposition: "Z"},
	}
	var buf bytes.Buffer
	if err := EmitCSV(scripts, &buf); err != nil {
		t.Fatalf("EmitCSV: %v", err)
	}
	r := csv.NewReader(&buf)
	if _, err := r.Read(); err != nil {
		t.Fatalf("read header: %v", err)
	}
	var paths []string
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		paths = append(paths, row[0])
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("paths not sorted: %v", paths)
	}
	want := []string{"alpha.sh", "mid.sh", "zeta.sh"}
	for i := range want {
		if paths[i] != want[i] {
			t.Errorf("paths[%d]: got %q, want %q", i, paths[i], want[i])
		}
	}
}

// === Additional coverage tests (push to >=90%) ===

// TestProposeDisposition_DelegateArgoCD — script with kubectl apply -f argocd
func TestProposeDisposition_DelegateArgoCD(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "tenant-sync.sh")
	body := "#!/bin/bash\nkubectl apply -f infra/argocd/tenants/\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	s := Script{Path: path, Body: body, LOC: 2, LastMod: time.Now()}
	if disp := ProposeDisposition(s, time.Now()); disp != "DELEGATE-ARGOCD" {
		t.Errorf("expected DELEGATE-ARGOCD, got %s", disp)
	}
}

// TestProposeDisposition_MigrateGoSDK — pulumi-*.sh
func TestProposeDisposition_MigrateGoSDK(t *testing.T) {
	s := Script{Path: "infra/scripts/pulumi-archive.sh", LOC: 50, LastMod: time.Now()}
	if disp := ProposeDisposition(s, time.Now()); disp != "MIGRATE-GO-SDK" {
		t.Errorf("expected MIGRATE-GO-SDK, got %s", disp)
	}
}

// TestProposeDisposition_Generator — gen-*.sh
func TestProposeDisposition_Generator(t *testing.T) {
	s := Script{Path: "infra/scripts/gen-state-machine-dots.sh", LOC: 30, LastMod: time.Now()}
	if disp := ProposeDisposition(s, time.Now()); disp != "MIGRATE-GENERATOR" {
		t.Errorf("expected MIGRATE-GENERATOR for gen-*, got %s", disp)
	}
	s2 := Script{Path: "infra/scripts/generate-secrets.sh", LOC: 20, LastMod: time.Now()}
	if disp := ProposeDisposition(s2, time.Now()); disp != "MIGRATE-GENERATOR" {
		t.Errorf("expected MIGRATE-GENERATOR for generate-*, got %s", disp)
	}
}

// TestProposeDisposition_MigrateGinkgo — test orchestrators
func TestProposeDisposition_MigrateGinkgo(t *testing.T) {
	s := Script{Path: "infra/tests/parity/validate-matrix.sh", LOC: 80, LastMod: time.Now()}
	if disp := ProposeDisposition(s, time.Now()); disp != "MIGRATE-GINKGO" {
		t.Errorf("expected MIGRATE-GINKGO for parity test, got %s", disp)
	}
	s2 := Script{Path: "infra/tests/dogfood/journey-01.sh", LOC: 60, LastMod: time.Now()}
	if disp := ProposeDisposition(s2, time.Now()); disp != "MIGRATE-GINKGO" {
		t.Errorf("expected MIGRATE-GINKGO for dogfood, got %s", disp)
	}
}

// TestProposeDisposition_KeepOperatorBreakGlass — break-glass small script
func TestProposeDisposition_KeepOperatorBreakGlass(t *testing.T) {
	s := Script{Path: "infra/scripts/break-glass.sh", LOC: 50, LastMod: time.Now()}
	if disp := ProposeDisposition(s, time.Now()); disp != "KEEP-OPERATOR" {
		t.Errorf("expected KEEP-OPERATOR for break-glass, got %s", disp)
	}
}

// TestProposeDisposition_ReviewNeededDefault — script not matching any heuristic
func TestProposeDisposition_ReviewNeededDefault(t *testing.T) {
	s := Script{Path: "infra/scripts/random-utility.sh", LOC: 200, LastMod: time.Now(), Callers: []string{"X"}}
	if disp := ProposeDisposition(s, time.Now()); disp != "REVIEW-NEEDED" {
		t.Errorf("expected REVIEW-NEEDED default, got %s", disp)
	}
}

// TestIsolatedStemPresent — direct test of stem-only matching with various prefixes
func TestIsolatedStemPresent(t *testing.T) {
	cases := []struct {
		text string
		stem string
		want bool
	}{
		{"bash /foo/scripts/install.sh", "install", true}, // preceded by /
		{"  install runs here", "install", true},          // preceded by space
		{"\"install\" is the name", "install", true},      // preceded by "
		{"thisinstall is embedded", "install", false},     // no isolated prefix (prev is word char)
		{"abc", "ab", false},                              // stem too short (<4)
		{"foo_install_bar", "install", false},             // prev=_ is word-char → no isolated prefix
	}
	for _, c := range cases {
		got := isolatedStemPresent(c.text, c.stem)
		if got != c.want {
			t.Errorf("isolatedStemPresent(%q, %q) = %v, want %v", c.text, c.stem, got, c.want)
		}
	}
}

func TestCountLines_EmptyContentIsZero(t *testing.T) {
	if n := countLines(nil); n != 0 {
		t.Errorf("countLines(empty) = %d, want 0", n)
	}
}

func TestCountLines_CountsALastLineWithoutNewline(t *testing.T) {
	if n := countLines([]byte("line1\nline2")); n != 2 {
		t.Errorf("countLines = %d, want 2", n)
	}
}

// DELETE-DEAD is decided against the instant the caller passes, so the verdict
// is a function of its inputs rather than of when the test runs.
func TestProposeDisposition_AgeIsMeasuredAgainstTheGivenInstant(t *testing.T) {
	lastMod := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s := Script{Path: "infra/scripts/x.sh", LOC: 10, LastMod: lastMod}

	if got := ProposeDisposition(s, lastMod.Add(89*24*time.Hour)); got == "DELETE-DEAD" {
		t.Errorf("at 89 days: %s, want not DELETE-DEAD", got)
	}
	if got := ProposeDisposition(s, lastMod.Add(91*24*time.Hour)); got != "DELETE-DEAD" {
		t.Errorf("at 91 days: %s, want DELETE-DEAD", got)
	}
}

// === Phase 0 T0.1: false-positive-aware reference detection ===

// TestDetectsDynamicSourcedLib — EC false-positive class #1: a lib sourced via
// `source "$DIR/cell2-gate-${g}.sh"` must be credited a caller even though its
// literal basename never appears in the parent. Without this, the 13 cell2-gate
// libs false-positive to DELETE-DEAD.
func TestDetectsDynamicSourcedLib(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/tests/parity/lib/cell2-gate-c5.sh": "#!/bin/bash\necho gate5\n",
		"infra/tests/parity/cell2-activation-gates.sh": `#!/bin/bash
LIB_DIR="$(dirname "$0")/lib"
for g in c1 c5 c12; do
  source "$LIB_DIR/cell2-gate-${g}.sh"
done
`,
	})
	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var lib *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "cell2-gate-c5.sh") {
			lib = &scripts[i]
			break
		}
	}
	if lib == nil {
		t.Fatal("cell2-gate-c5.sh not found")
	}
	if len(lib.Callers) == 0 {
		t.Errorf("dynamically-sourced lib must have a caller, got 0 (false-positive DELETE-DEAD risk)")
	}
}

// TestDetectsGlobRunnerTestScripts — EC false-positive class #2: a test script
// run by a CI glob-runner (`for f in infra/tests/scripts/drill-*_test.sh`) must
// be credited a caller even though only the glob (not the literal name) appears.
func TestDetectsGlobRunnerTestScripts(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"infra/tests/scripts/drill-mint-jwt_test.sh": "#!/bin/bash\necho t\n",
		".github/workflows/drill-hermetic-tests.yml": `
jobs:
  test:
    steps:
      - run: |
          for f in infra/tests/scripts/drill-*_test.sh; do bash "$f"; done
`,
	})
	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var ts *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "drill-mint-jwt_test.sh") {
			ts = &scripts[i]
			break
		}
	}
	if ts == nil {
		t.Fatal("drill-mint-jwt_test.sh not found")
	}
	if len(ts.Callers) == 0 {
		t.Errorf("glob-runner test script must have a caller, got 0")
	}
}

// TestDetectsDockerfileEntrypoint — EC false-positive class #3: a container
// entrypoint referenced by Dockerfile ENTRYPOINT must be credited a caller.
func TestDetectsDockerfileEntrypoint(t *testing.T) {
	root := fixtureRepo(t, map[string]string{
		"cloud/dashboard/docker-entrypoint.sh": "#!/bin/sh\nexec node server.js\n",
		"cloud/dashboard/Dockerfile": `FROM node:20
COPY docker-entrypoint.sh /usr/local/bin/
ENTRYPOINT ["docker-entrypoint.sh"]
`,
	})
	scripts, err := ScanRepo(root)
	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	var ep *Script
	for i := range scripts {
		if strings.HasSuffix(scripts[i].Path, "docker-entrypoint.sh") {
			ep = &scripts[i]
			break
		}
	}
	if ep == nil {
		t.Fatal("docker-entrypoint.sh not found")
	}
	foundDockerfile := false
	for _, c := range ep.Callers {
		if strings.Contains(c, "Dockerfile") {
			foundDockerfile = true
		}
	}
	if !foundDockerfile {
		t.Errorf("Dockerfile ENTRYPOINT must be a caller, got %v", ep.Callers)
	}
}

// TestDetectsHookRegistration — EC false-positive class #4: a Claude harness
// hook (.claude/hooks/*.sh) is registered with the harness, not invoked by
// literal basename; it must NOT be classified DELETE-DEAD even if old + 0-caller.
func TestDetectsHookRegistration(t *testing.T) {
	old := time.Now().Add(-200 * 24 * time.Hour)
	s := Script{
		Path:    ".claude/hooks/task-completed.sh",
		LOC:     100,
		LastMod: old,
		Callers: nil,
	}
	if disp := ProposeDisposition(s, time.Now()); disp == "DELETE-DEAD" {
		t.Errorf("harness hook must not be DELETE-DEAD, got %s", disp)
	}
}

// TestIsCallerSearchFile — exercises all branches
func TestIsCallerSearchFile(t *testing.T) {
	tests := map[string]bool{
		"Taskfile.yml":   true,
		"workflow.yml":   true,
		"config.yaml":    true,
		"helper.sh":      true,
		"chart.gotmpl":   true,
		"Tiltfile":       true,
		"Tiltfile.local": true,
		"README.md":      false,
		"main.go":        false,
	}
	for name, want := range tests {
		got := isCallerSearchFile(name)
		if got != want {
			t.Errorf("isCallerSearchFile(%q) = %v, want %v", name, got, want)
		}
	}
}

// ScanRepo must classify by content read against --root, not against the
// process working directory. The test process runs inside this package, so a
// relative Script.Path read with os.ReadFile resolves to nothing and every
// content rule silently falls through.
func TestScanRepo_ClassifiesByContentRegardlessOfWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "check-cluster.sh"), []byte("#!/bin/bash\nkubectl get pods\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "Taskfile.yml"), []byte("tasks:\n  c:\n    cmds:\n      - bash check-cluster.sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	scripts, err := ScanRepo(root)

	if err != nil {
		t.Fatalf("ScanRepo: %v", err)
	}
	if len(scripts) != 1 || scripts[0].Disposition != "KEEP-OPERATOR" {
		t.Fatalf("scripts = %+v, want check-cluster.sh as KEEP-OPERATOR (it shells out to kubectl)", scripts)
	}
}
