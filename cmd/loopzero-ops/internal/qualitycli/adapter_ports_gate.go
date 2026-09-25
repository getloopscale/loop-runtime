// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/adapterports"
)

func init() {
	register(Gate{
		Name:    "adapter-ports",
		Summary: "Every adapter declares at compile time which port it satisfies.",
		Setup:   setupAdapterPorts,
	})
}

func setupAdapterPorts(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := adapterports.Check(adapterports.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, adapterports.ErrNoPortDeclared) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "adapter-ports: FAIL — %v\n", err)
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "adapter-ports: PASS — %d adapter(s), each declaring the port it satisfies\n", out.Scanned)
		return Pass, nil
	}
}
