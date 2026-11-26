// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"strings"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/nolegacysatoken"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "no-legacy-sa-token-secret",
		Summary: "Forbids legacy kubernetes.io/service-account-token Secrets.",
		Setup:   setupNoLegacySATokenSecret,
	})
}

// setupNoLegacySATokenSecret is the Go home for the no-legacy-sa-token-secret
// gate (T7.6 of plan 2026-06-02-openbao-adoption v1.3).
func setupNoLegacySATokenSecret(fs *flag.FlagSet) Check {
	allowlist := fs.String("allowlist", "", "comma-separated allowlisted paths")
	return func(inv Invocation) (verdict.Result, error) {
		cfg := nolegacysatoken.Config{RepoRoot: inv.Root}
		if *allowlist != "" {
			for _, p := range strings.Split(*allowlist, ",") {
				cfg.AllowlistPaths = append(cfg.AllowlistPaths, strings.TrimSpace(p))
			}
		}
		outcome, err := nolegacysatoken.Check(cfg)
		if err != nil {
			return nil, err
		}
		inv.Stderr.Print(nolegacysatoken.FormatOutcome(outcome))
		if outcome.Verdict() == verdict.Fail {
			return outcome, nil
		}
		return outcome, nil
	}
}
