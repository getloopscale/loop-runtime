// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/optionbeso"
)

func init() {
	register(Gate{
		Name:    "option-b-eso-deploy-doc",
		Summary: "Locks docs/operations/option-b-mgmt-helmfile-eso-deploy.md contract (4 sub-paths named, B1 recommended, B4 rejected, canonical commands + rollback + decision matrix + cross-references present, ESO version drift check against runtime helmfile). M-Migrate2 2026-07-07 replaces infra/tests/check-option-b-eso-deploy-doc_test.sh.",
		Setup:   setupOptionBEsoDeployDoc,
	})
}

// setupOptionBEsoDeployDoc enforces docs/runbooks.md
// contract (4 sub-paths named, B1 recommended, B4 rejected, canonical commands
// documented, rollback + decision matrix + cross-references present, ESO
// version drift against runtime helmfile detected).
// Migrated from infra/tests/check-option-b-eso-deploy-doc_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupOptionBEsoDeployDoc(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := optionbeso.Check(optionbeso.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, optionbeso.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("option-b-eso-deploy-doc: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		inv.Stdout.Println("option-b-eso-deploy-doc: PASS — Option B doc contract preserved (4 sub-paths, decision rationale, canonical commands, ESO drift check)")
		return Pass, nil
	}
}
