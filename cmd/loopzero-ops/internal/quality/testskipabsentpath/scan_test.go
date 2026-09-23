// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testskipabsentpath

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// These are the basis paths of scanDir: how one package directory is read,
// which files count, and how the package-wide index lets a guard in a test file
// resolve a name declared elsewhere in the same directory.

func scanOne(t *testing.T, files map[string]string) (*Outcome, error) {
	t.Helper()
	root := tree(t, files)
	out := &Outcome{}
	err := scanDir(out, root, filepath.Join(root, "pkg"))
	return out, err
}

func TestScanDir_UnparseableFileIsAnErrorNamingIt(t *testing.T) {
	_, err := scanOne(t, map[string]string{"pkg/broken_test.go": "package p\nfunc {"})

	if err == nil || !strings.Contains(err.Error(), "pkg/broken_test.go") {
		t.Fatalf("err = %v, want a parse error naming the file", err)
	}
}

func TestScanDir_DirectoryWithoutGoFilesCountsNothing(t *testing.T) {
	out, err := scanOne(t, map[string]string{"pkg/README.md": "not go"})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.FilesScanned != 0 || out.SkipCalls != 0 {
		t.Errorf("scanned %d files, %d skips; want 0 and 0", out.FilesScanned, out.SkipCalls)
	}
}

func TestScanDir_UnreadableDirectoryIsAnError(t *testing.T) {
	out := &Outcome{}

	if err := scanDir(out, t.TempDir(), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("reading a directory that does not exist must be an error")
	}
}

func TestScanDir_CountsOnlyTestFilesAndIgnoresSubdirectories(t *testing.T) {
	out, err := scanOne(t, map[string]string{
		"pkg/helper.go":         "package p\n",
		"pkg/a_test.go":         guardedTest("p", "TestA", "pkg/helper.go"),
		"pkg/nested/b_test.go":  guardedTest("nested", "TestB", "pkg/helper.go"),
		"pkg/notes_test.go.txt": "not go",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.FilesScanned != 1 {
		t.Errorf("FilesScanned = %d, want 1: the non-test file is parsed for the index but not counted, "+
			"and a subdirectory is a different package", out.FilesScanned)
	}
	if len(out.Sites) != 1 || out.Sites[0].Func != "TestA" {
		t.Errorf("sites = %+v, want exactly TestA", out.Sites)
	}
}

func TestScanDir_HelperInANonTestFileResolvesTheGuardPath(t *testing.T) {
	out, err := scanOne(t, map[string]string{
		"pkg/helper.go": "package p\n\nfunc cliDir() string { return \"cli/src\" }\n",
		"pkg/a_test.go": `package p

import (
	"os"
	"testing"
)

func TestA(t *testing.T) {
	if _, err := os.Stat(cliDir()); os.IsNotExist(err) {
		t.Skip("absent")
	}
	t.Log("x")
}
`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Sites) != 1 || out.Sites[0].Path != "pkg/cli/src" || out.Sites[0].Verdict != Absent {
		t.Fatalf("sites = %+v, want one ABSENT site on pkg/cli/src (relative to the package directory) resolved through the helper", out.Sites)
	}
}

func TestScanDir_ConstantInAnotherFileResolvesTheGuardPath(t *testing.T) {
	out, err := scanOne(t, map[string]string{
		"pkg/consts.go": "package p\n\nconst cliDir = \"cli/src\"\n\nvar a, b = 1\n",
		"pkg/a_test.go": `package p

import (
	"os"
	"testing"
)

func TestA(t *testing.T) {
	if _, err := os.Stat(cliDir); os.IsNotExist(err) {
		t.Skip("absent")
	}
	t.Log("x")
}
`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Sites) != 1 || out.Sites[0].Path != "pkg/cli/src" {
		t.Fatalf("sites = %+v, want one site on pkg/cli/src resolved through the package constant "+
			"(a var spec with fewer values than names must be skipped, not crash)", out.Sites)
	}
}

func TestScanDir_MethodPredicateIsIndexedByName(t *testing.T) {
	out, err := scanOne(t, map[string]string{
		"pkg/h.go": `package p

import "os"

type h struct{}

func (h) present() bool { _, err := os.Stat("cli/src"); return err == nil }
`,
		"pkg/a_test.go": `package p

import "testing"

func TestA(t *testing.T) {
	var x h
	if !x.present() {
		t.Skip("absent")
	}
	t.Log("x")
}
`,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out.Sites) != 1 || out.Sites[0].Path != "pkg/cli/src" {
		t.Fatalf("sites = %+v, want the method's probe read through the method index", out.Sites)
	}
}

func TestScanDir_SkipCallsAreCountedInTestFilesOnly(t *testing.T) {
	out, err := scanOne(t, map[string]string{
		"pkg/helper.go": "package p\n\nimport \"testing\"\n\nfunc skipAlways(t *testing.T) { t.Skip(\"x\") }\n",
		"pkg/a_test.go": "package p\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { t.Skip(\"bare\") }\n",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.SkipCalls != 1 {
		t.Errorf("SkipCalls = %d, want 1 (the test file's)", out.SkipCalls)
	}
}

func TestScanDir_RejectsAPathThatIsAFile(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := scanDir(&Outcome{}, root, file); err == nil {
		t.Fatal("scanning a regular file as a directory must be an error")
	}
}
