// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/loopzero/loopzero/pkg/quality/actionpinning"
	"github.com/loopzero/loopzero/pkg/quality/agplisolation"
	"github.com/loopzero/loopzero/pkg/quality/alertrunbooks"
	"github.com/loopzero/loopzero/pkg/quality/baseimagedigest"
	"github.com/loopzero/loopzero/pkg/quality/baselinecompare"
	"github.com/loopzero/loopzero/pkg/quality/baselineupdate"
	"github.com/loopzero/loopzero/pkg/quality/clibundlesize"
	"github.com/loopzero/loopzero/pkg/quality/clusterimagepolicymode"
	"github.com/loopzero/loopzero/pkg/quality/coveragediff"
	"github.com/loopzero/loopzero/pkg/quality/coveragemerge"
	"github.com/loopzero/loopzero/pkg/quality/crdsemanticdiff"
	"github.com/loopzero/loopzero/pkg/quality/danglingrefs"
	"github.com/loopzero/loopzero/pkg/quality/deadlineratchet"
	"github.com/loopzero/loopzero/pkg/quality/decisionsdeletion"
	"github.com/loopzero/loopzero/pkg/quality/divergencematrix"
	"github.com/loopzero/loopzero/pkg/quality/drilloutputs"
	"github.com/loopzero/loopzero/pkg/quality/helmdiffstructural"
	"github.com/loopzero/loopzero/pkg/quality/helmdrift"
	"github.com/loopzero/loopzero/pkg/quality/migrationtestcoverage"
	"github.com/loopzero/loopzero/pkg/quality/mockbudget"
	"github.com/loopzero/loopzero/pkg/quality/nomigrateforce"
	"github.com/loopzero/loopzero/pkg/quality/nonewbash"
	"github.com/loopzero/loopzero/pkg/quality/noshellwrap"
	"github.com/loopzero/loopzero/pkg/quality/orphanalerts"
	"github.com/loopzero/loopzero/pkg/quality/pkgusagematrix"
	"github.com/loopzero/loopzero/pkg/quality/prodimagetags"
	"github.com/loopzero/loopzero/pkg/quality/regressiontest"
	"github.com/loopzero/loopzero/pkg/quality/runtimeslo"
	"github.com/loopzero/loopzero/pkg/quality/scriptsaudit"
	"github.com/loopzero/loopzero/pkg/quality/securitycontext"
	"github.com/loopzero/loopzero/pkg/quality/skiplink"
	"github.com/loopzero/loopzero/pkg/quality/stackcoverage"
	"github.com/loopzero/loopzero/pkg/quality/taskfileinterface"
	"github.com/loopzero/loopzero/pkg/quality/templatecompliance"
	"github.com/loopzero/loopzero/pkg/quality/testdeletion"
	"github.com/loopzero/loopzero/pkg/quality/testpyramid"
	"github.com/loopzero/loopzero/pkg/quality/testtargets"
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

  dangling-script-refs
                  Fail if a .sh deleted in the diff is still referenced (live)
                  by Taskfile/CI/SKILL/infra callers (T0.1 migration backstop).
                  Requires fetch-depth: 0 in CI checkout.
                  Flags:
                    --root DIR      repo root (default: ".")
                    --base-ref REF  base git ref (default: "origin/develop")
                    --head-ref REF  head git ref (default: "HEAD")

  migration-test-coverage
                  Fail if a DB migration lacks a regression test.
                  Flags:
                    --root DIR             repo root (default: ".")
                    --migrations-dir DIR   override migrations dir
                    --tests-dir DIR        override unit tests dir

  pkg-usage-matrix
                  Keep the pkg Real Usage Matrix doc in sync with imports.
                  Flags:
                    --root DIR       repo root (default: ".")
                    --doc-path PATH  override matrix doc path
                    --check          verify freshness (default)
                    --write          regenerate the matrix

  crd-semantic-diff
                  Detect semantic drift between source CRDs and the rendered
                  loopzero-crds chart. Needs helm at runtime (CI has it).
                  Flags:
                    --root DIR     repo root (default: ".")
                    --crd NAME     limit to a single CRD short-name
                    --verbose      append inline diff snippets

  helm-diff-structural
                  Fail only on structurally risky chart diffs (security-resource
                  removal, namespace change, security-context downgrade).
                  Flags:
                    --chart NAME   chart name (diagnostics)
                    --base PATH    base rendered YAML (required)
                    --pr PATH      PR rendered YAML (required)

  coverage-merge  Merge Go + TS coverage into one lcov.
                  Flags: --go-profile PATH  --cli-lcov PATH  --output PATH (req)

  coverage-diff   Diff-aware coverage threshold gate on touched lines.
                  Flags: --diff PATH  --lcov PATH  --threshold N  --report PATH

  baseline-compare  Per-package coverage regression gate vs baseline.
                  Flags: --baseline PATH  --current PATH  --default-min N
                         --slack N  --defer  --defer-reason STR

  baseline-update Regenerate coverage baseline JSON (--dry-run → stdout).
                  Flags: --go-profile PATH  --lcov PATH  --baseline PATH  --dry-run

  template-compliance  PR-template checkbox compliance gate.
                  Flags: --pr-body STR  --pr-author STR  --pr-labels CSV

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
	case "skip-link":
		return runSkipLink(args[1:])
	case "helm-drift":
		return runHelmDrift(args[1:])
	case "runtime-slo":
		return runRuntimeSLO(args[1:])
	case "action-pinning":
		return runActionPinning(args[1:])
	case "prod-image-tags":
		return runProdImageTags(args[1:])
	case "no-migrate-force":
		return runNoMigrateForce(args[1:])
	case "test-targets":
		return runTestTargets(args[1:])
	case "dangling-script-refs":
		return runDanglingScriptRefs(args[1:])
	case "migration-test-coverage":
		return runMigrationTestCoverage(args[1:])
	case "pkg-usage-matrix":
		return runPkgUsageMatrix(args[1:])
	case "crd-semantic-diff":
		return runCRDSemanticDiff(args[1:])
	case "helm-diff-structural":
		return runHelmDiffStructural(args[1:])
	case "coverage-merge":
		return runCoverageMerge(args[1:])
	case "coverage-diff":
		return runCoverageDiff(args[1:])
	case "baseline-compare":
		return runBaselineCompare(args[1:])
	case "baseline-update":
		return runBaselineUpdate(args[1:])
	case "template-compliance":
		return runTemplateCompliance(args[1:])
	case "cosign-cutover":
		return runCosignCutover(args[1:])
	case "service-auth-rotation":
		return runServiceAuthRotation(args[1:])
	case "stack-coverage":
		return runStackCoverage(args[1:])
	case "decisions-deletion":
		return runDecisionsDeletion(args[1:])
	case "drill-outputs-consumers":
		return runDrillOutputsConsumers(args[1:])
	case "cli-bundle-size":
		return runCLIBundleSize(args[1:])
	case "agpl-isolation":
		return runAGPLIsolation(args[1:])
	case "orphan-alerts":
		return runOrphanAlerts(args[1:])
	case "alert-runbooks":
		return runAlertRunbooks(args[1:])
	case "base-image-digest":
		return runBaseImageDigest(args[1:])
	case "clusterimagepolicy-mode":
		return runClusterImagePolicyMode(args[1:])
	case "security-context":
		return runSecurityContext(args[1:])
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

// runDanglingScriptRefs is the T0.1 backstop gate: fail if a deleted .sh is
// still referenced (live) by any caller surface. Requires fetch-depth: 0.
func runDanglingScriptRefs(args []string) int {
	fs := flag.NewFlagSet("dangling-script-refs", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	baseRef := fs.String("base-ref", "origin/develop", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := danglingrefs.Check(danglingrefs.Config{
		RepoRoot: *root,
		BaseRef:  *baseRef,
		HeadRef:  *headRef,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dangling-script-refs: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "dangling-script-refs: FAIL — deleted scripts still referenced by live callers:")
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Per plan D2: rewire ALL 4 caller surfaces (Taskfile/CI/SKILL/scripts) in the")
		fmt.Fprintln(os.Stderr, "same PR that deletes the script. See docs/plans/2026-05-23-incremental-bash-to-go-migration-plan.md")
		return 1
	}
	fmt.Println("dangling-script-refs: PASS — no deleted script left with live references")
	return 0
}

// runMigrationTestCoverage is the gate replacing migration_test_coverage.sh:
// every DB migration must have a regression test. Pure-logic, hermetic.
func runMigrationTestCoverage(args []string) int {
	fs := flag.NewFlagSet("migration-test-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	migrationsDir := fs.String("migrations-dir", "", "override migrations dir")
	testsDir := fs.String("tests-dir", "", "override unit tests dir")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := migrationtestcoverage.Check(migrationtestcoverage.Config{
		Root:          *root,
		MigrationsDir: *migrationsDir,
		UnitTestsDir:  *testsDir,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "migration-test-coverage: %v\n", err)
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

// runPkgUsageMatrix is the gate replacing pkg-usage-matrix.sh: keeps the pkg
// Real Usage Matrix doc in sync with actual imports. --check verifies, --write
// regenerates. Pure-logic (grep of imports), hermetic.
func runPkgUsageMatrix(args []string) int {
	fs := flag.NewFlagSet("pkg-usage-matrix", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	docPath := fs.String("doc-path", "", "override matrix doc path")
	write := fs.Bool("write", false, "regenerate the matrix (default: --check only)")
	fs.Bool("check", true, "verify matrix freshness (default)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	mode := pkgusagematrix.ModeCheck
	if *write {
		mode = pkgusagematrix.ModeWrite
	}

	outcome, err := pkgusagematrix.Run(pkgusagematrix.Config{
		Root:    *root,
		DocPath: *docPath,
	}, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pkg-usage-matrix: %v\n", err)
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

// runCRDSemanticDiff is the gate replacing crd-semantic-diff.sh: detects
// semantic drift between source CRDs and the rendered loopzero-crds chart.
// Shells out to `helm template` directly (no shared abstraction); unit tests
// are hermetic via the override hook.
func runCRDSemanticDiff(args []string) int {
	fs := flag.NewFlagSet("crd-semantic-diff", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	crd := fs.String("crd", "", "limit to a single CRD short-name (e.g. tenants)")
	verbose := fs.Bool("verbose", false, "append inline diff snippets")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := crdsemanticdiff.Check(crdsemanticdiff.Config{
		Root:      *root,
		CRDFilter: *crd,
		Verbose:   *verbose,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "crd-semantic-diff: %v\n", err)
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

// firstNonEmpty returns the first non-empty string (flag value > env fallback).
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// runBaselineCompare replaces baseline-compare.sh: per-package coverage
// regression gate vs the checked-in baseline.
func runBaselineCompare(args []string) int {
	fs := flag.NewFlagSet("baseline-compare", flag.ContinueOnError)
	baseline := fs.String("baseline", "", "checked-in baseline JSON")
	current := fs.String("current", "", "PR-side coverage JSON")
	defaultMin := fs.Float64("default-min", 80.0, "min for newly-added packages")
	slack := fs.Float64("slack", 3.0, "tolerance below baseline (pp)")
	deferLabel := fs.Bool("defer", false, "defer label present (EC-2 escape valve)")
	deferReason := fs.String("defer-reason", "", "defer justification")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// Env fallback matches the bash env contract (BASELINE_FILE, CURRENT_FILE,
	// DEFER_*). Untrusted PR-derived DEFER_REASON arrives via env, not CLI arg.
	baselinePath := firstNonEmpty(*baseline, os.Getenv("BASELINE_FILE"))
	currentPath := firstNonEmpty(*current, os.Getenv("CURRENT_FILE"))
	deferPresent := *deferLabel || os.Getenv("DEFER_LABEL_PRESENT") == "1"
	reason := firstNonEmpty(*deferReason, os.Getenv("DEFER_REASON"))
	outcome, err := baselinecompare.Run(baselinecompare.Config{
		BaselinePath:      baselinePath,
		CurrentPath:       currentPath,
		DefaultMin:        *defaultMin,
		SlackPP:           *slack,
		DeferLabelPresent: deferPresent,
		DeferReason:       reason,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "baseline-compare: %v\n", err)
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

// runBaselineUpdate replaces baseline-update.sh: regenerates the coverage
// baseline JSON. --dry-run prints the would-be JSON to stdout.
func runBaselineUpdate(args []string) int {
	fs := flag.NewFlagSet("baseline-update", flag.ContinueOnError)
	goProfile := fs.String("go-profile", "", "Go coverage profile")
	lcov := fs.String("lcov", "", "CLI lcov.info")
	baseline := fs.String("baseline", "", "baseline JSON write target")
	dryRun := fs.Bool("dry-run", false, "print would-be JSON to stdout instead of writing")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// Env fallback matches bash contract (DRY_RUN, GO_PROFILE, CLI_LCOV, BASELINE_FILE).
	result, err := baselineupdate.Run(baselineupdate.Config{
		GoProfilePath: firstNonEmpty(*goProfile, os.Getenv("GO_PROFILE")),
		LCOVPath:      firstNonEmpty(*lcov, os.Getenv("CLI_LCOV")),
		BaselinePath:  firstNonEmpty(*baseline, os.Getenv("BASELINE_FILE")),
		DryRun:        *dryRun || os.Getenv("DRY_RUN") == "1",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "baseline-update: %v\n", err)
		return 5
	}
	if *dryRun {
		fmt.Print(string(result.JSON))
	} else {
		fmt.Fprintf(os.Stderr, "baseline-update: wrote %d packages to %s\n", result.PackagesWritten, result.OutputPath)
	}
	return 0
}

// runTemplateCompliance replaces template-compliance.sh: PR-template checkbox
// compliance gate.
func runTemplateCompliance(args []string) int {
	fs := flag.NewFlagSet("template-compliance", flag.ContinueOnError)
	prBody := fs.String("pr-body", "", "PR markdown body (falls back to $PR_BODY)")
	prAuthor := fs.String("pr-author", "", "PR author login (falls back to $PR_AUTHOR)")
	prLabels := fs.String("pr-labels", "", "CSV PR labels (falls back to $PR_LABELS)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	// Env fallback: PR body is untrusted input. CI passes it via env var
	// (NOT a CLI arg) to avoid GitHub Actions ${{ }} shell-injection.
	body, author, labels := *prBody, *prAuthor, *prLabels
	if body == "" {
		body = os.Getenv("PR_BODY")
	}
	if author == "" {
		author = os.Getenv("PR_AUTHOR")
	}
	if labels == "" {
		labels = os.Getenv("PR_LABELS")
	}
	outcome, err := templatecompliance.Run(templatecompliance.Config{
		PRBody:   body,
		PRAuthor: author,
		PRLabels: labels,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "template-compliance: %v\n", err)
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

// runHelmDiffStructural is the gate replacing helm-diff-structural.sh: fails
// CI only on STRUCTURALLY RISKY chart diffs (security-resource removal,
// namespace change, security-context downgrade). Shells out to `diff -u`.
func runHelmDiffStructural(args []string) int {
	fs := flag.NewFlagSet("helm-diff-structural", flag.ContinueOnError)
	chart := fs.String("chart", "", "chart name (diagnostics)")
	base := fs.String("base", "", "path to base rendered YAML")
	pr := fs.String("pr", "", "path to PR rendered YAML")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if *base == "" || *pr == "" {
		fmt.Fprintln(os.Stderr, "helm-diff-structural: --base and --pr are required")
		return 1
	}

	outcome, err := helmdiffstructural.Check(helmdiffstructural.Config{
		Chart:    *chart,
		BasePath: *base,
		PRPath:   *pr,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "helm-diff-structural: %v\n", err)
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

func runSkipLink(args []string) int {
	fs := flag.NewFlagSet("skip-link", flag.ContinueOnError)
	dir := fs.String("dir", ".", "target directory to scan")
	baseline := fs.String("baseline", "infra/quality/skip-link-baseline.txt", "baseline file")
	ignoreBaseline := fs.Bool("ignore-baseline", false, "treat all violations as new")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := skiplink.Check(skiplink.Config{
		TargetDir:      *dir,
		BaselineFile:   *baseline,
		IgnoreBaseline: *ignoreBaseline,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[skip-link] %v\n", err)
		return 5
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[skip-link] FAIL: %d NEW skip(s) without issue URL:\n", len(o.NewViolations))
		for _, v := range o.NewViolations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[skip-link] fix: add a GitHub issue URL near the Skip, OR rename to *_skip_test.go")
		if o.BaselineCount > 0 {
			fmt.Fprintf(os.Stderr, "[skip-link] note: %d grandfathered violations in baseline — do NOT add new entries\n", o.BaselineCount)
		}
		return 1
	}
	if o.BaselineCount > 0 {
		fmt.Printf("[skip-link] PASS — no NEW violations (%d grandfathered)\n", o.BaselineCount)
	} else {
		fmt.Println("[skip-link] PASS — all skip calls have issue URL")
	}
	return 0
}

func runHelmDrift(args []string) int {
	fs := flag.NewFlagSet("helm-drift", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	format := fs.String("format", "human", "output format: human | github-annotations")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := helmdrift.Check(helmdrift.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "helm-drift: %v\n", err)
		return 5
	}

	if *format == "json" {
		// Match the legacy bash JSON schema (consumed by charts-drift-cron.yml).
		type driftJSON struct {
			Pair                     string   `json:"pair"`
			Intentional              bool     `json:"intentional"`
			ADR                      *string  `json:"adr"`
			Severity                 string   `json:"severity"`
			TemplatesOnlyInPublished []string `json:"templates_only_in_published"`
			TemplatesOnlyInBootstrap []string `json:"templates_only_in_bootstrap"`
		}
		out := struct {
			Drifts []driftJSON `json:"drifts"`
		}{Drifts: []driftJSON{}}
		for _, d := range o.Drifts {
			sev := "high"
			var adr *string
			if d.Intentional {
				sev = "info"
				a := d.ADR
				adr = &a
			}
			pub, boot := d.OnlyInPublished, d.OnlyInBootstrap
			if pub == nil {
				pub = []string{}
			}
			if boot == nil {
				boot = []string{}
			}
			out.Drifts = append(out.Drifts, driftJSON{
				Pair: d.Pair, Intentional: d.Intentional, ADR: adr, Severity: sev,
				TemplatesOnlyInPublished: pub, TemplatesOnlyInBootstrap: boot,
			})
		}
		b, _ := json.Marshal(out)
		fmt.Println(string(b))
		if !o.Pass {
			return 1
		}
		return 0
	}

	if len(o.Drifts) == 0 {
		fmt.Println("✓ charts/ ↔ infra/helm/ drift audit: no drifts detected.")
		return 0
	}
	for _, d := range o.Drifts {
		marker := "⚠"
		status := "needs ADR or fix"
		if d.Intentional {
			marker = "ℹ"
			status = "intentional (approved by " + d.ADR + ")"
		}
		if *format == "github-annotations" && !d.Intentional {
			fmt.Printf("::warning::Drift detected for %s: templates differ\n", d.Pair)
		}
		fmt.Printf("%s %s\n", marker, d.Pair)
		if len(d.OnlyInPublished) > 0 {
			fmt.Printf("  Only in published: %s\n", strings.Join(d.OnlyInPublished, " "))
		}
		if len(d.OnlyInBootstrap) > 0 {
			fmt.Printf("  Only in bootstrap: %s\n", strings.Join(d.OnlyInBootstrap, " "))
		}
		fmt.Printf("  STATUS: %s\n", status)
	}
	if !o.Pass {
		fmt.Fprintln(os.Stderr, "❌ Drifts without approving ADR detected.")
		return 1
	}
	fmt.Println("✓ All drifts have approving ADRs (intentional).")
	return 0
}

func runRuntimeSLO(args []string) int {
	fs := flag.NewFlagSet("runtime-slo", flag.ContinueOnError)
	logFile := fs.String("log", "/tmp/go-test-output.log", "go test -v output log")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := runtimeslo.Check(runtimeslo.Config{LogFile: *logFile, Limits: runtimeslo.DefaultLimits()})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[runtime-slo] %v\n", err)
		return 5
	}
	if o.Skipped {
		fmt.Printf("[runtime-slo] no go test log at %s — skip\n", *logFile)
		return 0
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[runtime-slo] FAIL: %d test(s)/suite(s) over SLO:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "[runtime-slo] for legitimate slow tests, add: // slow_test: <issue-url>")
		return 1
	}
	fmt.Println("[runtime-slo] PASS — all tests/suites within SLO")
	return 0
}

func runActionPinning(args []string) int {
	fs := flag.NewFlagSet("action-pinning", flag.ContinueOnError)
	dir := fs.String("workflows-dir", ".github/workflows", "directory of *.yml workflow files")
	actionsDir := fs.String("actions-dir", "", "optional dir of composite actions (<dir>/*/action.yml) to also scan")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := actionpinning.Check(actionpinning.Config{WorkflowsDir: *dir, ActionsDir: *actionsDir})
	if err != nil {
		fmt.Fprintf(os.Stderr, "action-pinning: %v\n", err)
		return 2
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "❌ INF-G18 FAILED — %d tag-pinned action(s):\n\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Action: replace each @v<N> with @<sha40> (40-char commit SHA).")
		fmt.Fprintln(os.Stderr, "If a line is genuinely exempt, prepend: # pragma: allow-tag-pin")
		return 1
	}
	fmt.Printf("✅ INF-G18 GREEN — 0 tag-pinned actions in %d files (%d workflows + composite actions).\n",
		o.FileCount, o.WorkflowCount)
	return 0
}

func runProdImageTags(args []string) int {
	fs := flag.NewFlagSet("prod-image-tags", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	scanPaths := fs.Args() // optional positional scan-path overrides
	o, err := prodimagetags.Check(prodimagetags.Config{Root: *root, ScanPaths: scanPaths})
	if err != nil {
		fmt.Fprintf(os.Stderr, "prod-image-tags: %v\n", err)
		return 2
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "✗ VIOLATION: %d mutable image tag(s) in production-scoped manifests:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Per ADR-0020 D1: production manifests MUST use immutable :sha7 tags.")
		fmt.Fprintln(os.Stderr, "To override an intentional case, add '# loopzero:mutable-tag-allowed' + ADR ref.")
		return 1
	}
	fmt.Printf("✓ Scanned %d file(s). No :prod or :latest tags in production manifests.\n", o.ScannedFiles)
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

func runNoMigrateForce(args []string) int {
	fs := flag.NewFlagSet("no-migrate-force", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := nomigrateforce.Check(nomigrateforce.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-migrate-force: %v\n", err)
		return 1
	}
	if !o.Pass {
		// allowlist: migrate force (this is the gate's own diagnostic message)
		msg := fmt.Sprintf("%d unallowlisted occurrence(s) of 'migrate force' in production code. "+
			"Per CORRECTIONS.txt item B, forcing a migration on a production DB corrupts "+
			"schema_migrations. Use psql for ad-hoc SQL, OR add a '# allowlist: migrate force "+
			"(rationale)' marker on the same line or within 3 lines above.", len(o.Blocked))
		if os.Getenv("GITHUB_ACTIONS") != "" {
			fmt.Printf("::error::%s\n", msg)
		} else {
			fmt.Fprintf(os.Stderr, "[no-migrate-force] BLOCK: %s\n", msg)
		}
		fmt.Fprintln(os.Stderr, "[no-migrate-force] blocked matches:")
		for _, b := range o.Blocked {
			fmt.Fprintf(os.Stderr, "  %s\n", b)
		}
		return 2 // contract: exit 2 for violations (matches legacy bash gate)
	}
	if o.MatchCount > 0 {
		fmt.Printf("[no-migrate-force] all %d matches allowlisted — OK.\n", o.MatchCount)
	} else {
		fmt.Println("[no-migrate-force] no matches — OK.")
	}
	return 0
}

func runCosignCutover(args []string) int {
	return runRatchet("cosign-cutover", deadlineratchet.Spec{
		MarkerRelPath:    ".github/v1.0-ga-date.txt",
		ExceptionRelPath: ".github/cosign-cutover-exception.txt",
		WarnDay:          75,
		DeadlineDay:      90,
	}, args, ratchetLabels{
		dormant:   "pre-GA — marker absent. Check is dormant.",
		subject:   "cosign keyless cutover",
		exception: ".github/cosign-cutover-exception.txt",
		adr:       "2026-05-12-cosign-keyless-v11-cutover.md",
	})
}

func runServiceAuthRotation(args []string) int {
	return runRatchet("service-auth-rotation", deadlineratchet.Spec{
		MarkerRelPath:    ".github/service-auth-ed25519-rotation-date.txt",
		ExceptionRelPath: ".github/service-auth-rotation-exception.txt",
		WarnDay:          60,
		DeadlineDay:      90,
	}, args, ratchetLabels{
		dormant:   "no rotation marker — check is dormant. First rotation should create it.",
		subject:   "service-auth Ed25519 rotation",
		exception: ".github/service-auth-rotation-exception.txt",
		adr:       "2026-05-13-service-auth-ed25519-adoption.md",
	})
}

func runSecurityContext(args []string) int {
	fs := flag.NewFlagSet("security-context", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	gate := fs.Bool("gate", false, "gate mode: exit 1 if any NON-CONFORMANT chart lacks an ADR exception")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := securitycontext.Check(securitycontext.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "security-context: %v\n", err)
		return 2
	}

	if *gate {
		if o.Pass {
			fmt.Println("✅ securityContext audit GREEN — 0 NON-CONFORMANT charts without ADR exception.")
			return 0
		}
		fmt.Fprintf(os.Stderr, "❌ securityContext audit FAILED — %d NON-CONFORMANT chart(s) without ADR:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
			fmt.Fprintf(os.Stderr, "    Action: fix securityContext in infra/helm/%s/, OR create infra/helm/%s/ADR-securitycontext-exception.md\n", v, v)
		}
		return 1
	}

	// report mode — markdown matrix to stdout.
	yn := func(b bool) string {
		if b {
			return "yes"
		}
		return "no"
	}
	fmt.Println("# securityContext Audit — infra/helm/")
	fmt.Println()
	fmt.Printf("**Charts inspected:** %d\n\n", len(o.Charts))
	fmt.Println("| Chart | runAsNonRoot | readOnlyRootFS | drop:[ALL] | allowPrivilegeEscalation:false | Status | ADR Exception |")
	fmt.Println("|---|---|---|---|---|---|---|")
	var conformant, partial, nonconf, na int
	for _, c := range o.Charts {
		if !c.PodBearing {
			fmt.Printf("| %s | n/a | n/a | n/a | n/a | N/A (no pods) | — |\n", c.Name)
			na++
			continue
		}
		adr := "—"
		if c.HasADR {
			adr = "✅ exists"
		} else if c.Status == "NON-CONFORMANT" {
			adr = "❌ missing"
		}
		fmt.Printf("| %s | %s | %s | %s | %s | %s | %s |\n",
			c.Name, yn(c.RunAsNonRoot), yn(c.ReadOnlyRootFS), yn(c.DropAll), yn(c.NoPrivEsc), c.Status, adr)
		switch c.Status {
		case "CONFORMANT":
			conformant++
		case "PARTIAL":
			partial++
		case "NON-CONFORMANT":
			nonconf++
		}
	}
	fmt.Printf("\n## Summary\n\n- CONFORMANT: %d/%d\n- PARTIAL: %d/%d\n- NON-CONFORMANT: %d/%d\n- N/A (no pods): %d/%d\n- NON-CONFORMANT without ADR: %d\n",
		conformant, len(o.Charts), partial, len(o.Charts), nonconf, len(o.Charts), na, len(o.Charts), len(o.Violations))
	return 0
}

func runClusterImagePolicyMode(args []string) int {
	fs := flag.NewFlagSet("clusterimagepolicy-mode", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := clusterimagepolicymode.Check(clusterimagepolicymode.Config{Root: *root})
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "FAIL — clusterImagePolicy.mode governance violation: %s\n", o.Reason)
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "The deny-flip rejects unsigned images at admission. Either revert mode to")
		fmt.Fprintln(os.Stderr, "'warn', OR add an ADR in .claude/knowledge-base/decisions/ (or a MEET record)")
		fmt.Fprintln(os.Stderr, "referencing the cutover (T8.6 / find_infra_008 / deny-mode / mitigation #4).")
		return 1
	}
	fmt.Printf("PASS — %s\n", o.Reason)
	return 0
}

func runBaseImageDigest(args []string) int {
	fs := flag.NewFlagSet("base-image-digest", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := baseimagedigest.Check(baseimagedigest.Config{Root: *root})
	if !o.Pass {
		for _, v := range o.Violations {
			fmt.Printf("BASE-IMAGE-DIGEST VIOLATION: %s\n", v)
		}
		fmt.Fprintf(os.Stderr, "\nFAIL — %d tag-only base image(s).\n", len(o.Violations))
		fmt.Fprintln(os.Stderr, "Fix: replace 'tag' with 'tag@sha256:<digest>' (Renovate pinDigests: true),")
		fmt.Fprintln(os.Stderr, "OR add '# pragma: allow-tag-pin' on the line above the FROM, OR refresh the")
		fmt.Fprintln(os.Stderr, "baseline in pkg/quality/baseimagedigest if the bump is intentional + grandfathered.")
		return 1
	}
	fmt.Printf("PASS — %d FROM directive(s); every base image is digest-pinned, baselined, or pragma'd.\n", o.Scanned)
	return 0
}

func runAlertRunbooks(args []string) int {
	fs := flag.NewFlagSet("alert-runbooks", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := alertrunbooks.Check(alertrunbooks.Config{Root: *root})
	if !o.Pass {
		for _, v := range o.Violations {
			fmt.Printf("RUNBOOK-MISSING VIOLATION: %s\n", v)
		}
		fmt.Fprintf(os.Stderr, "\nFAIL — %d alert(s) without runbook annotation.\n", len(o.Violations))
		fmt.Fprintln(os.Stderr, "To fix: add a `runbook_url:` annotation linking to recovery docs, OR add the")
		fmt.Fprintln(os.Stderr, "alert name to ExceptionList (pkg/quality/alertrunbooks) with an explanation.")
		return 1
	}
	fmt.Println("PASS — every alert has a runbook (or is in the exception list).")
	return 0
}

func runOrphanAlerts(args []string) int {
	fs := flag.NewFlagSet("orphan-alerts", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := orphanalerts.Check(orphanalerts.Config{Root: *root})
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "FAIL — %d orphan alert(s) (vector(0) > 1 placeholder, metric not emitted):\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  ORPHAN-ALERT VIOLATION: %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Either wire the real metric + rewrite the expr, OR add the alert name to")
		fmt.Fprintln(os.Stderr, "ExceptionList (pkg/quality/orphanalerts) with a comment + linked work item.")
		return 1
	}
	fmt.Println("PASS — no orphan alerts.")
	return 0
}

func runAGPLIsolation(args []string) int {
	fs := flag.NewFlagSet("agpl-isolation", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := agplisolation.Check(agplisolation.Config{Root: *root})
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "FAIL — %d AGPL violation(s) outside the ADR-0048 allow-list:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  AGPL VIOLATION: %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "AGPL-3.0 has copyleft network-distribution requirements. Either re-implement")
		fmt.Fprintln(os.Stderr, "with an Apache-2.0 alternative, OR add the import to DefaultAllowlist in")
		fmt.Fprintln(os.Stderr, "pkg/quality/agplisolation with a NEW ADR + legal sign-off.")
		return 1
	}
	fmt.Println("PASS — no AGPL imports outside the ADR-0048 allow-list.")
	return 0
}

func runCLIBundleSize(args []string) int {
	fs := flag.NewFlagSet("cli-bundle-size", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := clibundlesize.Check(clibundlesize.Config{Root: *root})
	mb := func(b int64) float64 { return float64(b) / 1024 / 1024 }
	if o.Skipped {
		fmt.Println("[cli-bundle-size] PASS — cli/dist not present (run 'cd cli && npm run build' first).")
		return 0
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "[cli-bundle-size] FAIL — cli/dist %.1f MB > cap %.1f MB (T9.3 governance).\n",
			mb(o.TotalBytes), mb(o.MaxBytes))
		fmt.Fprintln(os.Stderr, "Options: (1) remove/lazy-load the heavy dependency, OR")
		fmt.Fprintln(os.Stderr, "(2) bump MaxBytes in pkg/quality/clibundlesize + add an ADR justifying the growth.")
		return 1
	}
	fmt.Printf("[cli-bundle-size] PASS — cli/dist %.1f MB <= cap %.1f MB.\n", mb(o.TotalBytes), mb(o.MaxBytes))
	return 0
}

func runDrillOutputsConsumers(args []string) int {
	fs := flag.NewFlagSet("drill-outputs-consumers", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := drilloutputs.Check(drilloutputs.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "drill-outputs-consumers: %v\n", err)
		return 2
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "✗ Found %d legacy drill-outputs consumer reference(s):\n", len(o.Findings))
		for _, f := range o.Findings {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Each match MUST use the v2 FLAT NAMED EXPORTS schema: replace nested")
		fmt.Fprintln(os.Stderr, "cluster/pg/bucket access with direct named outputs, e.g. mgmtClusterKubeconfig,")
		fmt.Fprintln(os.Stderr, "pgHost, bucketBuilds (pulumi stack output <name>).")
		fmt.Fprintln(os.Stderr, "Per L2-unlock plan T1.3 + EC-02. See finding-2026-05-16-pulumi-outputs-rca.md.")
		return 1
	}
	fmt.Println("✓ No legacy drill-outputs consumers found.")
	return 0
}

func runDecisionsDeletion(args []string) int {
	fs := flag.NewFlagSet("decisions-deletion", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	base := fs.String("base-ref", "origin/main", "base git ref")
	head := fs.String("head-ref", "HEAD", "head git ref")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o := decisionsdeletion.Check(decisionsdeletion.Config{RepoRoot: *root, BaseRef: *base, HeadRef: *head})
	for _, s := range o.Superseded {
		fmt.Printf("✓ %s (properly superseded)\n", s)
	}
	if !o.Pass {
		fmt.Fprintf(os.Stderr, "✗ VIOLATION: %d protected file(s) deleted without a superseding record:\n", len(o.Violations))
		for _, v := range o.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Per Plan Amendment Policy, deletion of a ratified ADR/MEET/runbook/inventory")
		fmt.Fprintln(os.Stderr, "requires a superseding file with 'superseded_by: <deleted-basename>' frontmatter,")
		fmt.Fprintln(os.Stderr, "OR explicit MEET ratification of the deletion.")
		return 1
	}
	if len(o.DeletedFiles) == 0 {
		fmt.Println("✓ No protected decisions/meetings/runbooks deleted. Stability hook clean.")
	} else {
		fmt.Printf("✓ All %d deletion(s) properly superseded. Stability hook clean.\n", len(o.DeletedFiles))
	}
	return 0
}

func runStackCoverage(args []string) int {
	fs := flag.NewFlagSet("stack-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := stackcoverage.Check(stackcoverage.Config{Root: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "[stack-coverage] FAIL: %v\n", err)
		return 1
	}
	if !o.Pass {
		if len(o.TypeErrors) > 0 {
			fmt.Fprintln(os.Stderr, "[stack-coverage] strategy type drift in languages.go:")
			for _, e := range o.TypeErrors {
				fmt.Fprintf(os.Stderr, "  %s\n", e)
			}
		}
		if len(o.Missing) > 0 {
			fmt.Fprintln(os.Stderr, "[stack-coverage] strategies without E2E fixture coverage:")
			for _, m := range o.Missing {
				fmt.Fprintf(os.Stderr, "  %s (add infra/tests/e2e/fixtures/<name>/loopzero.yaml with framework: %s)\n", m, m)
			}
		}
		fmt.Fprintln(os.Stderr, "[stack-coverage] add fixtures OR deregister the strategy from languages.go::ResolveStrategy.")
		return 1
	}
	fmt.Printf("[stack-coverage] PASS: all %d strategies have fixture coverage.\n", o.StrategyCount)
	return 0
}

type ratchetLabels struct {
	dormant   string // dormant-state message
	subject   string // e.g. "cosign keyless cutover"
	exception string // exception file path (for the unblock hint)
	adr       string // governing ADR filename
}

// runRatchet is the shared CLI handler for deadline-ratchet gates (cosign
// cutover §6.B.4, service-auth rotation §6.B.5). Exit: 0 OK/warn/exception,
// 1 usage/parse error, 2 blocked.
func runRatchet(name string, spec deadlineratchet.Spec, args []string, lbl ratchetLabels) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	asOfStr := fs.String("as-of", "", "reference date YYYY-MM-DD (testing; default: today)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	cfg := deadlineratchet.Config{Root: *root}
	if *asOfStr != "" {
		t, err := time.Parse("2006-01-02", *asOfStr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "::error::invalid --as-of date: %s\n", *asOfStr)
			return 1
		}
		cfg.AsOf = t
	}
	o, err := deadlineratchet.Check(spec, cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error::%v\n", err)
		return 1
	}
	if o.Dormant {
		fmt.Printf("[%s] %s\n", name, lbl.dormant)
		return 0
	}
	fmt.Printf("[%s] start = %s; days since = %d\n", name, o.Date, o.Days)
	gha := os.Getenv("GITHUB_ACTIONS") != ""
	emit := func(level, msg string) {
		if gha {
			fmt.Printf("::%s::%s\n", level, msg)
		} else {
			fmt.Fprintf(os.Stderr, "[%s] %s: %s\n", name, strings.ToUpper(level), msg)
		}
	}
	switch {
	case o.Blocked:
		emit("error", fmt.Sprintf("%s DEADLINE BLOWN at day %d (>%d days). No sponsor exception found. "+
			"Per ADR %s this auto-blocks. To unblock: execute it and update the marker, OR create "+
			"%s with an 'approved-by: <name>' line.", lbl.subject, o.Days, spec.DeadlineDay, lbl.adr, lbl.exception))
		return 2
	case o.ExceptionPermitted:
		emit("warning", fmt.Sprintf("%s deadline (%dd) BLOWN at day %d — sponsor exception present. Permitting.",
			lbl.subject, spec.DeadlineDay, o.Days))
		return 0
	case o.Warn:
		emit("warning", fmt.Sprintf("%s slip-alert: day %d of %d (deadline approaching). T0+%dd window open.",
			lbl.subject, o.Days, spec.DeadlineDay, spec.WarnDay))
		return 0
	default:
		fmt.Printf("[%s] within budget (day %d of %d) — OK.\n", name, o.Days, spec.DeadlineDay)
		return 0
	}
}
