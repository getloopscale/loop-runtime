// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/valuesduplicatekeys"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
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
	return func(inv Invocation) (verdict.Result, error) {
		out, err := valuesduplicatekeys.Check(valuesduplicatekeys.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Skip {
			inv.Stdout.Printf("values-duplicate-keys: SKIP — %s\n", out.Reason)
			return out, nil
		}
		if out.Verdict() == verdict.Pass {
			inv.Stdout.Printf("values-duplicate-keys: PASS — %s\n", out.Reason)
			return out, nil
		}
		inv.Stderr.Println("values-duplicate-keys: FAIL")
		for _, f := range out.Findings {
			inv.Stderr.Printf("  %s:%d — key %q already declared at line %d; Helm keeps this one and drops the first\n",
				f.File, f.Line, f.Path, f.FirstLine)
		}
		inv.Stderr.Println("Fix: merge the two blocks into one key.")
		return out, nil
	}
}
