// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package workercsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/cmd/loopzero-ops/internal/quality/qtestutil"
)

// writeWorker drops a synthetic worker source into a temp repo root.
func writeWorker(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "infra", "cloudflare")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "worker.mjs"), []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantPass   bool
		wantDetail string
	}{
		{
			name: "the #235 shape — script-src permits unsafe-inline",
			body: `const H = {
  "Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'; img-src 'self' data:",
};`,
			wantPass:   false,
			wantDetail: "unsafe-inline",
		},
		{
			name:       "unsafe-eval is equally fatal",
			body:       `"Content-Security-Policy": "default-src 'self'; script-src 'self' 'unsafe-eval'"`,
			wantPass:   false,
			wantDetail: "unsafe-eval",
		},
		{
			name:     "nonce-based script-src passes",
			body:     `"Content-Security-Policy": "default-src 'self'; script-src 'self' 'nonce-abc123'; style-src 'self'"`,
			wantPass: true,
		},
		{
			// style-src is deliberately out of scope: inline styles do not
			// execute code. Failing on it would let the urgent script-src
			// problem be deferred alongside a cosmetic one.
			name:     "style-src unsafe-inline alone does NOT fail the gate",
			body:     `"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'"`,
			wantPass: true,
		},
		{
			// With no script-src, default-src is the script policy.
			name:       "default-src fallback with unsafe-inline fails",
			body:       `"Content-Security-Policy": "default-src 'self' 'unsafe-inline'; style-src 'self'"`,
			wantPass:   false,
			wantDetail: "default-src permits 'unsafe-inline'",
		},
		{
			name:       "a CSP with neither script-src nor default-src fails",
			body:       `"Content-Security-Policy": "style-src 'self'; img-src 'self'"`,
			wantPass:   false,
			wantDetail: "script is unrestricted",
		},
		{
			// Removing the header entirely used to PASS with a reason claiming
			// script-src permitted nothing unsafe — a directive that did not exist.
			name:       "no CSP header at all fails",
			body:       `const H = { "X-Frame-Options": "DENY" };`,
			wantPass:   false,
			wantDetail: "no Content-Security-Policy header",
		},
		{
			// The directive body stops at the `;`. Without that boundary a
			// later directive's value would be attributed to script-src and
			// the gate would fail for the wrong reason.
			name:     "unsafe-inline in a LATER directive is not attributed to script-src",
			body:     `"Content-Security-Policy": "script-src 'self'; style-src 'unsafe-inline'"`,
			wantPass: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := writeWorker(t, tc.body)
			out, err := Check(Config{RepoRoot: root})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if out.Skipped {
				t.Fatalf("unexpected skip: %s", out.Reason)
			}
			if out.Pass != tc.wantPass {
				t.Fatalf("Pass = %v, want %v (findings: %+v)", out.Pass, tc.wantPass, out.Findings)
			}
			if tc.wantDetail != "" {
				joined := ""
				for _, f := range out.Findings {
					joined += f.Detail
				}
				if !strings.Contains(joined, tc.wantDetail) {
					t.Errorf("findings do not mention %q — an operator must see WHICH directive value is the problem; got %+v",
						tc.wantDetail, out.Findings)
				}
			}
		})
	}
}

// TestCheck_MissingWorkerIsNotASilentPass: a gate that reports success when it
// could not read its subject is worse than no gate, because the green reads as
// proof.
func TestCheck_MissingWorkerIsNotASilentPass(t *testing.T) {
	out, err := Check(Config{RepoRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Skipped {
		t.Fatalf("missing worker should SKIP with a reason, got Pass=%v", out.Pass)
	}
	if !strings.Contains(out.Reason, "NOT verified") {
		t.Errorf("skip reason %q must say the CSP was not verified, otherwise the skip reads as a pass", out.Reason)
	}
}

// TestCheck_AgainstLiveRepo: the real worker passes — the gate is wired into
// quality:gates, so a regression here is a red gate.
func TestCheck_AgainstLiveRepo(t *testing.T) {
	out, err := Check(Config{RepoRoot: qtestutil.RootFile(t, DefaultWorkerPath)})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !out.Pass {
		t.Fatalf("real worker CSP: %+v", out.Findings)
	}
}
