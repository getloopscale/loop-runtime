// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loop-runtime-ops/internal/quality/liveprobescript"
)

func init() {
	register(Gate{
		Name:    "dev-public-billing-portal-syntax",
		Summary: "Hermetic contract for infra/tests/e2e/live/dev-public-billing-portal_test.sh — 8 assertions (bash -n / strict-mode / log+assert / billing endpoint / exit-code contract / allowlisted). M20 T2.1 Wave A gate 1/18.",
		Setup:   setupDevPublicBillingPortalSyntax,
	})
}

// setupDevPublicBillingPortalSyntax audits the billing-portal live probe script.
func setupDevPublicBillingPortalSyntax(_ *flag.FlagSet) Check {
	return liveProbeScriptCheck(liveprobescript.BillingPortal)
}
