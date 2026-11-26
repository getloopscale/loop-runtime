// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/esooverlay"
	"github.com/getloopscale/loop-runtime/cmd/loopruntime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "dev-public-loopruntime-external-secrets-overlay",
		Summary: "Locks post-T1.2 workload-identity contract for infra/helmfile/values/dev-public/loopruntime-external-secrets.yaml.gotmpl (enabled=false safe default, K8s-auth shape, no legacy AppRole roleId/secretRef, canonical mgmt DNS, EC-1/EC-2 traceability). M-Migrate2 2026-07-07 replaces infra/tests/check-dev-public-loopruntime-external-secrets-overlay_test.sh.",
		Setup:   setupDevPublicLoopRuntimeExternalSecretsOverlay,
	})
}

// setupDevPublicLoopRuntimeExternalSecretsOverlay enforces the dev-public loopruntime-external-secrets overlay contract
// (post-T1.2 workload identity: enabled=false safe default, K8s-auth shape,
// no legacy AppRole roleId/secretRef, canonical mgmt DNS, traceability).
// Migrated from infra/tests/check-dev-public-loopruntime-external-secrets-overlay_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupDevPublicLoopRuntimeExternalSecretsOverlay(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := esooverlay.Check(esooverlay.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("dev-public-loopruntime-external-secrets-overlay: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return outcome, nil
		}
		inv.Stdout.Println("dev-public-loopruntime-external-secrets-overlay: PASS — post-T1.2 workload-identity contract preserved (K8s-auth, no legacy AppRole)")
		return outcome, nil
	}
}
