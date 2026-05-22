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
	"github.com/loopzero/loopzero/pkg/quality/deadlineratchet"
	"github.com/loopzero/loopzero/pkg/quality/divergencematrix"
	"github.com/loopzero/loopzero/pkg/quality/helmdrift"
	"github.com/loopzero/loopzero/pkg/quality/mockbudget"
	"github.com/loopzero/loopzero/pkg/quality/nomigrateforce"
	"github.com/loopzero/loopzero/pkg/quality/nonewbash"
	"github.com/loopzero/loopzero/pkg/quality/noshellwrap"
	"github.com/loopzero/loopzero/pkg/quality/prodimagetags"
	"github.com/loopzero/loopzero/pkg/quality/regressiontest"
	"github.com/loopzero/loopzero/pkg/quality/runtimeslo"
	"github.com/loopzero/loopzero/pkg/quality/scriptsaudit"
	"github.com/loopzero/loopzero/pkg/quality/skiplink"
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
	case "cosign-cutover":
		return runCosignCutover(args[1:])
	case "service-auth-rotation":
		return runServiceAuthRotation(args[1:])
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
	if err := fs.Parse(args); err != nil {
		return 1
	}
	o, err := actionpinning.Check(actionpinning.Config{WorkflowsDir: *dir})
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
	fmt.Printf("✅ INF-G18 GREEN — 0 tag-pinned actions in %d workflows.\n", o.WorkflowCount)
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
