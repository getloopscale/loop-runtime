// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/moduleselfcontained"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "module-selfcontained",
		Summary: "Every Go module must build without go.work, which is what CI does.",
		Setup:   setupModuleSelfContained,
	})
}

// setupModuleSelfContained builds every Go module on its own, without go.work,
// which is how CI builds it: a module that only compiles inside the workspace
// passes locally and fails there.
func setupModuleSelfContained(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := moduleselfcontained.Check(moduleselfcontained.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("module-selfcontained: FAIL — %v\n", out.Violation)
			for _, f := range out.Failed {
				inv.Stderr.Printf("  %s:\n%s\n", f.Module, f.Err)
			}
			return out, nil
		}
		if out.Verdict() == verdict.NotVerified {
			inv.Stderr.Printf("module-selfcontained: NOT VERIFIED — %s\n", out.Reason)
			for _, u := range out.Unreachable {
				inv.Stderr.Printf("  %s: %s\n", u.Module, u.Err)
			}
			return out, nil
		}
		inv.Stdout.Printf("module-selfcontained: PASS — %s\n", out.Reason)
		return out, nil
	}
}
