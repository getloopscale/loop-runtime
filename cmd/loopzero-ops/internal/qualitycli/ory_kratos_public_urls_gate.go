// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/orykratospublicurls"
)

func init() {
	register(Gate{
		Name:    "ory-kratos-public-urls",
		Summary: "Hermetic helm-template golden test for Ory Kratos serve.public.base_url + CORS + cookie.domain + allowed_return_urls + dashboardRoute-mapped ui_url across standalone-public + dev envs. Advisory-fallthrough when helmfile absent. M22 empirical follow-up (the ONLY M22 target allowlisted — delivers allowlist -1).",
		Setup:   setupOryKratosPublicURLs,
	})
}

// setupOryKratosPublicURLs is the Go home for the ory-kratos-public-urls gate
// (M22 empirical follow-up — the ONLY M22 target that was allowlisted).
// Follows M16 T3.5 esochartsanity canonical shape (helm-template golden
// test + per-env regex assertions + advisory-fallthrough when helmfile
// binary absent).
func setupOryKratosPublicURLs(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := orykratospublicurls.Check(orykratospublicurls.Config{RepoRoot: inv.Root})
		if err != nil {
			if len(outcome.Failures) == 0 {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "ory-kratos-public-urls: FAIL — %d golden assertion(s) violated:\n", len(outcome.Failures))
			for _, v := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			return Fail, nil
		}
		if outcome.Skipped {
			fmt.Fprintf(inv.Stdout, "ory-kratos-public-urls: SKIP — %s\n", outcome.SkipReason)
			return Pass, nil
		}
		fmt.Fprintln(inv.Stdout, "ory-kratos-public-urls: PASS — helm-template golden assertions on Ory Kratos public URLs (M22 empirical follow-up)")
		return Pass, nil
	}
}
