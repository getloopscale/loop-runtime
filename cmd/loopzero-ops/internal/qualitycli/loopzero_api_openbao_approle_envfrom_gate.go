// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"
	"fmt"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/loopzeroapiopenbao"
)

func init() {
	register(Gate{
		Name:    "loopzero-api-openbao-approle-envfrom",
		Summary: "T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync: loopzero-api chart unconditionally wires AppRole secret_id via envFrom.secretRef to K8s Secret loopzero-openbao-approle (optional=true). Anti-hardcode gate — NO inline value: for OPENBAO_VAULT_SECRET_ID. Renders chart via helm template subprocess (Anti-Pattern #8 legitimate); advisory fallthrough when helm absent. M-Migrate2 2026-07-07 replaces infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh.",
		Setup:   setupLoopZeroApiOpenbaoApproleEnvfrom,
	})
}

// setupLoopZeroApiOpenbaoApproleEnvfrom enforces T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync:
// the loopzero-api chart unconditionally wires the AppRole secret_id via
// envFrom.secretRef to K8s Secret loopzero-openbao-approle with optional=true,
// with NO inline value: for OPENBAO_VAULT_SECRET_ID (anti-hardcode).
// Migrated from infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupLoopZeroApiOpenbaoApproleEnvfrom(_ *flag.FlagSet) Check {
	return func(inv Invocation) (Outcome, error) {
		outcome, err := loopzeroapiopenbao.Check(loopzeroapiopenbao.Config{RepoRoot: inv.Root})
		if err != nil {
			if !isViolation(err, loopzeroapiopenbao.ErrViolations) {
				return Fail, err
			}
			fmt.Fprintf(inv.Stderr, "loopzero-api-openbao-approle-envfrom: FAIL — %v\n", err)
			for _, f := range outcome.Failures {
				fmt.Fprintf(inv.Stderr, "  %s\n", f)
			}
			return Fail, nil
		}
		if outcome.Warn {
			fmt.Fprintln(inv.Stdout, "[loopzero-api-openbao] WARN: helm not installed — gate is advisory-only (installer notes: brew install helm / apt install helm).")
			return Pass, nil
		}
		fmt.Fprintln(inv.Stdout, "loopzero-api-openbao-approle-envfrom: PASS — envFrom.loopzero-openbao-approle optional=true wired, anti-hardcode preserved")
		return Pass, nil
	}
}
