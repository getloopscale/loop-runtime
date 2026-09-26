// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

// reportSubpackageFence prints a subpackagefence outcome for one gate. What the
// fence refuses (an invalid registry included) is the outcome's Violation; an
// error means the scan itself could not complete (I/O or parse) and is returned
// for the runner to report as incomplete.
func reportSubpackageFence(inv Invocation, o subpackagefence.Outcome, err error, fix string) (verdict.Result, error) {
	gate := inv.Name
	inv.Stdout.Printf("%s: %d subpackage(s), %d file(s) parsed, %d unregistered, %d forbidden edge(s)\n",
		gate, len(o.Subpackages), o.FilesParsed, len(o.Unregistered), len(o.Edges))
	for _, u := range o.Unregistered {
		inv.Stdout.Printf("  UNREGISTERED %s — a subpackage nobody decided on\n", u)
	}
	for _, e := range o.Edges {
		// The two kinds need different fixes — one moves shared code into a leaf,
		// the other moves the CALL into a _test.go — so they are labelled apart.
		if e.TestOnly {
			inv.Stdout.Printf("  TEST-ONLY LEAF IN PRODUCTION %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
			continue
		}
		inv.Stdout.Printf("  FORBIDDEN EDGE %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
	}
	if err != nil {
		return nil, err
	}
	if o.Verdict() == verdict.Pass {
		inv.Stdout.Printf("%s: PASS — every subpackage is registered and imports only what its allow-list names\n", gate)
		return o, nil
	}
	inv.Stderr.Printf("%s: FAIL — %v\n", gate, o.Violation)
	inv.Stderr.Printf("\nFix: %s\n", fix)
	return o, nil
}
