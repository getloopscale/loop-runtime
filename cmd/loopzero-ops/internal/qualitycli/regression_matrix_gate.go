// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/regressionmatrix"
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
// still resolve in the repo. FAILs if any critical (Pode Recorrer=NÃO) entry's
// artifact is unresolvable. Unlike the bash original, it has no set-e/return-2
// fragility (the prior gate silently aborted at exit 2 — see
// finding-2026-05-24-audit-regression-matrix-set-e-abort).
func setupRegressionMatrix(fs *flag.FlagSet) Check {
	matrix := fs.String("matrix", "docs/runbooks.md", "matrix markdown path (repo-relative)")
	strict := fs.Bool("strict", false, "fail on ANY miss, not just critical (Pode Recorrer=NÃO) ones")
	return func(inv Invocation) (Outcome, error) {
		matrixPath := *matrix
		if !filepath.IsAbs(matrixPath) {
			matrixPath = filepath.Join(inv.Root, matrixPath)
		}
		v, err := regressionmatrix.Check(matrixPath, inv.Root, *strict)
		if err != nil {
			return Fail, err
		}
		fmt.Fprintf(inv.Stdout, "regression-matrix: %d entries — OK=%d miss=%d (critical=%d) skip=%d\n",
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
			fmt.Fprintf(inv.Stdout, "  MISS%s #%s [%s] %s\n", marker, e.ID, e.CanRecur, verify)
		}
		if v.Failed() {
			fmt.Fprintf(inv.Stderr, "regression-matrix: FAIL — %d unresolved critical artifact(s); the matrix claims protection that no longer resolves in the repo\n", v.MissCritical)
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "regression-matrix: PASS")
		return Pass, nil
	}
}
