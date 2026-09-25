// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/esooverlay"
)

func init() {
	register(Gate{
		Name:    "dev-public-loopzero-external-secrets-overlay",
		Summary: "Locks post-T1.2 workload-identity contract for infra/helmfile/values/dev-public/loopzero-external-secrets.yaml.gotmpl (enabled=false safe default, K8s-auth shape, no legacy AppRole roleId/secretRef, canonical mgmt DNS, EC-1/EC-2 traceability). M-Migrate2 2026-07-07 replaces infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh.",
		Setup:   setupDevPublicLoopZeroExternalSecretsOverlay,
	})
}

// setupDevPublicLoopZeroExternalSecretsOverlay enforces the dev-public loopzero-external-secrets overlay contract
// (post-T1.2 workload identity: enabled=false safe default, K8s-auth shape,
// no legacy AppRole roleId/secretRef, canonical mgmt DNS, traceability).
// Migrated from infra/tests/check-dev-public-loopzero-external-secrets-overlay_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupDevPublicLoopZeroExternalSecretsOverlay(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := esooverlay.Check(esooverlay.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, esooverlay.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "dev-public-loopzero-external-secrets-overlay: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			return Fail, nil
		}
		fmt.Fprintln(inv.Stdout, "dev-public-loopzero-external-secrets-overlay: PASS — post-T1.2 workload-identity contract preserved (K8s-auth, no legacy AppRole)")
		return Pass, nil
	}
}
