// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package middleware

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
