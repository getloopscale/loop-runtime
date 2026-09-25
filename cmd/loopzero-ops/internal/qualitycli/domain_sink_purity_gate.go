// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/domainsinkpurity"
)

func init() {
	register(Gate{
		Name:    "domain-sink-purity",
		Summary: "Ratchet: the services DAG sink must not grow its persistence surface (B-049).",
		Setup:   setupDomainSinkPurity,
	})
}

// setupDomainSinkPurity — B-049 / slice-4 T1.1.
//
// A ratchet, not a fence: it freezes the persistence symbols measured crossing
// the services DAG sink and refuses the next one. Removing them costs 63
// call sites (B-079); stopping the growth costs this.
func setupDomainSinkPurity(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		o, err := domainsinkpurity.Check(domainsinkpurity.Config{RepoRoot: inv.Root})
		inv.Stdout.Println(o.Summary())
		for _, s := range o.New {
			inv.Stdout.Printf("  NEW LEAK %s — crosses the sink boundary and reaches every package that depends on it\n", s)
		}
		for _, s := range o.Gone {
			inv.Stdout.Printf("  REMOVED %s — tighten the frozen set in FrozenDefault to lock in the gain\n", s)
		}
		if err != nil {
			// Only a grown leak is a verdict. A missing sink or a dot-import means
			// the gate could not measure, and collapsing the two is exactly the
			// distinction B-040 cost this repo.
			if !isViolation(err, domainsinkpurity.ErrSinkLeakGrew) {
				return Fail, err
			}
			inv.Stderr.Printf("domain-sink-purity: FAIL — %v\n", err)
			return Fail, nil
		}
		inv.Stdout.Printf("PASS — no new symbol (persistence or application layer) crosses the sink (%d frozen, tracked in B-079)\n", o.FrozenCount)
		return Pass, nil
	}
}
