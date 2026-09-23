// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package mockbudget

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

// TestCheck_UnderBudget_Pass — file with ≤budget mocks passes.
func TestCheck_UnderBudget_Pass(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a_test.go"),
		"package a\nvar x = mock.NewThing()\nvar y = mock.NewOther()\n")
	o, err := Check(Config{TargetDir: root, Budget: 5})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (2 mocks ≤ 5), got %v", o.Violations)
	}
}

// TestCheck_OverBudget_Fail — file over budget fails.
func TestCheck_OverBudget_Fail(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "heavy.test.ts"),
		"jest.mock('a')\njest.mock('b')\njest.mock('c')\n")
	o, err := Check(Config{TargetDir: root, Budget: 2})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (3 mocks > 2)")
	}
	if len(o.Violations) != 1 {
		t.Errorf("expected 1 violation, got %v", o.Violations)
	}
}

// TestCheck_OptOutComment_Raises — `// mock-budget: N` raises the file's budget.
func TestCheck_OptOutComment_Raises(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "x_test.go"),
		"// mock-budget: 10 — heavy integration boundary\npackage x\n"+
			"var a = mock.NewA()\nvar b = mock.NewB()\nvar c = mock.NewC()\n")
	o, err := Check(Config{TargetDir: root, Budget: 2})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (opt-out budget 10), got %v", o.Violations)
	}
}

// TestCheck_TSViMock — vi.mock counts too.
func TestCheck_TSViMock(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "v.spec.ts"),
		"vi.mock('a')\nvi.mock('b')\n")
	o, err := Check(Config{TargetDir: root, Budget: 1})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected Fail (2 vi.mock > 1)")
	}
}

// TestCheck_InlineJestFnNotCounted — inline jest.fn() callbacks not counted.
func TestCheck_InlineJestFnNotCounted(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "inline.test.ts"),
		"it('x', () => { setInterval(jest.fn(), 100); jest.fn(); });\n")
	o, err := Check(Config{TargetDir: root, Budget: 0})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (inline jest.fn not counted), got %v", o.Violations)
	}
}

// TestCheck_NonTestFilesIgnored — non-test files not scanned.
func TestCheck_NonTestFilesIgnored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "prod.go"), "jest.mock('x')\njest.mock('y')\n")
	o, err := Check(Config{TargetDir: root, Budget: 0})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (prod.go not a test file), got %v", o.Violations)
	}
}

// TestCheck_VendorExcluded — vendor/node_modules excluded.
func TestCheck_VendorExcluded(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "node_modules/x/dep.test.ts"),
		"jest.mock('a')\njest.mock('b')\njest.mock('c')\n")
	o, err := Check(Config{TargetDir: root, Budget: 1})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected Pass (node_modules excluded), got %v", o.Violations)
	}
}

// TestCountMocks_GoPatterns — gomock + mock.New counted at top level.
func TestCountMocks_GoPatterns(t *testing.T) {
	src := "package a\n" +
		"var m = mock.NewController()\n" +
		"func TestX(t *testing.T) {\n  c := gomock.NewController(t)\n}\n"
	n := countGoMocks(src)
	if n < 1 {
		t.Errorf("expected ≥1 Go mock, got %d", n)
	}
}

// TestReadOptOut — parses `// mock-budget: N`.
func TestReadOptOut(t *testing.T) {
	if got := readOptOutBudget("// mock-budget: 7 — reason\npackage x\n"); got != 7 {
		t.Errorf("expected 7, got %d", got)
	}
	if got := readOptOutBudget("package x\nno annotation\n"); got != -1 {
		t.Errorf("expected -1 (no annotation), got %d", got)
	}
}

// A test file the gate cannot read used to be "not a test file" and pass; the
// budget cannot be vouched for over a file nobody read.
func TestCheck_UnreadableTestFileIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "missing-target"), filepath.Join(root, "a_test.go")); err != nil {
		t.Fatal(err)
	}

	if _, err := Check(Config{TargetDir: root, Budget: 5}); err == nil {
		t.Fatal("an unreadable test file must be an error")
	}
}
