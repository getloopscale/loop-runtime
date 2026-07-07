// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/danglingdata"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/danglingrefs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nonewbash"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/quarantineprotect"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scriptcount"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scriptsaudit"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellhygiene"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/webhookcanonicalpath"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/oidccredadminloopzero"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/canonicalfixturerepo"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/installgithooks"
)

// M9 Step 7: quality gate entry points for the "shell" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

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
		fmt.Fprintln(os.Stderr, "Ginkgo (T4), go:generate (T5.2). See docs/plans/completed/2026-05-22-shell-script-strategic-plan.md")
		return 1
	}
	fmt.Println("no-new-bash: PASS — no new bash scripts outside allowlist")
	return 0
}

// runQuarantineProtect is the T0.2 governance gate of plan canonical-tests-rewrite-v2 v1.0:
// fail if a PR touches archive/canonical-tests-v1/** during the strangler-fig
// window. DELETE-only allowed with [ARCHIVE-DELETE] PR title marker per ADR D5.
func runQuarantineProtect(args []string) int {
	fs := flag.NewFlagSet("quarantine-protect", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	archive := fs.String("archive", "archive/canonical-tests-v1", "quarantine path root prefix")
	baseRef := fs.String("base-ref", "origin/main", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	prTitle := fs.String("pr-title", os.Getenv("GITHUB_PR_TITLE"), "PR title (substring-matched for [ARCHIVE-DELETE] marker)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := quarantineprotect.Check(quarantineprotect.Config{
		RepoRoot:        *root,
		ArchivePathRoot: *archive,
		BaseRef:         *baseRef,
		HeadRef:         *headRef,
		PRTitle:         *prTitle,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "quarantine-protect: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "quarantine-protect: FAIL — PR touches quarantined files:")
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Per ADR-2026-06-17-T3 D5: build the new suite at infra/tests/livetest/ paths instead.")
		fmt.Fprintln(os.Stderr, "Plan: knowledge-base/plans/canonical-tests-rewrite-v2-plan.md")
		return 1
	}
	fmt.Println("quarantine-protect: PASS — no quarantine violations")
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
		fmt.Fprintln(os.Stderr, "same PR that deletes the script. See docs/plans/completed/2026-05-23-incremental-bash-to-go-migration-plan.md")
		return 1
	}
	fmt.Println("dangling-script-refs: PASS — no deleted script left with live references")
	return 0
}

// runDanglingDataRefs is the mirror backstop: fail if a deleted config/data
// file is still referenced (live) by an executable caller. Closes the
// data-orphan class (the 2026-05-11 consolidation deleted maturity-scoring data
// but left the governance scripts dangling). Requires fetch-depth: 0.
func runDanglingDataRefs(args []string) int {
	fs := flag.NewFlagSet("dangling-data-refs", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	baseRef := fs.String("base-ref", "origin/develop", "base git ref")
	headRef := fs.String("head-ref", "HEAD", "head git ref")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := danglingdata.Check(danglingdata.Config{
		RepoRoot: *root,
		BaseRef:  *baseRef,
		HeadRef:  *headRef,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dangling-data-refs: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintln(os.Stderr, "dangling-data-refs: FAIL — deleted config/data files still referenced by live callers:")
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  - %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "When deleting a config/data tree, remove or rewire the scripts that read it in")
		fmt.Fprintln(os.Stderr, "the SAME change. See finding-2026-05-23-governance-subsystem-orphaned.md")
		return 1
	}
	fmt.Println("dangling-data-refs: PASS — no deleted data file left with live references")
	return 0
}

// runShellHygiene is the Go home for the shell-hygiene gate (migrated from
// infra/tests/scripts/check-shell-hygiene_test.sh). Every non-lib .sh under
// infra/scripts, infra/helmfile/scripts, infra/tests must set a `set -euo
// pipefail` variant in its top 60 lines.
func runShellHygiene(args []string) int {
	fs := flag.NewFlagSet("shell-hygiene", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

	outcome, err := shellhygiene.Check(shellhygiene.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "shell-hygiene: %v\n", err)
		return 5
	}
	if !outcome.Pass {
		fmt.Fprintf(os.Stderr, "shell-hygiene: FAIL — %d / %d scripts missing set -euo pipefail in top 60 lines:\n", len(outcome.Violations), outcome.Total)
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Add 'set -euo pipefail' near the top. Sourced libs in **/lib/*.sh are exempt.")
		return 1
	}
	fmt.Printf("shell-hygiene: PASS — %d scripts have set -euo pipefail (libs in **/lib/*.sh exempt)\n", outcome.Total)
	return 0
}

// runScriptCount is the program-level monotonic ceiling on canonical .sh count
// (Script Minimization Program T0.1). Excludes .claude/worktrees/ and trash/
// (EC-1). --update-baseline ratchets the ceiling DOWN to current (never up).
func runScriptCount(args []string) int {
	fs := flag.NewFlagSet("script-count", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	baselinePath := fs.String("baseline", "infra/quality/script-count-baseline.json", "baseline JSON ceiling")
	update := fs.Bool("update-baseline", false, "ratchet the ceiling down to current count (write)")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	data, err := os.ReadFile(*baselinePath) // #nosec G304 -- repo-internal baseline path
	if err != nil {
		fmt.Fprintf(os.Stderr, "script-count: read baseline: %v\n", err)
		return 5
	}
	base, err := scriptcount.ParseBaseline(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "script-count: parse baseline: %v\n", err)
		return 5
	}
	v, err := scriptcount.Check(*root, base)
	if err != nil {
		fmt.Fprintf(os.Stderr, "script-count: %v\n", err)
		return 5
	}
	if *update {
		if v.Count > base.CanonicalSH {
			fmt.Fprintf(os.Stderr, "script-count: refuse to ratchet UP (%d > baseline %d)\n", v.Count, base.CanonicalSH)
			return 1
		}
		out := fmt.Sprintf("{\n  \"canonical_sh\": %d\n}\n", v.Count)
		if err := os.WriteFile(*baselinePath, []byte(out), 0o644); err != nil { // #nosec G306 -- repo-internal baseline
			fmt.Fprintf(os.Stderr, "script-count: write baseline: %v\n", err)
			return 5
		}
		fmt.Printf("script-count: baseline ratcheted to %d\n", v.Count)
		return 0
	}
	if !v.OK {
		fmt.Fprintf(os.Stderr, "script-count: FAIL — %d canonical .sh > baseline %d (a PR may never increase the count; see docs/plans/completed/2026-05-24-script-minimization-program-plan.md)\n", v.Count, v.Baseline)
		return 1
	}
	if v.Improved {
		fmt.Printf("script-count: PASS — %d canonical .sh (< baseline %d; run --update-baseline to ratchet)\n", v.Count, v.Baseline)
		return 0
	}
	fmt.Printf("script-count: PASS — %d canonical .sh (== baseline %d)\n", v.Count, v.Baseline)
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

// runWebhookCanonicalPath enforces that no test/seed script references the
// wrong /engine/api/v1/webhooks/github URL (Caddy 301-redirect + 404 trap).
// The canonical is /api/v1/webhooks/github (handler-direct).
// Migrated from infra/tests/check-webhook-canonical-path_test.sh (M-Migrate2, 2026-07-07).
func runWebhookCanonicalPath(args []string) int {
	fs := flag.NewFlagSet("webhook-canonical-path", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := webhookcanonicalpath.Check(webhookcanonicalpath.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "webhook-canonical-path: FAIL — /engine/api/v1/webhooks/github detected in %d place(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("webhook-canonical-path: PASS — no forbidden /engine/api/v1/webhooks/github references")
	return 0
}

// runOIDCCredAdminLoopzero enforces the canonical admin@loopzero.example / loopzero-dev-password
// credential — rejects fabricated paulo@loopzero.test / LoopZeroDev2026!.
// Migrated from infra/tests/check-oidc-cred-admin-loopzero_test.sh (M-Migrate2 2026-07-07).
func runOIDCCredAdminLoopzero(args []string) int {
	fs := flag.NewFlagSet("oidc-cred-admin-loopzero", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := oidccredadminloopzero.Check(oidccredadminloopzero.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "oidc-cred-admin-loopzero: FAIL — %d fabricated credential reference(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("oidc-cred-admin-loopzero: PASS — no fabricated paulo@loopzero.test/LoopZeroDev2026! (canonical: admin@loopzero.example)")
	return 0
}

// runCanonicalFixtureRepo enforces canonical getloopscale/loopzerokit-example fixture.
// Migrated from infra/tests/check-canonical-fixture-repo_test.sh (M-Migrate2 2026-07-07).
func runCanonicalFixtureRepo(args []string) int {
	fs := flag.NewFlagSet("canonical-fixture-repo", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := canonicalfixturerepo.Check(canonicalfixturerepo.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "canonical-fixture-repo: FAIL — %d legacy loopzero-repotest reference(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		return 1
	}
	fmt.Println("canonical-fixture-repo: PASS — no legacy loopzero-repotest (canonical: getloopscale/loopzerokit-example)")
	return 0
}

// runInstallGitHooks validates the M8 1.5 pre-push hook + installer.
// Migrated from infra/tests/check-install-git-hooks_test.sh (M-Migrate2 2026-07-07).
func runInstallGitHooks(args []string) int {
	fs := flag.NewFlagSet("install-git-hooks", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := installgithooks.Check(installgithooks.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "install-git-hooks: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("install-git-hooks: PASS — pre-push hook + installer canonical shape validated")
	return 0
}
