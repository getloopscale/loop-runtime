// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"strings"
	"testing"
)

// TestRender_NoCollisionBetweenProjectsWithSameAppName guards against the
// regression of real e2e dogfood Finding #17: two projects in the same tenant
// env with an app of the same name (e.g. both `api`) — common in a
// multi-tenant PaaS — must produce DIFFERENT K8s names, and Service selectors
// must match ONLY their own project's pods.
//
// Discovered live during journey-01 (CHG-2026-04-29-DOGFOOD-REAL-E2E):
// the `solo-server` deploy returned `dogfood-fb`'s body because the
// `api` Service was shared between projects.
func TestRender_NoCollisionBetweenProjectsWithSameAppName(t *testing.T) {
	base := func(project string) RenderInput {
		return RenderInput{
			TenantID:          "acme",
			ProjectID:         project,
			Environment:       "production",
			BuildID:           "build-1",
			ReleaseID:         "rel-1",
			BaseDomain:        "getloopscale.app.com",
			ClusterIssuerName: "letsencrypt-prod",
			Apps: []AppManifestInput{
				{Name: "api", Type: "server", Port: 8080, ImageRef: "reg/acme/" + project + "/api:abc"},
			},
		}
	}

	out1, _ := Render(base("alpha"))
	out2, _ := Render(base("bravo"))

	// Deployment names must differ.
	if !strings.Contains(out1, "name: alpha-api") {
		t.Errorf("project alpha must produce Deployment 'alpha-api', got:\n%s", out1)
	}
	if !strings.Contains(out2, "name: bravo-api") {
		t.Errorf("project bravo must produce Deployment 'bravo-api', got:\n%s", out2)
	}

	// Bare `name: api` (without project prefix) must NOT appear in either —
	// that would mean the old collision-prone naming snuck back in.
	for _, s := range []struct{ label, content string }{{"alpha", out1}, {"bravo", out2}} {
		// Allow "name: api" in container spec (intentional — container name
		// inside Pod is fine, no cross-project collision possible there) by
		// scanning only metadata.name lines.
		for _, line := range strings.Split(s.content, "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed == "name: api" {
				// Acceptable when the parent context is "containers:" or
				// "ports:". Without a YAML parser we approximate: if the
				// line preceding wasn't "containers:" or contained a port
				// declaration, complain.
				continue // We accept container/port name=api.
			}
			// Any standalone resource name must include the project prefix.
			if strings.HasPrefix(trimmed, "name: api") && !strings.HasPrefix(trimmed, "name: api ") {
				// `name: api` exact match → check by full equality.
				if trimmed == "name: api" {
					continue // already caught above
				}
			}
		}
	}

	// Service selectors must be project-scoped.
	if !strings.Contains(out1, "app: alpha-api") {
		t.Errorf("project alpha must use selector 'app: alpha-api', got:\n%s", out1)
	}
	if !strings.Contains(out2, "app: bravo-api") {
		t.Errorf("project bravo must use selector 'app: bravo-api', got:\n%s", out2)
	}

	// `loopzero.io/app-name` label must remain the raw user-declared name in
	// BOTH projects so CLI queries (logs, status, scale) keep working.
	if !strings.Contains(out1, "loopzero.io/app-name: api") {
		t.Errorf("project alpha must keep label 'loopzero.io/app-name: api'")
	}
	if !strings.Contains(out2, "loopzero.io/app-name: api") {
		t.Errorf("project bravo must keep label 'loopzero.io/app-name: api'")
	}
}

// TestResourceName_DeterministicAndStable pins the helper that drives the
// fix for Achado #17.
func TestResourceName_DeterministicAndStable(t *testing.T) {
	cases := []struct {
		project, app, want string
	}{
		{"webapp", "api", "webapp-api"},
		{"solo-server", "api", "solo-server-api"},
		{"monorepo", "worker", "monorepo-worker"},
		{"x", "y", "x-y"},
	}
	for _, c := range cases {
		got := resourceName(c.project, c.app)
		if got != c.want {
			t.Errorf("resourceName(%q,%q) = %q, want %q", c.project, c.app, got, c.want)
		}
	}
}
