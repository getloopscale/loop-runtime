// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/openbaobootstrapincore"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "openbao-bootstrap-in-core-task",
		Summary: "Forbids installing the secrets plane into the mgmt cluster: fails the bootstrap-in-core task and any secrets helmfile pointed at a mgmt context, across every taskfiles/*.yml (B-047, ADR 2026-08-12-openbao-in-core-rule-vs-task).",
		Setup:   setupOpenBaoBootstrapInCoreTask,
	})
}

// setupOpenBaoBootstrapInCoreTask fails when the secrets plane can be installed
// into the mgmt cluster: the `k3d:secrets:bootstrap-in-core` task existing at
// all, or a secrets helmfile invoked against a mgmt context. The gate was
// inverted on 2026-08-12 (B-047): it used to constrain the shortcut's shape and
// now refuses the shortcut. Migrated from a shell test on 2026-07-07.
func setupOpenBaoBootstrapInCoreTask(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := openbaobootstrapincore.Check(openbaobootstrapincore.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("openbao-bootstrap-in-core-task: FAIL — %d violation(s) of the SECRETS/CORE boundary:\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			inv.Stderr.Println("")
			inv.Stderr.Println("OpenBao runs in the SEPARATE loopzero-secrets cluster. Installing it into core is")
			inv.Stderr.Println("forbidden in EVERY tier — a topology violation, not a shortcut. The canonical")
			inv.Stderr.Println("path is `task k3d:secrets:create` + `task k3d:secrets:bootstrap`.")
			inv.Stderr.Println("See docs/decisions.md (B-047).")
			return outcome, nil
		}
		inv.Stdout.Printf("openbao-bootstrap-in-core-task: PASS — no core-cluster OpenBao shortcut (%d lines scanned)\n", outcome.LinesScanned)
		return outcome, nil
	}
}
