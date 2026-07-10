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
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyfilenodockerip"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/vaultbootstrapunsealloop"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/optionbeso"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/vaultbootstrapinmgmt"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/esooverlay"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/allowlistsunsets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/routeauthcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/k6loadrigor"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/skillpaths"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/devpublicenv"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/orykratoslifespan"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/shellcheckadvisory"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/loopzeroapiopenbao"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/renderschemagensql"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/credentialssot"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pulumirequiredsecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/sprintsyntax"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/archivescriptrepo"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nocloud"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/lokivaultretention"
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

// runCaddyfileNoDockerIP enforces that the canonical infra/local/Caddyfile does
// NOT hardcode Docker network IPs (172.18.x.x) in non-comment lines and that
// both wildcard blocks reverse_proxy to the stable https://127.0.0.1:8443.
// Migrated from infra/tests/check-caddyfile-no-docker-network-ip_test.sh
// (M-Migrate2 continuation 2026-07-07 — closes Red Team loopzero-cloud#127 guard).
func runCaddyfileNoDockerIP(args []string) int {
	fs := flag.NewFlagSet("caddyfile-no-docker-ip", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := caddyfilenodockerip.Check(caddyfilenodockerip.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "caddyfile-no-docker-ip: FAIL — %d violation(s):\n", len(outcome.Violations))
		for _, v := range outcome.Violations { fmt.Fprintf(os.Stderr, "  %s\n", v) }
		return 1
	}
	fmt.Println("caddyfile-no-docker-ip: PASS — Caddyfile canonical (no 172.18.x.x, both wildcard blocks use https://127.0.0.1:8443)")
	return 0
}

// runVaultBootstrapUnsealLoop enforces the 2026-06-15 canonical fix of
// infra/scripts/local/vault-bootstrap-dev.sh Steps 2 + 3 (rc-independent
// bao-status parse, capitalized "True" compare, no lowercase fallback trap).
// Migrated from infra/tests/check-vault-bootstrap-unseal-loop_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runVaultBootstrapUnsealLoop(args []string) int {
	fs := flag.NewFlagSet("vault-bootstrap-unseal-loop", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := vaultbootstrapunsealloop.Check(vaultbootstrapunsealloop.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault-bootstrap-unseal-loop: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("vault-bootstrap-unseal-loop: PASS — canonical fix preserved (rc-independent parse, capitalized True compare, anti-patterns absent)")
	return 0
}

// runOptionBESO enforces docs/operations/option-b-mgmt-helmfile-eso-deploy.md
// contract (4 sub-paths named, B1 recommended, B4 rejected, canonical commands
// documented, rollback + decision matrix + cross-references present, ESO
// version drift against runtime helmfile detected).
// Migrated from infra/tests/check-option-b-eso-deploy-doc_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runOptionBESO(args []string) int {
	fs := flag.NewFlagSet("option-b-eso-deploy-doc", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := optionbeso.Check(optionbeso.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "option-b-eso-deploy-doc: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("option-b-eso-deploy-doc: PASS — Option B doc contract preserved (4 sub-paths, decision rationale, canonical commands, ESO drift check)")
	return 0
}

// runVaultBootstrapInMgmt enforces the `task k3d:vault:bootstrap-in-mgmt`
// shortcut contract — canonical mirror of the upstream `k3d:vault:bootstrap`
// with exactly two well-known differences (context swap + pre-flight message).
// Migrated from infra/tests/check-vault-bootstrap-in-mgmt-task_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runVaultBootstrapInMgmt(args []string) int {
	fs := flag.NewFlagSet("vault-bootstrap-in-mgmt-task", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := vaultbootstrapinmgmt.Check(vaultbootstrapinmgmt.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault-bootstrap-in-mgmt-task: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("vault-bootstrap-in-mgmt-task: PASS — shortcut mirrors upstream (helmfile+wait counts match, context swap total, pre-flight cites task setup)")
	return 0
}

// runESOOverlay enforces the dev-public loopzero-external-secrets overlay contract
// (post-T1.2 workload identity: enabled=false safe default, K8s-auth shape,
// no legacy AppRole roleId/secretRef, canonical mgmt DNS, traceability).
// Migrated from infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runESOOverlay(args []string) int {
	fs := flag.NewFlagSet("dev-public-loopzero-external-secrets-overlay", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := esooverlay.Check(esooverlay.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dev-public-loopzero-external-secrets-overlay: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("dev-public-loopzero-external-secrets-overlay: PASS — post-T1.2 workload-identity contract preserved (K8s-auth, no legacy AppRole)")
	return 0
}

// runAllowlistSunsets is a hermetic contract test for the Python
// .claude/scripts/check-allowlist-sunsets.py gate (T5.2.a fixture verifier).
// Migrated from infra/tests/check-allowlist-sunsets_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runAllowlistSunsets(args []string) int {
	fs := flag.NewFlagSet("check-allowlist-sunsets", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := allowlistsunsets.Check(allowlistsunsets.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "check-allowlist-sunsets: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("check-allowlist-sunsets: PASS — 7 fixture cases + live check GREEN (future/today/past/malformed/bad-date/missing/live)")
	return 0
}

// runRouteAuthCoverage enforces R2-F8 (HIGH): every user-facing router.go route
// MUST be inside middleware.Auth group OR registerProtectedRoutes OR appear in
// the explicit public-allowlist.
// Migrated from infra/tests/check-route-auth-coverage_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runRouteAuthCoverage(args []string) int {
	fs := flag.NewFlagSet("route-auth-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := routeauthcoverage.Check(routeauthcoverage.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "route-auth-coverage: FAIL — %v\n", err)
		if len(outcome.Violations) > 0 {
			fmt.Fprintf(os.Stderr, "  Auth group spans lines %d..%d\n", outcome.AuthStart, outcome.AuthEnd)
			fmt.Fprintf(os.Stderr, "  registerProtectedRoutes spans lines %d..%d\n", outcome.ProtStart, outcome.ProtEnd)
			for _, v := range outcome.Violations { fmt.Fprintf(os.Stderr, "  %s\n", v) }
			fmt.Fprintln(os.Stderr, "Fix: move the route inside middleware.Auth group OR add explicit allowlist entry with rationale.")
		}
		return 1
	}
	fmt.Printf("route-auth-coverage: PASS — %d routes either in Auth group or allowlisted (auth spans %d..%d, protected %d..%d)\n",
		outcome.PassCount, outcome.AuthStart, outcome.AuthEnd, outcome.ProtStart, outcome.ProtEnd)
	return 0
}

// runK6LoadRigor enforces the k6 load-harness rigor fixes (dropped_iterations
// threshold in base + L5 distributed manifest shape + README per-level doc).
// Migrated from infra/tests/check-k6-load-rigor_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runK6LoadRigor(args []string) int {
	fs := flag.NewFlagSet("k6-load-rigor", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := k6loadrigor.Check(k6loadrigor.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "k6-load-rigor: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("k6-load-rigor: PASS — dropped_iterations threshold in base + L5 manifest shape + per-level docs canonical")
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
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := skillpaths.Check(skillpaths.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "skill-paths: FAIL — %d broken reference(s) across SKILL.md files\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Printf("skill-paths: PASS — %d skills, %d paths, %d task invocations resolve\n",
		outcome.CheckedSkills, outcome.CheckedPaths, outcome.CheckedTasks)
	return 0
}

// runDevPublicEnv enforces the canonical shape of
// infra/helmfile/environments/dev-public.yaml (dev URLs, cookieDomain,
// CORS + allowedReturnUrls, no localhost/legacy leaks, D5 OAuth reuse).
// Migrated from infra/tests/helm/dev-public-env_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runDevPublicEnv(args []string) int {
	fs := flag.NewFlagSet("dev-public-env", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := devpublicenv.Check(devpublicenv.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dev-public-env: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("dev-public-env: PASS — dev-public.yaml canonical shape preserved (dev URLs, cookieDomain, CORS, D5 OAuth reuse)")
	return 0
}

// runOryKratosLifespan enforces canonical self-service flow lifespans in
// infra/helm/ory-kratos/values.yaml.gotmpl (login=10m, registration=10m,
// recovery=15m — defense pair with edge rate_limit for Red Team #89).
// Migrated from infra/tests/helm/ory-kratos-lifespan_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runOryKratosLifespan(args []string) int {
	fs := flag.NewFlagSet("ory-kratos-lifespan", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := orykratoslifespan.Check(orykratoslifespan.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "ory-kratos-lifespan: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("ory-kratos-lifespan: PASS — login/registration=10m, recovery=15m (defense pair with edge rate_limit for RedTeam #89)")
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
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
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

// runLoopZeroAPIOpenbao enforces T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync:
// the loopzero-api chart unconditionally wires the AppRole secret_id via
// envFrom.secretRef to K8s Secret loopzero-openbao-approle with optional=true,
// with NO inline value: for OPENBAO_VAULT_SECRET_ID (anti-hardcode).
// Migrated from infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runLoopZeroAPIOpenbao(args []string) int {
	fs := flag.NewFlagSet("loopzero-api-openbao-approle-envfrom", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := loopzeroapiopenbao.Check(loopzeroapiopenbao.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "loopzero-api-openbao-approle-envfrom: FAIL — %v\n", err)
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	if outcome.Warn {
		fmt.Println("[loopzero-api-openbao] WARN: helm not installed — gate is advisory-only (installer notes: brew install helm / apt install helm).")
		return 0
	}
	fmt.Println("loopzero-api-openbao-approle-envfrom: PASS — envFrom.loopzero-openbao-approle optional=true wired, anti-hardcode preserved")
	return 0
}

// runRenderSchemaGenSQL renders api/internal/db/schema.gen.sql via
// an ephemeral PG 16 container + golang-migrate replay + pg_dump + sqlc
// compile pre-flight. Use --render for the full destructive pipeline; the
// default check-only mode validates toolchain + config presence.
// Migrated from infra/scripts/ci/render-schema-gen-sql.sh (M-Migrate2 continuation 2026-07-07).
func runRenderSchemaGenSQL(args []string) int {
	fs := flag.NewFlagSet("render-schema-gen-sql", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	render := fs.Bool("render", false, "run the destructive render pipeline (spins docker + rewrites schema.gen.sql); default is check-only")
	advisoryOnly := fs.Bool("advisory-only", false, "return Warn instead of failure when a required tool is missing")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	cfg := renderschemagensql.Config{RepoRoot: *root, AdvisoryOnly: *advisoryOnly}
	if !*render {
		outcome, err := renderschemagensql.Check(cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "render-schema-gen-sql: FAIL — %v\n", err)
			return 1
		}
		if outcome.Warn {
			fmt.Printf("render-schema-gen-sql: WARN (%s) — pass --advisory-only=false to convert to hard fail once toolchain lands\n", outcome.WarnReason)
			return 0
		}
		fmt.Println("render-schema-gen-sql: PASS — toolchain + migrations + sqlc.yaml present; pass --render to execute the pipeline")
		return 0
	}
	outcome, err := renderschemagensql.Render(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "render-schema-gen-sql: FAIL — %v\n%s\n", err, outcome.Diagnostic)
		return 1
	}
	if outcome.Warn {
		fmt.Printf("render-schema-gen-sql: WARN (%s)\n", outcome.WarnReason)
		return 0
	}
	fmt.Printf("render-schema-gen-sql: PASS — schema.gen.sql rendered (%d lines) + sqlc compile clean\n", outcome.SchemaLines)
	return 0
}

// runCredentialsSot enforces T1.1 SoT drift audit — every Secret created by
// canonical generator scripts is enumerated in docs/operations/credentials-required.md.
// Migrated from infra/tests/docs/credentials-sot_test.sh
// (M-Migrate2 continuation 2026-07-07 — final KEEP-CI-WIRED wave).
func runCredentialsSot(args []string) int {
	fs := flag.NewFlagSet("credentials-sot", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := credentialssot.Check(credentialssot.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "credentials-sot: FAIL — %d issue(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		if len(outcome.Discovered) > 0 {
			fmt.Fprintln(os.Stderr, "Discovered Secret names:")
			for _, s := range outcome.Discovered { fmt.Fprintf(os.Stderr, "  - %s\n", s) }
		}
		return 1
	}
	fmt.Printf("credentials-sot: PASS — %d bootstrap-created Secrets enumerated in docs/operations/credentials-required.md\n", len(outcome.Discovered))
	return 0
}

// runPulumiRequiredSecrets enforces T1.5 — every Pulumi.<env>.yaml enumerates
// the required secret keys for that environment (either as `secure:` entry
// or as a comment). Cross-checks the same keys appear in the SoT doc.
// Migrated from infra/tests/docs/pulumi-required-secrets_test.sh
// (M-Migrate2 continuation 2026-07-07 — final KEEP-CI-WIRED wave).
func runPulumiRequiredSecrets(args []string) int {
	fs := flag.NewFlagSet("pulumi-required-secrets", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil { return 1 }
	if absRoot, err := os.Getwd(); err == nil && *root == "." { *root = absRoot }
	outcome, err := pulumirequiredsecrets.Check(pulumirequiredsecrets.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pulumi-required-secrets: FAIL — %d issue(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures { fmt.Fprintf(os.Stderr, "  %s\n", f) }
		return 1
	}
	fmt.Println("pulumi-required-secrets: PASS — prod(6) + staging(4) + drill(3) secrets all listed in Pulumi.<env>.yaml + SoT doc")
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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
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

// runArchiveScriptRepoRoot enforces T1.2 of plan m1-commit-canonical-30-fixes —
// no script in archive/canonical-tests-v1/scripts/local/ may resolve REPO_ROOT
// via 3-dirs-up ($(dirname "$0")/../../..); canonical post-quarantine 289cf9d6
// is 4-dirs-up.
// Migrated from infra/tests/check-archive-script-repo-root_test.sh
// (M-Migrate2 2026-07-10 M16 T2.2 Wave 1).
func runArchiveScriptRepoRoot(args []string) int {
	fs := flag.NewFlagSet("archive-script-repo-root", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := archivescriptrepo.Check(archivescriptrepo.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "archive-script-repo-root: FAIL — %d 3-dirs-up violation(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		fmt.Fprintln(os.Stderr, "Fix: replace `$(dirname \"$0\")/../../..` with `$(dirname \"$0\")/../../../..` (canonical post-quarantine 289cf9d6).")
		return 1
	}
	fmt.Println("archive-script-repo-root: PASS — no 3-dirs-up REPO_ROOT resolution in archive/canonical-tests-v1/scripts/local/")
	return 0
}

// runNoCloudDevDefault enforces T1.1 of plan m1-commit-canonical-30-fixes —
// no test/seed script may declare a default env-var value pointing at
// `cloud-dev.loopzero.example` (Caddy 301 redirect trap). Webhook handler is
// the single exception (EC-1 absorption).
// Migrated from infra/tests/check-no-cloud-dev-default_test.sh
// (M-Migrate2 2026-07-10 M16 T2.3 Wave 1).
func runNoCloudDevDefault(args []string) int {
	fs := flag.NewFlagSet("no-cloud-dev-default", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := nocloud.Check(nocloud.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-cloud-dev-default: FAIL — %d cloud-dev default(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		fmt.Fprintln(os.Stderr, "Fix: replace `${VAR:-https://cloud-dev.loopzero.example/...}` with the canonical hostname (app-dev/auth-dev/api-dev.loopzero.example per surface).")
		return 1
	}
	fmt.Println("no-cloud-dev-default: PASS — no cloud-dev.loopzero.example default env-vars in test/seed scripts (webhook handler exception allowed)")
	return 0
}

// runLokiVaultRetention enforces T8.2 + T8.5 of plan 2026-06-02-openbao-adoption:
// Loki per-NS retention config + archive CronJob hardening. 14 assertions.
// Migrated from infra/tests/check-loki-vault-retention_test.sh
// (M-Migrate2 2026-07-10 M16 T3.1 Wave 2).
func runLokiVaultRetention(args []string) int {
	fs := flag.NewFlagSet("loki-vault-retention", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
	outcome, err := lokivaultretention.Check(lokivaultretention.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "loki-vault-retention: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("loki-vault-retention: PASS — 14 hermetic assertions (T8.2 retention_stream + T8.5 archive CronJob hardening) clean")
	return 0
}
