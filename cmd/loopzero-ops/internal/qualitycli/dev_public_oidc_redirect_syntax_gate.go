// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package qualitycli

import (
	"flag"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/liveprobescript"
)

func init() {
	register(Gate{
		Name:    "dev-public-oidc-redirect-syntax",
		Summary: "Hermetic contract for infra/tests/e2e/live/dev-public-oidc-redirect_test.sh — 10 assertions (bash -n / strict-mode / log+assert+test_provider / OIDC provider ref / Kratos browser flow path / exit-code / allowlisted). M22 T2.1 Wave A gate 2/17.",
		Setup:   setupDevPublicOIDCRedirectSyntax,
	})
}

// setupDevPublicOIDCRedirectSyntax audits the OIDC-redirect live probe script.
func setupDevPublicOIDCRedirectSyntax(_ *flag.FlagSet) Check {
	return liveProbeScriptCheck(liveprobescript.OIDCRedirect)
}
