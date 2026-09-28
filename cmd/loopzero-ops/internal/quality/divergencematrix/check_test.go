// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package divergencematrix

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
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

// fixtureRepo builds a repo with a matrix file + scan dirs.
func fixtureRepo(t *testing.T, matrix string, codeFiles map[string]string) string {
	t.Helper()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, MatrixRelPath),
		"# Operations\n\n## Dev loop\n\nNot the matrix: LP-777 mentioned in prose.\n\n"+
			MatrixHeading+"\n\n| LP-NNN | Status | Code site(s) | What diverges |\n|---|---|---|---|\n"+
			matrix+"\n## Related\n\n| LP-555 | OPEN | a table in another section |\n")
	for rel, content := range codeFiles {
		writeFile(t, filepath.Join(root, rel), content)
	}
	return root
}

// TestCheck_AllMarkersHaveMatrixEntry — Pass when every code LP-NNN marker has
// a matrix entry and every OPEN matrix entry has a code marker.
func TestCheck_AllMatched(t *testing.T) {
	root := fixtureRepo(t,
		`| LP-001 | OPEN | foo | x |
| LP-002 | RESOLVED | bar | x |
`,
		map[string]string{
			"api/main.go": "// LOCAL ONLY (LP-001) — dev shortcut\n",
		})
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true, got violations: %v", outcome.Violations)
	}
}

// TestCheck_UnknownID — code marker references LP-NNN not in matrix → violation.
func TestCheck_UnknownID(t *testing.T) {
	root := fixtureRepo(t,
		`| LP-001 | OPEN | foo | x |
`,
		map[string]string{
			"api/main.go": "// LOCAL ONLY (LP-999) — typo\n",
		})
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Error("expected Pass=false (LP-999 unknown)")
	}
	found := false
	for _, v := range outcome.Violations {
		if strings.Contains(v, "UNKNOWN_ID") && strings.Contains(v, "LP-999") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected UNKNOWN_ID LP-999, got %v", outcome.Violations)
	}
}

// TestCheck_OrphanMatrixEntry — matrix OPEN entry with no code marker → violation.
func TestCheck_OrphanMatrixEntry(t *testing.T) {
	root := fixtureRepo(t,
		`| LP-001 | OPEN | foo | x |
| LP-042 | OPEN | orphan | x |
`,
		map[string]string{
			"api/main.go": "// LOCAL ONLY (LP-001)\n",
		})
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Error("expected Pass=false (LP-042 orphan)")
	}
	found := false
	for _, v := range outcome.Violations {
		if strings.Contains(v, "ORPHAN_MATRIX_ENTRY") && strings.Contains(v, "LP-042") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected ORPHAN_MATRIX_ENTRY LP-042, got %v", outcome.Violations)
	}
}

// TestCheck_ResolvedOrphanIgnored — RESOLVED matrix entry without code marker
// is NOT a violation (only OPEN entries require a live marker).
func TestCheck_ResolvedOrphanIgnored(t *testing.T) {
	root := fixtureRepo(t,
		`| LP-001 | OPEN | foo | x |
| LP-050 | RESOLVED | done | x |
`,
		map[string]string{
			"api/main.go": "// LOCAL ONLY (LP-001)\n",
		})
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (LP-050 RESOLVED needs no marker), got %v", outcome.Violations)
	}
}

// A missing matrix is a failed gate, not a gate error: the markers in the
// code have nothing to be checked against.
func TestCheck_MissingMatrixDocument_IsAViolation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "api/main.go"), "// LOCAL ONLY (LP-001)\n")
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass || !strings.Contains(strings.Join(outcome.Violations, "\n"), "MATRIX_MISSING") {
		t.Fatalf("want a MATRIX_MISSING violation, got %+v", outcome)
	}
}

func TestCheck_MissingMatrixSection_IsAViolation(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, MatrixRelPath), "# Operations\n\n## Dev loop\n")
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass || !strings.Contains(strings.Join(outcome.Violations, "\n"), "MATRIX_MISSING") {
		t.Fatalf("want a MATRIX_MISSING violation, got %+v", outcome)
	}
}

// Only rows of the matrix section count: an LP id mentioned elsewhere in the
// operations document is not a matrix entry.
func TestCheck_IDsOutsideTheSectionAreNotMatrixEntries(t *testing.T) {
	root := fixtureRepo(t, "| LP-001 | OPEN | a | b |\n", map[string]string{
		"api/main.go":  "// LOCAL ONLY (LP-001)\n",
		"api/other.go": "// LOCAL ONLY (LP-777)\n",
	})
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if outcome.Pass {
		t.Fatal("LP-777 appears only in prose outside the matrix section; it must be UNKNOWN_ID")
	}
	for _, v := range outcome.Violations {
		if strings.Contains(v, "LP-555") {
			t.Fatalf("an OPEN row in another section is not a matrix entry: %v", outcome.Violations)
		}
	}
}

// An unreadable source file is an unaudited file, never a file without markers.
func TestCheck_UnreadableSourceFile_FailsAsGateError(t *testing.T) {
	root := fixtureRepo(t, "| LP-001 | OPEN | a | b |\n", map[string]string{
		"api/main.go": "// LOCAL ONLY (LP-001)\n",
		"api/x.go":    "package x\n",
	})
	p := filepath.Join(root, "api/x.go")
	// A dangling symlink is unreadable for every uid, root included.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(filepath.Dir(p), "does-not-exist"), p); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(Config{RepoRoot: root}); err == nil {
		t.Fatal("want an error for an unreadable source file, got nil")
	}
}

// LIVE: the real matrix and the real markers agree.
func TestCheck_LIVE_RepositoryMatrixIsConsistent(t *testing.T) {
	outcome, err := Check(Config{RepoRoot: qtestutil.MustRepoRoot()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Fatalf("matrix and markers disagree: %v", outcome.Violations)
	}
}

// TestCheck_ExcludesVendorNodeModules — markers in node_modules/dist/vendor ignored.
func TestCheck_ExcludesVendorNodeModules(t *testing.T) {
	root := fixtureRepo(t,
		`| LP-001 | OPEN | foo | x |
`,
		map[string]string{
			"api/main.go":                   "// LOCAL ONLY (LP-001)\n",
			"cli/src/node_modules/x/dep.go": "// LOCAL ONLY (LP-888)\n", // must be ignored
		})
	outcome, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !outcome.Pass {
		t.Errorf("expected Pass=true (node_modules LP-888 ignored), got %v", outcome.Violations)
	}
}

func TestParseMatrixRows(t *testing.T) {
	rows := parseMatrixRows("| LP-001 | OPEN | x |\n| LP-002 | RESOLVED | y |\nrandom LP-003 inline\n")
	if rows["LP-001"] != "OPEN" || rows["LP-002"] != "RESOLVED" || len(rows) != 2 {
		t.Errorf("want LP-001 OPEN and LP-002 RESOLVED only, got %v", rows)
	}
}
