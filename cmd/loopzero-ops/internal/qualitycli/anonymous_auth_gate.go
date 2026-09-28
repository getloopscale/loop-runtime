// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/anonymousauth"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "anonymous-auth",
		Summary: "Forbids anonymous/unauthenticated access on protected API surfaces.",
		Setup:   setupAnonymousAuth,
	})
}

// setupAnonymousAuth blocks `--kube-apiserver-arg=anonymous-auth=true` (the
// substring `anonymous-auth=true`) anywhere outside `infra/k3d/`. T1.5 / CR-3
// of MEET-2026-05-28-001 — see ADR
// 2026-05-28-T1.4-anonymous-auth-local-only.md for why the flag is
// LOCAL-only and why drift into a prod chart would broaden the apiserver
// attack surface.
func setupAnonymousAuth(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := anonymousauth.Check(anonymousauth.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("anonymous-auth: FAIL — %d %q occurrence(s) outside infra/k3d/:\n", len(outcome.Violations), anonymousauth.ForbiddenLiteral)
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Fix: remove the flag, or move the file under infra/k3d/. Production DOKS uses authenticated OIDC discovery via Pulumi sigstore.ts — see ADR 2026-05-28-T1.4-anonymous-auth-local-only.")
			return outcome, nil
		}
		inv.Stdout.Printf("anonymous-auth: PASS — no %q outside infra/k3d/\n", anonymousauth.ForbiddenLiteral)
		return outcome, nil
	}
}
