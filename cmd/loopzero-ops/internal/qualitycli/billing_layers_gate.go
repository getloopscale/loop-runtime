// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/billinglayers"
)

func init() {
	register(Gate{
		Name:    "billing-layers",
		Summary: "Billing layers must be enumerable and owned; closed debt without coverage fails.",
		Setup:   setupBillingLayers,
	})
}

// setupBillingLayers checks that the billing CHAIN stays enumerable and owned.
// Its sibling billing-secrets covers ONE layer; this covers the list of all of
// them, which is what B-038 measured: "the fifth layer was born without the
// check the other four had".
func setupBillingLayers(fs *flag.FlagSet) Check {
	sibling := fs.String("sibling-root", "", "root of ../loopzero-cloud (default: the sibling of --root)")
	return func(inv Invocation) (Outcome, error) {
		if *sibling == "" {
			*sibling = filepath.Join(inv.Root, "..", "loopzero-cloud")
		}

		out, err := billinglayers.Check(billinglayers.Config{RepoRoot: inv.Root, SiblingRoot: *sibling})
		if err != nil {
			if !isViolation(err, billinglayers.ErrUndeclaredSurface, billinglayers.ErrBelowFloor, billinglayers.ErrGuardUnresolved, billinglayers.ErrRegistryMalformed) {
				return Fail, err
			}
			// No branch returns 0 without a verdict: a guard that does not resolve,
			// closed debt without coverage, or an undeclared new layer all fail here.
			fmt.Fprintf(inv.Stderr, "billing-layers: FAIL — %v\n", err)
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "billing-layers: PASS — %s\n", out.Reason)
		return Pass, nil
	}
}
