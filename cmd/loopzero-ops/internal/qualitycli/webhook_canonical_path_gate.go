// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"errors"
	"flag"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/verdict"
	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/webhookcanonicalpath"
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
			// Distinguish a genuine violation from a real I/O error (fail-closed):
			// reporting an I/O error as "detected in 0 place(s)" would mislead.
			if !errors.Is(err, webhookcanonicalpath.ErrViolations) {
				return Fail, err
			}
			inv.Stderr.Printf("webhook-canonical-path: FAIL — /engine/api/v1/webhooks/github detected in %d place(s):\n", len(outcome.Violations))
			for _, v := range outcome.Violations {
				inv.Stderr.Printf("  %s\n", v)
			}
			return Fail, nil
		}
		inv.Stdout.Println("webhook-canonical-path: PASS — no forbidden /engine/api/v1/webhooks/github references")
		return Pass, nil
	}
}
