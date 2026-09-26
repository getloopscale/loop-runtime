// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/devpublicenv"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "dev-public-env",
		Summary: "Locks dev-public.yaml canonical shape: publicAuthUrl/publicCloudUrl/uiBaseUrl/dashboardOrigin point to *-dev.loopzero.example, cookieDomain=loopzero.example, CORS + allowedReturnUrls cover both dev hosts with trailing-slash variants, no localhost or legacy non-dev URLs leak, D5 OAuth reuse (no _DEV env var override), Stripe webhook per-env split documented. M-Migrate2 2026-07-07 replaces infra/tests/helm/dev-public-env_test.sh.",
		Setup:   setupDevPublicEnv,
	})
}

// setupDevPublicEnv enforces the canonical shape of
// infra/helmfile/environments/dev-public.yaml (dev URLs, cookieDomain,
// CORS + allowedReturnUrls, no localhost/legacy leaks, D5 OAuth reuse).
// Migrated from infra/tests/helm/dev-public-env_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupDevPublicEnv(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := devpublicenv.Check(devpublicenv.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, devpublicenv.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("dev-public-env: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		inv.Stdout.Println("dev-public-env: PASS — dev-public.yaml canonical shape preserved (dev URLs, cookieDomain, CORS, D5 OAuth reuse)")
		return Pass, nil
	}
}
