// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/credentialssot"
)

func init() {
	register(Gate{
		Name:    "credentials-sot",
		Summary: "T1.1 SoT drift audit — every Secret created by canonical generator scripts (generate-secrets.sh, generate-build-secrets.sh, generate-runtime-secrets.sh, provision-api-tokens.sh) MUST be enumerated in docs/operations.md. Also asserts 4 mandatory drill env vars + Chart-hardcoded credentials section + Rotation playbook section. M-Migrate2 2026-07-07 replaces infra/tests/docs/credentials-sot_test.sh.",
		Setup:   setupCredentialsSot,
	})
}

// setupCredentialsSot enforces T1.1 SoT drift audit — every Secret created by
// canonical generator scripts is enumerated in docs/operations.md.
// Migrated from infra/tests/docs/credentials-sot_test.sh
// (M-Migrate2 continuation 2026-07-07 — final KEEP-CI-WIRED wave).
func setupCredentialsSot(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := credentialssot.Check(credentialssot.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, credentialssot.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("credentials-sot: FAIL — %d issue(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			if len(outcome.Discovered) > 0 {
				inv.Stderr.Println("Discovered Secret names:")
				for _, s := range outcome.Discovered {
					inv.Stderr.Printf("  - %s\n", s)
				}
			}
			return Fail, nil
		}
		inv.Stdout.Printf("credentials-sot: PASS — %d bootstrap-created Secrets enumerated in docs/operations.md\n", len(outcome.Discovered))
		return Pass, nil
	}
}
