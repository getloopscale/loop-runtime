// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package liveprobescript

import "regexp"

// BillingPortal is the dev-public Stripe billing portal probe.
var BillingPortal = Spec{
	Gate:   "dev-public-billing-portal-syntax",
	Script: "infra/tests/e2e/live/dev-public-billing-portal_test.sh",
	Probes: []Pattern{
		{regexp.MustCompile(`billing|/v1/billing|stripe`), "Stripe billing portal probe: no reference to billing|/v1/billing|stripe"},
	},
}

// OIDCRedirect is the dev-public OIDC redirect probe through the Kratos
// browser flow.
var OIDCRedirect = Spec{
	Gate:   "dev-public-oidc-redirect-syntax",
	Script: "infra/tests/e2e/live/dev-public-oidc-redirect_test.sh",
	Probes: []Pattern{
		{regexp.MustCompile(`(?m)^test_provider\(\) \{`), "test_provider function defined: `test_provider() {` not found at line start"},
		{regexp.MustCompile(`(?i)google|github|oidc`), "OIDC redirect probe: no reference to google|github|oidc (case-insensitive)"},
		{regexp.MustCompile(`/self-service/login/browser|/self-service/methods/oidc/callback`), "Kratos browser flow: no reference to /self-service/login/browser or /self-service/methods/oidc/callback"},
	},
}

// Specs lists every live probe script under contract.
var Specs = []Spec{BillingPortal, OIDCRedirect}
