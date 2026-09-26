// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testskipabsentpath"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "test-skip-absent-path",
		Summary: "B-180/B-183/B-186/B-188: a test under infra/tests/ or api/ may not skip on an absent path without declaring it — a skip guarded by a path that does not exist is unconditional in practice, and everything below it is dead code. Reads the probe inline, through same-package predicates (functions or unambiguously-named methods) that delegate to one another, in either arm of the guard; and reads the SKIP wherever it sits — inside a function literal, a defer, a select or type-switch clause, in the condition of a tag-less switch case, one same-package call away from the arm, or handed to a helper as an argument. Past its depth bound it reports UNRESOLVED and fails rather than going quiet.",
		Setup:   setupTestSkipAbsentPath,
	})
}

// setupTestSkipAbsentPath is the B-180 gate: a test under one of the scanned
// trees may not skip on a filesystem path that is absent from this tree unless
// the skip is declared, with a written reason, in
// infra/quality/test-skip-absent-path.txt.
//
// Exit codes: 0 clean, 1 contract violated, 5 broken setup (no scan tree, no
// test files, no skip calls found, unreadable or malformed pin). 5 rather than
// 0 is the whole point — a checker that cannot find its subject says so.
func setupTestSkipAbsentPath(fs *flag.FlagSet) Check {
	scanDir := fs.String("scan", strings.Join(testskipabsentpath.DefaultScanDirs, ","),
		"comma-separated test trees to scan (repo-relative)")
	pin := fs.String("pin", testskipabsentpath.DefaultPinFile, "declaration file (repo-relative)")
	return func(inv Invocation) (verdict.Result, error) {
		var scanDirs []string
		for _, d := range strings.Split(*scanDir, ",") {
			if d = strings.TrimSpace(d); d != "" {
				scanDirs = append(scanDirs, d)
			}
		}

		o, err := testskipabsentpath.Check(testskipabsentpath.Config{
			RepoRoot: inv.Root,
			ScanDirs: scanDirs,
			PinFile:  *pin,
		})
		if err != nil {
			return Fail, err
		}

		inv.Stdout.Printf("[test-skip-absent-path] scanned %d *_test.go file(s) under %s — %d skip call(s), %d path-guarded\n",
			o.FilesScanned, strings.Join(o.ScanRoots, ", "), o.SkipCalls, len(o.Sites))
		for _, c := range o.Conditional {
			inv.Stdout.Printf("  CONDITIONAL  %s\n", c)
		}
		// Tracked absences and external paths print on EVERY run, pass or fail. A
		// declaration nobody sees is the silent skip with extra steps, and a path
		// this gate did not judge has to say so rather than vanish into a PASS.
		for _, t := range o.Tracked {
			inv.Stdout.Printf("  TRACKED      %s\n", t)
		}
		for _, e := range o.Externals {
			inv.Stdout.Printf("  EXTERNAL     %s\n", e)
		}
		if !o.Pass {
			inv.Stderr.Printf("[test-skip-absent-path] FAIL — %d violation(s):\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("[test-skip-absent-path] a skip that always fires is a deleted test with good " +
				"manners (B-169): remove it naming the guard that covers the contract, or declare it in the pin.")
			return Fail, nil
		}
		inv.Stdout.Printf("[test-skip-absent-path] PASS — %d path-guarded skip(s): %d conditional here, %d tracked absence(s), %d external\n",
			len(o.Sites), len(o.Conditional), len(o.Tracked), len(o.Externals))
		return Pass, nil
	}
}
