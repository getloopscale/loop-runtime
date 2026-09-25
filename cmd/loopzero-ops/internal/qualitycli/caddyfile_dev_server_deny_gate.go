// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyfiledevserverdeny"
)

func init() {
	register(Gate{
		Name:    "caddyfile-dev-server-deny",
		Summary: "Defense-in-depth: app-dev block MUST 404 dev-server-only paths (/src/*, /.env, /@vite/*, /node_modules/*, /vite.config.*). Regression protection for loopzero-cloud#141.",
		Setup:   setupCaddyfileDevServerDeny,
	})
}

// setupCaddyfileDevServerDeny is the Go home for the caddyfile-dev-server-deny
// gate (loopzero-cloud#141 defense-in-depth). Enforces that the app-dev block
// explicitly 404s dev-server-only paths (/src/*, /.env, /@vite/*, etc.) so
// that a stale/dev build accidentally landed in DASHBOARD_DIST cannot leak
// source or secrets. Upstream root cause (dashboard pod running vite dev
// instead of nginx serving the prod bundle) belongs to the loopzero-cloud repo.
func setupCaddyfileDevServerDeny(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := caddyfiledevserverdeny.Check(caddyfiledevserverdeny.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if outcome.Skipped {
			fmt.Fprintln(inv.Stdout, "caddyfile-dev-server-deny: SKIP — infra/local/Caddyfile not present")
			return Pass, nil
		}
		if !outcome.Pass {
			fmt.Fprintf(inv.Stderr, "caddyfile-dev-server-deny: FAIL — %d invariant(s) violated:\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "loopzero-cloud#141 defense-in-depth: the operator-local Caddyfile app-dev block")
			fmt.Fprintln(inv.Stderr, "MUST 404 dev-server-only paths (/src/*, /.env, /@vite/*, /node_modules/*,")
			fmt.Fprintln(inv.Stderr, "/vite.config.*, /package.json). The @dev_server_only matcher + `respond 404`")
			fmt.Fprintln(inv.Stderr, "must be declared BEFORE the catch-all `handle {` so try_files fallback cannot")
			fmt.Fprintln(inv.Stderr, "bypass the deny with SPA-fallback to index.html.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "caddyfile-dev-server-deny: PASS — app-dev block explicitly 404s dev-server-only paths (loopzero-cloud#141 defense-in-depth)")
		return Pass, nil
	}
}
