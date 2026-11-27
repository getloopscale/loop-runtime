// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/openbaoinitauth"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "openbao-init-auth-mounts",
		Summary: "T5.1-T5.9 openbao-init auth mounts + per-binary roles: every k8s-* mount in authMethods declared once and rendered (k8s-core, k8s-build, at least one k8s-cell-*) + loop-runtime-api TTL 1h on k8s-core + loop-runtime-tenant/app/preview/platform-operator roles on k8s-core + argo-workflows on k8s-build + external-secrets-broker on each cell + isolation invariants (loop-runtime-api NOT on cells, ESO-broker NOT on core). Advisory when helm absent. M-Migrate2 2026-07-10 M16 T3.7 Wave 2 replaces infra/tests/check-openbao-init-auth-mounts_test.sh (Wave 2 COMPLETE).",
		Setup:   setupOpenbaoInitAuthMounts,
	})
}

// setupOpenbaoInitAuthMounts enforces T5.1-T5.9 openbao-init auth mounts + per-binary roles.
// Migrated from infra/tests/check-openbao-init-auth-mounts_test.sh
// (M-Migrate2 2026-07-10 M16 T3.7 Wave 2 — Wave 2 COMPLETE).
func setupOpenbaoInitAuthMounts(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := openbaoinitauth.Check(openbaoinitauth.Config{RepoRoot: inv.Root})
		// A missing chart (openbaoinitauth.ErrChartMissing) verified nothing; the
		// runner reports it and exits Incomplete.
		if err != nil {
			return nil, err
		}
		switch outcome.Verdict() {
		case verdict.Fail:
			inv.Stderr.Printf("openbao-init-auth-mounts: FAIL — %d assertion(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
		case verdict.Skip:
			inv.Stdout.Println("openbao-init-auth-mounts: WARN (advisory-only: helm not on PATH)")
		default:
			inv.Stdout.Println("openbao-init-auth-mounts: PASS — T5.1-T5.9 auth mounts + per-binary roles + isolation invariants clean")
		}
		return outcome, nil
	}
}
