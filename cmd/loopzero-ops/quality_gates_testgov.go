// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/coveragediff"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/coveragemerge"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/mockbudget"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/regressionmatrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/regressiontest"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testdeletion"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testpyramid"
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
	matrix := fs.String("matrix", "docs/runbooks/bootstrap-regression-matrix.md", "matrix markdown path (repo-relative)")
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
	o := testdeletion.Check(testdeletion.Config{
		BaseRef:  os.Getenv("BASE_REF"),
		HeadRef:  os.Getenv("HEAD_REF"),
		PRBody:   os.Getenv("PR_BODY"),
		PRLabels: os.Getenv("PR_LABELS"),
	})
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
	inv := fs.String("inventory", "infra/quality/test-inventory.json", "test inventory JSON")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := testpyramid.Check(testpyramid.Config{
		InventoryFile: *inv,
		PRLabels:      os.Getenv("PR_LABELS"),
		Thresholds:    testpyramid.DefaultThresholds(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[pyramid] %v\n", err)
		return 1
	}
	if o.Deferred {
		fmt.Println("[pyramid] EC-12: 'pyramid-defer' label — PASS (auto-issue should be opened)")
		return 0
	}
	if !o.Pass {
		fmt.Fprintln(os.Stderr, "[pyramid] FAIL: pyramid out of bounds:")
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[pyramid] use 'pyramid-defer' label for one-PR override")
		return 1
	}
	fmt.Println("[pyramid] PASS: ratios within bounds")
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
