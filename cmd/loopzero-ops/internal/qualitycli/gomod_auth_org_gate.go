// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/gomodauthorg"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "gomod-auth-org",
		Summary: "The org build files authenticate to matches the org go.mod requires (a drift makes git fetch unauthenticated and GitHub answer 'not found').",
		Setup:   setupGoModAuthOrg,
	})
}

// setupGoModAuthOrg implements the `gomod-auth-org` gate.
//
// Keeps the org that build files AUTHENTICATE to in sync with the org the go.mod files
// REQUIRE. When they drift, git fetches the private module unauthenticated and GitHub answers
// "not found" — the same message as a repo that does not exist — so the build fails naming the
// wrong cause. This shipped twice in two days from one root cause (ci.yml, then api/Dockerfile).
func setupGoModAuthOrg(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		rep, err := gomodauthorg.Check(inv.Root)
		if err != nil {
			return Fail, err
		}
		if rep.OK() {
			inv.Stdout.Printf("gomod-auth-org: PASS — auth org matches the go.mod requirement (%v)\n",
				rep.RequiredOrgs)
			return Pass, nil
		}
		for _, f := range rep.Findings {
			inv.Stderr.Printf("gomod-auth-org: FAIL %s — %s\n", f.File, f.Message)
		}
		return Fail, nil
	}
}
