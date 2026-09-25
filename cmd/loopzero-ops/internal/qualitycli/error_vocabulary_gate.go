// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/errorvocabulary"
)

func init() {
	register(Gate{
		Name:    "error-vocabulary",
		Summary: "An authored error message must not carry an internal term forbidden by Rule 3.",
		Setup:   setupErrorVocabulary,
	})
}

func setupErrorVocabulary(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		out, err := errorvocabulary.Check(errorvocabulary.Config{RepoRoot: inv.Root})
		if err != nil {
			return Fail, err
		}
		if !out.Pass {
			fmt.Fprintf(inv.Stderr, "error-vocabulary: FAIL — %s\n", out.Summary())
			for _, v := range out.Violations {
				fmt.Fprintf(inv.Stderr, "  %s:%d carries %q in user-facing output: %q\n", v.File, v.Line, v.Term, v.Message)
			}
			fmt.Fprintln(inv.Stderr, "  Unbreakable Rule 3 forbids these terms in user-facing output. Use product language.")
			return Fail, nil
		}
		fmt.Fprintf(inv.Stdout, "error-vocabulary: PASS — %s\n", out.Summary())
		return Pass, nil
	}
}
