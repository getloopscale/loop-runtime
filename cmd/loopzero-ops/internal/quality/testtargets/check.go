// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Package testtargets implements a CI gate that prevents "test-target rot":
// a `*_test.sh` whose SCRIPT=/SCRIPT_UNDER_TEST=/SCRIPT_PATH=/LIB= assignment
// points at a repo-relative .sh path that no longer exists.
//
// Motivation: the dr/ relocation (commit e286effc) + bootstrap-cell
// consolidation (1a9f1f77) moved scripts-under-test without repointing their
// tests; CI path-filters masked the breakage and ~7 hermetic drill tests rotted
// silently (finding-2026-05-22-drill-hermetic-suite-rot). This gate fails loudly
// the moment a test points at a missing target — catching the next relocation.
//
// Pure-Go (filepath walk + regexp + os.Stat) — no external CLI; a migration
// target per the shell-script-strategic plan, not new bash.
package testtargets

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// excludedDirs are pruned anywhere in the tree.
var excludedDirs = map[string]bool{
	".git": true, "node_modules": true, "_legacy": true, "vendor": true,
	"dist": true, "build": true, "worktrees": true,
}

// Config drives the gate.
type Config struct {
	Root string
}

// Outcome captures the result.
type Outcome struct {
	Pass       bool
	Checked    int      // number of resolvable target assignments examined
	Violations []string // "test:line: VAR=... → MISSING <target>"
}

// Verdict is Pass when the check passed and Fail otherwise.
func (o Outcome) Verdict() verdict.Verdict { return verdict.FromPass(o.Pass) }

// assignRe matches a SCRIPT*/LIB assignment line, capturing the RHS value.
var assignRe = regexp.MustCompile(`^\s*(SCRIPT|SCRIPT_UNDER_TEST|SCRIPT_PATH|LIB)=(.+)$`)

// repoPathRe extracts a repo-relative .sh path (one of the known top dirs) that
// contains no remaining shell metachars.
var repoPathRe = regexp.MustCompile(`(infra|tools|cli|api|pkg|operators)/[A-Za-z0-9/_.-]+\.sh`)

// fixtureVarRe marks an RHS as a TEST-CREATED stub path (not a repo target).
var fixtureVarRe = regexp.MustCompile(`(?i)(\$\{?tmpdir|\$\{?work\b|/tmp/|mktemp|\$\{?tmp\b)`)

// ErrNothingScanned means the walk found no *_test.sh at all.
var ErrNothingScanned = errors.New("no *_test.sh found under the root — the gate read nothing")

// Check walks cfg.Root and verifies every resolvable *_test.sh SCRIPT target
// exists. A test script it cannot read, and a tree with no test script, are
// errors rather than a clean result.
func Check(cfg Config) (*Outcome, error) {
	root := cfg.Root
	if root == "" {
		root = "."
	}
	out := &Outcome{Pass: true}
	scanned := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if excludedDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.sh") {
			return nil
		}
		scanned++
		return scanTest(root, path, out)
	})
	if err != nil {
		return nil, err
	}
	if scanned == 0 {
		return nil, fmt.Errorf("%s: %w", root, ErrNothingScanned)
	}
	if len(out.Violations) > 0 {
		out.Pass = false
	}
	return out, nil
}

func scanTest(root, path string, out *Outcome) error {
	data, err := os.ReadFile(path) // #nosec G304 -- gate intentionally reads scanned test scripts
	if err != nil {
		return fmt.Errorf("reading test script: %w", err)
	}
	rel, _ := filepath.Rel(root, path)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Text()
		m := assignRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		rhs := m[2]
		target, ok := resolveTarget(rhs)
		if !ok {
			continue
		}
		out.Checked++
		// #nosec G703 -- target is a repo-relative .sh path extracted from the
		// scanned test and joined under root; existence check only, no traversal.
		if _, statErr := os.Stat(filepath.Join(root, target)); statErr != nil {
			out.Violations = append(out.Violations,
				fmt.Sprintf("%s:%d: %s= → MISSING %s", rel, lineNo, m[1], target))
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading %s: %w", rel, err)
	}
	return nil
}

// resolveTarget extracts a checkable repo-relative .sh path from an assignment
// RHS. Returns (path, true) only when the path is unambiguously a repo target
// (not a $tmpdir/$WORK fixture stub, not an unresolvable variable expression).
func resolveTarget(rhs string) (string, bool) {
	// Strip surrounding quotes for inspection.
	v := strings.TrimSpace(rhs)
	v = strings.Trim(v, `"'`)
	// Test-created fixture stubs are not repo targets.
	if fixtureVarRe.MatchString(v) {
		return "", false
	}
	loc := repoPathRe.FindString(v)
	if loc == "" {
		return "", false
	}
	// The matched path itself must be free of shell metachars (defensive — the
	// regex char class already excludes them, but a glob `*` could sneak via a
	// different top-dir; reject to avoid false positives).
	if strings.ContainsAny(loc, "$*?{}") {
		return "", false
	}
	return loc, true
}
