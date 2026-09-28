// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/adapterports"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "adapter-ports",
		Summary: "Every adapter declares at compile time which port it satisfies.",
		Setup:   setupAdapterPorts,
	})
}

func setupAdapterPorts(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := adapterports.Check(adapterports.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("adapter-ports: FAIL — %v\n", out.Violation)
			return out, nil
		}
		inv.Stdout.Printf("adapter-ports: PASS — %d adapter(s), each declaring the port it satisfies\n", out.Scanned)
		return out, nil
	}
}
