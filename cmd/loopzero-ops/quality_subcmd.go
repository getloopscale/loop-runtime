// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"fmt"
	"os"
)

const qualityHelp = `loopzero-ops quality — quality gate orchestrator

USAGE:
  loopzero-ops quality <gate> [flags]

INTROSPECTION:
  --list          Print the gate inventory as NAME<TAB>TASKS<TAB>SUMMARY, sorted
                  and byte-stable (the M9 gate-set oracle). TASKS = the comma-
                  joined sorted taskfiles/quality.yml task(s) invoking the gate
                  (e.g. "gates", "diff", "diff,gates"), or "-" if none. Per-task
                  attribution: a silent un-wire from quality:gates flips the cell.
                  Flags:
                    --names         names only, one per line (for a per-gate loop)
                    --json          JSON array of {name, tasks, summary}
                    --taskfile PATH taskfile scanned for the wired bit
                                    (default: "taskfiles/quality.yml")

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

  quarantine-protect
                  Fail if PR touches archive/canonical-tests-v1/** during the
                  canonical-tests-rewrite-v2 strangler-fig window. Per ADR
                  2026-06-17-T3 D5: archive is READ-ONLY consultable; DELETE
                  allowed only with PR title containing [ARCHIVE-DELETE] marker.
                  Plan: knowledge-base/plans/canonical-tests-rewrite-v2-plan.md
                  Flags:
                    --root DIR        repo root (default: ".")
                    --archive PATH    quarantine path root (default: "archive/canonical-tests-v1")
                    --base-ref REF    base git ref (default: "origin/main")
                    --head-ref REF    head git ref (default: "HEAD")
                    --pr-title STR    PR title for marker check (default: env GITHUB_PR_TITLE)

  dangling-script-refs
                  Fail if a .sh deleted in the diff is still referenced (live)
                  by Taskfile/CI/SKILL/infra callers (T0.1 migration backstop).
                  Requires fetch-depth: 0 in CI checkout.
                  Flags:
                    --root DIR      repo root (default: ".")
                    --base-ref REF  base git ref (default: "origin/develop")
                    --head-ref REF  head git ref (default: "HEAD")

  dangling-data-refs
                  Mirror of dangling-script-refs: fail if a config/data file
                  (.yaml/.yml/.json) deleted in the diff is still referenced
                  (live) by an executable caller (.sh or workflow/Taskfile YAML).
                  Closes the data-orphan class (governance subsystem 2026-05-23).
                  Requires fetch-depth: 0 in CI checkout.
                  Flags:
                    --root DIR      repo root (default: ".")
                    --base-ref REF  base git ref (default: "origin/develop")
                    --head-ref REF  head git ref (default: "HEAD")

  postgresql-pdb-config
                  Fail if infra/helm/postgresql/values.yaml does not set
                  primary.pdb.create: false (B2 regression). Migrated from
                  postgresql-pdb-config_test.sh (plan 2026-05-23).
                  Flags:
                    --root DIR      repo root (default: ".")

  dockerfile-no-docker-hub
                  Fail if any infra/images/*/Dockerfile FROM resolves to Docker
                  Hub (must use mirror.gcr.io / DOCR / full-path registry; B8
                  regression). Migrated from dockerfile-no-docker-hub_test.sh.
                  Flags:
                    --root DIR      repo root (default: ".")

  advisory-lock-scope
                  Fail if production code (api/, infra/scripts/) uses session-
                  scoped pg_advisory_lock( (Rule 7 — only the _xact_ variant is
                  pool-safe). Migrated from check-advisory-lock-scope_test.sh.
                  Flags:
                    --root DIR      repo root (default: ".")

  anonymous-auth
                  Fail if any file outside infra/k3d/ contains the LOCAL-only
                  apiserver auth flag literal (see anonymousauth.ForbiddenLiteral).
                  T1.5 / CR-3 of MEET-2026-05-28-001. The flag is a LOCAL k3d
                  Sigstore-OIDC convenience; production DOKS uses authenticated
                  discovery via Pulumi sigstore.ts. See ADR
                  2026-05-28-T1.4-anonymous-auth-local-only.
                  Flags:
                    --root DIR      repo root (default: ".")

  helm-overlap-parity
                  Fail if any of the 3 bootstrap charts (infra/helm/loopzero-api,
                  infra/helm/loopzero-cloud, infra/helm/admission-policies) has a
                  version GREATER than its published counterpart in charts/.
                  T1.2 of plan 2026-05-26-maturity-3-62-to-4-0-roadmap-plan.md.
                  Flags:
                    --root DIR      repo root (default: ".")

  prod-image-digests
                  Detect hardcoded mutable :tag image references in published
                  platform charts (charts/loopzero-*/templates). T2.3 of plan
                  2026-05-26-maturity-3-62-to-4-0-roadmap. Default mode: WARN
                  (violations reported, exit 0). Pass --enforce to fail on
                  violations after 7-day soak validates zero false positives.
                  Flags:
                    --root DIR      repo root (default: ".")
                    --enforce       fail on violations (default: warn-only)

  cli-exit-codes
                  Fail if cli/src process.exit() uses a code outside 0-5
                  (Rule 4; 130/143 allowed only in SIG handlers). Migrated from
                  check-cli-exit-codes_test.sh.
                  Flags:
                    --root DIR      repo root (default: ".")

  caddyfile-csp
                  Assert the app-dev.loopzero.example block of infra/local/Caddyfile
                  serves the prod-bundle via file_server + try_files SPA
                  fallback (Red Team loopzero-cloud#66 closed), emits the canonical
                  CSP matching dashboard/nginx.conf, preserves the 4 non-CSP
                  security headers (X-Frame-Options DENY, nosniff, Referrer-
                  Policy, HSTS), and does NOT reverse-proxy to Vite dev (which
                  leaked source TypeScript publicly). Per plan
                  caddyfile-app-dev-prod-bundle-file-server-canonical v1
                  (supersedes caddyfile-csp-vite-dev-vs-prod-bundle ADR D2).
                  Flags:
                    --root DIR      repo root (default: ".")

  shell-hygiene
                  Fail if any non-lib .sh (infra/scripts, infra/helmfile/scripts,
                  infra/tests) lacks 'set -euo pipefail' in its top 60 lines
                  (**/lib/*.sh exempt). Migrated from check-shell-hygiene_test.sh.
                  Flags:
                    --root DIR      repo root (default: ".")

  script-count    Program-level monotonic ceiling on canonical .sh count
                  (Script Minimization Program T0.1). Fails if a PR increases
                  the count above the baseline. Excludes .claude/worktrees/ and
                  trash/ (EC-1). --update-baseline ratchets the ceiling DOWN.
                  Flags:
                    --root DIR         repo root (default ".")
                    --baseline PATH    baseline JSON (default infra/quality/script-count-baseline.json)
                    --update-baseline  ratchet ceiling down to current (write)

  corrections-txt-closed
                  Fail if infra/tests/performance/load/CORRECTIONS.txt has any item without
                  a STATUS annotation, or a header missing the audit-finding
                  ref. Migrated from check-corrections-txt-closed_test.sh.
                  Flags:
                    --root DIR      repo root (default: ".")

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

  spotcheck-dimension  Seeded pick of 1 audit dimension + persona.
                  Flags: --seed N (0 → $SPOTCHECK_SEED or time.Now)

  unhomed-logic   Unbiased migration tracker — count un-homed shell scripts
                  per capability domain; monotonic baseline ratchet.
                  Flags: --report  --update-baseline  --allowlist PATH  --baseline PATH

  git-diff-to-lines  Emit one path:line per PR-touched line (git diff --unified=0).
                  Flags: --base REF  --head REF (fall back to $BASE_REF/$HEAD_REF)

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
	case "--list":
		return runQualityList(args[1:])
	}
	if g, ok := gateDispatch()[args[0]]; ok {
		return g.Run(args[1:])
	}
	fmt.Fprintf(os.Stderr, "unknown gate: %s\n\n%s", args[0], qualityHelp)
	return 1
}
