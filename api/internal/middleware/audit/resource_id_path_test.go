// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package audit

import "testing"

// AUDIT-17: extractResourceID should extract the domain from /domains/<domain>.
func TestExtractResourceID_Domain(t *testing.T) {
	if got := extractResourceID("/api/v1/domains/my.example.com"); got != "my.example.com" {
		t.Errorf("extractResourceID for domains = %q, want %q", got, "my.example.com")
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
