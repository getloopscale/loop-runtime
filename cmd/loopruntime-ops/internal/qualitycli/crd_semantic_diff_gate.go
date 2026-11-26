// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/crdsemanticdiff"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "crd-semantic-diff",
		Summary: "Semantic diff of CRD schemas to catch breaking spec changes.",
		Setup:   setupCRDSemanticDiff,
	})
}

// setupCRDSemanticDiff is the gate replacing crd-semantic-diff.sh: detects
// semantic drift between source CRDs and the rendered loopruntime-crds chart.
// Shells out to `helm template` directly (no shared abstraction); unit tests
// are hermetic via the override hook.
func setupCRDSemanticDiff(fs *flag.FlagSet) Check {
	crd := fs.String("crd", "", "limit to a single CRD short-name (e.g. tenants)")
	verbose := fs.Bool("verbose", false, "append inline diff snippets")
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := crdsemanticdiff.Check(crdsemanticdiff.Config{
			Root:      inv.Root,
			CRDFilter: *crd,
			Verbose:   *verbose,
		})
		if err != nil {
			return nil, err
		}
		for _, m := range outcome.Messages {
			inv.Stdout.Println(m)
		}
		if outcome.Verdict() == verdict.Fail {
			return outcome, nil
		}
		return outcome, nil
	}
}
