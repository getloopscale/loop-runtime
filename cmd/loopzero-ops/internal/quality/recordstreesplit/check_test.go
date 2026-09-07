// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package recordstreesplit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a two-tree layout under a temp root and returns the root.
//
// live and archive are lists of paths relative to their own tree; manifest is
// the raw content written to the freeze manifest, or "" to leave it absent.
func fixture(t *testing.T, live, archive []string, manifest string) string {
	t.Helper()
	root := t.TempDir()
	write := func(base string, rels []string) {
		for _, rel := range rels {
			full := filepath.Join(root, base, rel)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", full, err)
			}
			if err := os.WriteFile(full, []byte("# record\n"), 0o644); err != nil {
				t.Fatalf("write %s: %v", full, err)
			}
		}
	}
	write(DefaultLiveDir, live)
	write(DefaultArchiveDir, archive)
	if manifest != "" {
		full := filepath.Join(root, DefaultManifestPath)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir manifest dir: %v", err)
		}
		if err := os.WriteFile(full, []byte(manifest), 0o644); err != nil {
			t.Fatalf("write manifest: %v", err)
		}
	}
	return root
}

const manifestHeader = "# frozen archive manifest\n# see .claude/rules/records-location.md\n\n"

// The split reopening is the defect B-166 names: the retired tree starts
// carrying `.md` again while the canonical tree is live, and a reader who
// checks one reports absence where evidence exists.
func TestCheck_ArchiveGainsRecord_Fails(t *testing.T) {
	root := fixture(t,
		[]string{"plans/b200-plan.md"},
		[]string{"plans/old-plan.md", "plans/written-today.md"},
		manifestHeader+"plans/old-plan.md\n")

	out := Check(Config{RepoRoot: root})

	if out.Pass {
		t.Fatalf("a new .md in the frozen archive must FAIL: %+v", out)
	}
	if !strings.Contains(strings.Join(out.Added, "\n"), "written-today.md") {
		t.Fatalf("the added record must be named; got Added=%v", out.Added)
	}
}

func TestCheck_ArchiveMatchesManifest_Passes(t *testing.T) {
	root := fixture(t,
		[]string{"plans/b200-plan.md", "reviews/b200-review.md"},
		[]string{"plans/old-plan.md", "decisions/2026-05-13-adr.md"},
		manifestHeader+"decisions/2026-05-13-adr.md\nplans/old-plan.md\n")

	out := Check(Config{RepoRoot: root})

	if !out.Pass {
		t.Fatalf("a frozen archive that matches its manifest must PASS: %+v", out)
	}
}

// The archive is evidence, not clutter: every file in it exists in no other
// tree (measured 2026-09-04 — zero content overlap across 75 + 155 files).
// Deleting one destroys the only copy, so the freeze runs in both directions.
func TestCheck_ArchivedRecordDeleted_Fails(t *testing.T) {
	root := fixture(t,
		[]string{"plans/b200-plan.md"},
		[]string{"plans/old-plan.md"},
		manifestHeader+"decisions/2026-05-13-adr.md\nplans/old-plan.md\n")

	out := Check(Config{RepoRoot: root})

	if out.Pass {
		t.Fatalf("a deleted archived record must FAIL: %+v", out)
	}
	if !strings.Contains(strings.Join(out.Removed, "\n"), "2026-05-13-adr.md") {
		t.Fatalf("the removed record must be named; got Removed=%v", out.Removed)
	}
}

// The DoD allows the losing tree to be REMOVED instead of frozen. A repo that
// took that path has one tree and no manifest, and the gate must not invent a
// failure for it.
func TestCheck_NoSecondTree_Passes(t *testing.T) {
	root := fixture(t, []string{"plans/b200-plan.md"}, nil, "")

	out := Check(Config{RepoRoot: root})

	if !out.Pass {
		t.Fatalf("a repo with a single records tree must PASS: %+v", out)
	}
}

// An archive with no manifest is the unguarded split — exactly the state
// B-166 found. Passing here would make the gate report health for the disease.
func TestCheck_ArchiveWithoutManifest_Fails(t *testing.T) {
	root := fixture(t,
		[]string{"plans/b200-plan.md"},
		[]string{"plans/old-plan.md"},
		"")

	out := Check(Config{RepoRoot: root})

	if out.Pass {
		t.Fatalf("a second tree with no freeze manifest must FAIL: %+v", out)
	}
	if !strings.Contains(strings.Join(out.Violations, "\n"), DefaultManifestPath) {
		t.Fatalf("the missing manifest must be named; got Violations=%v", out.Violations)
	}
}

// Comments and blank lines are how the manifest explains itself; they must not
// be read as record paths.
func TestCheck_ManifestCommentsIgnored(t *testing.T) {
	root := fixture(t,
		[]string{"plans/b200-plan.md"},
		[]string{"plans/old-plan.md"},
		"# a comment\n\n   \nplans/old-plan.md\n# trailing note\n")

	out := Check(Config{RepoRoot: root})

	if !out.Pass {
		t.Fatalf("comments and blanks must not count as records: %+v", out)
	}
}

// Non-.md files (JSON snapshots, JSONL event logs) are not the records the
// split is about, and freezing them would make the gate fail on housekeeping.
func TestCheck_NonMarkdownIgnored(t *testing.T) {
	root := fixture(t,
		[]string{"plans/b200-plan.md"},
		[]string{"plans/old-plan.md", "discoveries/snapshots/baseline.json"},
		manifestHeader+"plans/old-plan.md\n")

	out := Check(Config{RepoRoot: root})

	if !out.Pass {
		t.Fatalf("a non-.md file in the archive must not FAIL the freeze: %+v", out)
	}
}

// The measurement that closed B-166, run against the repository itself: the
// committed manifest must describe the archive as it actually is on disk.
func TestCheck_ThisRepository_ArchiveIsFrozen(t *testing.T) {
	root := filepath.Join("..", "..", "..", "..", "..")
	// Absence is a FAILURE, not a skip. A gate about a records tree that cannot
	// find the records tree must say so — skipping would report green for the
	// exact shape this item exists to catch (same lesson as B-006).
	if _, err := os.Stat(filepath.Join(root, DefaultLiveDir)); err != nil {
		t.Fatalf("%s not reachable from the test binary (%v) — the live records tree must exist", DefaultLiveDir, err)
	}

	out := Check(Config{RepoRoot: root})

	if !out.Pass {
		t.Fatalf("this repository's archive drifted from its freeze manifest:\n"+
			"  added:      %v\n  removed:    %v\n  violations: %v",
			out.Added, out.Removed, out.Violations)
	}
}
