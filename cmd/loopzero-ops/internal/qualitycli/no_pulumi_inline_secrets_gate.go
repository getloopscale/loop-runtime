// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/nopulumiinlinesecrets"
)

func init() {
	register(Gate{
		Name:    "no-pulumi-inline-secrets",
		Summary: "Forbids inline secret literals in Pulumi code.",
		// 9 inline encrypted secrets across infra/pulumi/Pulumi.{dev,prod}.yaml.
		// Closing this is the T9 Vault-provider migration, on the legacy DO/Pulumi
		// layer that the AWS re-platform is retiring (ADR 2026-06-17) — moving live
		// production secrets, not an edit. Measured 2026-08-06.
		Wiring: failingGate,
		Setup:  setupNoPulumiInlineSecrets,
	})
}

// setupNoPulumiInlineSecrets is the Go home for the no-pulumi-inline-secrets
// gate (T9.6 of plan 2026-06-02-openbao-adoption v1.3).
func setupNoPulumiInlineSecrets(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "", "comma-separated allowlisted paths")
	return func(inv Invocation) (Outcome, error) {
		cfg := nopulumiinlinesecrets.Config{RepoRoot: inv.Root}
		if *allowlist != "" {
			for _, p := range strings.Split(*allowlist, ",") {
				cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
			}
		}
		outcome, err := nopulumiinlinesecrets.Check(cfg)
		if err != nil {
			return Fail, err
		}
		fmt.Fprint(inv.Stderr, nopulumiinlinesecrets.FormatOutcome(outcome))
		if !outcome.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
