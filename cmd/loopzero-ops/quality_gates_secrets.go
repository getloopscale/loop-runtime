// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/drillcredisolation"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noaesremaining"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noenvvaultsecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/transitcoverage"
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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}

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
	if absRoot, err := os.Getwd(); err == nil && *root == "." {
		*root = absRoot
	}
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
