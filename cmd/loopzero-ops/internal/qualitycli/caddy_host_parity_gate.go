// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/caddyhostparity"
)

func init() {
	register(Gate{
		Name:    "caddy-host-parity",
		Summary: "A hostname the edge check requires needs a site block that terminates TLS.",
		Setup:   setupCaddyHostParity,
	})
}

func setupCaddyHostParity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := caddyhostparity.Check(caddyhostparity.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, caddyhostparity.ErrUnserved) {
				return Fail, err
			}
			inv.Stderr.Printf("caddy-host-parity: FAIL — %v\n", err)
			for _, f := range out.Findings {
				inv.Stderr.Printf("  %s has no site block; without a certificate the handshake fails before any HTTP\n", f.Host)
			}
			return Fail, nil
		}
		inv.Stdout.Printf("caddy-host-parity: PASS — %d hostnames required, %d served\n", out.Scanned, out.Served)
		return Pass, nil
	}
}
