// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package testtargetcoverage

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// # Why the granularity moved from module to PACKAGE
//
// `runsTests` answered *"is there ANY `go test` in this module?"* — and never looked at the
// pattern. A target `cd infra/tests && go test ./unit/...` marked the whole module
// as covered.
//
// Measured on 2026-08-15 while executing `B-092`: `infra/tests` **passed** because of the
// `./unit/...`, while `scripts/` (12 files), `config/` (6) and `performance/` (23)
// ran in no target at all. **41 test files invisible in a module the gate
// reported as covered.**
//
// The gate was not wrong in what it claimed; it was wrong in the granularity at which it
// claimed it — and granularity is what decides what it can see.
//
// The cost of the change was measured before deciding: **every module has a `go test ./...`
// except `infra/tests`**, and the narrower patterns of the others are ADDITIONAL focused
// targets over modules already covered by `./...`. So the change bites in one module
// only.

// testBearingPackages returns every package directory under module that holds a
// `_test.go`, relative to the module root ("." for the module's own directory).
//
// A package WITHOUT tests is left out: demanding a target for it would turn the gate into a
// request to write tests for everything, which is a different conversation. What this gate
// claims is narrower and verifiable — *what exists is being run*.
func testBearingPackages(root, module string) ([]string, error) {
	base := filepath.Join(root, module)
	seen := map[string]bool{}
	err := filepath.WalkDir(base, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil //nolint:nilerr
		}
		if d.IsDir() {
			if prunedDirs[d.Name()] {
				return filepath.SkipDir
			}
			// A submodule has its own go.mod and is audited as a module of its own.
			// Counting it here would make two modules compete for the same package.
			if path != base {
				if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
					return filepath.SkipDir
				}
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(base, filepath.Dir(path))
		if relErr != nil {
			return nil //nolint:nilerr
		}
		seen[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// normalizeTargets makes a multi-line command block readable by the same
// "cd X ... go test" logic that a single line already satisfies.
//
// # The finding that forced this
//
// `taskfiles/quality.yml:388` runs `./chaos/...` and `./livetest/...` — but in a BLOCK:
//
//	cmds:
//	  - |
//	    cd infra/tests
//	    go test -race ... \
//	      ./charts/... ./chaos/... ./capacity/... ./livetest/...
//
// The `cd` is on one line and the `go test` on another, and line-by-line matching never saw
// the two together. Without this normalization, the five subtrees would show up
// as uncovered and I would have EXEMPTED them — declaring in writing that they are not
// run, when they are. **A false exemption is worse than the gap**: it documents a
// lie and makes the problem disappear for good.
//
// Two transformations, both conservative:
//
//  1. `\` continuation is a literal join — there is no ambiguity;
//  2. a `cd X` alone on its line starts prefixing the following lines, until the next
//     `cd` or the start of another list item (`- `), which is where the block ends.
func normalizeTargets(text string) string {
	// (1) join line continuations.
	text = strings.ReplaceAll(text, "\\\n", " ")

	var out []string
	cur := ""
	for _, raw := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(stripComment(raw))
		// A new list item closes the previous block.
		if strings.HasPrefix(trimmed, "- ") {
			cur = ""
			trimmed = strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		}
		if strings.HasPrefix(trimmed, "cd ") && !strings.Contains(trimmed, "&&") {
			cur = strings.TrimSpace(strings.TrimPrefix(trimmed, "cd "))
			out = append(out, raw)
			continue
		}
		if cur != "" && strings.Contains(trimmed, "go test") {
			out = append(out, "cd "+cur+" && "+trimmed)
			continue
		}
		out = append(out, raw)
	}
	return strings.Join(out, "\n")
}

// patternsFor extracts the `go test` patterns declared for one module across the
// Taskfile targets.
//
// Reuses the two boundaries `runsTests` already earned by review: the trailing space
// after the module name (so `cd operators/api` does not satisfy `operators`), and
// cutting the scope at the NEXT `cd` (so a later command testing a different module
// does not count).
func patternsFor(targets, module string) []string {
	var pats []string
	for _, raw := range strings.Split(normalizeTargets(targets), "\n") {
		line := stripComment(raw)
		i := strings.Index(line, "cd "+module+" ")
		if i < 0 {
			continue
		}
		scope := line[i+len("cd "+module+" "):]
		if next := strings.Index(scope, "cd "); next >= 0 {
			scope = scope[:next]
		}
		j := strings.Index(scope, "go test")
		if j < 0 {
			continue
		}
		// Every `./…` token up to the end of the command is a pattern.
		//
		// The first version stopped at the first flag, and it was wrong: flags come
		// BEFORE patterns in the real form — `go test -race -count=1 -coverprofile=X
		// ./charts/... ./chaos/...`. That rule returned zero patterns precisely
		// for the target that covers five subtrees, and I nearly exempted them because of it.
		//
		// What ends the command is a shell operator, not a flag.
		for _, tok := range strings.Fields(scope[j+len("go test"):]) {
			if tok == "|" || tok == "&&" || tok == ";" || strings.HasPrefix(tok, "2>") {
				break
			}
			if strings.HasPrefix(tok, "./") {
				pats = append(pats, tok)
			}
		}
	}
	return pats
}

// patternCovers reports whether a `go test` pattern matches a package path.
//
// `./...`      → everything
// `./sub/...`  → `sub` and its subtree
// `./sub`      → exactly `sub`
func patternCovers(pattern, pkg string) bool {
	p := strings.TrimSuffix(strings.TrimPrefix(pattern, "./"), "/")
	switch {
	case p == "..." || p == "":
		return true
	case strings.HasSuffix(p, "/..."):
		prefix := strings.TrimSuffix(p, "/...")
		return pkg == prefix || strings.HasPrefix(pkg, prefix+"/")
	default:
		return pkg == p
	}
}

// uncoveredPackages lists the test-bearing packages of a module that no target runs.
func uncoveredPackages(root, module, targets string) ([]string, error) {
	pkgs, err := testBearingPackages(root, module)
	if err != nil {
		return nil, err
	}
	pats := patternsFor(targets, module)
	var out []string
	for _, pkg := range pkgs {
		covered := false
		for _, pat := range pats {
			if patternCovers(pat, pkg) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, pkg)
		}
	}
	return out, nil
}
