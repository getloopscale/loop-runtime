// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import "testing"

// CHG-2026-04-23-AUDIT-17: audit action naming uniformity.
// Previously domain endpoints fell through to the fallback
// "METHOD.segment" format (e.g., "DELETE.domains"), breaking the
// repo-wide "resource.verb_past" convention used by 9 other types.
func TestDeriveAction_DomainConsistency(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/domains", "domain.added"},
		{"POST", "/api/v1/domains/example.com/verify", "domain.verified"},
		{"DELETE", "/api/v1/domains/example.com", "domain.removed"},
	}
	for _, tc := range cases {
		got := deriveAction(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("deriveAction(%s, %s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestDeriveAction_PreservesExistingBranches(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/projects", "project.created"},
		{"DELETE", "/api/v1/projects/my-app", "project.deleted"},
		{"POST", "/api/v1/environments", "environment.created"},
		{"DELETE", "/api/v1/environments/staging", "environment.deleted"},
		{"DELETE", "/api/v1/teams/members/bob", "member.removed"},
		{"PATCH", "/api/v1/teams/members/bob", "member.role_changed"},
		{"DELETE", "/api/v1/secrets/API_KEY", "secret.deleted"},
		{"POST", "/api/v1/builds", "build.submitted"},
		{"POST", "/api/v1/builds/b1/cancel", "build.cancelled"},
	}
	for _, tc := range cases {
		got := deriveAction(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("deriveAction(%s, %s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// AUDIT-17: extractResourceType should return "domain" for domain paths.
func TestExtractResourceType_Domain(t *testing.T) {
	if got := extractResourceType("/api/v1/domains/example.com"); got != "domain" {
		t.Errorf("extractResourceType for domains = %q, want %q", got, "domain")
	}
}

// AUDIT-17: extractResourceID should extract the domain from /domains/<domain>.
func TestExtractResourceID_Domain(t *testing.T) {
	if got := extractResourceID("/api/v1/domains/my.example.com"); got != "my.example.com" {
		t.Errorf("extractResourceID for domains = %q, want %q", got, "my.example.com")
	}
}

// CHG-2026-04-23-AUDIT-18: source upload previously fell through to
// "POST.upload". Source bundles are part of the deploy flow and must
// be auditable under the repo-wide resource.verb_past convention.
func TestDeriveAction_SourceUpload(t *testing.T) {
	got := deriveAction("POST", "/api/v1/source/upload")
	if got != "source.uploaded" {
		t.Errorf("deriveAction(POST, /api/v1/source/upload) = %q, want %q", got, "source.uploaded")
	}
}

// AUDIT-18: unknown actions must be prefixed with "unknown." so
// dashboards can alert on regressions (e.g., new endpoint added
// without a branch). Silent "METHOD.segment" allowed DELETE.domains
// to live undetected — this guard prevents a repeat.
func TestDeriveAction_UnknownFallback_PrefixedWithUnknown(t *testing.T) {
	got := deriveAction("POST", "/api/v1/totally-new-endpoint")
	if got != "unknown.POST.totally-new-endpoint" {
		t.Errorf("deriveAction(POST, /api/v1/totally-new-endpoint) = %q, want %q",
			got, "unknown.POST.totally-new-endpoint")
	}
}

// AUDIT-18: extractResourceType should return "source" for /source/upload.
func TestExtractResourceType_Source(t *testing.T) {
	if got := extractResourceType("/api/v1/source/upload"); got != "source" {
		t.Errorf("extractResourceType for source = %q, want %q", got, "source")
	}
}

// CHG-2026-04-23-AUDIT-35: extractResourceType should return "build"
// for /builds/{id}/cancel — the cancel verb acts on a build resource.
// Previously fell through to the default branch returning "cancel"
// literally, polluting audit dashboards that group by resource_type.
func TestExtractResourceType_Cancel(t *testing.T) {
	if got := extractResourceType("/api/v1/builds/build-abc/cancel"); got != "build" {
		t.Errorf("extractResourceType for cancel = %q, want %q", got, "build")
	}
}

// CHG-2026-04-23-AUDIT-19: promote/connect/disconnect previously fell
// back to the parent "projects" segment, recording wrong CRUD actions
// ("project.created" / "project.deleted") for what are actually release
// and repository operations. This misleads audit forensics.
func TestDeriveAction_PromoteConnectDisconnect(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/projects/myapp/promote", "release.promoted"},
		{"POST", "/api/v1/projects/myapp/connect", "repository.connected"},
		{"DELETE", "/api/v1/projects/myapp/connect", "repository.disconnected"},
	}
	for _, tc := range cases {
		got := deriveAction(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("deriveAction(%s, %s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

// AUDIT-19: extractResourceType for /promote and /connect paths must
// expose the true resource (release, repository) — not the parent
// collection ("project"). Dashboards group by resource_type.
func TestExtractResourceType_PromoteConnect(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/api/v1/projects/myapp/promote", "release"},
		{"/api/v1/projects/myapp/connect", "repository"},
	}
	for _, tc := range cases {
		if got := extractResourceType(tc.path); got != tc.want {
			t.Errorf("extractResourceType(%s) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// CHG-2026-05-06-AUDIT-RESOURCE-ID: extractResourceID returns "" for
// endpoints whose resource lives in the body or query string (the URL
// only names the collection). The audit middleware now compensates by
// snooping the body and query — this guard asserts the path-only side
// continues to return "" so the enrichment fallback is exercised.
func TestExtractResourceID_BodyOnlyEndpointsReturnEmpty(t *testing.T) {
	cases := []string{
		"/api/v1/builds",
		"/api/v1/secrets",
		"/api/v1/domains",
	}
	for _, path := range cases {
		if got := extractResourceID(path); got != "" {
			t.Errorf("extractResourceID(%s) = %q, want %q (no ID in URL path)",
				path, got, "")
		}
	}
}

// AUDIT-19: extractResourceID on these paths should still return the
// project name (positioned after "projects"), so audit trails remain
// queryable by project. Regression guard.
func TestExtractResourceID_PromoteConnect(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/api/v1/projects/myapp/promote", "myapp"},
		{"/api/v1/projects/myapp/connect", "myapp"},
	}
	for _, tc := range cases {
		if got := extractResourceID(tc.path); got != tc.want {
			t.Errorf("extractResourceID(%s) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// CHG-2026-04-29-DOGFOOD-REAL-E2E iter 13 / Achado #27.
// Preview create/delete + admin tenants evict were silently falling through
// to "unknown.<METHOD>.<segment>" in audit_log because the path segments
// weren't in deriveAction's switch / knownCollections set. Dogfood journeys
// 09/11 had no audit entries to assert.
func TestDeriveAction_PreviewAndAdmin(t *testing.T) {
	cases := []struct {
		method, path, want string
	}{
		{"POST", "/api/v1/projects/myapp/previews", "preview.created"},
		{"DELETE", "/api/v1/projects/myapp/previews/prev-123", "preview.deleted"},
		{"POST", "/api/v1/admin/tenants/evict", "admin.tenants_evicted"},
	}
	for _, tc := range cases {
		got := deriveAction(tc.method, tc.path)
		if got != tc.want {
			t.Errorf("deriveAction(%s, %s) = %q, want %q", tc.method, tc.path, got, tc.want)
		}
	}
}

func TestExtractResourceType_PreviewAndAdmin(t *testing.T) {
	cases := []struct {
		path, want string
	}{
		{"/api/v1/projects/myapp/previews", "preview"},
		{"/api/v1/projects/myapp/previews/prev-123", "preview"},
		{"/api/v1/admin/tenants/evict", "admin"},
	}
	for _, tc := range cases {
		if got := extractResourceType(tc.path); got != tc.want {
			t.Errorf("extractResourceType(%s) = %q, want %q", tc.path, got, tc.want)
		}
	}
}
