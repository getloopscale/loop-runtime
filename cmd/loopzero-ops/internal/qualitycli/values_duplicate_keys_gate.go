// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/valuesduplicatekeys"
)

func init() {
	register(Gate{
		Name:    "values-duplicate-keys",
		Summary: "A Helm values file must not declare one mapping key twice (Helm silently keeps only the last).",
		Setup:   setupValuesDuplicateKeys,
	})
}

// setupValuesDuplicateKeys refuses a Helm values file that declares one mapping
// key twice: Helm keeps the last declaration silently, so the first block never
// renders while the file reads as configured.
func setupValuesDuplicateKeys(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := valuesduplicatekeys.Check(valuesduplicatekeys.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if out.Skipped {
			fmt.Fprintf(inv.Stdout, "values-duplicate-keys: SKIP — %s\n", out.Reason)
			return Pass, nil
		}
		if out.Pass {
			fmt.Fprintf(inv.Stdout, "values-duplicate-keys: PASS — %s\n", out.Reason)
			return Pass, nil
		}
		fmt.Fprintln(inv.Stderr, "values-duplicate-keys: FAIL")
		for _, f := range out.Findings {
			fmt.Fprintf(inv.Stderr, "  %s:%d — key %q already declared at line %d; Helm keeps this one and drops the first\n",
				f.File, f.Line, f.Path, f.FirstLine)
		}
		fmt.Fprintln(inv.Stderr, "Fix: merge the two blocks into one key.")
		return Fail, nil
	}
}
