// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import "github.com/getloopscale/loopzero/pkg/naming"

// userFacingDomain is the domain a URL shown to the user is composed on: the
// operator's public wildcard when one is configured, the base domain otherwise.
// The base-domain fallback keeps local development (no LOOPZERO_PUBLIC_DOMAIN)
// working; a configured public domain always wins, because the base domain is
// the internal cluster placeholder and does not resolve for the user (#61).
func userFacingDomain(publicDomain, baseDomain string) string {
	if publicDomain != "" {
		return publicDomain
	}
	return baseDomain
}

// entryAppURL is the URL of a release's entry app — the one Traefik routes —
// composed by naming.AppURL, the same formula the IngressRoute renderer uses.
// It is empty when the release has no routable app (worker- or static-only) or
// no domain is configured: an empty URL renders as "pending", while a composed
// one the router does not serve reports success over a 404 (loopzero-cloud#159).
func entryAppURL(tenantID, projectID, env string, appsJSON []byte, domain string) string {
	entryApp := naming.ResolveEntryAppFromApps(appsJSON)
	if entryApp == "" || domain == "" {
		return ""
	}
	return naming.AppURL(tenantID, projectID, entryApp, env, domain)
}
