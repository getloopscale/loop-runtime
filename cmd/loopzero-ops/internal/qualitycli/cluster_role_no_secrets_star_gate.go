// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterrolenosecretsstar"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "cluster-role-no-secrets-star",
		Summary: "Forbids unconditional cluster-wide secrets/configmaps RBAC grants.",
		Setup:   setupClusterRoleNoSecretsStar,
	})
}

// setupClusterRoleNoSecretsStar is the Go home for the
// cluster-role-no-secrets-star gate (T5.15 of plan 2026-06-02-openbao-adoption
// v1.3). Walks templates/clusterrole.yaml in charts/ + infra/helm/ and FAILs
// if any unconditional secrets/configmaps cluster-wide grant is found.
func setupClusterRoleNoSecretsStar(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "", "comma-separated extra allowlisted chart paths (relative to root)")
	return func(inv Invocation) (verdict.Result, error) {
		cfg := clusterrolenosecretsstar.Config{
			RepoRoot: inv.Root,
		}
		if *allowlist != "" {
			for _, p := range strings.Split(*allowlist, ",") {
				cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
			}
		}

		outcome, err := clusterrolenosecretsstar.Check(cfg)
		if err != nil {
			return nil, err
		}
		inv.Stderr.Print(clusterrolenosecretsstar.FormatOutcome(outcome))
		if outcome.Verdict() == verdict.Fail {
			return outcome, nil
		}
		return outcome, nil
	}
}
