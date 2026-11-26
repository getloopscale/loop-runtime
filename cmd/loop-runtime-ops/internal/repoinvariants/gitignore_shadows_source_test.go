// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Regression-lock: a .gitignore pattern that silently swallows SOURCE files.
//
// `.gitignore` carried a bare `build/` under a "Node.js / TypeScript (cli/)"
// heading. A git pattern without a leading slash matches at ANY depth, so it
// also matched `api/internal/services/build/` — a Go package. The 42 files
// already tracked there survived (git keeps tracking what it tracks), but every
// NEW file in that package was ignored.
//
// The failure mode is the dangerous kind: `git add -A` skips the file without a
// word, the commit looks complete, and what lands is a caller wired to code
// that does not exist in the repository. It surfaces later as a build break in
// CI, or a reviewer wondering why a diff references a missing symbol.
//
// `cli/` itself left for the sibling repo at M2, so the pattern was protecting
// nothing: no `build/` or `dist/` artifact directory exists in this worktree at
// all. The pattern outlived its subject and kept only its side effect.
//
// This guard is CLASS-shaped: it asks git which files are ignored, and fails on
// any that look like source. It catches the next over-broad pattern — `out/`,
// `target/`, `bin/` — without anyone remembering to look.
package repoinvariants

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/qtestutil"
)

// sourceExtensions are extensions that should never be ignored wholesale. Build
// artifacts (.js bundles, .map, compiled binaries) are deliberately absent.
var sourceExtensions = map[string]bool{
	".go":   true,
	".ts":   true,
	".tsx":  true,
	".py":   true,
	".rs":   true,
	".sql":  true,
	".sh":   true,
	".yml":  true,
	".yaml": true,
}

// ignoredSourceExceptions are paths that are legitimately ignored despite
// looking like source. Each entry needs a reason — an unexplained exception is
// how a guard rots into a rubber stamp.
var ignoredSourceExceptions = []string{
	"knowledge-base/references/", // third-party study material (rules/reference-provenance.md)
	"knowledge-base/tools/",      // ditto
	"node_modules/",              // vendored dependencies
	".claude/",                   // local cycle state (governance-gate.local.md, loop files)
	// `.squad/records/` is `.claude/`'s successor for run output, moved there on
	// 2026-09-09 so a project's own records stop sharing a directory with an
	// installed dependency. `.gitignore:312` excludes `.squad/*` and `:313`
	// un-ignores `.squad/wiki/` — so ONLY the records half is excepted here.
	// Excepting `.squad/` whole would hide the wiki, which is versioned on purpose.
	".squad/records/", // dated run trail: plans, alignments, reviews (rules/records-location.md)
	"archive/",        // frozen historical test corpus
	".tilt-rendered/", // Tilt's rendered manifest output — generated YAML, not source
	// `loop-system-cartography` creates this and writes its own `.gitignore` that
	// excludes everything — it holds raw tool output and a database of source
	// excerpts from the repository being mapped. The `.py` files under `work/` are
	// that run's extractors, not this repository's source.
	"cartography-output/", // an audit workspace, self-ignoring by the plugin's design
}

func isExcepted(path string) bool {
	for _, ex := range ignoredSourceExceptions {
		if strings.Contains(path, ex) {
			return true
		}
	}
	return false
}

// TestGitignoreDoesNotShadowSourceFiles asks git directly which untracked files
// its ignore rules exclude, and fails when any of them is source.
func TestGitignoreDoesNotShadowSourceFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		// A failure, not a skip: git is a prerequisite of this repository, and a
		// skip would erase the guard on exactly the machine that lacks it.
		t.Fatal("git is not on PATH")
	}

	cmd := exec.Command("git", "ls-files", "--others", "--ignored", "--exclude-standard")
	cmd.Dir = qtestutil.MustRepoRoot()
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git ls-files: %v", err)
	}

	var shadowed []string
	for _, line := range strings.Split(string(out), "\n") {
		path := strings.TrimSpace(line)
		if path == "" || isExcepted(path) {
			continue
		}
		if sourceExtensions[filepath.Ext(path)] {
			shadowed = append(shadowed, path)
		}
	}

	if len(shadowed) > 0 {
		t.Errorf(".gitignore is swallowing %d source file(s):\n      %s\n\n"+
			"    A pattern without a leading slash matches at EVERY depth, so `build/` intended for a\n"+
			"    Node artifact directory also hides the Go package `api/internal/services/build/`.\n"+
			"    `git add -A` then skips the file silently and the commit lands a caller wired to code\n"+
			"    that is not in the repository.\n"+
			"    Anchor the pattern (`/build/`, or the directory that actually produces artifacts), or\n"+
			"    add a justified entry to ignoredSourceExceptions.",
			len(shadowed), strings.Join(shadowed, "\n      "))
	}
}
