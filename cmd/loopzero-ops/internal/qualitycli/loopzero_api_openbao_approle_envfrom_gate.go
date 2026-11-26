// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/loopzeroapiopenbao"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "loopzero-api-openbao-approle-envfrom",
		Summary: "T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync: loopzero-api chart unconditionally wires AppRole secret_id via envFrom.secretRef to K8s Secret loopzero-openbao-approle (optional=true). Anti-hardcode gate — NO inline value: for OPENBAO_VAULT_SECRET_ID. Renders chart via helm template subprocess (Anti-Pattern #8 legitimate); advisory fallthrough when helm absent. M-Migrate2 2026-07-07 replaces infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh.",
		Setup:   setupLoopZeroAPIOpenbaoApproleEnvfrom,
	})
}

// setupLoopZeroAPIOpenbaoApproleEnvfrom enforces T6.1 of plan 2026-06-10-j22-openbao-transit-eso-sync:
// the loopzero-api chart unconditionally wires the AppRole secret_id via
// envFrom.secretRef to K8s Secret loopzero-openbao-approle with optional=true,
// with NO inline value: for OPENBAO_VAULT_SECRET_ID (anti-hardcode).
// Migrated from infra/tests/helm/loopzero-api-openbao-approle-envfrom_test.sh
// (M-Migrate2 continuation 2026-07-07).
func setupLoopZeroAPIOpenbaoApproleEnvfrom(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := loopzeroapiopenbao.Check(loopzeroapiopenbao.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		switch outcome.Verdict() {
		case verdict.Fail:
			inv.Stderr.Printf("loopzero-api-openbao-approle-envfrom: FAIL — %v\n", outcome.Violation)
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
		case verdict.Skip:
			inv.Stdout.Println("[loopzero-api-openbao] WARN: helm not installed — gate is advisory-only (installer notes: brew install helm / apt install helm).")
		default:
			inv.Stdout.Println("loopzero-api-openbao-approle-envfrom: PASS — envFrom.loopzero-openbao-approle optional=true wired, anti-hardcode preserved")
		}
		return outcome, nil
	}
}
