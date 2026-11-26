// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/caddyfiledevserverdeny"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "caddyfile-dev-server-deny",
		Summary: "Defense-in-depth: app-dev block MUST 404 dev-server-only paths (/src/*, /.env, /@vite/*, /node_modules/*, /vite.config.*). Regression protection for loop-console#141.",
		Setup:   setupCaddyfileDevServerDeny,
	})
}

// setupCaddyfileDevServerDeny is the Go home for the caddyfile-dev-server-deny
// gate (loop-console#141 defense-in-depth). Enforces that the app-dev block
// explicitly 404s dev-server-only paths (/src/*, /.env, /@vite/*, etc.) so
// that a stale/dev build accidentally landed in DASHBOARD_DIST cannot leak
// source or secrets. Upstream root cause (dashboard pod running vite dev
// instead of nginx serving the prod bundle) belongs to the loop-console repo.
func setupCaddyfileDevServerDeny(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := caddyfiledevserverdeny.Check(caddyfiledevserverdeny.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Skip {
			inv.Stdout.Println("caddyfile-dev-server-deny: SKIP — infra/local/Caddyfile not present")
			return outcome, nil
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("caddyfile-dev-server-deny: FAIL — %d invariant(s) violated:\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("loop-console#141 defense-in-depth: the operator-local Caddyfile app-dev block")
			inv.Stderr.Println("MUST 404 dev-server-only paths (/src/*, /.env, /@vite/*, /node_modules/*,")
			inv.Stderr.Println("/vite.config.*, /package.json). The @dev_server_only matcher + `respond 404`")
			inv.Stderr.Println("must be declared BEFORE the catch-all `handle {` so try_files fallback cannot")
			inv.Stderr.Println("bypass the deny with SPA-fallback to index.html.")
			return outcome, nil
		}
		inv.Stdout.Println("caddyfile-dev-server-deny: PASS — app-dev block explicitly 404s dev-server-only paths (loop-console#141 defense-in-depth)")
		return outcome, nil
	}
}
