// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/credentialssot"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/drillcredisolation"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/esochartsanity"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/esocreationpolicy"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/esooverlay"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noaesremaining"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noenvvaultsecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/openbaobootstrapincore"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/openbaoinitauth"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/optionbeso"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pulumirequiredsecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/loopzeroapiopenbao"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/transitcoverage"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/vaultbootstrapunsealloop"
)

// M9 Step 7: quality gate entry points for the "secrets" theme, decomposed out of the
// former quality_subcmd.go god file. Each func is referenced as a Run in
// gateRegistry (quality_registry.go) — the single source of truth
// (ADR 2026-07-03-m9-step7-gate-registration-mechanism).

// runTransitCoverage is the Go home for the vault-prefix-format / transit-
// coverage gate (T3.7 of plan 2026-06-02-openbao-adoption v1.3). Enforces
// the FAANG dual-path invariant: Set/ListForResync MUST use the dispatcher
// (encryptForStore/decryptFromStore), never bypass to raw s.encrypt/decrypt.
func runTransitCoverage(args []string) int {
	fs := flag.NewFlagSet("transit-coverage", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := transitcoverage.Check(transitcoverage.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "transit-coverage: %v\n", err)
		return 5
	}
	fmt.Fprint(os.Stderr, outcome.FormatReport())
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runNoEnvVaultSecrets is the Go home for the no-env-vault-secrets gate
// (originally T2.7 of plan 2026-06-02-openbao-adoption v1.3). Forbids env
// vars carrying secrets that should live in Vault under D11 ESO pattern.
func runNoEnvVaultSecrets(args []string) int {
	fs := flag.NewFlagSet("no-env-vault-secrets", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	warnOnly := fs.Bool("warn-only", false, "treat pattern matches as WARN instead of FAIL (FORBIDDEN still FAIL)")
	allowlist := fs.String("allowlist", "", "comma-separated extra allowlisted env names")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)

	cfg := noenvvaultsecrets.Config{
		RepoRoot: *root,
		WarnOnly: *warnOnly,
	}
	if *allowlist != "" {
		for _, n := range strings.Split(*allowlist, ",") {
			cfg.Allowlist = append(cfg.Allowlist, strings.TrimSpace(n))
		}
	}

	outcome, err := noenvvaultsecrets.Check(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-env-vault-secrets: %v\n", err)
		return 5
	}
	fmt.Fprint(os.Stderr, outcome.FormatReport())
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runNoAESRemaining is the Go home for the no-aes-remaining gate (T10.2 of
// plan 2026-06-02-openbao-adoption v1.3). Calendar-gated — enforces only
// after T+60d from CutoverDay.
func runNoAESRemaining(args []string) int {
	fs := flag.NewFlagSet("no-aes-remaining", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	allowlist := fs.String("allowlist", "", "comma-separated allowlisted paths")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	cfg := noaesremaining.Config{RepoRoot: *root}
	if *allowlist != "" {
		for _, p := range strings.Split(*allowlist, ",") {
			cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
		}
	}
	outcome, err := noaesremaining.Check(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "no-aes-remaining: %v\n", err)
		return 5
	}
	fmt.Fprint(os.Stderr, noaesremaining.FormatOutcome(outcome))
	if !outcome.Pass {
		return 1
	}
	return 0
}

// runDrillCredsIsolation is the pure-logic port of
// infra/scripts/check-drill-credentials-isolation.sh (MIGRATE-GO-PUREFN). It
// reads the DRILL_*/prod token env vars and verifies no DRILL_* token equals
// its prod counterpart (catastrophic per D7). Exit: 0 safe / 1 catastrophic /
// 2 missing.
func runDrillCredsIsolation(_ []string) int {
	r := drillcredisolation.Check(drillcredisolation.Env{
		DrillDO:         os.Getenv("DRILL_DO_TOKEN"),
		DigitalOcean:    os.Getenv("DIGITALOCEAN_TOKEN"),
		DO:              os.Getenv("DO_TOKEN"),
		DrillPulumi:     os.Getenv("DRILL_PULUMI_TOKEN"),
		PulumiAccess:    os.Getenv("PULUMI_ACCESS_TOKEN"),
		DrillCF:         os.Getenv("DRILL_CF_TOKEN"),
		CloudflareAPI:   os.Getenv("CLOUDFLARE_API_TOKEN"),
		RequireIsolated: os.Getenv("DRILL_REQUIRE_ISOLATED_TOKENS"),
	})
	for _, m := range r.Messages {
		fmt.Fprintf(os.Stderr, "[drill-creds-isolation] %s\n", m)
	}
	return r.ExitCode
}

// runCredentialsSot enforces T1.1 SoT drift audit — every Secret created by
// canonical generator scripts is enumerated in docs/operations/credentials-required.md.
// Migrated from infra/tests/docs/credentials-sot_test.sh
// (M-Migrate2 continuation 2026-07-07 — final KEEP-CI-WIRED wave).
func runCredentialsSot(args []string) int {
	fs := flag.NewFlagSet("credentials-sot", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := credentialssot.Check(credentialssot.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "credentials-sot: FAIL — %d issue(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		if len(outcome.Discovered) > 0 {
			fmt.Fprintln(os.Stderr, "Discovered Secret names:")
			for _, s := range outcome.Discovered {
				fmt.Fprintf(os.Stderr, "  - %s\n", s)
			}
		}
		return 1
	}
	fmt.Printf("credentials-sot: PASS — %d bootstrap-created Secrets enumerated in docs/operations/credentials-required.md\n", len(outcome.Discovered))
	return 0
}

// runDevPublicLoopZeroExternalSecretsOverlay enforces the dev-public loopzero-external-secrets overlay contract
// (post-T1.2 workload identity: enabled=false safe default, K8s-auth shape,
// no legacy AppRole roleId/secretRef, canonical mgmt DNS, traceability).
// Migrated from infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runDevPublicLoopZeroExternalSecretsOverlay(args []string) int {
	fs := flag.NewFlagSet("dev-public-loopzero-external-secrets-overlay", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := esooverlay.Check(esooverlay.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "dev-public-loopzero-external-secrets-overlay: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("dev-public-loopzero-external-secrets-overlay: PASS — post-T1.2 workload-identity contract preserved (K8s-auth, no legacy AppRole)")
	return 0
}

// runOpenbaoInitAuthMounts enforces T5.1-T5.9 openbao-init auth mounts + per-binary roles.
// Migrated from infra/tests/check-openbao-init-auth-mounts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.7 Wave 2 — Wave 2 COMPLETE).
func runOpenbaoInitAuthMounts(args []string) int {
	fs := flag.NewFlagSet("openbao-init-auth-mounts", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := openbaoinitauth.Check(openbaoinitauth.Config{RepoRoot: *root})
	if errors.Is(err, openbaoinitauth.ErrChartMissing) {
		fmt.Fprintln(os.Stderr, "openbao-init-auth-mounts: FAIL — openbao-init chart missing")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "openbao-init-auth-mounts: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	if outcome.Warn {
		fmt.Println("openbao-init-auth-mounts: WARN (advisory-only: helm not on PATH)")
		return 0
	}
	fmt.Println("openbao-init-auth-mounts: PASS — T5.1-T5.9 auth mounts + per-binary roles + isolation invariants clean")
	return 0
}

// runOptionBEsoDeployDoc enforces docs/operations/option-b-mgmt-helmfile-eso-deploy.md
// contract (4 sub-paths named, B1 recommended, B4 rejected, canonical commands
// documented, rollback + decision matrix + cross-references present, ESO
// version drift against runtime helmfile detected).
// Migrated from infra/tests/check-option-b-eso-deploy-doc_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runOptionBEsoDeployDoc(args []string) int {
	fs := flag.NewFlagSet("option-b-eso-deploy-doc", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := optionbeso.Check(optionbeso.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "option-b-eso-deploy-doc: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("option-b-eso-deploy-doc: PASS — Option B doc contract preserved (4 sub-paths, decision rationale, canonical commands, ESO drift check)")
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
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := pulumirequiredsecrets.Check(pulumirequiredsecrets.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "pulumi-required-secrets: FAIL — %d issue(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("pulumi-required-secrets: PASS — prod(6) + staging(4) + drill(3) secrets all listed in Pulumi.<env>.yaml + SoT doc")
	return 0
}

// runLoopZeroApiOpenbaoApproleEnvfrom enforces T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync:
// the loopzero-api chart unconditionally wires the AppRole secret_id via
// envFrom.secretRef to K8s Secret loopzero-openbao-approle with optional=true,
// with NO inline value: for OPENBAO_VAULT_SECRET_ID (anti-hardcode).
// Migrated from infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runLoopZeroApiOpenbaoApproleEnvfrom(args []string) int {
	fs := flag.NewFlagSet("loopzero-api-openbao-approle-envfrom", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := loopzeroapiopenbao.Check(loopzeroapiopenbao.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "loopzero-api-openbao-approle-envfrom: FAIL — %v\n", err)
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	if outcome.Warn {
		fmt.Println("[loopzero-api-openbao] WARN: helm not installed — gate is advisory-only (installer notes: brew install helm / apt install helm).")
		return 0
	}
	fmt.Println("loopzero-api-openbao-approle-envfrom: PASS — envFrom.loopzero-openbao-approle optional=true wired, anti-hardcode preserved")
	return 0
}

// runLoopZeroExternalSecretsChartSanity enforces the iter 23 chart-sanity contract for
// infra/helm/loopzero-external-secrets. 8 assertions across 5 canonical envs.
// Migrated from infra/tests/check-loopzero-external-secrets-chart-sanity_test.sh
// (M-Migrate2 2026-07-10 M16 T3.5 Wave 2).
func runLoopZeroExternalSecretsChartSanity(args []string) int {
	fs := flag.NewFlagSet("loopzero-external-secrets-chart-sanity", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := esochartsanity.Check(esochartsanity.Config{RepoRoot: *root})
	if errors.Is(err, esochartsanity.ErrChartMissing) {
		fmt.Fprintln(os.Stderr, "loopzero-external-secrets-chart-sanity: FAIL — loopzero-external-secrets chart missing")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "loopzero-external-secrets-chart-sanity: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	if outcome.Warn {
		fmt.Println("loopzero-external-secrets-chart-sanity: WARN (advisory-only: helm not on PATH)")
		return 0
	}
	fmt.Println("loopzero-external-secrets-chart-sanity: PASS — 8 chart-sanity assertions across 5 canonical envs clean")
	return 0
}

// runLoopZeroExternalSecretsCreationPolicy is the iter 20 P0 regression for creationPolicy: Orphan.
// Migrated from infra/tests/check-loopzero-external-secrets-creation-policy_test.sh
// (M-Migrate2 2026-07-10 M16 T3.6 Wave 2).
func runLoopZeroExternalSecretsCreationPolicy(args []string) int {
	fs := flag.NewFlagSet("loopzero-external-secrets-creation-policy", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := esocreationpolicy.Check(esocreationpolicy.Config{RepoRoot: *root})
	if errors.Is(err, esocreationpolicy.ErrChartMissing) {
		fmt.Fprintln(os.Stderr, "loopzero-external-secrets-creation-policy: FAIL — chart missing")
		return 1
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "loopzero-external-secrets-creation-policy: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	if outcome.Warn {
		fmt.Println("loopzero-external-secrets-creation-policy: WARN (advisory-only: helm not on PATH)")
		return 0
	}
	fmt.Println("loopzero-external-secrets-creation-policy: PASS — 4 assertions (creationPolicy Orphan + Owner=0 + deletionPolicy Retain + count parity) clean")
	return 0
}

// runOpenBaoBootstrapInCoreTask enforces the `task k3d:secrets:bootstrap-in-core`
// shortcut contract — canonical mirror of the upstream `k3d:secrets:bootstrap`
// with exactly two well-known differences (context swap + pre-flight message).
// Migrated from infra/tests/check-openbao-bootstrap-in-core-task_test.sh
// (M-Migrate2 continuation 2026-07-07).
func runOpenBaoBootstrapInCoreTask(args []string) int {
	fs := flag.NewFlagSet("openbao-bootstrap-in-core-task", flag.ContinueOnError)
	root := fs.String("root", ".", "repo root directory")
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := openbaobootstrapincore.Check(openbaobootstrapincore.Config{RepoRoot: *root})
	switch {
	case errors.Is(err, openbaobootstrapincore.ErrNothingScanned):
		fmt.Fprintf(os.Stderr, "openbao-bootstrap-in-core-task: %v\n", err)
		return 2
	case err != nil:
		fmt.Fprintf(os.Stderr, "openbao-bootstrap-in-core-task: FAIL — %d violation(s) of the SECRETS/CORE boundary:\n", len(outcome.Violations))
		for _, v := range outcome.Violations {
			fmt.Fprintf(os.Stderr, "  %s\n", v)
		}
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "OpenBao runs in the SEPARATE loopzero-secrets cluster. Installing it into core is")
		fmt.Fprintln(os.Stderr, "forbidden in EVERY tier — a topology violation, not a shortcut. The canonical")
		fmt.Fprintln(os.Stderr, "path is `task k3d:secrets:create` + `task k3d:secrets:bootstrap`.")
		fmt.Fprintln(os.Stderr, "See .claude/rules/cleanroom-up-live-test-mandate.md and")
		fmt.Fprintln(os.Stderr, "docs/wiki/decisions/2026-08-12-openbao-in-core-rule-vs-task.md (B-047).")
		return 1
	}
	fmt.Printf("openbao-bootstrap-in-core-task: PASS — no core-cluster OpenBao shortcut (%d lines scanned)\n", outcome.LinesScanned)
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
	if err := fs.Parse(args); err != nil {
		return 1
	}
	resolveDotRoot(root)
	outcome, err := vaultbootstrapunsealloop.Check(vaultbootstrapunsealloop.Config{RepoRoot: *root})
	if err != nil {
		fmt.Fprintf(os.Stderr, "vault-bootstrap-unseal-loop: FAIL — %d assertion(s):\n", len(outcome.Failures))
		for _, f := range outcome.Failures {
			fmt.Fprintf(os.Stderr, "  %s\n", f)
		}
		return 1
	}
	fmt.Println("vault-bootstrap-unseal-loop: PASS — canonical fix preserved (rc-independent parse, capitalized True compare, anti-patterns absent)")
	return 0
}
