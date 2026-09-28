// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/stdlibonly"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "stdlib-only",
		Summary: "The shared vocabulary package must not import outside the standard library.",
		Setup:   setupStdlibOnly,
	})
}

func setupStdlibOnly(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := stdlibonly.Check(stdlibonly.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("stdlib-only: FAIL — %v\n", out.Violation)
			return out, nil
		}
		inv.Stdout.Printf("stdlib-only: PASS — %d import(s), all from the standard library\n", out.Scanned)
		return out, nil
	}
}
