// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenantcsppassthrough

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writePair(t *testing.T, middlewares, renderer string) string {
	t.Helper()
	root := t.TempDir()
	for rel, body := range map[string]string{
		defaultMiddlewares: middlewares,
		defaultRenderer:    renderer,
	} {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	return root
}

const mwWithCSP = `apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: rate-limit
spec:
  rateLimit:
    average: 100
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: security-headers
spec:
  headers:
    contentSecurityPolicy: "default-src 'self'; script-src 'self' https://js.stripe.com"
`

func TestCheck(t *testing.T) {
	cases := []struct {
		name     string
		renderer string
		wantPass bool
		wantName string
	}{
		{
			// The state today: tenant routes carry no middleware, so the
			// static CSP never reaches a LoopZeroKit response.
			name:     "renderer attaches no middleware — nonce passes through",
			renderer: `func renderIngress() { r := Route{Match: m, Services: s} ; _ = r }`,
			wantPass: true,
		},
		{
			// The #59 risk made concrete: somebody "finally applies the
			// security defaults" and every inline script dies silently.
			name:     "renderer wires the static-CSP middleware — guard must fire",
			renderer: `Middlewares: []MiddlewareRef{{Name: "security-headers"}},`,
			wantPass: false,
			wantName: "security-headers",
		},
		{
			// Attaching a middleware that carries NO CSP is fine — the guard
			// must not become a blanket ban on middleware, or it will be
			// exempted away the first time rate limiting is wired.
			name:     "attaching a non-CSP middleware is allowed",
			renderer: `Middlewares: []MiddlewareRef{{Name: "rate-limit"}},`,
			wantPass: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writePair(t, mwWithCSP, tc.renderer)
			out, err := Check(Config{RepoRoot: root})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if out.Skipped {
				t.Fatalf("unexpected skip: %s", out.Reason)
			}
			if out.Pass != tc.wantPass {
				t.Fatalf("Pass = %v, want %v (csp middleware: %v, attached: %v)",
					out.Pass, tc.wantPass, out.CSPMiddleware, out.Attached)
			}
			if tc.wantName != "" && !contains(out.Attached, tc.wantName) {
				t.Errorf("Attached = %v, want it to name %q — a failure must identify WHICH middleware to detach",
					out.Attached, tc.wantName)
			}
		})
	}
}

// TestCheck_DocumentScoping: a CSP in one document must not attribute itself to
// a neighbour's name. Without per-document splitting the guard would report
// `rate-limit` as CSP-bearing and fail the moment anyone wired rate limiting —
// a false positive that would get the guard removed.
func TestCheck_DocumentScoping(t *testing.T) {
	names := cspBearingMiddlewares(mwWithCSP)
	if len(names) != 1 || names[0] != "security-headers" {
		t.Fatalf("cspBearingMiddlewares = %v, want exactly [security-headers]", names)
	}
}

// TestCheck_UnreadableInputIsNotASilentPass: green must never mean "could not look".
func TestCheck_UnreadableInputIsNotASilentPass(t *testing.T) {
	out, err := Check(Config{RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Skipped {
		t.Fatalf("missing inputs should SKIP, got Pass=%v", out.Pass)
	}
	if !strings.Contains(out.Reason, "NOT verified") {
		t.Errorf("skip reason %q must state the invariant was not verified", out.Reason)
	}
}

// TestCheck_AgainstLiveRepo asserts the invariant holds in the real tree.
// Expected GREEN — see the package doc: #59's defect is that a correct state
// was undefended, not that the code is wrong.
func TestCheck_AgainstLiveRepo(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", "..", "..", ".."))
	if err != nil {
		t.Fatalf("resolve repo root: %v", err)
	}
	out, err := Check(Config{RepoRoot: root})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if out.Skipped {
		t.Skipf("%s", out.Reason)
	}
	if !out.Pass {
		t.Errorf("REGRESSION (#59): the tenant IngressRoute renderer now references static-CSP middleware %v.\n"+
			"    Every LoopZeroKit per-request nonce is replaced by that middleware's fixed policy, so the header\n"+
			"    stops matching the <script nonce=…> in the body and every inline script is blocked in the\n"+
			"    browser — with no server-side error. Detach it from tenant routes.", out.Attached)
	}
	t.Logf("invariant holds: %s", out.Reason)
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
