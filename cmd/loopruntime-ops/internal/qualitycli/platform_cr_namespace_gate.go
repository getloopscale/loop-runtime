// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/platformcrnamespace"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "platform-cr-namespace",
		Summary: "Tenant/Environment CRs in a versioned manifest must live in loopruntime-system.",
		Setup:   setupPlatformCRNamespace,
	})
}

// setupPlatformCRNamespace refuses a versioned manifest that applies a Tenant/Environment
// outside `loopruntime-system` (B-095). Out of place, the CR is invisible to the operator, whose cache
// is restricted to that namespace — and undeletable if the admission fence is switched on,
// because removing a finalizer is an UPDATE.
func setupPlatformCRNamespace(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := platformcrnamespace.Check(platformcrnamespace.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("platform-cr-namespace: %v\n", out.Violation)
			return out, nil
		}
		inv.Stdout.Printf("platform-cr-namespace: PASS — %d platform CR(s), all in %s (%d file(s))\n",
			len(out.Checked), platformcrnamespace.CanonicalNamespace, out.Files)
		return out, nil
	}
}
