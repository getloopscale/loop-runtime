// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/clusterrolenosecretsstar"
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
	return func(inv Invocation) (Outcome, error) {
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
			return Fail, err
		}
		fmt.Fprint(inv.Stderr, clusterrolenosecretsstar.FormatOutcome(outcome))
		if !outcome.Pass {
			return Fail, nil
		}
		return Pass, nil
	}
}
