// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/caddyhostparity"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "caddy-host-parity",
		Summary: "A hostname the edge check requires needs a site block that terminates TLS.",
		Setup:   setupCaddyHostParity,
	})
}

func setupCaddyHostParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("caddy-host-parity: FAIL — %v\n", out.Violation)
			for _, f := range out.Findings {
				inv.Stderr.Printf("  %s has no site block; without a certificate the handshake fails before any HTTP\n", f.Host)
			}
			return out, nil
		}
		inv.Stdout.Printf("caddy-host-parity: PASS — %d hostnames required, %d served\n", out.Scanned, out.Served)
		return out, nil
	}
}
