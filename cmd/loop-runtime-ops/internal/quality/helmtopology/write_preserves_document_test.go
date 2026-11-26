// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package helmtopology

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The generated index is spliced between markers inside a document that is written and
// maintained by hand. It used to be the whole document: writeDoc built header + table +
// footer and replaced the file, which is correct for an output nobody else writes and
// destructive for `docs/architecture.md`, one of the six living documents the 2026-09-11
// consolidation left behind.
//
// Measured 2026-09-16: running --write against it took the document from 217 lines and
// thirteen sections to 78 lines and one table, and deleted the sibling `pkg-usage-matrix`
// section with it. The gate had been failing for weeks because the markers were missing,
// so the instruction it printed on every run — "run --write and commit it" — was an
// instruction to destroy the file.
//
// This asserts the property that makes the two gates able to share a document: whatever
// sits outside the markers survives the write.
func TestWriteKeepsEverythingOutsideTheMarkers(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "architecture.md")

	const before = "# Architecture\n\n## A hand-written section\n\nProse nobody generated.\n\n"
	const after = "\n## Another hand-written section\n\nMore prose.\n\n<!-- USAGE-MATRIX-START -->\nsibling table\n<!-- USAGE-MATRIX-END -->\n"
	original := before + bodyStart + "\nstale table\n" + bodyEnd + after

	if err := os.WriteFile(doc, []byte(original), 0o600); err != nil {
		t.Fatalf("seed doc: %v", err)
	}
	if err := writeDoc(doc, "| Chart | Cluster(s) |\n|---|---|\n| `x` | core |\n"); err != nil {
		t.Fatalf("writeDoc: %v", err)
	}

	got, err := os.ReadFile(doc)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	s := string(got)

	for _, keep := range []string{
		"## A hand-written section",
		"Prose nobody generated.",
		"## Another hand-written section",
		"<!-- USAGE-MATRIX-START -->",
		"sibling table",
	} {
		if !strings.Contains(s, keep) {
			t.Errorf("writeDoc deleted content outside its markers: %q is gone", keep)
		}
	}
	if strings.Contains(s, "stale table") {
		t.Error("writeDoc kept the previous table; it must replace what is between the markers")
	}
	if !strings.Contains(s, "| `x` | core |") {
		t.Error("writeDoc did not write the fresh table")
	}
}

// A document with no markers is not a document to overwrite. Refusing names what to do;
// rewriting it silently is how the file above was lost.
func TestWriteRefusesADocumentWithNoMarkers(t *testing.T) {
	dir := t.TempDir()
	doc := filepath.Join(dir, "architecture.md")
	const original = "# Architecture\n\nA document this gate does not own.\n"
	if err := os.WriteFile(doc, []byte(original), 0o600); err != nil {
		t.Fatalf("seed doc: %v", err)
	}

	err := writeDoc(doc, "| Chart |\n")
	if err == nil {
		t.Fatal("writeDoc accepted a document with no markers — it must refuse rather than replace it")
	}

	got, _ := os.ReadFile(doc)
	if string(got) != original {
		t.Error("writeDoc modified a document it had just refused")
	}
}
