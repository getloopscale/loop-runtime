// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/pulumirequiredsecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "pulumi-required-secrets",
		Summary: "T1.5 — every Pulumi.<env>.yaml enumerates required secret keys (as `secure:` entry OR comment). Prod=6 keys, staging=4, drill=3 (DO token via env var per D7' sponsor override). Cross-checks prod keys also appear backticked in docs/operations.md. M-Migrate2 2026-07-07 replaces infra/tests/docs/pulumi-required-secrets_test.sh.",
		Setup:   setupPulumiRequiredSecrets,
	})
}

// setupPulumiRequiredSecrets enforces T1.5 — every Pulumi.<env>.yaml enumerates
// the required secret keys for that environment (either as `secure:` entry
// or as a comment). Cross-checks the same keys appear in the SoT doc.
// Migrated from infra/tests/docs/pulumi-required-secrets_test.sh
// (M-Migrate2 continuation 2026-07-07 — final KEEP-CI-WIRED wave).
func setupPulumiRequiredSecrets(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := pulumirequiredsecrets.Check(pulumirequiredsecrets.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, pulumirequiredsecrets.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("pulumi-required-secrets: FAIL — %d issue(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		inv.Stdout.Println("pulumi-required-secrets: PASS — prod(6) + staging(4) + drill(3) secrets all listed in Pulumi.<env>.yaml + SoT doc")
		return Pass, nil
	}
}
