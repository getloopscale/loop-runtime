// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/nocloud"
	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/verdict"
)

func init() {
	register(Gate{
		Name:    "no-cloud-dev-default",
		Summary: "T1.1 hermetic guard of plan m1-commit-canonical-30-fixes: no test/seed script in archive/canonical-tests-v1/scripts/local/, infra/tests/e2e/, or infra/scripts/local/ may declare a default env-var value pointing at cloud-dev.getloopscale.com (Caddy 301 redirect trap post commit ee77b8da 2026-06-22). EC-1 absorption: /api/v1/webhooks/github handler is single exception (Caddyfile 142-156). M-Migrate2 2026-07-10 M16 T2.3 Wave 1 replaces infra/tests/check-no-cloud-dev-default_test.sh.",
		Setup:   setupNoCloudDevDefault,
	})
}

// setupNoCloudDevDefault enforces T1.1 of plan m1-commit-canonical-30-fixes —
// no test/seed script may declare a default env-var value pointing at
// `cloud-dev.getloopscale.com` (Caddy 301 redirect trap). Webhook handler is
// the single exception (EC-1 absorption).
// Migrated from infra/tests/check-no-cloud-dev-default_test.sh
// (M-Migrate2 2026-07-10 M16 T2.3 Wave 1).
func setupNoCloudDevDefault(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := nocloud.Check(nocloud.Config{RepoRoot: inv.Root})
		if err != nil {
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("no-cloud-dev-default: FAIL — %d cloud-dev default(s):\n", len(outcome.Failures))
			for _, f := range outcome.Failures {
				inv.Stderr.Printf("  %s\n", f)
			}
			inv.Stderr.Println("Fix: replace `${VAR:-https://cloud-dev.getloopscale.com/...}` with the canonical hostname (app-dev/auth-dev/api-dev.getloopscale.com per surface).")
			return outcome, nil
		}
		inv.Stdout.Println("no-cloud-dev-default: PASS — no cloud-dev.getloopscale.com default env-vars in test/seed scripts (webhook handler exception allowed)")
		return outcome, nil
	}
}
