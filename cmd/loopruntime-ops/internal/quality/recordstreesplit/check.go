// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package recordstreesplit implements the `records-tree-split` gate: the
// repository keeps ONE live records tree, and the retired one stays frozen.
//
// Why this needs a gate (B-166, measured 2026-09-04 on this repository):
//
//	.claude/records/          75 tracked files, first written 2026-09-03
//	.claude/knowledge-base/  155 tracked files, last written 2026-08-28
//	content overlap between the two trees: ZERO — no file in either tree has a
//	byte-identical twin in the other, and no relative path occurs in both.
//
// Two trees never error. They just accumulate half the truth, and a reader who
// checks the wrong one reports absence where evidence exists — the failure
// `.claude/rules/records-location.md` measured across three consumers, where an
// audit reported "0 implementations, 0 reviews, 0 releases" for a repository
// that had 6, 12 and 8.
//
// The disposition this gate enforces:
//
//   - `.claude/records/` is the live tree. `rules/records-location.md` states it
//     without a per-project option ("is canonical. Always."), the kit's own
//     boundary hook lists `records/` as project-owned and does not list
//     `knowledge-base/`, and every cycle artifact since 2026-09-03 landed there.
//   - `.claude/knowledge-base/` is a FROZEN archive, not deleted. Nothing in it
//     is duplicated, so every file is the only copy of its evidence, and 59
//     path-shaped citations elsewhere in the repository still resolve into it.
//     Moving those 149 records would convert live citations into dead ones —
//     the defect class this repository files items about.
//
// So the guard is not "one tree exists" (false: two do, by design) but "only
// one tree is WRITTEN". The archive's `.md` set is frozen in a manifest; a file
// appearing there means the split reopened, and a file disappearing means
// evidence was destroyed. Both fail.
//
// The gate is hermetic: it reads the filesystem, never git, and needs no refs.
package recordstreesplit

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

// DefaultLiveDir is the canonical records tree, per rules/records-location.md.
//
// It was `.claude/records` until 2026-09-09, when that rule moved the write root to
// `<project>/.squad/`: a project must not share a directory with an installed
// dependency, because `.claude/` is gitignored and replaced whole by the next
// install.
//
// `.squad/records/` is gitignored too (`.squad/*`), so a fresh clone has no live
// tree. The gate reports that as LiveMissing — unverified — never as a pass over
// a tree it did not find.
const DefaultLiveDir = ".squad/records"

// DefaultArchiveDir is the tree retired on 2026-09-03 and frozen by this gate.
const DefaultArchiveDir = ".claude/knowledge-base"

// DefaultManifestPath is the freeze manifest: every `.md` the archive held at
// the moment it was frozen, one path per line, relative to the archive root.
//
// `.claude/rules/*.txt` is where this repository already keeps the inputs its
// gates read (`code-quality-baseline.txt`, `deps-audit-allowlist.txt`), and the
// kit's boundary hook (`.claude/hooks/boundary-check.py`) lists `rules/*.txt`
// among the paths a consumer owns — so a deliberate re-freeze stays writable.
// It is deliberately NOT under `docs/operations/baselines/`: `docs/README.md`
// classifies that tree as audit trail, "never edited to match present state",
// and B-006 already moved the gate-set oracle out of it for that exact reason.
const DefaultManifestPath = ".claude/rules/records-archive-frozen.txt"

// Config drives the gate. Zero values select the defaults above, which is what
// the wired invocation uses; the fields exist so the tests can build fixtures.
type Config struct {
	RepoRoot   string
	LiveDir    string
	ArchiveDir string
	Manifest   string
}

func (c Config) resolve() (root, live, archive, manifest string) {
	root = c.RepoRoot
	if root == "" {
		root = "."
	}
	live, archive, manifest = c.LiveDir, c.ArchiveDir, c.Manifest
	if live == "" {
		live = DefaultLiveDir
	}
	if archive == "" {
		archive = DefaultArchiveDir
	}
	if manifest == "" {
		manifest = DefaultManifestPath
	}
	return root, live, archive, manifest
}

// Outcome captures the result. Added and Removed are archive-relative paths;
// Violations carries the configuration-level failures that are not about one
// specific record.
type Outcome struct {
	Pass bool
	// LiveMissing means the live tree does not exist on this machine, so nothing
	// was verified. Pass is false; it is not a violation either.
	LiveMissing bool
	// ArchiveAbsent means neither the retired archive nor its manifest exists:
	// nothing is frozen, and FrozenSize is zero because there is no freeze.
	ArchiveAbsent bool
	LiveCount     int      // `.md` files in the live tree — reported, never bounded
	FrozenSize    int      // records the manifest declares
	Added         []string // in the archive, absent from the manifest
	Removed       []string // in the manifest, absent from the archive
	Violations    []string
}

// Verdict is Skip when the live tree is absent on this machine — the records
// root is gitignored, so every fresh clone lacks it (owner decision,
// 2026-09-26) — and otherwise Pass when the check passed and Fail when it did not.
func (o Outcome) Verdict() verdict.Verdict {
	if o.LiveMissing {
		return verdict.Skip
	}
	return verdict.FromPass(o.Pass)
}

// Check runs the gate.
func Check(cfg Config) *Outcome {
	root, liveRel, archiveRel, manifestRel := cfg.resolve()
	out := &Outcome{Pass: true}

	if !dirExists(filepath.Join(root, liveRel)) {
		out.Pass = false
		out.LiveMissing = true
		return out
	}
	live, err := markdownSet(filepath.Join(root, liveRel))
	if err != nil {
		out.Pass = false
		out.Violations = append(out.Violations,
			fmt.Sprintf("cannot read the live records tree %s: %v", liveRel, err))
		return out
	}
	out.LiveCount = len(live)

	archivePath := filepath.Join(root, archiveRel)
	archiveExists := dirExists(archivePath)
	manifestPath := filepath.Join(root, manifestRel)
	manifestExists := fileExists(manifestPath)

	// The losing tree removed outright is the DoD's other allowed disposition:
	// one tree, nothing to freeze, nothing to guard.
	if !archiveExists && !manifestExists {
		out.ArchiveAbsent = true
		return out
	}

	if archiveExists && !manifestExists {
		out.Pass = false
		out.Violations = append(out.Violations, fmt.Sprintf(
			"%s exists beside %s but no freeze manifest does. Two records trees with "+
				"nothing declaring which is live is the unguarded split. Freeze it: %s",
			archiveRel, liveRel, manifestRel))
		return out
	}

	frozen, err := readManifest(manifestPath)
	if err != nil {
		out.Pass = false
		out.Violations = append(out.Violations,
			fmt.Sprintf("cannot read the freeze manifest %s: %v", manifestRel, err))
		return out
	}
	out.FrozenSize = len(frozen)

	archive, err := markdownSet(archivePath)
	if err != nil {
		out.Pass = false
		out.Violations = append(out.Violations,
			fmt.Sprintf("cannot read the frozen archive %s: %v", archiveRel, err))
		return out
	}

	for p := range archive {
		if !frozen[p] {
			out.Added = append(out.Added, p)
		}
	}
	for p := range frozen {
		if !archive[p] {
			out.Removed = append(out.Removed, p)
		}
	}
	sort.Strings(out.Added)
	sort.Strings(out.Removed)

	if len(out.Added) > 0 || len(out.Removed) > 0 {
		out.Pass = false
	}
	return out
}

// markdownSet returns every `.md` under base, keyed by its base-relative path.
// A missing directory is an empty set, not an error: the caller decides whether
// absence is a failure, and here it is one of the allowed dispositions.
func markdownSet(base string) (map[string]bool, error) {
	set := map[string]bool{}
	if !dirExists(base) {
		return set, nil
	}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		set[filepath.ToSlash(rel)] = true
		return nil
	})
	return set, err
}

// readManifest parses the freeze manifest: one archive-relative path per line,
// `#` comments and blank lines ignored.
func readManifest(path string) (map[string]bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	set := map[string]bool{}
	for _, line := range strings.Split(string(raw), "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		set[filepath.ToSlash(line)] = true
	}
	return set, nil
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
