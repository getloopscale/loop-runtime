// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/noshellwrap"
)

func init() {
	register(Gate{
		Name:        "no-shell-wrap",
		Summary:     "Forbids thin shell wrappers around pure-logic that belongs in Go.",
		RootDefault: "pkg",
		Setup:       setupNoShellWrap,
	})
}

func setupNoShellWrap(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := noshellwrap.Check(noshellwrap.Config{Root: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !outcome.Pass {
			inv.Stderr.Println("no-shell-wrap: FAIL — Go code wraps forbidden CLI without nolint:")
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("To fix: use the Go SDK (pulumi-go-sdk, helm.sh/helm/v3, client-go,")
			inv.Stderr.Println("aws-sdk-go-v2, go-github) OR add //nolint:shell-wrap on the call line")
			inv.Stderr.Println("with justificativa per plan D5 + Anti-Pattern #8.")
			return Fail, nil
		}
		inv.Stdout.Println("no-shell-wrap: PASS — no forbidden shell wraps detected")
		return Pass, nil
	}
}
