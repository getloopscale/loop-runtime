// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/proxyexposure"
)

func init() {
	register(Gate{
		Name:    "proxy-exposure",
		Summary: "Every edge reverse_proxy declares what it lets through.",
		Setup:   setupProxyExposure,
	})
}

func setupProxyExposure(fs *flag.FlagSet) Check {
	caddyfile := fs.String("caddyfile", "", "edge config (default infra/local/Caddyfile)")
	registry := fs.String("registry", "", "declaration file (default infra/quality/proxy-exposure.txt)")
	return func(inv Invocation) (Outcome, error) {
		o, err := proxyexposure.Check(proxyexposure.Config{
			Root: inv.Root, Caddyfile: *caddyfile, Registry: *registry,
		})
		if err != nil {
			return Fail, err
		}
		if !o.Pass {
			fmt.Fprintf(inv.Stderr, "[proxy-exposure] FAIL — the edge exposes something nobody declared.\n")
			for _, u := range o.Undeclared {
				fmt.Fprintf(inv.Stderr, "  UNDECLARED %s\n", u)
			}
			for _, st := range o.Stale {
				fmt.Fprintf(inv.Stderr, "  STALE      %s — declared, but no such proxy exists\n", st)
			}
			for _, m := range o.Malformed {
				fmt.Fprintf(inv.Stderr, "  MALFORMED  %s\n", m)
			}
			fmt.Fprintln(inv.Stderr, "Fix: add a row to infra/quality/proxy-exposure.txt saying what the proxy lets through.")
			fmt.Fprintln(inv.Stderr, "A reverse_proxy inherits its response from the upstream — if nobody wrote it down, nobody checked it.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "[proxy-exposure] PASS — %d edge proxies, all declared\n", o.Scanned)
		return Pass, nil
	}
}
