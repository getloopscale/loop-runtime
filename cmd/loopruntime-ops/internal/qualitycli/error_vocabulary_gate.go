// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/errorvocabulary"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "error-vocabulary",
		Summary: "An authored error message must not carry an internal term forbidden by Rule 3.",
		Setup:   setupErrorVocabulary,
	})
}

func setupErrorVocabulary(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		out, err := errorvocabulary.Check(errorvocabulary.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if out.Verdict() == verdict.Fail {
			inv.Stderr.Printf("error-vocabulary: FAIL — %s\n", out.Summary())
			for _, v := range out.Violations {
				inv.Stderr.Printf("  %s:%d carries %q in user-facing output: %q\n", v.File, v.Line, v.Term, v.Message)
			}
			inv.Stderr.Println("  Unbreakable Rule 3 forbids these terms in user-facing output. Use product language.")
			return out, nil
		}
		inv.Stdout.Printf("error-vocabulary: PASS — %s\n", out.Summary())
		return out, nil
	}
}
