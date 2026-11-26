// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package testinventory derives the test pyramid FROM THE TREE, instead of reading
// numbers a file declares about itself.
//
// # The defect this removes
//
// `infra/quality/test-inventory.json` was live config maintained by hand, and
// `testpyramid` read three percentages **written in the file**. Measured on 2026-08-17:
//
//	declared test files               710
//	of those, missing from disk       335   (47%)
//	`_test.go` files on disk          988
//	on disk and ABSENT from inventory 649
//
// The inventory described ~375 real files out of 988: **62% blind**. And the pyramid
// it declared (99.4% unit) was computed over that sample, which excludes the whole
// `infra/tests/e2e` suite.
//
// The gate compared a threshold against a number the file itself asserted. **A file
// could declare whatever pyramid it wanted** — and the one it declared no longer matched
// the repository on any of the four lines.
//
// Fixing the 649 by hand would fix the appearance and not the mechanism: the list would
// diverge again on the first slice that moved a test. Derived from the tree, it cannot
// diverge — there is nothing to update.
//
// # The classification is heuristic, and this is written down on purpose
//
// Whether a test is unit or integration is, in the limit, an editorial judgement. What
// this package does is apply ONE declared rule, in the same order, always:
//
//  1. **build tag** — the author declared it. `live` is e2e, `helm_render` is integration.
//  2. **path** — the subtrees whose purpose is their own name.
//  3. **unit** — everything else.
//
// Build tag before path because the tag is an explicit declaration by whoever wrote the
// test, and the path is an inference by whoever wrote this classifier.
package testinventory

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Category is a pyramid tier.
type Category string

const (
	Unit        Category = "unit"
	Integration Category = "integration"
	E2E         Category = "e2e"
)

// Entry is one classified test file.
type Entry struct {
	Path     string
	Category Category
	// Reason names WHICH rule classified it. Without it the derived inventory would
	// have the same defect as the declared one: a number with no way to check where it
	// came from.
	Reason string
}

// Inventory is the derived state of the tree.
type Inventory struct {
	Entries []Entry
	Unit    int
	Integr  int
	E2E     int
}

// Total counts every classified file.
func (i Inventory) Total() int { return len(i.Entries) }

// Ratios returns the three percentages, derived.
func (i Inventory) Ratios() (unitPct, integrationPct, e2ePct float64) {
	n := float64(i.Total())
	if n == 0 {
		return 0, 0, 0
	}
	return float64(i.Unit) / n * 100, float64(i.Integr) / n * 100, float64(i.E2E) / n * 100
}

// e2ePrefixes are subtrees whose whole purpose is end-to-end exercise.
var e2ePrefixes = []string{
	"infra/tests/e2e/",
	"infra/tests/livetest/",
	"infra/tests/chaos/",
	"infra/tests/performance/",
}

// integrationPrefixes are subtrees that cross a real boundary — a rendered chart, a
// running cluster, a database.
var integrationPrefixes = []string{
	"infra/tests/charts/",
	"infra/tests/integration/",
	"api/tests/",
}

// e2eTags / integrationTags are build tags whose meaning is declared by the author.
var (
	e2eTags         = []string{"live"}
	integrationTags = []string{"helm_render", "integration"}
)

// Classify decides the tier for one file.
//
// `header` is the top of the file — build constraints must appear before the package
// clause, so reading past it would only add cost.
func Classify(path, header string) (Category, string) {
	for _, tag := range e2eTags {
		if hasBuildTag(header, tag) {
			return E2E, "build tag " + tag
		}
	}
	for _, tag := range integrationTags {
		if hasBuildTag(header, tag) {
			return Integration, "build tag " + tag
		}
	}
	slash := filepath.ToSlash(path)
	for _, p := range e2ePrefixes {
		if strings.HasPrefix(slash, p) {
			return E2E, "path " + p
		}
	}
	for _, p := range integrationPrefixes {
		if strings.HasPrefix(slash, p) {
			return Integration, "path " + p
		}
	}
	return Unit, "default"
}

func hasBuildTag(header, tag string) bool {
	for _, line := range strings.Split(header, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "//go:build") {
			continue
		}
		// Match the whole token: `live` must not match inside `livetest`.
		for _, tok := range strings.FieldsFunc(line[len("//go:build"):], func(r rune) bool {
			return r == ' ' || r == '(' || r == ')' || r == '&' || r == '|' || r == '!'
		}) {
			if tok == tag {
				return true
			}
		}
	}
	return false
}

// skipDirs are trees that hold foreign or generated code. Counting them would make
// the pyramid describe someone else's tests.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true,
	".claude": true, "archive": true, "testdata": true,
}

// Derive walks the tree and classifies every Go test file.
func Derive(root string) (Inventory, error) {
	var inv Inventory
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// An unreadable directory hides test files the pyramid would count.
			return walkErr
		}
		if d.IsDir() {
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		header, err := readHeader(path)
		if err != nil {
			return err
		}
		cat, reason := Classify(filepath.ToSlash(rel), header)
		inv.Entries = append(inv.Entries, Entry{Path: filepath.ToSlash(rel), Category: cat, Reason: reason})
		switch cat {
		case E2E:
			inv.E2E++
		case Integration:
			inv.Integr++
		case Unit:
			inv.Unit++
		}
		return nil
	})
	if err != nil {
		return inv, fmt.Errorf("walk %s: %w", root, err)
	}
	return inv, nil
}

// readHeader returns the first lines of a file, where build constraints live.
// A file that cannot be read is an error: classifying it by path alone would
// drop the build tag that decides its category.
func readHeader(path string) (string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	defer f.Close()
	var b strings.Builder
	sc := bufio.NewScanner(f)
	for n := 0; n < 20 && sc.Scan(); n++ {
		line := sc.Text()
		b.WriteString(line)
		b.WriteString("\n")
		// The package clause ends the region where build constraints live.
		if strings.HasPrefix(strings.TrimSpace(line), "package ") {
			break
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}
	return b.String(), nil
}
