// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statusadapter

import (
	"encoding/json"
	"testing"
)

// resolveEntryApp picks the canonical "entry" app for dashboard "Visit" URL.
// J-25 sponsor walkthrough 2026-06-12 LIVE catch: when a release has both
// `web` (frontend, rendering=static — CF Worker delivery) AND `api` (server,
// Traefik IngressRoute), the resolver MUST pick `api`. Picking `web` produces
// a Traefik 404 because no IngressRoute exists at that hostname (CF Worker
// serves it instead).

func TestResolveEntryApp_PrefersServerOverStaticFrontend(t *testing.T) {
	// Real-world fixture from `getloopscale/loopzerokit-example` (production release
	// rel-1e40cd32782faeca on 2026-06-12): web frontend FIRST in array,
	// api server SECOND. The previous resolver picked the first match and
	// returned "web" — which has no IngressRoute (rendering=static → CF
	// Worker delivery). status.url then emitted an unreachable hostname.
	appsRaw := json.RawMessage(`[
		{"name":"web","type":"frontend","rendering":"static"},
		{"name":"api","type":"server"}
	]`)
	got := resolveEntryApp(appsRaw)
	if got != "api" {
		t.Fatalf("want server preference 'api', got %q (would produce Traefik 404 on dashboard Visit click)", got)
	}
}

func TestResolveEntryApp_FallsBackToNonStaticFrontend(t *testing.T) {
	// Frontend-only project (e.g., SSR Next.js) with rendering=server → has
	// IngressRoute → resolver picks it. No `server` type in this fixture.
	appsRaw := json.RawMessage(`[
		{"name":"site","type":"frontend","rendering":"server"}
	]`)
	got := resolveEntryApp(appsRaw)
	if got != "site" {
		t.Fatalf("want non-static frontend 'site', got %q", got)
	}
}

func TestResolveEntryApp_SkipsStaticOnlyProject(t *testing.T) {
	// 100% static frontend project — no Traefik route exists. Returning ""
	// triggers StatusHandler to omit status.url → dashboard renders
	// "URL pending" badge (ADR D3 fail-closed default). The static URL
	// is served separately by CF Worker; surfacing it as the canonical
	// "Visit" URL via this code path is incorrect.
	appsRaw := json.RawMessage(`[
		{"name":"web","type":"frontend","rendering":"static"}
	]`)
	got := resolveEntryApp(appsRaw)
	if got != "" {
		t.Fatalf("want empty (static-only release → URL pending), got %q", got)
	}
}

func TestResolveEntryApp_WorkerOnlyReleaseReturnsEmpty(t *testing.T) {
	// Worker-only release — no server/frontend at all. The dashboard
	// shouldn't emit a Visit URL.
	appsRaw := json.RawMessage(`[
		{"name":"queue-worker","type":"worker"}
	]`)
	got := resolveEntryApp(appsRaw)
	if got != "" {
		t.Fatalf("want empty (worker-only release), got %q", got)
	}
}

func TestResolveEntryApp_PicksFirstServerWhenMultiple(t *testing.T) {
	// Multi-server release — pick the first one declared (deterministic
	// based on loopzero.yaml ordering).
	appsRaw := json.RawMessage(`[
		{"name":"api-public","type":"server"},
		{"name":"api-internal","type":"server"}
	]`)
	got := resolveEntryApp(appsRaw)
	if got != "api-public" {
		t.Fatalf("want first server 'api-public', got %q", got)
	}
}

func TestResolveEntryApp_EmptyAndMalformedReturnEmpty(t *testing.T) {
	cases := []struct {
		name    string
		appsRaw json.RawMessage
	}{
		{"empty raw", json.RawMessage("")},
		{"empty array", json.RawMessage("[]")},
		{"malformed", json.RawMessage("{not json")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveEntryApp(tc.appsRaw); got != "" {
				t.Fatalf("want empty, got %q", got)
			}
		})
	}
}
