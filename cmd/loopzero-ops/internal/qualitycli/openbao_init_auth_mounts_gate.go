// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/openbaoinitauth"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "openbao-init-auth-mounts",
		Summary: "T5.1-T5.9 openbao-init auth mounts + per-binary roles: every k8s-* mount in authMethods declared once and rendered (k8s-core, k8s-build, at least one k8s-cell-*) + loopzero-api TTL 1h on k8s-core + loopzero-tenant/app/preview/platform-operator roles on k8s-core + argo-workflows on k8s-build + external-secrets-broker on each cell + isolation invariants (loopzero-api NOT on cells, ESO-broker NOT on core). Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.7 Wave 2 replaces infra/tests/check-openbao-init-auth-mounts_test.sh (Wave 2 COMPLETE).",
		Setup:   setupOpenbaoInitAuthMounts,
	})
}

// setupOpenbaoInitAuthMounts enforces T5.1-T5.9 openbao-init auth mounts + per-binary roles.
// Migrated from infra/tests/check-openbao-init-auth-mounts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.7 Wave 2 — Wave 2 COMPLETE).
func setupOpenbaoInitAuthMounts(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := openbaoinitauth.Check(openbaoinitauth.Config{RepoRoot: inv.Root})
		if errors.Is(err, openbaoinitauth.ErrChartMissing) {
			inv.Stderr.Println("openbao-init-auth-mounts: FAIL — openbao-init chart missing")
			return Fail, err
		}
		if err != nil {
			if !isViolation(err, openbaoinitauth.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("openbao-init-auth-mounts: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			return Fail, nil
		}
		if outcome.Warn {
			inv.Stdout.Println("openbao-init-auth-mounts: WARN (advisory-only: helm not on PATH)")
			return Pass, nil
		}
		inv.Stdout.Println("openbao-init-auth-mounts: PASS — T5.1-T5.9 auth mounts + per-binary roles + isolation invariants clean")
		return Pass, nil
	}
}
