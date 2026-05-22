// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/loopzero/loopzero/pkg/quality/divergencematrix"
	"github.com/loopzero/loopzero/pkg/quality/mockbudget"
	"github.com/loopzero/loopzero/pkg/quality/regressiontest"
	"github.com/loopzero/loopzero/pkg/quality/nonewbash"
	"github.com/loopzero/loopzero/pkg/quality/noshellwrap"
	"github.com/loopzero/loopzero/pkg/quality/scriptsaudit"
	"github.com/loopzero/loopzero/pkg/quality/taskfileinterface"
	"github.com/loopzero/loopzero/pkg/quality/testdeletion"
	"github.com/loopzero/loopzero/pkg/quality/testpyramid"
)

const qualityHelp = `loopzero-ops quality — quality gate orchestrator

USAGE:
  loopzero-ops quality <gate> [flags]

GATES:
  scripts-audit   Walk repo, classify .sh files, emit CSV to stdout
                  Flags:
                    --root DIR     repo root (default: ".")
                    --output FILE  write to file instead of stdout

  no-new-bash     Fail if PR introduces new .sh outside allowlist (D3 gate).
                  Requires fetch-depth: 0 in CI checkout.
                  Flags:
                    --root DIR        repo root (default: ".")
                    --allowlist PATH  allowlist MD (default: ".claude/rules/shell-scripts-allowlist.md")
                    --base-ref REF    base git ref (default: "origin/main")
                    --head-ref REF    head git ref (default: "HEAD")

  no-shell-wrap   Fail if Go code in pkg/* uses shellrun.Run(<forbidden-cli>, ...)
                  without //nolint:shell-wrap suppression (D5 gate).
                  Forbidden: pulumi, helm, kubectl, aws, gh (use Go SDK instead).
                  Flags:
                    --root DIR  directory to scan (default: "pkg")

  taskfile-interface  Fail if any public Taskfile command no longer resolves.
                  Parses Taskfile.yml + includes directly (no task binary).
                  Flags:
                    --root DIR  repo root (default: ".")
`

func runQuality(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, qualityHelp)
		return 1
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Print(qualityHelp)
		return 0
	case "scripts-audit":
		return runScriptsAudit(args[1:])
	case "no-new-bash":
		return runNoNewBash(args[1:])
	case "no-shell-wrap":
		return runNoShellWrap(args[1:])
	case "taskfile-interface":
		return runTaskfileInterface(args[1:])
	case "divergence-matrix":
		return runDivergenceMatrix(args[1:])
	case "test-deletion":
		return runTestDeletion(args[1:])
	case "test-pyramid":
		return runTestPyramid(args[1:])
	case "regression-test":
		return runRegressionTest(args[1:])
	case "mock-budget":
		return runMockBudget(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown gate: %s\n\n%s", args[0], qualityHelp)
		return 1
	}
}

func runNoNewBash(args []string) int {
	fs := flag.NewFlagSet("no-new-bash", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", ".claude/rules/shell-scripts-allowlist.md", "allowlist markdown file")
	baseRef := fs.String("base-ref", "origin/main", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	absRoot, err := os.Getwd()
	if err == nil && *root == "." {
		// Resolve "." to absolute path for clearer error messages
		*root = absRoot
	}

	outcome, err := nonewbash.Check(nonewbash.Config{
		RepoRoot:      *root,
		AllowlistPath: *allowlist,
		BaseRef:       *baseRef,
		HeadRef:       *headRef,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-new-bash: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "no-new-bash: FAIL — new bash scripts detected outside allowlist:")
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "To fix: add the script to %s with justificativa per D4 criteria,\n", *allowlist)
		fmt.Fprintln(os.Stderr, "OR migrate the function to: helmfile (D2), pkg/quality/X Go gate (D5),")
		fmt.Fprintln(os.Stderr, "Ginkgo (T4), go:generate (T5.2). See docs/plans/2026-05-22-shell-script-strategic-plan.md")
		return 1
	}
	fmt.Println("no-new-bash: PASS — no new bash scripts outside allowlist")
	return 0
}

func runScriptsAudit(args []string) int {
	fs := flag.NewFlagSet("scripts-audit", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	output := fs.String("output", "", "write CSV to file (default: stdout)")
	if err := fs.Parse(args); err != nil {
		return 1
	}

	scripts, err := scriptsaudit.ScanRepo(*root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scripts-audit: %v\n", err)
		return 5
	}

	var w *os.File
	if *output != "" {
		f, err := os.Create(*output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "scripts-audit: create %s: %v\n", *output, err)
			return 5
		}
		defer f.Close()
		w = f
	} else {
		w = os.Stdout
	}

	if err := scriptsaudit.EmitCSV(scripts, w); err != nil {
		fmt.Fprintf(os.Stderr, "scripts-audit: emit: %v\n", err)
		return 5
	}
	if *output != "" {
		// Visibility for operator running interactively
		fmt.Fprintf(os.Stderr, "scripts-audit: %d scripts inventoried to %s\n", len(scripts), *output)
	}
	return 0
}

func runNoShellWrap(args []string) int {
	fs := flag.NewFlagSet("no-shell-wrap", flag.ContinueOnError)
	root := fs.String("root", "pkg", "directory to scan")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	outcome, err := noshellwrap.Check(noshellwrap.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-shell-wrap: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "no-shell-wrap: FAIL — Go code wraps forbidden CLI without nolint:")
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "To fix: use the Go SDK (pulumi-go-sdk, helm.sh/helm/v3, client-go,")
		fmt.Fprintln(os.Stderr, "aws-sdk-go-v2, go-github) OR add //nolint:shell-wrap on the call line")
		fmt.Fprintln(os.Stderr, "with justificativa per plan D5 + Anti-Pattern #8.")
		return 1
	}
	fmt.Println("no-shell-wrap: PASS — no forbidden shell wraps detected")
	return 0
}

func runTaskfileInterface(args []string) int {
	fs := flag.NewFlagSet("taskfile-interface", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	outcome, err := taskfileinterface.Check(taskfileinterface.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "taskfile-interface: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "taskfile-interface: FAIL — public Taskfile command(s) missing:")
		for _, m := range outcome.Missing {
			fmt.Fprintf(os.Stderr, "  - %s\n", m)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Action: define these in Taskfile.yml or an included taskfiles/*.yml.")
		fmt.Fprintln(os.Stderr, "If intentionally renamed/removed, update ExpectedCommands in")
		fmt.Fprintln(os.Stderr, "pkg/quality/taskfileinterface/check.go AND CI consumers.")
		return 1
	}
	fmt.Printf("taskfile-interface: PASS — all %d public commands resolve\n", len(taskfileinterface.ExpectedCommands))
	return 0
}

func runDivergenceMatrix(args []string) int {
	fs := flag.NewFlagSet("divergence-matrix", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	outcome, err := divergencematrix.Check(divergencematrix.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "divergence-matrix: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "divergence-matrix: FAIL — %d issue(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("divergence-matrix: GREEN — matrix integrity OK")
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
		fmt.Fprintln(os.Stderr, "Add 'test-deletion-approved' label OR 'Test deletion justification: <reason>' to PR body.")
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
