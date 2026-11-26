// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/noshellwrap"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := noshellwrap.Check(noshellwrap.Config{Root: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Println("no-shell-wrap: FAIL — Go code wraps forbidden CLI without nolint:")
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  - %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("To fix: use the Go SDK (pulumi-go-sdk, helm.sh/helm/v3, client-go,")
			inv.Stderr.Println("aws-sdk-go-v2, go-github) OR add //nolint:shell-wrap on the call line")
			inv.Stderr.Println("with a justification per plan D5 + Anti-Pattern #8.")
			return outcome, nil
		}
		inv.Stdout.Println("no-shell-wrap: PASS — no forbidden shell wraps detected")
		return outcome, nil
	}
}
