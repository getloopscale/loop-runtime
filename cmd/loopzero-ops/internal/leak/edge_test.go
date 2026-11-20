// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package leak

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The assertion that gives the whole probe its value: accepting 404 as closed
// would make the probe useless in exactly the case it exists for — a broken
// route answers 404 and would pass as "protected". Hiding is not denying.
func TestClosedRejects404(t *testing.T) {
	cases := []struct {
		status int
		closes bool
		why    string
	}{
		{http.StatusUnauthorized, true, "denies explicitly"},
		{http.StatusForbidden, true, "denies explicitly"},
		{http.StatusFound, true, "leads to the login flow"},
		{http.StatusSeeOther, true, "leads to the login flow"},
		{http.StatusNotFound, false, "hiding is not denying, and it disguises a broken route"},
		{http.StatusOK, false, "delivered the data without a credential"},
		{http.StatusInternalServerError, false, "failed, but did not deny"},
		{http.StatusBadGateway, false, "upstream down; not proof of closure"},
	}
	for _, c := range cases {
		if got := closed(c.status); got != c.closes {
			t.Errorf("closed(%d) = %v, want %v — %s", c.status, got, c.closes, c.why)
		}
	}
}

func writeInventory(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proxy-exposure.txt")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write inventory: %v", err)
	}
	return path
}

// Coverage follows the surface: targets come from the inventory, not from a
// list of their own that would age alone.
func TestTargetsDeriveFromTheInventory(t *testing.T) {
	inv := writeInventory(t, `# comments are ignored
app-dev.getloopscale.com /engine/* | reviewed | engine via BFF
app-dev.getloopscale.com /v1/* | reviewed | BFF
other-host.getloopscale.com /v1/* | reviewed | a different host, must not be included
`)
	targets, uncovered, err := Targets(inv, "app-dev.getloopscale.com")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(uncovered) != 0 {
		t.Errorf("no family should be uncovered, got %v", uncovered)
	}
	if len(targets) != 2 {
		t.Fatalf("want 2 targets, got %d: %+v", len(targets), targets)
	}
	for _, target := range targets {
		if target.Host != "app-dev.getloopscale.com" {
			t.Errorf("a target from another host leaked in: %+v", target)
		}
		if strings.Contains(target.Path, "*") {
			t.Errorf("target has a wildcard and is not requestable: %q", target.Path)
		}
	}
}

// A new declared family without a representative must not vanish in silence:
// silence becomes a PASS that measured nothing.
func TestTargetsReportAFamilyWithoutARepresentative(t *testing.T) {
	inv := writeInventory(t, "app-dev.getloopscale.com /v1/invoicing/* | reviewed | new surface\n")
	targets, uncovered, err := Targets(inv, "app-dev.getloopscale.com")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(targets) != 0 {
		t.Errorf("a family without a representative must not become a target: %+v", targets)
	}
	if len(uncovered) != 1 {
		t.Fatalf("want exactly one reported hole, got %v", uncovered)
	}
	if !strings.Contains(uncovered[0], "/v1/invoicing/*") || !strings.Contains(uncovered[0], "app-dev.getloopscale.com") {
		t.Errorf("the hole must name its family and its host: %q", uncovered[0])
	}
}

// Exempting is a decision, forgetting is a defect: exempt families produce
// neither a target nor a hole.
func TestTargetsDoNotConfuseExemptWithForgotten(t *testing.T) {
	inv := writeInventory(t, `app-dev.getloopscale.com /self-service/* | reviewed | browser flow
app-dev.getloopscale.com @auth_bff | reviewed | named matcher
app-dev.getloopscale.com /api/v1/webhooks/github | internal | HMAC
`)
	targets, uncovered, err := Targets(inv, "app-dev.getloopscale.com")
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if len(targets) != 0 || len(uncovered) != 0 {
		t.Errorf("exempt families yield no target and no hole; got targets=%+v uncovered=%v", targets, uncovered)
	}
}

func tlsServer(t *testing.T, h http.HandlerFunc) (*http.Client, string) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	cli := srv.Client()
	cli.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return cli, strings.TrimPrefix(srv.URL, "https://")
}

// A 401 on OIDC discovery breaks login instead of protecting it: clients read
// that document before holding any token.
func TestProbeRequiresAPublicBySpecRouteToAnswer(t *testing.T) {
	cli, host := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusUnauthorized) })
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/.well-known/openid-configuration", PublicBySpec: true})
	if v.Passed {
		t.Error("a route public by specification answering 401 must FAIL — it breaks OIDC clients")
	}
	if !strings.Contains(v.Reason, "specification") {
		t.Errorf("the reason should explain the OIDC contract, got %q", v.Reason)
	}
}

// The case the probe exists to catch.
func TestProbeFailsADataLeak(t *testing.T) {
	cli, host := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"projects":[{"id":"tn-1"}]}`))
	})
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/engine/api/v1/projects"})
	if v.Passed {
		t.Fatal("200 without a credential must FAIL — it is exactly the leak the probe looks for")
	}
	if v.Status != http.StatusOK {
		t.Errorf("observed status = %d, want 200", v.Status)
	}
}

// An unreachable target is not proof of closure: counting a connection error
// as PASS would turn "the environment is down" into "the edge is safe".
func TestProbeMarksAnUnreachableTargetSkipped(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	host := strings.TrimPrefix(srv.URL, "https://")
	cli := srv.Client()
	srv.Close()

	v := Probe(context.Background(), cli, Target{Host: host, Path: "/v1/anything"})
	if v.Passed {
		t.Error("an unreachable target must NOT pass")
	}
	if !v.Skipped {
		t.Error("an unreachable target must be marked skipped, so the report tells it apart from a failure")
	}
}

// The identity root redirects to login on purpose; demanding 200 on public
// routes would fail that fix. 200 serves the content, 302 leads to it.
func TestProbeAcceptsARedirectOnAPublicRoute(t *testing.T) {
	cli, host := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Redirect(w, &http.Request{}, "https://app-dev.getloopscale.com/login", http.StatusFound)
	})
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/", PublicBySpec: true,
		Why: "whoever types the address is a person and needs a way to the login"})
	if !v.Passed {
		t.Errorf("a 302 to login is a valid destination on a public route; got reason %q", v.Reason)
	}
}

// A dead end still fails, and the reason names who depends on the route.
func TestProbeFailsA404OnAPublicRoute(t *testing.T) {
	cli, host := tlsServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "404 page not found", http.StatusNotFound)
	})
	v := Probe(context.Background(), cli, Target{Host: host, Path: "/", PublicBySpec: true,
		Why: "whoever types the address is a person and needs a way to the login"})
	if v.Passed {
		t.Error("404 on a public route is a dead end and must FAIL")
	}
	if !strings.Contains(v.Reason, "person") {
		t.Errorf("the reason should say WHO depends on the route, got %q", v.Reason)
	}
}

// The two modes measured on 2026-08-10: internal host and plain text.
func TestDiscoveryLeakCatchesAnInternalHost(t *testing.T) {
	doc := map[string]any{
		"issuer":                 "http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/",
		"jwks_uri":               "http://ory-hydra-public.loopzero-auth.svc.cluster.local:4444/.well-known/jwks.json",
		"authorization_endpoint": "https://auth-dev.getloopscale.com/oauth2/auth",
		"scopes_supported":       []any{"openid"},
	}
	got := DiscoveryLeak(doc)
	if len(got) != 2 {
		t.Fatalf("want 2 leaks (issuer + jwks_uri), got %d: %v", len(got), got)
	}
	for _, g := range got {
		if !strings.Contains(g, "internal") {
			t.Errorf("the leak should name the internal host: %q", g)
		}
	}
}

func TestDiscoveryLeakAcceptsACorrectDocument(t *testing.T) {
	doc := map[string]any{
		"issuer":                 "https://auth-dev.getloopscale.com/",
		"jwks_uri":               "https://auth-dev.getloopscale.com/.well-known/jwks.json",
		"authorization_endpoint": "https://auth-dev.getloopscale.com/oauth2/auth",
	}
	if got := DiscoveryLeak(doc); len(got) != 0 {
		t.Errorf("a correct document must not yield a leak, got %v", got)
	}
}

// The defect that broke Google login.
func TestPlaceholderLeakCatchesAnUnsubstitutedCredential(t *testing.T) {
	cfg := `
      providers:
        - id: google
          client_id: "REPLACE_VIA_PULUMI"
          client_secret: "REPLACE_VIA_PULUMI"
        - id: github
          client_id: "Ov23li3uhReal"
`
	got := PlaceholderLeak(cfg)
	if len(got) != 2 {
		t.Fatalf("want 2 findings (client_id + client_secret), got %d: %v", len(got), got)
	}
	for _, g := range got {
		if !strings.Contains(g, "marker") {
			t.Errorf("the finding should name the marker: %q", g)
		}
	}
}

func TestPlaceholderLeakAcceptsARealCredential(t *testing.T) {
	cfg := `client_id: "46285214407-8mrcsu.apps.googleusercontent.com"
client_secret: "GOCSPX-real"`
	if got := PlaceholderLeak(cfg); len(got) != 0 {
		t.Errorf("a real credential must not yield a finding, got %v", got)
	}
}
