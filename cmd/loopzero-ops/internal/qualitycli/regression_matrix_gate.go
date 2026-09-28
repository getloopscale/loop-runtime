// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/regressionmatrix"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "regression-matrix",
		Summary: "Verifies the regression-test matrix covers required scenarios.",
		Setup:   setupRegressionMatrix,
	})
}

// setupRegressionMatrix is the matrix-integrity gate replacing
// audit-regression-matrix.sh + audit-golden-test.sh (Track B TB5). It audits
// the bootstrap regression matrix: every entry's Verification artifact must
// still resolve in the repo. It FAILs when the artifact of a critical entry (one
// whose can-recur column is NO) does not resolve. Unlike the bash original it
// has no set-e fragility: the prior gate silently aborted at its exit 2.
func setupRegressionMatrix(fs *flag.FlagSet) Check {
	matrix := fs.String("matrix", "docs/runbooks.md", "matrix markdown path (repo-relative)")
	strict := fs.Bool("strict", false, "fail on ANY miss, not just critical ones (can-recur column NO)")
	return func(inv Invocation) (verdict.Result, error) {
		matrixPath := *matrix
		if !filepath.IsAbs(matrixPath) {
			matrixPath = filepath.Join(inv.Root, matrixPath)
		}
		v, err := regressionmatrix.Check(matrixPath, inv.Root, *strict)
		if err != nil {
			return nil, err
		}
		inv.Stdout.Printf("regression-matrix: %d entries — OK=%d miss=%d (critical=%d) skip=%d\n",
			v.Total, v.OK, v.Miss, v.MissCritical, v.Skip)
		for _, e := range v.MissEntries {
			marker := " "
			if e.CanRecur == regressionmatrix.CanRecurNo {
				marker = "!"
			}
			verify := e.Verification
			if len(verify) > 80 {
				verify = verify[:80]
			}
			inv.Stdout.Printf("  MISS%s #%s [%s] %s\n", marker, e.ID, e.CanRecur, verify)
		}
		if v.Verdict() == verdict.Fail {
			inv.Stderr.Printf("regression-matrix: FAIL — %d unresolved critical artifact(s); the matrix claims protection that no longer resolves in the repo\n", v.MissCritical)
			return v, nil
		}
		inv.Stdout.Println("regression-matrix: PASS")
		return v, nil
	}
}
