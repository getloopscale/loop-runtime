// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package divergencematrix

import (
	"os"
	"path/filepath"
	"strings"
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

// fixtureRepo builds a repo with a matrix file + scan dirs.
func fixtureRepo(t *testing.T, matrix string, codeFiles map[string]string) string {
	t.Helper()
	root := t.TempDir()
	matrixPath := filepath.Join(root, ".claude/knowledge-base/concepts/operations/local-prod-divergence-matrix.md")
	writeFile(t, matrixPath, matrix)
	for rel, content := range codeFiles {
		writeFile(t, filepath.Join(root, rel), content)
	}
	return root
}

// TestCheck_AllMarkersHaveMatrixEntry — Pass when every code LP-NNN marker has
// a matrix entry and every OPEN matrix entry has a code marker.
func TestCheck_AllMatched(t *testing.T) {
	root := fixtureRepo(t,
		`| LP-001 | foo | OPEN |
| LP-002 | bar | RESOLVED |
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
		`| LP-001 | foo | OPEN |
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
		`| LP-001 | foo | OPEN |
| LP-042 | orphan | OPEN |
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
		`| LP-001 | foo | OPEN |
| LP-050 | done | RESOLVED |
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

// TestCheck_MissingMatrix_Errors — no matrix file → error (exit 2 in bash).
func TestCheck_MissingMatrix_Errors(t *testing.T) {
	root := t.TempDir()
	_, err := Check(Config{RepoRoot: root})
	if err == nil {
		t.Error("expected error when matrix file missing")
	}
}

// TestCheck_ExcludesVendorNodeModules — markers in node_modules/dist/vendor ignored.
func TestCheck_ExcludesVendorNodeModules(t *testing.T) {
	root := fixtureRepo(t,
		`| LP-001 | foo | OPEN |
`,
		map[string]string{
			"api/main.go":                  "// LOCAL ONLY (LP-001)\n",
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

// TestExtractMatrixIDs — parses LP-NNN ids from matrix content.
func TestExtractMatrixIDs(t *testing.T) {
	ids := extractMatrixIDs("| LP-001 | x | OPEN |\n| LP-002 | y | RESOLVED |\nrandom LP-003 inline\n")
	if !ids["LP-001"] || !ids["LP-002"] || !ids["LP-003"] {
		t.Errorf("expected LP-001/002/003, got %v", ids)
	}
}
