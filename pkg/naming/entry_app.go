package naming

import "encoding/json"

// entryAppSpec is the minimal shape ResolveEntryAppFromApps needs to inspect
// each app entry inside a release.Apps JSON payload. Kept intentionally small
// (name/type/rendering only) so pkg/naming stays free of the wider
// db.BuildAppSpec dependency — pkg/naming is a low-level canonical primitive
// and importing api/internal/db here would reverse the dependency direction.
type entryAppSpec struct {
	Name      string `json:"name"`
	Type      string `json:"type"`
	Rendering string `json:"rendering"`
}

// ResolveEntryAppFromApps picks the canonical "entry" app from a release.Apps
// JSON payload — the one whose IngressRoute the dashboard "Visit" link (and
// the promote-emitted URL) should target. Resolution rules (FAANG canonical
// J-25 sponsor walkthrough 2026-06-12 LIVE catch, extended to promote by
// Red Team loopzero-cloud#159 2026-07-01):
//
//  1. Prefer the FIRST `server` app — server apps are always Traefik-routed
//     via IngressRoute, so the canonical URL formula resolves cleanly.
//  2. Fall back to the first `frontend` with rendering != "static" — those
//     also get Traefik IngressRoutes (containerized HTTP server).
//  3. SKIP `rendering: static` frontends — they are delivered via CF Worker +
//     DO Spaces, NOT Traefik. Their URL field uses the same internal SNI
//     formula but no IngressRoute exists at that host, so emitting it as the
//     Live URL produces a Traefik 404.
//  4. Worker-only releases (no server/frontend) AND frontend-only releases
//     that are 100% static return "" — the caller then suppresses the URL
//     and the dashboard renders "URL pending" (ADR D3 fail-closed).
//
// Malformed JSON (should never happen — emitted by the API at release
// activation) returns "" rather than propagating an error, degrading the URL
// gracefully into "URL pending".
//
// Canonical SoT — call sites:
//   - api/internal/status/statushttp/status.go (dashboard "Visit" URL)
//   - api/internal/status/statusadapter/status_release_store_adapter.go (delegates here — was the
//     original home; moved 2026-07-01 to make it reusable by promote)
//   - api/internal/release/promotion_orchestrator.go (promote-emitted URL —
//     Red Team #159 closure)
func ResolveEntryAppFromApps(appsRaw []byte) string {
	if len(appsRaw) == 0 {
		return ""
	}
	var specs []entryAppSpec
	if err := json.Unmarshal(appsRaw, &specs); err != nil {
		return ""
	}
	// Pass 1: prefer server apps — Traefik-routed by definition.
	for _, s := range specs {
		if s.Type == "server" {
			return s.Name
		}
	}
	// Pass 2: fall back to non-static frontend — also Traefik-routed.
	for _, s := range specs {
		if s.Type == "frontend" && s.Rendering != "static" {
			return s.Name
		}
	}
	return ""
}
