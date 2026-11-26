// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/verdict"
	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/webhookcanonicalpath"
)

func init() {
	register(Gate{
		Name:    "webhook-canonical-path",
		Summary: "Blocks the /engine/api/v1/webhooks/github 301 trap; enforces the canonical /api/v1/webhooks/github (M-Migrate2 2026-07-07 replaces infra/tests/check-webhook-canonical-path_test.sh).",
		Setup:   setupWebhookCanonicalPath,
	})
}

// setupWebhookCanonicalPath enforces that no test/seed script references the
// wrong /engine/api/v1/webhooks/github URL (Caddy 301-redirect + 404 trap).
// The canonical is /api/v1/webhooks/github (handler-direct).
// Migrated from infra/tests/check-webhook-canonical-path_test.sh (M-Migrate2, 2026-07-07).
func setupWebhookCanonicalPath(_ *flag.FlagSet) Check {
	return func(inv Invocation) (verdict.Result, error) {
		outcome, err := webhookcanonicalpath.Check(webhookcanonicalpath.Config{RepoRoot: inv.Root})
		if err != nil {
			// A real I/O error, never a violation (fail-closed): reporting it as
			// "detected in 0 place(s)" would mislead.
			return nil, err
		}
		if outcome.Verdict() == verdict.Fail {
			inv.Stderr.Printf("webhook-canonical-path: FAIL — /engine/api/v1/webhooks/github detected in %d place(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return outcome, nil
		}
		inv.Stdout.Println("webhook-canonical-path: PASS — no forbidden /engine/api/v1/webhooks/github references")
		return outcome, nil
	}
}
