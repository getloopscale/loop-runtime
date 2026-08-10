// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package workercsp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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

// TestCheck_AgainstLiveRepo pins the CURRENT state of the real worker. It is
// expected RED until issue #235 is fixed — the fix lives in
// infra/cloudflare/, a governance-gated path. The gate is registered but
// deliberately NOT wired into quality:gates for exactly that reason; see the
// `failingGate` entry in cmd/loopzero-ops/quality_gate_wiring_test.go.
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
		t.Skipf("worker not found from this path: %s", out.Reason)
	}
	if out.Pass {
		t.Log("worker CSP is clean — issue #235 appears fixed; wire this gate into taskfiles/quality.yml and drop its failingGate ledger entry")
		return
	}
	for _, f := range out.Findings {
		t.Logf("KNOWN (#235) %s:%d — %s", DefaultWorkerPath, f.Line, f.Detail)
	}
}
