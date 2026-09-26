// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noenvvaultsecrets"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "no-env-vault-secrets",
		Summary: "Forbids secrets-as-env-var regression after the ESO/OpenBao migration.",
		Setup:   setupNoEnvVaultSecrets,
	})
}

// setupNoEnvVaultSecrets is the Go home for the no-env-vault-secrets gate
// (originally T2.7 of plan 2026-06-02-openbao-adoption v1.3). Forbids env
// vars carrying secrets that should live in Vault under D11 ESO pattern.
func setupNoEnvVaultSecrets(fs *flag.FlagSet) Check {
	warnOnly := fs.Bool("warn-only", false, "treat pattern matches as WARN instead of FAIL (FORBIDDEN still FAIL)")
	allowlist := fs.String("allowlist", "", "comma-separated extra allowlisted env names")
	return func(inv Invocation) (verdict.Result, error) {
		cfg := noenvvaultsecrets.Config{
			RepoRoot: inv.Root,
			WarnOnly: *warnOnly,
		}
		if *allowlist != "" {
			for _, n := range strings.Split(*allowlist, ",") {
				cfg.Allowlist = append(cfg.Allowlist, strings.TrimSpace(n))
			}
		}

		outcome, err := noenvvaultsecrets.Check(cfg)
		if err != nil {
			return nil, err
		}
		inv.Stderr.Print(outcome.FormatReport())
		if outcome.Verdict() == verdict.Fail {
			return outcome, nil
		}
		return outcome, nil
	}
}
