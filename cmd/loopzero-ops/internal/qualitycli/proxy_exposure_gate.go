// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/proxyexposure"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		o, err := proxyexposure.Check(proxyexposure.Config{
			Root: inv.Root, Caddyfile: *caddyfile, Registry: *registry,
		})
		if err != nil {
			return nil, err
		}
		if o.Verdict() == verdict.Fail {
			inv.Stderr.Printf("[proxy-exposure] FAIL — the edge exposes something nobody declared.\n")
			for _, u := range o.Undeclared {
				inv.Stderr.Printf("  UNDECLARED %s\n", u)
			}
			for _, st := range o.Stale {
				inv.Stderr.Printf("  STALE      %s — declared, but no such proxy exists\n", st)
			}
			for _, m := range o.Malformed {
				inv.Stderr.Printf("  MALFORMED  %s\n", m)
			}
			inv.Stderr.Println("Fix: add a row to infra/quality/proxy-exposure.txt saying what the proxy lets through.")
			inv.Stderr.Println("A reverse_proxy inherits its response from the upstream — if nobody wrote it down, nobody checked it.")
			return o, nil
		}
		inv.Stdout.Printf("[proxy-exposure] PASS — %d edge proxies, all declared\n", o.Scanned)
		return o, nil
	}
}
