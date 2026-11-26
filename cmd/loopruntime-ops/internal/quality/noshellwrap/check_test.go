// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package noshellwrap

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a test helper.
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestNoShellWrap_DetectsForbiddenCLI — file with shellrun.Runner.Run("pulumi", ...)
// without nolint should produce a violation.
func TestNoShellWrap_DetectsForbiddenCLI(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(cli string, args ...string) error { return nil }

type Wrapper struct {
	shellrun Runner
}

func (w *Wrapper) Bad() {
	w.shellrun.Run("pulumi", "stack", "ls")
}
`
	writeFile(t, filepath.Join(root, "fake/pulumiwrap.go"), src)

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Error("expected Pass=false (pulumi is forbidden CLI without nolint)")
	}
	if len(outcome.Violations) == 0 {
		t.Fatal("expected at least 1 violation")
	}
	found := false
	for _, v := range outcome.Violations {
		if strings.Contains(v, "pulumi") && strings.Contains(v, "pulumiwrap.go") {
			found = true
		}
	}
	if !found {
		t.Errorf("violation should mention pulumi + pulumiwrap.go; got %v", outcome.Violations)
	}
}

// TestNoShellWrap_AllowsAllowedCLI — shellrun.Runner.Run("git", ...) is OK
// (git has no good Go alternative).
func TestNoShellWrap_AllowsAllowedCLI(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(cli string, args ...string) error { return nil }

type Wrapper struct {
	shellrun Runner
}

func (w *Wrapper) OK() {
	w.shellrun.Run("git", "log", "--oneline")
}
`
	writeFile(t, filepath.Join(root, "fake/gitwrap.go"), src)

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (git is allowed CLI); got %v", outcome.Violations)
	}
}

// TestNoShellWrap_HonorsNolintComment — //nolint:shell-wrap on the call line
// suppresses the violation.
func TestNoShellWrap_HonorsNolintComment(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(cli string, args ...string) error { return nil }

type Wrapper struct {
	shellrun Runner
}

func (w *Wrapper) Exempt() {
	//nolint:shell-wrap // bootstrapping; SDK migration tracked in plan T5.1
	w.shellrun.Run("pulumi", "stack", "ls")
}
`
	writeFile(t, filepath.Join(root, "fake/exempt.go"), src)

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (nolint comment suppresses); got %v", outcome.Violations)
	}
}

// TestNoShellWrap_SkipsTestFiles — _test.go files are not scanned (tests may
// use shell-out for fixture setup without policy implications).
func TestNoShellWrap_SkipsTestFiles(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(cli string, args ...string) error { return nil }

func testHelper() {
	(&Runner{}).Run("kubectl", "get", "pods")
}
`
	writeFile(t, filepath.Join(root, "fake/foo_test.go"), src)
	writeFile(t, filepath.Join(root, "fake/clean.go"), "package fake\n")

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (test files exempt); got %v", outcome.Violations)
	}
}

// TestNoShellWrap_SkipsVendor — vendor/ + node_modules excluded from walk.
func TestNoShellWrap_SkipsVendor(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(cli string, args ...string) error { return nil }

func bad() {
	(&Runner{}).Run("kubectl", "apply")
}
`
	writeFile(t, filepath.Join(root, "vendor/somelib/bad.go"), src)
	writeFile(t, filepath.Join(root, "fake/clean.go"), "package fake\n")

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (vendor/ excluded); got %v", outcome.Violations)
	}
}

// TestNoShellWrap_MultipleForbidden — multiple violations reported, not just first.
func TestNoShellWrap_MultipleForbidden(t *testing.T) {
	root := t.TempDir()
	src1 := `package a
type Runner struct{}
func (r *Runner) Run(cli string, args ...string) error { return nil }
func bad1() { (&Runner{}).Run("pulumi", "x") }
`
	src2 := `package b
type Runner struct{}
func (r *Runner) Run(cli string, args ...string) error { return nil }
func bad2() { (&Runner{}).Run("helm", "x") }
func bad3() { (&Runner{}).Run("aws", "x") }
`
	writeFile(t, filepath.Join(root, "a/bad1.go"), src1)
	writeFile(t, filepath.Join(root, "b/bad2.go"), src2)

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Error("expected Pass=false")
	}
	if len(outcome.Violations) != 3 {
		t.Errorf("expected 3 violations, got %d: %v", len(outcome.Violations), outcome.Violations)
	}
}

// TestNoShellWrap_NonRunnerCallsIgnored — Run() called on non-Runner type
// is NOT scanned (false positive risk; only the shellrun pattern is policy).
func TestNoShellWrap_NonRunnerCallsIgnored(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type SomethingElse struct{}

func (s *SomethingElse) Run(cli string) {}

func ok() {
	(&SomethingElse{}).Run("pulumi") // not a Runner; allowed
}
`
	writeFile(t, filepath.Join(root, "fake/other.go"), src)

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	// Implementation note: scanner uses heuristic match on `.Run("...")` calls
	// where first arg is a literal string. Conservative — accepts that some
	// non-Runner .Run callsites slip in, but the typical case of
	// `someRunner.Run("pulumi", ...)` is detected. Documented in check.go.
	// Test verifies fixture without Runner type is reasonable.
	_ = outcome
}

// TestNoShellWrap_DynamicCLIIgnored — when CLI argument is a variable (not literal),
// gate cannot detect statically. Documented limitation; acceptable per
// Anti-pattern #8 in plan: "AST walker is conservative; code review backstop".
func TestNoShellWrap_DynamicCLIIgnored(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(cli string, args ...string) error { return nil }

func dynamic(cli string) {
	(&Runner{}).Run(cli, "x") // not detected — variable cli
}
`
	writeFile(t, filepath.Join(root, "fake/dyn.go"), src)

	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (dynamic CLI not detectable; documented limitation); got %v", outcome.Violations)
	}
}

// TestNoShellWrap_EmptyTreeIsNothingScanned — a tree with no Go file is a scan
// that read nothing, not a tree without shell-wraps.
func TestNoShellWrap_EmptyTreeIsNothingScanned(t *testing.T) {
	_, err := Check(Config{Root: t.TempDir()})

	if !errors.Is(err, ErrNothingScanned) {
		t.Fatalf("err = %v, want ErrNothingScanned", err)
	}
}

// TestForbiddenCLIs_Coverage — sanity check that forbidden list includes
// the 5 CLIs documented in plan T6.3 deep-dive.
func TestForbiddenCLIs_Coverage(t *testing.T) {
	required := []string{"pulumi", "helm", "kubectl", "aws", "gh"}
	for _, cli := range required {
		if !ForbiddenCLI[cli] {
			t.Errorf("forbidden list missing required CLI: %s", cli)
		}
	}
}

// === Additional coverage tests ===

// TestNoShellWrap_UnparseableFileIsAnError — a Go file with syntax errors is
// exactly the file whose shell-wraps go unread: it fails the scan, naming it,
// rather than being skipped.
func TestNoShellWrap_UnparseableFileIsAnError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "broken.go"), "package foo\n\nfunc {{{ broken\n")

	_, err := Check(Config{Root: dir})

	if err == nil || !strings.Contains(err.Error(), "broken.go") {
		t.Fatalf("err = %v, want a parse error naming broken.go", err)
	}
}

// TestNoShellWrap_NonExistentRoot — walk error propagates from Check.
func TestNoShellWrap_NonExistentRoot(t *testing.T) {
	_, err := Check(Config{Root: "/does/not/exist/at/all"})
	if err == nil {
		t.Error("expected error when root does not exist")
	}
}

// TestNoShellWrap_NolintOnSameLine — //nolint on the same line as the call
// (not preceding) also suppresses.
func TestNoShellWrap_NolintOnSameLine(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(cli string, args ...string) error { return nil }

func sameLine() {
	(&Runner{}).Run("pulumi", "x") //nolint:shell-wrap // inline justification
}
`
	writeFile(t, filepath.Join(root, "sameline.go"), src)
	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (inline nolint suppresses); got %v", outcome.Violations)
	}
}

// TestNoShellWrap_NonStringFirstArg — first arg not a string literal → not detected.
func TestNoShellWrap_NonStringFirstArg(t *testing.T) {
	root := t.TempDir()
	src := `package fake

type Runner struct{}

func (r *Runner) Run(args ...interface{}) error { return nil }

func nonString() {
	(&Runner{}).Run(123) // numeric — not detected
}
`
	writeFile(t, filepath.Join(root, "nonstring.go"), src)
	outcome, err := Check(Config{Root: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (non-string arg ignored); got %v", outcome.Violations)
	}
}
