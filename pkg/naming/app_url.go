package naming

import (
	"fmt"
)

// AppHostname returns the canonical internal hostname for a deployed app in
// a permanent environment (production, staging, etc.). It is the single
// source of truth previously inlined at
// `api/internal/manifests/renderer_helpers.go:appHostname` (used by the
// IngressRoute renderer) AND missing entirely at
// `api/internal/routes/status.go:252` (which emitted a simplified
// `<projectID>-<envName>.<baseDomain>` that did NOT match the IngressRoute
// Host rule — the J-25 sponsor walkthrough LIVE catch on 2026-06-12).
//
// Format: "{tenantID}-{projectID}-{appName}-{envName}.{baseDomain}"
//
// RFC 1123 hard cap: the subdomain label MUST stay <= 63 chars and MUST NOT
// end with a hyphen. Long inputs are truncated then right-trimmed of
// trailing hyphens. Truncation is an exceptional path (typical inputs
// produce ~30-40 char labels) but the cap is part of the contract — mirrors
// PreviewSubdomain in preview.go.
//
// Cross-references:
//   - `pkg/naming.PreviewSubdomain` (canonical formula for ephemeral PR previews)
//   - `api/internal/manifests/renderer_helpers.go:appHostname` (delegates here post-refactor)
//   - `api/internal/routes/status.go` (must call this to align status.url with IngressRoute)
//
// Plan: 2026-06-12-dashboard-journey-j25-live-url-discoverability (FAANG-level
// canonical fix discovered during sponsor LIVE walkthrough — the simplified
// formula at status.go:252 produced URLs that DNS+TLS+Caddy delivered correctly
// but Traefik couldn't match against the canonical IngressRoute Host).
func AppHostname(tenantID, projectID, appName, envName, baseDomain string) string {
	subdomain := fmt.Sprintf("%s-%s-%s-%s", tenantID, projectID, appName, envName)
	return fmt.Sprintf("%s.%s", truncateLabel(subdomain, MaxKubernetesNameLength), baseDomain)
}

// AppURL composes the full public URL exposed to users for a permanent
// environment app. Mirror of PreviewURL in preview.go.
//
// Shape: "https://{AppHostname(... publicDomain)}". Returns the empty string
// when publicDomain is empty so callers can detect "no public URL configured"
// without parsing the result (canonical pattern matching PreviewURL).
//
// publicDomain is the operator-configured wildcard root (e.g.
// `prod-dev.loopzero.example` for the dev-public env). Callers in StatusHandler
// resolve it from PublicDomain with fallback to BaseDomain — that resolution
// stays at the call site because it is a service-wiring concern, not a
// naming concern (mirrors PreviewURL's `PreviewPublicDomain` resolution at
// PreviewService).
func AppURL(tenantID, projectID, appName, envName, publicDomain string) string {
	if publicDomain == "" {
		return ""
	}
	return fmt.Sprintf("https://%s", AppHostname(tenantID, projectID, appName, envName, publicDomain))
}
