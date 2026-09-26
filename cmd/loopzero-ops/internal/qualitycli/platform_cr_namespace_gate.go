// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/platformcrnamespace"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "platform-cr-namespace",
		Summary: "Tenant/Environment CRs in a versioned manifest must live in loopzero-system.",
		Setup:   setupPlatformCRNamespace,
	})
}

// setupPlatformCRNamespace refuses a versioned manifest that applies a Tenant/Environment
// outside `loopzero-system` (B-095). Out of place, the CR is invisible to the operator, whose cache
// is restricted to that namespace — and undeletable if the admission fence is switched on,
// because removing a finalizer is an UPDATE.
func setupPlatformCRNamespace(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := platformcrnamespace.Check(platformcrnamespace.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, platformcrnamespace.ErrOutsideNamespace) {
				return Fail, err
			}
			inv.Stderr.Printf("platform-cr-namespace: %v\n", err)
			return Fail, nil
		}
		inv.Stdout.Printf("platform-cr-namespace: PASS — %d platform CR(s), all in %s (%d file(s))\n",
			len(out.Checked), platformcrnamespace.CanonicalNamespace, out.Files)
		return Pass, nil
	}
}
