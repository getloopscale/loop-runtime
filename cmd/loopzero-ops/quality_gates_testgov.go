// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/coveragediff"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/coveragemerge"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/mockbudget"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/regressionmatrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/regressiontest"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testdeletion"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testinventory"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testpyramid"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testskipabsentpath"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testskipunconditional"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testtargets"
)

// M9 Step 7: quality gate entry points for the "testgov" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runRegressionMatrix is the matrix-integrity gate replacing
// audit-regression-matrix.sh + audit-golden-test.sh (Track B TB5). It audits
// the bootstrap regression matrix: every entry's Verification artifact must
// still resolve in the repo. FAILs if any critical (Pode Recorrer=NÃO) entry's
// artifact is unresolvable. Unlike the bash original, it has no set-e/return-2
// fragility (the prior gate silently aborted at exit 2 — see
// finding-2026-05-24-audit-regression-matrix-set-e-abort).
func runRegressionMatrix(args []string) int {
	fs := flag.NewFlagSet("regression-matrix", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	matrix := fs.String("matrix", "docs/runbooks.md", "matrix markdown path (repo-relative)")
	strict := fs.Bool("strict", false, "fail on ANY miss, not just critical (Pode Recorrer=NÃO) ones")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	matrixPath := *matrix
	if !filepath.IsAbs(matrixPath) {
		matrixPath = filepath.Join(*root, matrixPath)
	}
	v, err := regressionmatrix.Check(matrixPath, *root, *strict)
	if err != nil {
		fmt.Fprintf(os.Stderr, "regression-matrix: %v\n", err)
		return 5
	}
	fmt.Printf("regression-matrix: %d entries — OK=%d miss=%d (critical=%d) skip=%d\n",
		v.Total, v.OK, v.Miss, v.MissCritical, v.Skip)
	for _, e := range v.MissEntries {
		marker := " "
		if e.CanRecur == regressionmatrix.CanRecurNo {
			marker = "!"
		}
		verify := e.Verification
		if len(verify) > 80 {
			verify = verify[:80]
		}
		fmt.Printf("  MISS%s #%s [%s] %s\n", marker, e.ID, e.CanRecur, verify)
	}
	if v.Failed() {
		fmt.Fprintf(os.Stderr, "regression-matrix: FAIL — %d unresolved critical artifact(s); the matrix claims protection that no longer resolves in the repo\n", v.MissCritical)
		return 1
	}
	fmt.Println("regression-matrix: PASS")
	return 0
}

// runCoverageMerge replaces coverage-merge.sh: merges Go + TS lcov into one.
func runCoverageMerge(args []string) int {
	fs := flag.NewFlagSet("coverage-merge", flag.ContinueOnError)
	goProfile := fs.String("go-profile", "", "Go coverage profile path")
	cliLCOV := fs.String("cli-lcov", "", "TypeScript lcov path")
	output := fs.String("output", "", "merged lcov destination (required)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *output == "" {
		fmt.Fprintln(os.Stderr, "coverage-merge: --output required")
		return 1
	}
	outcome, err := coveragemerge.Merge(coveragemerge.Config{
		GoProfilePath: *goProfile,
		CLILCOVPath:   *cliLCOV,
		OutputPath:    *output,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-merge: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	return 0
}

// runCoverageDiff replaces coverage-diff.sh: diff-aware coverage threshold gate.
func runCoverageDiff(args []string) int {
	fs := flag.NewFlagSet("coverage-diff", flag.ContinueOnError)
	diff := fs.String("diff", "", "touched-lines file (path:line per line)")
	lcov := fs.String("lcov", "", "merged lcov report")
	threshold := fs.Float64("threshold", 80.0, "minimum coverage percent on touched lines")
	report := fs.String("report", "", "optional Markdown report destination")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	outcome, err := coveragediff.Run(coveragediff.Config{
		DiffPath:   *diff,
		LCOVPath:   *lcov,
		Threshold:  *threshold,
		ReportPath: *report,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "coverage-diff: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	if !outcome.Pass {
		return 1
	}
	return 0
}

func runTestDeletion(args []string) int {
	_ = args
	o, err := testdeletion.Check(testdeletion.Config{
		BaseRef:  os.Getenv("BASE_REF"),
		HeadRef:  os.Getenv("HEAD_REF"),
		PRBody:   os.Getenv("PR_BODY"),
		PRLabels: os.Getenv("PR_LABELS"),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[test-deletion] FAIL — the range could not be read: %v\n", err)
		return 5
	}
	if len(o.DeletedTests) > 0 {
		fmt.Fprintln(os.Stderr, "[test-deletion] TRUE test deletions detected:")
		for _, f := range o.DeletedTests {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[test-deletion] FAIL: %s\n", o.Reason)
		fmt.Fprintln(os.Stderr, "Justify it in ONE of these — the commit body is the one that lives in the repo:")
		fmt.Fprintln(os.Stderr, "  - a commit message in the range:  Test deletion justification: <reason>")
		fmt.Fprintln(os.Stderr, "  - the PR body:                    Test deletion justification: <reason>")
		fmt.Fprintln(os.Stderr, "  - the 'test-deletion-approved' PR label")
		return 1
	}
	fmt.Printf("[test-deletion] PASS — %s\n", o.Reason)
	return 0
}

func runTestPyramid(args []string) int {
	fs := flag.NewFlagSet("test-pyramid", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	base := fs.String("baseline", "infra/quality/test-pyramid-baseline.json", "frozen measured pyramid")
	write := fs.Bool("write", false, "re-freeze the baseline from the current tree")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	basePath := filepath.Join(*root, *base)

	// The PR label remains the way out for a single PR. It comes first because it
	// is the one case where NOT measuring is the right answer.
	for _, lbl := range strings.Split(os.Getenv("PR_LABELS"), ",") {
		if strings.TrimSpace(lbl) == "pyramid-defer" {
			fmt.Println("[pyramid] label 'pyramid-defer' — PASS (open the automatic issue)")
			return 0
		}
	}

	if *write {
		inv, err := testinventory.Derive(*root)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[pyramid] %v\n", err)
			return 1
		}
		note := "Derived from the tree by testinventory.Derive, not declared (B-106)."
		if err := testpyramid.WriteBaseline(basePath, inv, time.Now().UTC().Format("2006-01-02"), note); err != nil {
			fmt.Fprintf(os.Stderr, "[pyramid] %v\n", err)
			return 1
		}
		u, i, e := inv.Ratios()
		fmt.Printf("[pyramid] baseline frozen: %d files — unit %.1f%% / integration %.1f%% / e2e %.1f%%\n",
			inv.Total(), u, i, e)
		return 0
	}

	o, err := testpyramid.CheckDerived(*root, basePath, testpyramid.DefaultThresholds())
	if err != nil {
		fmt.Fprintf(os.Stderr, "[pyramid] FAIL — %v\n", err)
		for _, v := range o.Regressions {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[pyramid] the ratchet only allows improvement; use the 'pyramid-defer' label for a single PR")
		return 1
	}
	fmt.Printf("[pyramid] PASS — %d files derived from the tree: unit %.1f%% / integration %.1f%% / e2e %.1f%%\n",
		o.Total, o.UnitPct, o.IntegPct, o.E2EPct)
	// The distance to the ADR target is reported and does NOT block: that target was
	// calibrated against a file that declared 0.1% e2e. Silencing it would hide the
	// pending decision; blocking on it would create a lock the repo cannot satisfy.
	for _, g := range o.AspirationGaps {
		fmt.Printf("[pyramid] distance to the ADR D7 target (reported, does not block): %s\n", g)
	}
	return 0
}

func runRegressionTest(args []string) int {
	_ = args
	o := regressiontest.Check(regressiontest.Config{
		Title:     os.Getenv("PR_TITLE"),
		Body:      os.Getenv("PR_BODY"),
		Commits:   os.Getenv("PR_COMMITS"),
		DiffFiles: os.Getenv("PR_DIFF_FILES"),
	})
	if o.Skipped {
		fmt.Println("[regression-gate] no bug-fix references — SKIP gate")
		return 0
	}
	if len(o.IssueRefs) > 0 {
		fmt.Println("[regression-gate] detected bug-fix references:")
		for _, r := range o.IssueRefs {
			fmt.Printf("  %s\n", r)
		}
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[regression-gate] FAIL: %s\n", o.Reason)
		fmt.Fprintln(os.Stderr, "Add a regression test in a *_test.go or *.test.ts file referencing the bug.")
		return 1
	}
	fmt.Println("[regression-gate] PASS — test file found in diff")
	return 0
}

func runMockBudget(args []string) int {
	fs := flag.NewFlagSet("mock-budget", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target directory to scan")
	budget := fs.Int("budget", mockbudget.DefaultBudget, "max mocks per test file")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if env := os.Getenv("BUDGET"); env != "" {
		if n, e := fmt.Sscanf(env, "%d", budget); n == 1 && e == nil {
			// budget set from env
		}
	}
	o, err := mockbudget.Check(mockbudget.Config{TargetDir: *dir, Budget: *budget})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[mock-budget] %v\n", err)
		return 5
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[mock-budget] FAIL: %d file(s) over budget:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[mock-budget] design smell — refactor/split OR add: // mock-budget: N — <reason>")
		return 1
	}
	fmt.Printf("[mock-budget] PASS — all test files within budget (%d mocks/file)\n", *budget)
	return 0
}

func runTestTargets(args []string) int {
	fs := flag.NewFlagSet("test-targets", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := testtargets.Check(testtargets.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-targets: %v\n", err)
		return 1
	}
	if !o.Pass {
		msg := fmt.Sprintf("%d *_test.sh target(s) point at a missing repo path. "+
			"A relocated/deleted script-under-test left its test pointing at a dead "+
			"SCRIPT=/SCRIPT_UNDER_TEST= path (the dr/ relocation rot, finding "+
			"2026-05-22). Repoint the test to the new path or delete the orphan test.",
			len(o.Violations))
		if os.Getenv("GITHUB_ACTIONS") != "" {
			fmt.Printf("::error::%s\n", msg)
		} else {
			fmt.Fprintf(os.Stderr, "[test-targets] BLOCK: %s\n", msg)
		}
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 2
	}
	fmt.Printf("[test-targets] all %d test target(s) resolve — OK.\n", o.Checked)
	return 0
}

// runTestSkipAbsentPath is the B-180 gate: a test under one of the scanned
// trees may not skip on a filesystem path that is absent from this tree unless
// the skip is declared, with a written reason, in
// infra/quality/test-skip-absent-path.txt.
//
// Exit codes: 0 clean, 1 contract violated, 5 broken setup (no scan tree, no
// test files, no skip calls found, unreadable or malformed pin). 5 rather than
// 0 is the whole point — a checker that cannot find its subject says so.
func runTestSkipAbsentPath(args []string) int {
	fs := flag.NewFlagSet("test-skip-absent-path", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	scanDir := fs.String("scan", strings.Join(testskipabsentpath.DefaultScanDirs, ","),
		"comma-separated test trees to scan (repo-relative)")
	pin := fs.String("pin", testskipabsentpath.DefaultPinFile, "declaration file (repo-relative)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	var scanDirs []string
	for _, d := range strings.Split(*scanDir, ",") {
		if d = strings.TrimSpace(d); d != "" {
			scanDirs = append(scanDirs, d)
		}
	}

	o, err := testskipabsentpath.Check(testskipabsentpath.Config{
		RepoRoot: *root,
		ScanDirs: scanDirs,
		PinFile:  *pin,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[test-skip-absent-path] SETUP BROKEN — %v\n", err)
		return 5
	}

	fmt.Printf("[test-skip-absent-path] scanned %d *_test.go file(s) under %s — %d skip call(s), %d path-guarded\n",
		o.FilesScanned, strings.Join(o.ScanRoots, ", "), o.SkipCalls, len(o.Sites))
	for _, c := range o.Conditional {
		fmt.Printf("  CONDITIONAL  %s\n", c)
	}
	// Tracked absences and external paths print on EVERY run, pass or fail. A
	// declaration nobody sees is the silent skip with extra steps, and a path
	// this gate did not judge has to say so rather than vanish into a PASS.
	for _, t := range o.Tracked {
		fmt.Printf("  TRACKED      %s\n", t)
	}
	for _, e := range o.Externals {
		fmt.Printf("  EXTERNAL     %s\n", e)
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[test-skip-absent-path] FAIL — %d violation(s):\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[test-skip-absent-path] a skip that always fires is a deleted test with good "+
			"manners (B-169): remove it naming the guard that covers the contract, or declare it in the pin.")
		return 1
	}
	fmt.Printf("[test-skip-absent-path] PASS — %d path-guarded skip(s): %d conditional here, %d tracked absence(s), %d external\n",
		len(o.Sites), len(o.Conditional), len(o.Tracked), len(o.Externals))
	return 0
}

// runTestSkipUnconditional is the B-184 gate: a test under one of the scanned
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
func runTestSkipUnconditional(args []string) int {
	fs := flag.NewFlagSet("test-skip-unconditional", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	scanDir := fs.String("scan", strings.Join(testskipunconditional.DefaultScanDirs, ","),
		"comma-separated test trees to scan (repo-relative)")
	pin := fs.String("pin", testskipunconditional.DefaultPinFile, "declaration file (repo-relative)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	var scanDirs []string
	for _, d := range strings.Split(*scanDir, ",") {
		if d = strings.TrimSpace(d); d != "" {
			scanDirs = append(scanDirs, d)
		}
	}

	o, err := testskipunconditional.Check(testskipunconditional.Config{
		RepoRoot: *root,
		ScanDirs: scanDirs,
		PinFile:  *pin,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[test-skip-unconditional] SETUP BROKEN -- %v\n", err)
		return 5
	}

	fmt.Printf("[test-skip-unconditional] scanned %d *_test.go file(s) under %s -- %d skip call(s), %d unconditional, %d with code below\n",
		o.FilesScanned, strings.Join(o.ScanRoots, ", "), o.SkipCalls, len(o.Unconditional), len(o.Sites))
	// Declared sites print on EVERY run, pass or fail. A declaration nobody sees
	// is the dead assertion with extra steps.
	for _, t := range o.Tracked {
		fmt.Printf("  TRACKED      %s\n", t)
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[test-skip-unconditional] FAIL -- %d violation(s):\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[test-skip-unconditional] a skip aborts the function, so nothing "+
			"below it has ever run (B-184): either the assertion comes back and asserts something "+
			"that is in this tree, or the test goes with the reason written, naming what covers the "+
			"contract in its place.")
		return 1
	}
	fmt.Printf("[test-skip-unconditional] PASS -- %d unconditional skip(s), %d with code below, all %d declared\n",
		len(o.Unconditional), len(o.Sites), len(o.Tracked))
	return 0
}
