// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/subpackagefence"
)

// reportSubpackageFence prints a subpackagefence verdict for one gate. A fence
// rule refusing (an invalid registry included) is a violation; any other error
// means the scan itself could not complete (I/O or parse) and is returned for
// the runner to report as incomplete.
func reportSubpackageFence(inv Invocation, o subpackagefence.Outcome, err error, fix string) (Outcome, error) {
	gate := inv.Name
	fmt.Fprintf(inv.Stdout, "%s: %d subpackage(s), %d file(s) parsed, %d unregistered, %d forbidden edge(s)\n",
		gate, len(o.Subpackages), o.FilesParsed, len(o.Unregistered), len(o.Edges))
	for _, u := range o.Unregistered {
		fmt.Fprintf(inv.Stdout, "  UNREGISTERED %s — a subpackage nobody decided on\n", u)
	}
	for _, e := range o.Edges {
		// The two kinds need different fixes — one moves shared code into a leaf,
		// the other moves the CALL into a _test.go — so they are labelled apart.
		if e.TestOnly {
			fmt.Fprintf(inv.Stdout, "  TEST-ONLY LEAF IN PRODUCTION %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
			continue
		}
		fmt.Fprintf(inv.Stdout, "  FORBIDDEN EDGE %s → %s (%s:%d)\n", e.From, e.To, e.File, e.Line)
	}
	if err == nil {
		fmt.Fprintf(inv.Stdout, "%s: PASS — every subpackage is registered and imports only what its allow-list names\n", gate)
		return Pass, nil
	}
	if !isViolation(err,
		subpackagefence.ErrEmptyAllowList,
		subpackagefence.ErrIncoherentRegistry,
		subpackagefence.ErrRegistryCycle,
		subpackagefence.ErrUnregisteredSubpackage,
		subpackagefence.ErrForbiddenEdge,
		subpackagefence.ErrTestOnlyLeafFromProduction,
		subpackagefence.ErrNothingParsed,
	) {
		return Fail, err
	}
	fmt.Fprintf(inv.Stderr, "%s: FAIL — %v\n", gate, err)
	fmt.Fprintf(inv.Stderr, "\nFix: %s\n", fix)
	return Fail, nil
}
