// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package recordstreesplit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
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

// The live tree lives under .squad/, which .gitignore excludes, so a fresh
// clone has none. Reporting "one live tree, 0 records" there is a pass over a
// tree nobody found; the gate must say it verified nothing.
func TestCheck_LiveTreeAbsent_IsNotAPass(t *testing.T) {
	root := fixture(t, nil, nil, "")

	out := Check(Config{RepoRoot: root})

	if out.Pass || !out.LiveMissing {
		t.Fatalf("an absent live tree must be reported as unverified, got %+v", out)
	}
}

// With the retired archive and its manifest both gone there is nothing frozen,
// and the outcome must say so rather than report a freeze of size zero.
func TestCheck_NoArchive_IsReportedAsAbsent(t *testing.T) {
	root := fixture(t, []string{"plans/b200-plan.md"}, nil, "")

	out := Check(Config{RepoRoot: root})

	if !out.Pass || !out.ArchiveAbsent {
		t.Fatalf("a single live tree with no archive must pass and name the absence, got %+v", out)
	}
}

// Both trees sit in gitignored directories, so which state this repository is
// in depends on the machine. The assertion follows the state instead of skipping:
// with no live tree the gate must refuse to pass; with one it must pass.
func TestCheck_ThisRepository(t *testing.T) {
	root := qtestutil.Root(t)

	out := Check(Config{RepoRoot: root})

	if _, err := os.Stat(filepath.Join(root, DefaultLiveDir)); err != nil {
		if out.Pass || !out.LiveMissing {
			t.Fatalf("with %s absent the gate must report it unverified, got %+v", DefaultLiveDir, out)
		}
		return
	}
	if !out.Pass {
		t.Fatalf("this repository's archive drifted from its freeze manifest:\n"+
			"  added:      %v\n  removed:    %v\n  violations: %v",
			out.Added, out.Removed, out.Violations)
	}
}
