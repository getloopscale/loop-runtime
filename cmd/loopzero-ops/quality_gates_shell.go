// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/danglingdata"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/danglingrefs"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/installgithooks"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nonewbash"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/quarantineprotect"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scriptcount"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/scriptsaudit"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellcheckadvisory"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellhygiene"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillpaths"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/sprintsyntax"
)

// M9 Step 7: quality gate entry points for the "shell" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

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
	resolveDotRoot(root)

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
	resolveDotRoot(root)

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
		fmt.Fprintln(os.Stderr, "same PR that deletes the script. See 2026-05-23-incremental-bash-to-go-migration-plan")
		return 1
	}
	fmt.Println("dangling-script-refs: PASS — no deleted script left with live references")
	return 0
}

// runInstallGitHooks validates the M8 1.5 pre-push hook + installer.
// Migrated from infra/tests/check-install-git-hooks_test.sh (M-Migrate2 2026-07-07).
func runInstallGitHooks(args []string) int {
	fs := flag.NewFlagSet("install-git-hooks", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := installgithooks.Check(installgithooks.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "install-git-hooks: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("install-git-hooks: PASS — pre-push hook + installer canonical shape validated")
	return 0
}

func runNoNewBash(args []string) int {
	fs := flag.NewFlagSet("no-new-bash", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", "infra/quality/shell-scripts-allowlist.md", "allowlist markdown file")
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
		fmt.Fprintln(os.Stderr, "OR migrate the function to: helmfile (D2), cmd/loopzero-ops/internal/quality/X Go gate (D5),")
		fmt.Fprintln(os.Stderr, "Ginkgo (T4), go:generate (T5.2). See 2026-05-22-shell-script-strategic-plan")
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
	resolveDotRoot(root)

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
	resolveDotRoot(root)
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
		fmt.Fprintf(os.Stderr, "script-count: FAIL — %d canonical .sh > baseline %d (a PR may never increase the count; see 2026-05-24-script-minimization-program-plan)\n", v.Count, v.Baseline)
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
	resolveDotRoot(root)

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

// runShellcheckAdvisory runs shellcheck as an advisory-only quality gate.
// When shellcheck is missing on PATH, emits WARN + exits 0 (advisory).
// When shellcheck is installed, exits 1 on violations (excluding tolerated
// codes: SC1091, SC2155, SC2086, SC2046).
// Migrated from infra/scripts/local/shellcheck-advisory.sh
// (M-Migrate2 continuation 2026-07-07).
func runShellcheckAdvisory(args []string) int {
	fs := flag.NewFlagSet("shellcheck-advisory", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := shellcheckadvisory.Check(shellcheckadvisory.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "shellcheck-advisory: FAIL — %d file(s) scanned; violations follow:\n%s\n", outcome.ScannedFiles, outcome.Diagnostic)
		return 1
	}
	if outcome.Warn {
		fmt.Println("[shellcheck] WARN: shellcheck not installed — gate is advisory-only.")
		fmt.Println("[shellcheck] Install via apt/brew/go to enable enforcement (R1-F5 T4.1).")
		return 0
	}
	fmt.Printf("shellcheck-advisory: PASS — %d files clean (SC1091/SC2155/SC2086/SC2046 tolerated)\n", outcome.ScannedFiles)
	return 0
}

// runSkillPaths enforces T0.1 EC D5 — every infra/scripts/ path AND every
// `task <name>` invocation referenced in any .claude/skills/*/SKILL.md
// MUST resolve on disk (or be an allow-listed generic skill).
// Migrated from infra/tests/skills/skill-paths_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runSkillPaths(args []string) int {
	fs := flag.NewFlagSet("skill-paths", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := skillpaths.Check(skillpaths.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "skill-paths: FAIL — %d broken reference(s) across SKILL.md files\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Printf("skill-paths: PASS — %d skills, %d paths, %d task invocations resolve\n",
		outcome.CheckedSkills, outcome.CheckedPaths, outcome.CheckedTasks)
	return 0
}

// runSprintSyntax enforces the check-sprint.sh hermetic contract: script exists,
// bash -n clean, helper compiles, no python3 heredoc, JSON report shape valid,
// allowlist entry present. Requires bash + python3 on PATH.
// Migrated from infra/tests/check-sprint-syntax_test.sh
// (M-Migrate2 2026-07-10 M16 T2.1 Wave 1).
func runSprintSyntax(args []string) int {
	fs := flag.NewFlagSet("sprint-syntax", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := sprintsyntax.Check(sprintsyntax.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "sprint-syntax: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("sprint-syntax: PASS — 11 hermetic contract assertions clean (script + bash-n + helper + no-heredoc + json-shape + allowlist)")
	return 0
}
