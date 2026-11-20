// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package proxyexposure

import (
	"os"
	"path/filepath"
	"testing"
)

func setup(t *testing.T, caddy, registry string) Config {
	t.Helper()
	root := t.TempDir()
	c := filepath.Join(root, "Caddyfile")
	r := filepath.Join(root, "registry.txt")
	if err := os.WriteFile(c, []byte(caddy), 0o644); err != nil {
		t.Fatalf("write Caddyfile: %v", err)
	}
	if err := os.WriteFile(r, []byte(registry), 0o644); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	return Config{Root: root, Caddyfile: c, Registry: r}
}

const twoProxies = `app-dev.getloopscale.com {
  handle /sessions/* {
    reverse_proxy localhost:14433 {
      header_up Host auth-dev.getloopscale.com
    }
  }
  handle /assets/* {
    reverse_proxy localhost:5173
  }
}
`

// The gate's whole purpose: a proxy nobody declared must fail. This is the
// shape that let /sessions/* pass the entire Ory session — including the
// user's IP and recovery addresses — with nothing in the repo recording it.
func TestUndeclaredProxyFails(t *testing.T) {
	cfg := setup(t, twoProxies,
		"app-dev.getloopscale.com /sessions/* | filtered | field allowlist applied at the edge\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected FAIL — /assets/* is not declared")
	}
	if len(o.Undeclared) != 1 {
		t.Fatalf("expected 1 undeclared, got %v", o.Undeclared)
	}
	if !contains(o.Undeclared[0], "app-dev.getloopscale.com /assets/*") {
		t.Errorf("undeclared entry should name site+path, got %q", o.Undeclared[0])
	}
}

func TestFullyDeclaredPasses(t *testing.T) {
	cfg := setup(t, twoProxies,
		"# header\napp-dev.getloopscale.com /sessions/* | filtered | field allowlist applied at the edge\n"+
			"app-dev.getloopscale.com /assets/* | internal | static bundle, no user data\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected PASS, undeclared=%v stale=%v malformed=%v", o.Undeclared, o.Stale, o.Malformed)
	}
	if o.Scanned != 2 {
		t.Errorf("expected 2 proxies scanned, got %d", o.Scanned)
	}
}

// A row for an edge that no longer exists makes the registry read as covering
// more than it does.
func TestStaleDeclarationFails(t *testing.T) {
	cfg := setup(t, twoProxies,
		"app-dev.getloopscale.com /sessions/* | filtered | allowlist\n"+
			"app-dev.getloopscale.com /assets/* | internal | static\n"+
			"app-dev.getloopscale.com /gone/* | reviewed | removed last quarter\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected FAIL on the stale row")
	}
	if len(o.Stale) != 1 || o.Stale[0] != "app-dev.getloopscale.com /gone/*" {
		t.Errorf("expected the stale row named, got %v", o.Stale)
	}
}

// A site-level reverse_proxy (no enclosing handle) is still an exposure.
func TestSiteLevelProxyIsScanned(t *testing.T) {
	cfg := setup(t, "auth-dev.getloopscale.com {\n  reverse_proxy localhost:14433\n}\n",
		"auth-dev.getloopscale.com - | reviewed | Kratos public surface, deliberately whole\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("expected PASS, undeclared=%v", o.Undeclared)
	}
	if o.Scanned != 1 {
		t.Errorf("expected 1 scanned, got %d", o.Scanned)
	}
}

// Negative case, and the one that matters most: a class without a reason is an
// oversight wearing a label, so an empty rationale must not satisfy the gate.
func TestEmptyRationaleIsMalformed(t *testing.T) {
	cfg := setup(t, twoProxies,
		"app-dev.getloopscale.com /sessions/* | filtered |\n"+
			"app-dev.getloopscale.com /assets/* | internal | static\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("expected FAIL — a declaration with no rationale asserts nothing")
	}
	if len(o.Malformed) != 1 {
		t.Errorf("expected 1 malformed row, got %v", o.Malformed)
	}
}

func TestUnknownClassIsMalformed(t *testing.T) {
	cfg := setup(t, twoProxies,
		"app-dev.getloopscale.com /sessions/* | probably-fine | vibes\n"+
			"app-dev.getloopscale.com /assets/* | internal | static\n")
	o, _ := Check(cfg)
	if o.Pass || len(o.Malformed) != 1 {
		t.Errorf("expected 1 malformed row for the unknown class, got pass=%v %v", o.Pass, o.Malformed)
	}
}

// Comments must not be read as proxies — the Caddyfile documents the
// /sessions/whoami hazard in prose, and a naive scan would match it.
func TestCommentedProxyIgnored(t *testing.T) {
	cfg := setup(t,
		"app-dev.getloopscale.com {\n  # reverse_proxy localhost:9999 (historical note)\n  handle /assets/* {\n    reverse_proxy localhost:5173\n  }\n}\n",
		"app-dev.getloopscale.com /assets/* | internal | static bundle\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass || o.Scanned != 1 {
		t.Errorf("comment must not count, got pass=%v scanned=%d undeclared=%v", o.Pass, o.Scanned, o.Undeclared)
	}
}

// Zero scanned is a broken parser or a wrong path, never a clean edge.
func TestZeroProxiesFailsClosed(t *testing.T) {
	cfg := setup(t, "app-dev.getloopscale.com {\n  encode gzip\n}\n", "")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Error("an edge config with no reverse_proxy must fail closed, not pass vacuously")
	}
}

func TestMissingCaddyfileErrors(t *testing.T) {
	cfg := setup(t, twoProxies, "")
	cfg.Caddyfile = filepath.Join(cfg.Root, "nope")
	if _, err := Check(cfg); err == nil {
		t.Error("expected an error for a missing Caddyfile")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}

const snippetProxy = `(cell_edge) {
  forward_auth localhost:13001 {
    uri /internal/edge/route
  }
  reverse_proxy {cell_ingress}
}

*.prod-dev.getloopscale.app {
  import cell_edge
}

*.preview-dev.getloopscale.app {
  handle {
    import cell_edge
  }
}
`

// A proxy written once in a snippet reaches the browser through every site that
// imports it. Scanning only site blocks let the wildcard app edge pass with no
// proxy found at all once its reverse_proxy moved into a snippet.
func TestSnippetProxyIsAttributedToEverySiteThatImportsIt(t *testing.T) {
	cfg := setup(t, snippetProxy, "*.prod-dev.getloopscale.app - | reviewed | tenant app\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass {
		t.Fatal("expected FAIL — the preview site imports the proxy and declares nothing")
	}
	if len(o.Undeclared) != 1 || !contains(o.Undeclared[0], "*.preview-dev.getloopscale.app /*") {
		t.Errorf("undeclared = %v, want the preview site's import", o.Undeclared)
	}
}

// forward_auth is a proxy too: on a non-2xx answer the upstream's response goes to
// the client verbatim.
func TestForwardAuthIsScannedAsAProxy(t *testing.T) {
	cfg := setup(t, "app.example.com {\n  forward_auth localhost:9000 {\n    uri /check\n  }\n}\n", "")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if o.Pass || len(o.Undeclared) != 1 || !contains(o.Undeclared[0], "app.example.com -") {
		t.Errorf("forward_auth was not scanned: pass=%v undeclared=%v", o.Pass, o.Undeclared)
	}
}

// A snippet nobody imports exposes nothing.
func TestUnimportedSnippetExposesNothing(t *testing.T) {
	cfg := setup(t, "(unused) {\n  reverse_proxy localhost:1\n}\n\nsite.example.com {\n  reverse_proxy localhost:2\n}\n",
		"site.example.com - | reviewed | the site's own upstream\n")
	o, err := Check(cfg)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !o.Pass {
		t.Errorf("an unimported snippet was counted: undeclared=%v stale=%v", o.Undeclared, o.Stale)
	}
}
