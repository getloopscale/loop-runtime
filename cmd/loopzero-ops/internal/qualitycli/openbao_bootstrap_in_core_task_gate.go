// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/openbaobootstrapincore"
)

func init() {
	register(Gate{
		Name:    "openbao-bootstrap-in-core-task",
		Summary: "Forbids installing the secrets plane into the mgmt cluster: fails the bootstrap-in-core task and any secrets helmfile pointed at a mgmt context, across every taskfiles/*.yml (B-047, ADR 2026-08-12-openbao-in-core-rule-vs-task).",
		Setup:   setupOpenBaoBootstrapInCoreTask,
	})
}

// setupOpenBaoBootstrapInCoreTask enforces the `task k3d:secrets:bootstrap-in-core`
// shortcut contract — canonical mirror of the upstream `k3d:secrets:bootstrap`
// with exactly two well-known differences (context swap + pre-flight message).
// Migrated from infra/tests/check-openbao-bootstrap-in-core-task_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupOpenBaoBootstrapInCoreTask(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := openbaobootstrapincore.Check(openbaobootstrapincore.Config{RepoRoot: inv.Root})
		switch {
		case errors.Is(err, openbaobootstrapincore.ErrForbiddenShortcut):
			fmt.Fprintf(inv.Stderr, "openbao-bootstrap-in-core-task: FAIL — %d violation(s) of the SECRETS/CORE boundary:\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				fmt.Fprintf(inv.Stderr, "  %s\n", v)
			}
			fmt.Fprintln(inv.Stderr, "")
			fmt.Fprintln(inv.Stderr, "OpenBao runs in the SEPARATE loopzero-secrets cluster. Installing it into core is")
			fmt.Fprintln(inv.Stderr, "forbidden in EVERY tier — a topology violation, not a shortcut. The canonical")
			fmt.Fprintln(inv.Stderr, "path is `task k3d:secrets:create` + `task k3d:secrets:bootstrap`.")
			fmt.Fprintln(inv.Stderr, "See .claude/rules/cleanroom-up-live-test-mandate.md and")
			fmt.Fprintln(inv.Stderr, "docs/decisions.md (B-047).")
			return Fail, nil
		case err != nil:
			return Fail, err
		}
		fmt.Fprintf(inv.Stdout, "openbao-bootstrap-in-core-task: PASS — no core-cluster OpenBao shortcut (%d lines scanned)\n", outcome.LinesScanned)
		return Pass, nil
	}
}
