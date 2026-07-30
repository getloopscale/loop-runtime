// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baselinecompare"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/baselineupdate"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyfilecspgate"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyfiledevserverdeny"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clibundlesize"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/cliexitcodes"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/correctionstxt"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/decisionsdeletion"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/devpublicbillingportalsyntax"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/devpubliocidcredirectsyntax"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/divergencematrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/drilloutputs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/gitdiff"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/orykratospublicurls"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skiplink"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/spotcheck"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/taskfileinterface"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/templatecompliance"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/testcoveragereport"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/workercsp"
)

// M9 Step 7: quality gate entry points for the "governance" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runCLIExitCodes is the Go home for the cli-exit-codes gate (migrated from
// infra/tests/scripts/check-cli-exit-codes_test.sh). Enforces Rule 4: cli/src
// process.exit() codes are 0-5 only (130/143 allowed only in SIG handlers).
func runCLIExitCodes(args []string) int {
	fs := flag.NewFlagSet("cli-exit-codes", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := cliexitcodes.Check(cliexitcodes.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "cli-exit-codes: %v\n", err)
		return 5
	}
	if outcome.Skipped {
		fmt.Println("cli-exit-codes: SKIP — cli/src/ not present")
		return 0
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "cli-exit-codes: FAIL — %d process.exit() call(s) outside 0-5 contract:\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Permanent contract (Rule 4): 0/1/2/3/4/5 only. SIG handlers may use 130/143.")
		return 1
	}
	fmt.Println("cli-exit-codes: PASS — all cli/src exit codes within 0-5 contract (signal handlers exempt)")
	return 0
}

// runCorrectionsTxtClosed is the Go home for the corrections-txt-closed gate
// (migrated from infra/tests/check-corrections-txt-closed_test.sh). Enforces
// close-out hygiene on the drill war-journal infra/tests/performance/load/CORRECTIONS.txt.
func runCorrectionsTxtClosed(args []string) int {
	fs := flag.NewFlagSet("corrections-txt-closed", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := correctionstxt.Check(filepath.Join(*root, "infra", "tests", "performance", "load", "CORRECTIONS.txt"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "corrections-txt-closed: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "corrections-txt-closed: FAIL — %d close-out violation(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("corrections-txt-closed: PASS — all CORRECTIONS.txt items are annotated + closed out")
	return 0
}

// runSpotcheckDimension replaces spotcheck-dimension.sh: seeded pick of one
// audit dimension + persona for the nightly adversarial spotcheck.
func runSpotcheckDimension(args []string) int {
	fs := flag.NewFlagSet("spotcheck-dimension", flag.ContinueOnError)
	seed := fs.Int64("seed", 0, "selection seed (0 → $SPOTCHECK_SEED or time.Now)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	s := *seed
	if s == 0 {
		if env := os.Getenv("SPOTCHECK_SEED"); env != "" {
			if v, err := strconv.ParseInt(env, 10, 64); err == nil {
				s = v
			}
		}
	}
	if s == 0 {
		s = time.Now().Unix()
	}
	outcome, err := spotcheck.Run(spotcheck.Config{Seed: s})
	if err != nil {
		fmt.Fprintf(os.Stderr, "spotcheck-dimension: %v\n", err)
		return 5
	}
	for _, m := range outcome.Messages {
		fmt.Println(m)
	}
	return 0
}

// runGitDiffToLines replaces git-diff-to-lines.sh (a bash+Python wrapper):
// runs `git diff --unified=0 base...head` and emits one `path:line` per
// touched line. Parse logic lives in the tested cmd/loopzero-ops/internal/quality/gitdiff.
func runGitDiffToLines(args []string) int {
	fs := flag.NewFlagSet("git-diff-to-lines", flag.ContinueOnError)
	base := fs.String("base", "", "base ref (falls back to $BASE_REF, then origin/main)")
	head := fs.String("head", "", "head ref (falls back to $HEAD_REF, then HEAD)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	b := firstNonEmpty(*base, os.Getenv("BASE_REF"), "origin/main")
	h := firstNonEmpty(*head, os.Getenv("HEAD_REF"), "HEAD")
	// #nosec G204 -- b/h are CI-controlled git refs, not user input.
	cmd := exec.Command("git", "diff", "--unified=0", b+"..."+h)
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-diff-to-lines: git diff %s...%s: %v\n", b, h, err)
		return 5
	}
	pairs, err := gitdiff.Parse(strings.NewReader(string(out)))
	if err != nil {
		fmt.Fprintf(os.Stderr, "git-diff-to-lines: parse: %v\n", err)
		return 5
	}
	if s := gitdiff.FormatLines(pairs); s != "" {
		fmt.Print(s)
		if !strings.HasSuffix(s, "\n") {
			fmt.Println()
		}
	}
	return 0
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
		fmt.Fprintln(os.Stderr, "cmd/loopzero-ops/internal/quality/taskfileinterface/check.go AND CI consumers.")
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
		fmt.Fprintln(os.Stderr, "(2) bump MaxBytes in cmd/loopzero-ops/internal/quality/clibundlesize + add an ADR justifying the growth.")
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

// runCaddyfileCSP is the Go home for the caddyfile-csp gate (migrated from
// infra/tests/check-caddyfile-app-dev-no-csp_test.sh) per plan
// caddyfile-csp-vite-dev-vs-prod-bundle v1.1 T1.1.
//
// Closes the 2026-06-26 LIVE regression on app-dev.loopzero.example/device-verify
// where the operator-local-edge Caddyfile inherited prod-strict CSP from
// #44/#45/#46 that broke the Vite-dev upstream. ADR D1+D2+D3 invariants
// asserted via Go regex/string matching on the app-dev block; sibling blocks
// (cloud-dev, auth-dev) are not affected (block-bound extraction via brace
// counting).
func runCaddyfileCSP(args []string) int {
	fs := flag.NewFlagSet("caddyfile-csp", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := caddyfilecspgate.Check(caddyfilecspgate.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "caddyfile-csp: %v\n", err)
		return 5
	}
	if outcome.Skipped {
		fmt.Println("caddyfile-csp: SKIP — infra/local/Caddyfile not present")
		return 0
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "caddyfile-csp: FAIL — %d invariant(s) violated:\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Per plan caddyfile-csp-vite-dev-vs-prod-bundle v1.1: the operator-local-edge")
		fmt.Fprintln(os.Stderr, "Per plan caddyfile-app-dev-prod-bundle-file-server-canonical v1 (closes Red Team")
		fmt.Fprintln(os.Stderr, "loopzero-cloud#66): the operator-local Caddyfile app-dev block MUST serve the")
		fmt.Fprintln(os.Stderr, "prod-bundle via file_server + emit canonical CSP from dashboard/nginx.conf.")
		return 1
	}
	fmt.Println("caddyfile-csp: PASS — app-dev block serves prod-bundle via file_server + canonical CSP + 4 preserved headers + try_files SPA fallback (Red Team #66 closed)")
	return 0
}

// runCaddyfileDevServerDeny is the Go home for the caddyfile-dev-server-deny
// gate (loopzero-cloud#141 defense-in-depth). Enforces that the app-dev block
// explicitly 404s dev-server-only paths (/src/*, /.env, /@vite/*, etc.) so
// that a stale/dev build accidentally landed in DASHBOARD_DIST cannot leak
// source or secrets. Upstream root cause (dashboard pod running vite dev
// instead of nginx serving the prod bundle) belongs to the loopzero-cloud repo.
func runCaddyfileDevServerDeny(args []string) int {
	fs := flag.NewFlagSet("caddyfile-dev-server-deny", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := caddyfiledevserverdeny.Check(caddyfiledevserverdeny.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "caddyfile-dev-server-deny: %v\n", err)
		return 5
	}
	if outcome.Skipped {
		fmt.Println("caddyfile-dev-server-deny: SKIP — infra/local/Caddyfile not present")
		return 0
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "caddyfile-dev-server-deny: FAIL — %d invariant(s) violated:\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "loopzero-cloud#141 defense-in-depth: the operator-local Caddyfile app-dev block")
		fmt.Fprintln(os.Stderr, "MUST 404 dev-server-only paths (/src/*, /.env, /@vite/*, /node_modules/*,")
		fmt.Fprintln(os.Stderr, "/vite.config.*, /package.json). The @dev_server_only matcher + `respond 404`")
		fmt.Fprintln(os.Stderr, "must be declared BEFORE the catch-all `handle {` so try_files fallback cannot")
		fmt.Fprintln(os.Stderr, "bypass the deny with SPA-fallback to index.html.")
		return 1
	}
	fmt.Println("caddyfile-dev-server-deny: PASS — app-dev block explicitly 404s dev-server-only paths (loopzero-cloud#141 defense-in-depth)")
	return 0
}

// runDevPublicBillingPortalSyntax is the Go home for M20 T2.1 gate 1/18
// (M-Migrate2 canonical shape from M16 T2.1 sprintsyntax reference).
func runDevPublicBillingPortalSyntax(args []string) int {
	fs := flag.NewFlagSet("dev-public-billing-portal-syntax", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	outcome, err := devpublicbillingportalsyntax.Check(devpublicbillingportalsyntax.Config{RepoRoot: *root})
	if err != nil {
		if len(outcome.Failures) == 0 {
			fmt.Fprintf(os.Stderr, "dev-public-billing-portal-syntax: %v\n", err)
			return 5
		}
		fmt.Fprintf(os.Stderr, "dev-public-billing-portal-syntax: FAIL — %d assertion(s) violated:\n", len(outcome.Failures))
		for _, v := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("dev-public-billing-portal-syntax: PASS — 8/8 hermetic assertions on infra/tests/e2e/live/dev-public-billing-portal_test.sh (M20 T2.1)")
	return 0
}

// runDevPublicOIDCRedirectSyntax is the Go home for M22 T2.1 Wave A gate 2/17
// (M-Migrate2 canonical shape reused verbatim from M20 T2.1 devpublicbillingportalsyntax).
func runDevPublicOIDCRedirectSyntax(args []string) int {
	fs := flag.NewFlagSet("dev-public-oidc-redirect-syntax", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := devpubliocidcredirectsyntax.Check(devpubliocidcredirectsyntax.Config{RepoRoot: *root})
	if err != nil {
		if len(outcome.Failures) == 0 {
			fmt.Fprintf(os.Stderr, "dev-public-oidc-redirect-syntax: %v\n", err)
			return 5
		}
		fmt.Fprintf(os.Stderr, "dev-public-oidc-redirect-syntax: FAIL — %d assertion(s) violated:\n", len(outcome.Failures))
		for _, v := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("dev-public-oidc-redirect-syntax: PASS — 9/9 hermetic assertions on infra/tests/e2e/live/dev-public-oidc-redirect_test.sh (M22 T2.1)")
	return 0
}

// runOryKratosPublicURLs is the Go home for the ory-kratos-public-urls gate
// (M22 empirical follow-up — the ONLY M22 target that was allowlisted).
// Follows M16 T3.5 esochartsanity canonical shape (helm-template golden
// test + per-env regex assertions + advisory-fallthrough when helmfile
// binary absent).
func runOryKratosPublicURLs(args []string) int {
	fs := flag.NewFlagSet("ory-kratos-public-urls", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := orykratospublicurls.Check(orykratospublicurls.Config{RepoRoot: *root})
	if err != nil {
		if len(outcome.Failures) == 0 {
			fmt.Fprintf(os.Stderr, "ory-kratos-public-urls: %v\n", err)
			return 5
		}
		fmt.Fprintf(os.Stderr, "ory-kratos-public-urls: FAIL — %d golden assertion(s) violated:\n", len(outcome.Failures))
		for _, v := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	if outcome.Skipped {
		fmt.Printf("ory-kratos-public-urls: SKIP — %s\n", outcome.SkipReason)
		return 0
	}
	fmt.Println("ory-kratos-public-urls: PASS — helm-template golden assertions on Ory Kratos public URLs (M22 empirical follow-up)")
	return 0
}

// runTestCoverageReport is the Go home for the test-coverage-report gate
// (M23 T5.1 F22 closure — coverage_pct not measured). Runs `go test -cover -json`
// against configured scope + threshold. Advisory-fallthrough when `go` binary
// absent.
func runTestCoverageReport(args []string) int {
	fs := flag.NewFlagSet("test-coverage-report", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	threshold := fs.Float64("threshold", testcoveragereport.DefaultCoverageThreshold, "coverage percentage floor (0-100)")
	tags := fs.String("tags", "", "go build tags (e.g., helm_render)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := testcoveragereport.Check(testcoveragereport.Config{
		RepoRoot:          *root,
		CoverageThreshold: *threshold,
		BuildTags:         *tags,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "test-coverage-report: %v\n", err)
		return 5
	}
	if outcome.Skipped {
		fmt.Printf("test-coverage-report: SKIP — %s\n", outcome.SkipReason)
		return 0
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "test-coverage-report: FAIL — coverage=%.2f%% (threshold=%.2f%%)", outcome.CoveragePct, *threshold)
		if len(outcome.FailedPackages) > 0 {
			fmt.Fprintf(os.Stderr, "; failed packages:")
			for _, p := range outcome.FailedPackages {
				fmt.Fprintf(os.Stderr, " %s", p)
			}
		}
		fmt.Fprintln(os.Stderr)
		return 1
	}
	fmt.Printf("test-coverage-report: PASS — coverage=%.2f%% (threshold=%.2f%%)\n", outcome.CoveragePct, *threshold)
	return 0
}

// runWorkerCSP is the #235 gate: the Cloudflare static worker's CSP must not
// permit arbitrary inline/eval'd script. Registered but NOT wired into
// quality:gates — the finding is real and its fix lives in a
// governance-gated path; see the failingGate entry in quality_gate_wiring_test.go.
func runWorkerCSP(args []string) int {
	fs := flag.NewFlagSet("worker-csp", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root")
	worker := fs.String("worker", "", "worker source path relative to root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	out, err := workercsp.Check(workercsp.Config{RepoRoot: *root, WorkerPath: *worker})
	if err != nil {
		fmt.Fprintf(os.Stderr, "worker-csp: %v\n", err)
		return 2
	}
	if out.Skipped {
		fmt.Printf("worker-csp: SKIP — %s\n", out.Reason)
		return 0
	}
	if out.Pass {
		fmt.Printf("worker-csp: PASS — %s\n", out.Reason)
		return 0
	}
	fmt.Fprintln(os.Stderr, "worker-csp: FAIL")
	for _, f := range out.Findings {
		fmt.Fprintf(os.Stderr, "  %s:%d — %s\n", workercsp.DefaultWorkerPath, f.Line, f.Detail)
	}
	fmt.Fprintln(os.Stderr, "Fix: drop 'unsafe-inline' from script-src; use a per-request nonce or external+hashed scripts (issue #235).")
	return 1
}
