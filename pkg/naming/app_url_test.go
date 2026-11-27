package naming

import "testing"

// Canonical builders for non-preview (permanent env) app URLs.
// Mirror of PreviewSubdomain + PreviewURL contracts established in T2.5 of
// plan 2026-06-11-code-review-remediation v1.2.
//
// Single source of truth for the hostname formula
//   `<tenantID>-<projectID>-<appName>-<envName>.<baseDomain>`
// previously inlined at api/internal/manifests/renderer_helpers.go:17 (used
// by IngressRoute renderer) AND missing entirely at
// api/internal/routes/status.go:252 (which emitted a simplified, incompatible
// `<projectID>-<envName>.<baseDomain>` — the J-25 LIVE catch on 2026-06-12).
//
// Centralizing here closes the catch + DRY-aligns the IngressRoute and the
// status URL emitter on a single formula.

func TestAppHostname_canonical_full_formula(t *testing.T) {
	cases := []struct {
		name      string
		tenantID  string
		projectID string
		appName   string
		envName   string
		base      string
		want      string
	}{
		{
			name:      "production canonical",
			tenantID:  "tn-82988ea9aef248ef",
			projectID: "sample",
			appName:   "api",
			envName:   "production",
			base:      "loop-runtime.local",
			want:      "tn-82988ea9aef248ef-sample-api-production.loop-runtime.local",
		},
		{
			name:      "public domain (dev-public prod-dev wildcard)",
			tenantID:  "tn-82988ea9aef248ef",
			projectID: "sample",
			appName:   "api",
			envName:   "production",
			base:      "prod-dev.getloopscale.app",
			want:      "tn-82988ea9aef248ef-sample-api-production.prod-dev.getloopscale.app",
		},
		{
			name:      "staging env",
			tenantID:  "tn-abc",
			projectID: "foo",
			appName:   "web",
			envName:   "staging",
			base:      "loop-runtime.local",
			want:      "tn-abc-foo-web-staging.loop-runtime.local",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := AppHostname(tc.tenantID, tc.projectID, tc.appName, tc.envName, tc.base)
			if got != tc.want {
				t.Fatalf("AppHostname(%q,%q,%q,%q,%q) = %q; want %q",
					tc.tenantID, tc.projectID, tc.appName, tc.envName, tc.base, got, tc.want)
			}
		})
	}
}

func TestAppHostname_truncates_over_63_chars_rfc1123(t *testing.T) {
	// Real-world tenants use a 16-char ULID; combined with very long project
	// names the subdomain label can exceed RFC 1123's 63-char hard cap.
	// Mirror of PreviewSubdomain truncation contract.
	out := AppHostname(
		"tn-very-long-tenant-id-here",
		"verylongprojectnamethatpushespastthelimit",
		"app",
		"production",
		"loop-runtime.local",
	)
	// Extract the label (everything before first dot)
	dot := -1
	for i, r := range out {
		if r == '.' {
			dot = i
			break
		}
	}
	if dot == -1 {
		t.Fatalf("AppHostname output has no dot: %q", out)
	}
	if dot > 63 {
		t.Fatalf("AppHostname label > 63 chars: %d in %q", dot, out)
	}
	// Must not end in trailing hyphen (RFC 1123)
	if out[dot-1] == '-' {
		t.Fatalf("AppHostname label ends with hyphen: %q", out)
	}
}

func TestAppURL_https_scheme_and_publicDomain(t *testing.T) {
	got := AppURL("tn-82988ea9aef248ef", "sample", "api", "production", "prod-dev.getloopscale.app")
	want := "https://tn-82988ea9aef248ef-sample-api-production.prod-dev.getloopscale.app"
	if got != want {
		t.Fatalf("AppURL = %q; want %q", got, want)
	}
}

func TestAppURL_empty_publicDomain_returns_empty(t *testing.T) {
	got := AppURL("tn-foo", "p", "a", "production", "")
	if got != "" {
		t.Fatalf("AppURL with empty publicDomain = %q; want empty", got)
	}
}

func TestAppURL_consistent_with_AppHostname(t *testing.T) {
	// Invariant: AppURL == "https://" + AppHostname(... publicDomain)
	tenant, project, app, env, pub := "tn-x", "p", "api", "production", "example.dev"
	url := AppURL(tenant, project, app, env, pub)
	host := AppHostname(tenant, project, app, env, pub)
	if url != "https://"+host {
		t.Fatalf("AppURL/AppHostname mismatch: url=%q host=%q", url, host)
	}
}

// An empty trailing part leaves a trailing hyphen, which RFC 1123 forbids;
// the label is trimmed even when it is well under the cap.
func TestAppHostname_trims_a_trailing_hyphen_without_truncating(t *testing.T) {
	if got := AppHostname("tn-a", "shop", "web", "", "apps.example.com"); got != "tn-a-shop-web.apps.example.com" {
		t.Fatalf("got %q", got)
	}
}
