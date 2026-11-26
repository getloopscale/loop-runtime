// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package skiplink

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestCheck_SkipWithURL_Pass — t.Skip with nearby issue URL passes.
func TestCheck_SkipWithURL_Pass(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  // https://github.com/getloopscale/loop-runtime/issues/42\n  t.Skip(\"pending\")\n}\n")
	o, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (skip has URL), got %v", o.NewViolations)
	}
}

// TestCheck_SkipNoURL_Fail — t.Skip without URL fails.
func TestCheck_SkipNoURL_Fail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  t.Skip(\"no reason\")\n}\n")
	o, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (skip without URL)")
	}
	if len(o.NewViolations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.NewViolations)
	}
}

// TestCheck_SkipFileNameExempt — *_skip_test.go is exempt.
func TestCheck_SkipFileNameExempt(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x_skip_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  t.Skip(\"intentional\")\n}\n")
	o, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (*_skip_test.go exempt), got %v", o.NewViolations)
	}
}

// TestCheck_TSSkipNoURL_Fail — it.skip without URL fails.
func TestCheck_TSSkipNoURL_Fail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.test.ts"),
		"it.skip('x', () => {});\n")
	o, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (it.skip without URL)")
	}
}

// TestCheck_TSXitNoURL_Fail — xit(...) without URL fails.
func TestCheck_TSXitNoURL_Fail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "b.spec.ts"),
		"xit('flaky', () => {});\n")
	o, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (xit without URL)")
	}
}

// TestCheck_BaselineGrandfathers — violation in baseline is suppressed.
func TestCheck_BaselineGrandfathers(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "legacy_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  t.Skip(\"old\")\n}\n")
	baseline := filepath.Join(root, "baseline.txt")
	// Key = relpath TAB normalized skip line (not relpath:LINE — see baselineKey).
	writeFile(t, baseline, "# grandfathered\nlegacy_test.go\tt.Skip(\"old\")\n")
	o, err := Check(Config{TargetDir: root, BaselineFile: baseline})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (baseline grandfathers), got %v", o.NewViolations)
	}
	if o.BaselineCount != 1 {
		t.Errorf("expected baseline count 1, got %d", o.BaselineCount)
	}
}

// TestCheck_NewViolationNotInBaseline_Fail — a violation not in baseline fails.
func TestCheck_NewViolationNotInBaseline_Fail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "new_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  t.Skip(\"new\")\n}\n")
	baseline := filepath.Join(root, "baseline.txt")
	writeFile(t, baseline, "other_test.go:99\n")
	o, err := Check(Config{TargetDir: root, BaselineFile: baseline})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (new violation not grandfathered)")
	}
}

// TestCheck_VendorExcluded — node_modules/vendor excluded.
func TestCheck_VendorExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "node_modules/x/dep.test.ts"),
		"it.skip('x', () => {});\n")
	o, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (node_modules excluded), got %v", o.NewViolations)
	}
}

// TestCheck_NoSkips_Pass — clean repo passes.
func TestCheck_NoSkips_Pass(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  // ok\n}\n")
	o, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (no skips), got %v", o.NewViolations)
	}
}

// TestParseBaseline — strips comments + blanks.
func TestParseBaseline(t *testing.T) {
	keys := parseBaseline("# header\n\nfoo_test.go:10\n  \nbar_test.go:20\n")
	if !keys["foo_test.go:10"] || !keys["bar_test.go:20"] {
		t.Errorf("expected foo+bar keys, got %v", keys)
	}
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d: %v", len(keys), keys)
	}
}

// A baselined skip that MOVES because unrelated lines were added above it must
// stay grandfathered. Keying the baseline on relpath:LINE makes every edit
// above a skip re-report it as new: measured 2026-08-06, the 145-entry baseline
// was matching almost nothing and the gate reported 189 "NEW" skips, most of
// them the same conditional environment skips it had already accepted.
func TestBaselineSurvivesLineShift(t *testing.T) {
	root := t.TempDir()
	src := "package a\nfunc TestX(t *testing.T) {\n  t.Skip(\"needs a cluster\")\n}\n"
	writeFile(t, filepath.Join(root, "a_test.go"), src)

	first, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(first.NewViolations) != 1 {
		t.Fatalf("expected the skip to be reported once, got %v", first.NewViolations)
	}

	// Freeze it, then push it down with an unrelated comment.
	baselineFile := filepath.Join(root, "baseline.txt")
	writeFile(t, baselineFile, first.Keys[0]+"\n")
	writeFile(t, filepath.Join(root, "a_test.go"),
		"package a\n\n// an unrelated comment added above\nfunc TestX(t *testing.T) {\n  t.Skip(\"needs a cluster\")\n}\n")

	second, err := Check(Config{TargetDir: root, BaselineFile: baselineFile})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(second.NewViolations) != 0 {
		t.Errorf("a baselined skip that only moved must stay grandfathered, got %v", second.NewViolations)
	}
}

// Changing the skip's own text is a different skip, and must be reported.
// Otherwise the content key would grandfather anything in the same file.
func TestBaselineDoesNotCoverAChangedSkip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  t.Skip(\"needs a cluster\")\n}\n")
	first, err := Check(Config{TargetDir: root, IgnoreBaseline: true})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	baselineFile := filepath.Join(root, "baseline.txt")
	writeFile(t, baselineFile, first.Keys[0]+"\n")

	writeFile(t, filepath.Join(root, "a_test.go"),
		"package a\nfunc TestX(t *testing.T) {\n  t.Skip(\"disabled while we rewrite it\")\n}\n")
	second, err := Check(Config{TargetDir: root, BaselineFile: baselineFile})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if len(second.NewViolations) != 1 {
		t.Errorf("a skip whose reason changed is a new skip; got %v", second.NewViolations)
	}
}
