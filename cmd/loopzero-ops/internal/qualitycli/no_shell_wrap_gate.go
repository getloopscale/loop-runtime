// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

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
			fmt.Fprintln(inv.Stderr, "no-shell-wrap: FAIL — Go code wraps forbidden CLI without nolint:")
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  - %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "To fix: use the Go SDK (pulumi-go-sdk, helm.sh/helm/v3, client-go,")
			fmt.Fprintln(inv.Stderr, "aws-sdk-go-v2, go-github) OR add //nolint:shell-wrap on the call line")
			fmt.Fprintln(inv.Stderr, "with justificativa per plan D5 + Anti-Pattern #8.")
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "no-shell-wrap: PASS — no forbidden shell wraps detected")
		return Pass, nil
	}
}
