// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package liveprobescript

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/cmd/loopzero-ops/internal/quality/qtestutil"
)

// validScripts satisfies every assertion of the matching Spec.
var validScripts = map[string]string{
	BillingPortal.Gate: `#!/usr/bin/env bash
set -euo pipefail

log() {
  echo "[log] $*"
}

assert() {
  if "$@"; then echo "PASS"; else exit 1; fi
}

curl -s https://billing.stripe.com/v1/billing/portal

exit 0
`,
	OIDCRedirect.Gate: `#!/usr/bin/env bash
set -euo pipefail

log() { echo "[log] $*"; }
assert() { if "$@"; then echo PASS; else exit 1; fi; }
test_provider() { curl -s /self-service/login/browser; }

# google oidc provider
curl -s /self-service/methods/oidc/callback

exit 0
`,
}

func writeRepo(t *testing.T, spec Spec, script, allowlist string) string {
	t.Helper()
	root := t.TempDir()
	if script != "" {
		path := filepath.Join(root, spec.Script)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if allowlist != "" {
		path := filepath.Join(root, AllowlistRel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(allowlist), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func allowlistFor(spec Spec) string {
	return "| " + spec.Script + " | ... |\n"
}

func failuresOf(t *testing.T, root string, spec Spec) []string {
	t.Helper()
	out, err := violationOf(Check(root, spec))
	if !errors.Is(err, ErrViolations) {
		t.Fatalf("want ErrViolations, got %v", err)
	}
	return out.Failures
}

func requireFailure(t *testing.T, failures []string, substr string) {
	t.Helper()
	for _, f := range failures {
		if strings.Contains(f, substr) {
			return
		}
	}
	t.Fatalf("want a failure containing %q, got %v", substr, failures)
}

func TestCheck_ValidScriptPasses(t *testing.T) {
	for _, spec := range Specs {
		t.Run(spec.Gate, func(t *testing.T) {
			root := writeRepo(t, spec, validScripts[spec.Gate], allowlistFor(spec))
			out, err := violationOf(Check(root, spec))
			if err != nil {
				t.Fatalf("want pass, got %v: %v", err, out.Failures)
			}
		})
	}
}

func TestCheck_MissingScriptFails(t *testing.T) {
	for _, spec := range Specs {
		t.Run(spec.Gate, func(t *testing.T) {
			root := writeRepo(t, spec, "", allowlistFor(spec))
			requireFailure(t, failuresOf(t, root, spec), "file exists")
		})
	}
}

func TestCheck_ScriptThatBashRejectsFails(t *testing.T) {
	for _, spec := range Specs {
		t.Run(spec.Gate, func(t *testing.T) {
			broken := validScripts[spec.Gate] + "\nif then\n"
			root := writeRepo(t, spec, broken, allowlistFor(spec))
			requireFailure(t, failuresOf(t, root, spec), "bash -n")
		})
	}
}

// Every pattern, common or script-specific, must fail on its own when the
// text it looks for is removed.
func TestCheck_EachMissingPatternIsReported(t *testing.T) {
	removals := map[string][]struct{ drop, want string }{
		BillingPortal.Gate: {
			{"set -euo pipefail", "strict mode"},
			{"log() {", "log function defined"},
			{"assert() {", "assert function defined"},
			{"curl -s https://billing.stripe.com/v1/billing/portal", "Stripe billing portal probe"},
		},
		OIDCRedirect.Gate: {
			{"test_provider() {", "test_provider function defined"},
			{"# google oidc provider\ncurl -s /self-service/methods/oidc/callback", "OIDC redirect probe"},
		},
	}
	for _, spec := range Specs {
		for _, r := range removals[spec.Gate] {
			t.Run(spec.Gate+"/"+r.want, func(t *testing.T) {
				script := strings.Replace(validScripts[spec.Gate], r.drop, "", 1)
				if script == validScripts[spec.Gate] {
					t.Fatalf("fixture does not contain %q", r.drop)
				}
				root := writeRepo(t, spec, script, allowlistFor(spec))
				requireFailure(t, failuresOf(t, root, spec), r.want)
			})
		}
	}
}

func TestCheck_KratosBrowserFlowWithoutPathIsReported(t *testing.T) {
	script := strings.NewReplacer(
		"/self-service/login/browser", "/login",
		"/self-service/methods/oidc/callback", "/callback",
	).Replace(validScripts[OIDCRedirect.Gate])
	root := writeRepo(t, OIDCRedirect, script, allowlistFor(OIDCRedirect))
	requireFailure(t, failuresOf(t, root, OIDCRedirect), "Kratos browser flow")
}

func TestCheck_ForeignExitCodeFails(t *testing.T) {
	for _, spec := range Specs {
		t.Run(spec.Gate, func(t *testing.T) {
			script := strings.Replace(validScripts[spec.Gate], "\nexit 0\n", "\nexit 5\n", 1)
			root := writeRepo(t, spec, script, allowlistFor(spec))
			requireFailure(t, failuresOf(t, root, spec), "foreign exit code 5")
		})
	}
}

func TestCheck_ScriptMissingFromAllowlistFails(t *testing.T) {
	for _, spec := range Specs {
		t.Run(spec.Gate, func(t *testing.T) {
			root := writeRepo(t, spec, validScripts[spec.Gate], "| some-other-script.sh | ... |\n")
			requireFailure(t, failuresOf(t, root, spec), "script allowlisted")
		})
	}
}

func TestCheck_MissingAllowlistFileFails(t *testing.T) {
	for _, spec := range Specs {
		t.Run(spec.Gate, func(t *testing.T) {
			root := writeRepo(t, spec, validScripts[spec.Gate], "")
			requireFailure(t, failuresOf(t, root, spec), "allowlist file exists")
		})
	}
}

func TestAssertions_CountsEveryCheck(t *testing.T) {
	if got := BillingPortal.Assertions(); got != 8 {
		t.Errorf("BillingPortal: want 8 assertions, got %d", got)
	}
	if got := OIDCRedirect.Assertions(); got != 10 {
		t.Errorf("OIDCRedirect: want 10 assertions, got %d", got)
	}
}

func TestCheck_RealRepoPasses(t *testing.T) {
	root := qtestutil.Root(t)
	for _, spec := range Specs {
		t.Run(spec.Gate, func(t *testing.T) {
			out, err := violationOf(Check(root, spec))
			if err != nil {
				t.Fatalf("real repository fails %s: %v: %v", spec.Gate, err, out.Failures)
			}
		})
	}
}
