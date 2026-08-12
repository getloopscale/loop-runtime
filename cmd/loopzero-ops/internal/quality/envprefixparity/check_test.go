// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package envprefixparity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// twoSites writes two synthetic Go files, each declaring the symbol with the
// given literal body, and returns a Config pointing at both.
func twoSites(t *testing.T, aBody, bBody string) Config {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("a/a.go", "package a\n\nvar reservedEnvKeyPrefixes = []string{"+aBody+"}\n")
	write("b/b.go", "package b\n\nvar reservedEnvKeyPrefixes = []string{"+bBody+"}\n")
	return Config{
		RepoRoot: root,
		Sites: []Site{
			{RelPath: "a/a.go", Symbol: "reservedEnvKeyPrefixes"},
			{RelPath: "b/b.go", Symbol: "reservedEnvKeyPrefixes"},
		},
	}
}

func TestCheck_DivergentListsFail(t *testing.T) {
	out, err := Check(twoSites(t, `"LOOPZERO_", "AWS_", "GOOGLE_"`, `"LOOPZERO_", "AWS_"`))
	if !errors.Is(err, ErrPrefixSetsDiverge) {
		t.Fatalf("divergent sets must fail; got %v", err)
	}
	only := out.OnlyIn["a/a.go"]
	if len(only) != 1 || only[0] != "GOOGLE_" {
		t.Errorf("the failure must NAME the prefix and the file that carries it; got %+v", out.OnlyIn)
	}
}

// EC-5: order is not part of the rule.
func TestCheck_SameSetDifferentOrderPasses(t *testing.T) {
	if _, err := Check(twoSites(t, `"AWS_", "LOOPZERO_"`, `"LOOPZERO_", "AWS_"`)); err != nil {
		t.Errorf("the same set written in a different order is the same rule; got %v", err)
	}
}

// EC-6: repetition is not part of the rule either.
func TestCheck_DuplicateWithinOneListIsNormalised(t *testing.T) {
	if _, err := Check(twoSites(t, `"LOOPZERO_", "LOOPZERO_", "AWS_"`, `"LOOPZERO_", "AWS_"`)); err != nil {
		t.Errorf("a repeated element does not change the set; got %v", err)
	}
}

// R4 / negative control: a declaration that is no longer a plain list must be a
// LOUD error. Passing because the subject could not be found is the shape this
// whole slice exists to remove.
func TestCheck_MissingLiteralIsAnError(t *testing.T) {
	root := t.TempDir()
	writeFile := func(rel, body string) {
		p := filepath.Join(root, rel)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeFile("a/a.go", "package a\n\nvar reservedEnvKeyPrefixes = []string{\"LOOPZERO_\"}\n")
	// The symbol exists but is built by a call, not declared as a literal.
	writeFile("b/b.go", "package b\n\nfunc load() []string { return nil }\n\nvar reservedEnvKeyPrefixes = load()\n")

	cfg := Config{RepoRoot: root, Sites: []Site{
		{RelPath: "a/a.go", Symbol: "reservedEnvKeyPrefixes"},
		{RelPath: "b/b.go", Symbol: "reservedEnvKeyPrefixes"},
	}}
	if _, err := Check(cfg); !errors.Is(err, ErrDeclarationNotFound) {
		t.Errorf("a declaration that is not a plain literal must be a typed error, never a silent pass; got %v", err)
	}
}

func TestCheck_AbsentFileIsAnError(t *testing.T) {
	cfg := Config{RepoRoot: t.TempDir(), Sites: []Site{{RelPath: "nope/x.go", Symbol: "reservedEnvKeyPrefixes"}}}
	if _, err := Check(cfg); err == nil {
		t.Errorf("a missing site must fail; a gate that cannot read its subject has verified nothing")
	}
}

// The symbol name must be matched exactly — a comment or a string mentioning it
// is not the declaration. This is why the extractor is AST-based.
func TestCheck_MentionInACommentIsNotTheDeclaration(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a", "a.go")
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	body := "package a\n\n// reservedEnvKeyPrefixes lives elsewhere now.\nconst note = \"reservedEnvKeyPrefixes\"\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := Config{RepoRoot: root, Sites: []Site{{RelPath: "a/a.go", Symbol: "reservedEnvKeyPrefixes"}}}
	if _, err := Check(cfg); !errors.Is(err, ErrDeclarationNotFound) {
		t.Errorf("a comment or string mentioning the symbol is not a declaration; got %v", err)
	}
}

// Against the real tree. This asserts the CURRENT state on purpose: the two
// copies diverge on GOOGLE_, and the DIRECTION of the fix is a pending owner
// decision (EC-4 / MEET-ASYNC-AMENDMENT-2026-08-11-001), because the two
// directions have opposite blast radius — adding it to the linter breaks tenant
// builds that pass today, removing it from the API weakens find_sec_004.
//
// So the gate ships REPORTING the divergence rather than silently picking a
// side. When the decision lands, this test flips to asserting parity.
func TestCheck_RealTreeDivergesOnGoogle(t *testing.T) {
	root := repoRoot(t)
	out, err := Check(Config{RepoRoot: root})
	if !errors.Is(err, ErrPrefixSetsDiverge) {
		t.Fatalf("expected the measured divergence to still be present; got err=%v decls=%+v", err, out.Declarations)
	}
	only := out.OnlyIn["api/internal/services/deploycontracts/build_plan.go"]
	if len(only) != 1 || only[0] != "GOOGLE_" {
		t.Errorf("the divergence should be exactly GOOGLE_ on the API side; got %+v", out.OnlyIn)
	}
}

// repoRoot walks up until it finds go.work, so the test does not depend on the
// working directory the runner chose.
//
// Not finding it is a FAILURE, never a skip. A skip here would make the one test
// that measures the REAL tree pass by not running — which is precisely the
// vacuous-pass shape this whole slice exists to remove (see B-040:
// `route-auth-coverage: PASS — 0 routes`). The same reasoning is already written
// into TestOracle_MatchesFrozenBaseline: "Absence is a FAILURE, not a skip …
// a skip would report green while the protection silently vanished."
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
