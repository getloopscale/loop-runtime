// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/stdlibonly"
)

func init() {
	register(Gate{
		Name:    "stdlib-only",
		Summary: "The shared vocabulary package must not import outside the standard library.",
		Setup:   setupStdlibOnly,
	})
}

func setupStdlibOnly(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := stdlibonly.Check(stdlibonly.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, stdlibonly.ErrExternalImport) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "stdlib-only: FAIL — %v\n", err)
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "stdlib-only: PASS — %d import(s), all from the standard library\n", out.Scanned)
		return Pass, nil
	}
}
