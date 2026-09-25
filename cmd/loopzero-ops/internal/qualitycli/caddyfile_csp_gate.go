// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyfilecspgate"
)

func init() {
	register(Gate{
		Name:    "caddyfile-csp",
		Summary: "Operator-local edge Caddyfile invariants (prod-bundle app-dev with a CSP superset of the dashboard meta CSP, preserved headers, auth-dev rate limits, trusted proxies).",
		Setup:   setupCaddyfileCSP,
	})
}

// setupCaddyfileCSP is the Go home for the caddyfile-csp gate (migrated from
// infra/tests/check-caddyfile-app-dev-no-csp_test.sh) per plan
// caddyfile-csp-vite-dev-vs-prod-bundle v1.1 T1.1.
//
// Closes the 2026-06-26 LIVE regression on app-dev.loopzero.example/device-verify
// where the operator-local-edge Caddyfile inherited prod-strict CSP from
// #44/#45/#46 that broke the Vite-dev upstream. ADR D1+D2+D3 invariants
// asserted via Go regex/string matching on the app-dev block; sibling blocks
// (cloud-dev, auth-dev) are not affected (block-bound extraction via brace
// counting).
func setupCaddyfileCSP(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := caddyfilecspgate.Check(caddyfilecspgate.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if outcome.Skipped {
			inv.Stdout.Println("caddyfile-csp: SKIP — infra/local/Caddyfile not present")
			return Pass, nil
		}
		if !outcome.Pass {
			inv.Stderr.Printf("caddyfile-csp: FAIL — %d invariant(s) violated:\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("Per plan caddyfile-app-dev-prod-bundle-file-server-canonical v1 (closes Red Team")
			inv.Stderr.Println("loopzero-cloud#66): the operator-local Caddyfile app-dev block MUST serve the")
			inv.Stderr.Println("prod-bundle via file_server + emit canonical CSP from dashboard/nginx.conf.")
			return Fail, nil
		}
		inv.Stdout.Println("caddyfile-csp: PASS — app-dev block serves prod-bundle via file_server + canonical CSP + 4 preserved headers + try_files SPA fallback (Red Team #66 closed)")
		return Pass, nil
	}
}
