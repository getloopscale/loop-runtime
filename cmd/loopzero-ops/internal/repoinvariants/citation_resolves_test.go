// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package repoinvariants

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// B-013 RED test. A citation that points at something deleted is followed by
// the next reader, and nothing in this repository refuses one.
//
// The two surfaces asserted here are the ones where a citation has EXACTLY ONE
// reading, so a failure is a defect and never a matter of taste:
//
//   - a CODEOWNERS pattern anchored with a leading `/` is a repo-root path, by
//     GitHub's own grammar. A pattern naming a deleted file assigns no reviewer
//     and GitHub reports nothing.
//   - a short commit SHA quoted in a workflow comment either names an object in
//     this repository or it does not.
//
// The wider surfaces the item also names -- path citations in Go comments and in
// markdown -- are deliberately NOT asserted here. Measured 2026-09-12, they carry
// 859 unresolved tokens of which 545 are legitimate (globs, provenance notes,
// sibling-repo paths, prose that names an absence on purpose). A test that failed
// on all of them would be a test somebody deletes.

// codeownersPathPattern matches a root-anchored CODEOWNERS pattern: the leading
// `/` is what makes it a path rather than a glob over basenames.
var codeownersPathPattern = regexp.MustCompile(`^(/[A-Za-z0-9_./@-]+)`)

func TestCodeownersPatternsResolveOnDisk(t *testing.T) {
	root := qtestutil.MustRepoRoot()
	body, err := os.ReadFile(filepath.Join(root, ".github", "CODEOWNERS"))
	if err != nil {
		t.Fatalf("read CODEOWNERS: %v", err)
	}

	checked := 0
	for i, line := range strings.Split(string(body), "\n") {
		s := strings.TrimSpace(line)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		m := codeownersPathPattern.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		checked++
		rel := strings.Trim(m[1], "/")
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Errorf(".github/CODEOWNERS:%d assigns ownership of %q, which does not exist — "+
				"the rule matches nothing and GitHub requests no review for it", i+1, m[1])
		}
	}

	// A scan that examined nothing has abstained, not passed -- the same floor
	// docsrefs.ErrNoReferencesEnumerated states next door.
	if checked == 0 {
		t.Fatal("found ZERO root-anchored patterns in CODEOWNERS: the parser is wrong, " +
			"so this test cannot vouch for anything")
	}
}

// shaCandidate matches a hex token of commit-abbreviation length. RE2 has no
// lookahead, so the "must contain a letter" half is hasHexLetter below -- that
// half is what separates `464b9d7e3` from the all-decimal GitHub run ids
// (`30621505439`) and API object ids (`158891596`) that share the shape and are
// not commits.
var shaCandidate = regexp.MustCompile(`\b[0-9a-f]{7,12}\b`)

func hasHexLetter(s string) bool {
	for _, r := range s {
		if r >= 'a' && r <= 'f' {
			return true
		}
	}
	return false
}

// notCommitWords are hex-shaped tokens that are words, not object names.
var notCommitWords = map[string]bool{"ed25519": true, "deadbeef": true, "abc1234": true}

func TestCommitSHAsCitedInWorkflowsAreReachable(t *testing.T) {
	root := qtestutil.MustRepoRoot()
	// This check needs local history to resolve a pin, so it cannot measure without it.
	// Under CI that is a configuration error and must be loud: `public-checks` sets
	// fetch-depth: 0 precisely so this runs, and a silent skip there would report an
	// unverified supply chain as a clean one. Locally it stays a skip — failing a
	// developer's shallow clone would claim a defect where there is only a missing input.
	// Verifying without history is tracked at:
	// https://github.com/getloopscale/loopzero/issues/365
	underCI := os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("CI") == "true"
	cannotMeasure := func(format string, args ...any) {
		t.Helper()
		if underCI {
			t.Fatalf(format+" — CI must check out with fetch-depth: 0", args...)
		}
		// https://github.com/getloopscale/loopzero/issues/365
		t.Skipf(format, args...)
	}
	if err := exec.Command("git", "-C", root, "rev-parse", "--git-dir").Run(); err != nil {
		cannotMeasure("not a git worktree at %s: %v", root, err)
		return
	}
	if out, err := exec.Command("git", "-C", root, "rev-parse", "--is-shallow-repository").Output(); err == nil {
		if strings.TrimSpace(string(out)) == "true" {
			cannotMeasure("shallow clone: an unreachable SHA here proves nothing about the citation")
			return
		}
	}

	dir := filepath.Join(root, ".github", "workflows")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}

	checked := 0
	for _, e := range ents {
		if e.IsDir() || !(strings.HasSuffix(e.Name(), ".yml") || strings.HasSuffix(e.Name(), ".yaml")) {
			continue
		}
		p := filepath.Join(dir, e.Name())
		body, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatalf("read %s: %v", p, rerr)
		}
		for i, line := range strings.Split(string(body), "\n") {
			// Only COMMENTS. A hex token in a live step is an action pin or a
			// digest, which names an object in somebody else's repository.
			idx := strings.Index(line, "#")
			if idx < 0 {
				continue
			}
			for _, tok := range shaCandidate.FindAllString(line[idx:], -1) {
				if notCommitWords[tok] || !hasHexLetter(tok) {
					continue
				}
				checked++
				if exec.Command("git", "-C", root, "cat-file", "-e", tok+"^{commit}").Run() != nil {
					t.Errorf("%s:%d cites commit %s, which names no object in this repository — "+
						"the comment explains a decision by pointing at a commit nobody can read",
						filepath.Join(".github", "workflows", e.Name()), i+1, tok)
				}
			}
		}
	}

	if checked == 0 {
		t.Fatal("found ZERO commit-SHA citations in workflow comments: the matcher is wrong, " +
			"so this test cannot vouch for anything")
	}
}
