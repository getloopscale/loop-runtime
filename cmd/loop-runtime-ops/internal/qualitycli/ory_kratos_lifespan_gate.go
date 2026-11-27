// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/orykratoslifespan"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "ory-kratos-lifespan",
		Summary: "Locks canonical self-service flow lifespans in infra/helm/ory-kratos/values.yaml.gotmpl: login=10m + registration=10m + recovery=15m (defense pair with edge rate_limit for Red Team #89 email-bomb closure). Section-scoped stricter than bash grep -A N (sibling lifespan can NOT leak). M-Migrate2 2026-07-07 replaces infra/tests/helm/ory-kratos-lifespan_test.sh.",
		Setup:   setupOryKratosLifespan,
	})
}

// setupOryKratosLifespan enforces canonical self-service flow lifespans in
// infra/helm/ory-kratos/values.yaml.gotmpl (login=10m, registration=10m,
// recovery=15m — defense pair with edge rate_limit for Red Team #89).
// Migrated from infra/tests/helm/ory-kratos-lifespan_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupOryKratosLifespan(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := orykratoslifespan.Check(orykratoslifespan.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("ory-kratos-lifespan: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return outcome, nil
		}
		inv.Stdout.Println("ory-kratos-lifespan: PASS — login/registration=10m, recovery=15m (defense pair with edge rate_limit for RedTeam #89)")
		return outcome, nil
	}
}
