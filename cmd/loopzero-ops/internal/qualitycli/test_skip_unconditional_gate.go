// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testskipunconditional"
)

func init() {
	register(Gate{
		Name:    "test-skip-unconditional",
		Summary: "B-184: a test under infra/tests/ or api/ may not open with a BARE t.Skip — no guard at all — and carry statements underneath it. A skip aborts the function, so everything below it is unreachable code that reads like a live assertion; two such tests had asserted nothing since T1.4 and six more sat in t.Run subtests. The unguarded half of the class test-skip-absent-path guards. Its pin holds live anchors, so a detector that stops recognising the shape goes red rather than quiet.",
		Setup:   setupTestSkipUnconditional,
	})
}

// setupTestSkipUnconditional is the B-184 gate: a test under one of the scanned
// trees may not open with a BARE t.Skip -- no guard at all -- and carry
// statements underneath it, unless the site is declared, with a written reason,
// in infra/quality/test-skip-unconditional.txt. A skip aborts the function, so
// everything below it is unreachable code that reads like a live assertion.
//
// It is the unguarded half of the class test-skip-absent-path guards: that gate
// reads a filesystem probe in a guard, and here there is no guard to read.
//
// Exit codes: 0 clean, 1 contract violated, 5 broken setup (no scan tree, no
// test files, no skip calls, no unconditional skip, unreadable or ambiguous
// pin). 5 rather than 0 is the whole point -- a checker that cannot find its
// subject says so.
func setupTestSkipUnconditional(fs *flag.FlagSet) Check {
	scanDir := fs.String("scan", strings.Join(testskipunconditional.DefaultScanDirs, ","),
		"comma-separated test trees to scan (repo-relative)")
	pin := fs.String("pin", testskipunconditional.DefaultPinFile, "declaration file (repo-relative)")
	return func(inv Invocation) (Outcome, error) {
		var scanDirs []string
		for _, d := range strings.Split(*scanDir, ",") {
			if d = strings.TrimSpace(d); d != "" {
				scanDirs = append(scanDirs, d)
			}
		}

		o, err := testskipunconditional.Check(testskipunconditional.Config{
			RepoRoot: inv.Root,
			ScanDirs: scanDirs,
			PinFile:  *pin,
		})
		if err != nil {
			return Fail, err
		}

		inv.Stdout.Printf("[test-skip-unconditional] scanned %d *_test.go file(s) under %s -- %d skip call(s), %d unconditional, %d with code below\n",
			o.FilesScanned, strings.Join(o.ScanRoots, ", "), o.SkipCalls, len(o.Unconditional), len(o.Sites))
		// Declared sites print on EVERY run, pass or fail. A declaration nobody sees
		// is the dead assertion with extra steps.
		for _, t := range o.Tracked {
			inv.Stdout.Printf("  TRACKED      %s\n", t)
		}
		if !o.Pass {
			inv.Stderr.Printf("[test-skip-unconditional] FAIL -- %d violation(s):\n", len(o.Violations))
			for _, v := range o.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("[test-skip-unconditional] a skip aborts the function, so nothing " +
				"below it has ever run (B-184): either the assertion comes back and asserts something " +
				"that is in this tree, or the test goes with the reason written, naming what covers the " +
				"contract in its place.")
			return Fail, nil
		}
		inv.Stdout.Printf("[test-skip-unconditional] PASS -- %d unconditional skip(s), %d with code below, all %d declared\n",
			len(o.Unconditional), len(o.Sites), len(o.Tracked))
		return Pass, nil
	}
}
