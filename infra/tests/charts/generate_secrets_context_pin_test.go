// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Regression-lock for issue #115: `generate-secrets.sh` did not pin
// `kubectl --context`, so it applied Secrets to whatever kubecontext
// was active. During a 2026-06-20 dev-public provision, an active
// `k3d-loopzero-secrets` context (residue from a prior T0.1 session) caused
// `task setup` to misprovision Secrets to loopzero-secrets instead of mgmt
// — `infra:install:core` failed silently with the wrong target.
//
// Fix: pin via `KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-core}"` +
// a `kubectl()` shell-function wrapper that injects
// `--context="$KUBECTL_CONTEXT"` on every call. All bare `kubectl`
// invocations downstream automatically inherit the canonical mgmt
// context. Override via `KUBECTL_CONTEXT` env var for non-k3d targets.
//
// SCOPE DISCIPLINE — read before extending:
//
//	This guard asserts the wrapper shape + canonical default. It does
//	NOT assert which specific `kubectl` calls exist downstream — the
//	`kubectl()` function override is package-scoped, so every bare call
//	inherits the context by construction. Future audits of the
//	`command kubectl` escape hatch (deliberate raw bypass) belong in a
//	different guard.
//
// Cross-references:
//   - Script: infra/helmfile/scripts/generate-secrets.sh
//   - Issue: getloopscale/loop-runtime#115
//   - Sister guards: dogfood_all_repo_root_test.go,
//     live_proof_env_file_test.go, loopzero_bin_workspace_discovery_test.go
//     (same post-2026-06-20 hermetic-Go-guard pattern)
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const generateSecretsRelPath = "infra/helmfile/scripts/generate-secrets.sh"

func loadGenerateSecrets(t *testing.T) (body string, scriptPath string) {
	t.Helper()
	root := repoRoot(t)
	scriptPath = filepath.Join(root, generateSecretsRelPath)
	b, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", scriptPath, err)
	}
	return string(b), scriptPath
}

// Regexes — raw strings preserve on-disk shape.
var (
	// Canonical assignment: KUBECTL_CONTEXT="${KUBECTL_CONTEXT:-k3d-loopzero-core}"
	reKubectlContextDefault = regexp.MustCompile(`KUBECTL_CONTEXT="\$\{KUBECTL_CONTEXT:-k3d-loopzero-core\}"`)

	// Canonical function override: kubectl() { command kubectl --context="$KUBECTL_CONTEXT" "$@"; }
	reKubectlFnOverride = regexp.MustCompile(`kubectl\(\)\s*\{\s*command\s+kubectl\s+--context="\$KUBECTL_CONTEXT"\s+"\$@";?\s*\}`)
)

// auditGenerateSecretsContextPin encodes the #115 regression-lock contract.
// Returns failure messages (empty slice => guard passes).
func auditGenerateSecretsContextPin(body string) []string {
	var errs []string

	// (a) Canonical KUBECTL_CONTEXT default MUST be present.
	if !reKubectlContextDefault.MatchString(body) {
		errs = append(errs, "(a) script does NOT declare `KUBECTL_CONTEXT=\"${KUBECTL_CONTEXT:-k3d-loopzero-core}\"` — without the canonical mgmt-pin default, an active `k3d-loopzero-secrets` (or any other) context silently retargets Secret provisioning to the wrong cluster (issue #115).")
	}

	// (b) Canonical kubectl() function override MUST be present.
	if !reKubectlFnOverride.MatchString(body) {
		errs = append(errs, "(b) script does NOT declare the canonical `kubectl()` function override that injects `--context=\"$KUBECTL_CONTEXT\"` on every call. Without it, bare `kubectl` invocations downstream inherit the active shell context — the exact bug shape of #115.")
	}

	// (c) Audit-trail comment cites issue #115 + the canonical-pin
	//     reasoning. Operators reading this in 6 months must find the
	//     rationale without spelunking GitHub.
	hasIssue := strings.Contains(body, "#115") ||
		strings.Contains(body, "issue 115") ||
		strings.Contains(body, "issue/#115")
	hasReason := strings.Contains(body, "context") &&
		(strings.Contains(body, "mgmt") ||
			strings.Contains(body, "pin") ||
			strings.Contains(body, "canonical"))
	if !hasIssue {
		errs = append(errs, "(c) audit-trail comment does NOT cite issue #115 — operators won't find the GitHub thread that motivated the pin.")
	}
	if !hasReason {
		errs = append(errs, "(c) audit-trail comment does NOT cite the context-pin reasoning (mgmt / canonical / pin) — operators won't know why this exists.")
	}

	return errs
}

// TestGenerateSecretsContextPin runs the #115 regression-lock guard
// against the canonical generate-secrets.sh on disk.
func TestGenerateSecretsContextPin(t *testing.T) {
	body, _ := loadGenerateSecrets(t)
	if errs := auditGenerateSecretsContextPin(body); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
}

// TestGenerateSecretsContextPin_NegativeControl proves the guard would
// catch the #115 regression by mutating the in-memory body to remove
// the canonical pin AND the function override, then asserting the
// guard reports the missing pieces. A guard that passes the negative
// control is theatre.
func TestGenerateSecretsContextPin_NegativeControl(t *testing.T) {
	body, _ := loadGenerateSecrets(t)

	// Sub-case 1: remove the canonical KUBECTL_CONTEXT default →
	// check (a) should fire.
	mutated1 := reKubectlContextDefault.ReplaceAllString(body, "# REMOVED FOR NEGATIVE CONTROL")
	if mutated1 == body {
		t.Fatalf("negative-control sub-case 1 setup failed: canonical KUBECTL_CONTEXT default not found.")
	}
	errs1 := auditGenerateSecretsContextPin(mutated1)
	aFired := false
	for _, e := range errs1 {
		if strings.HasPrefix(e, "(a)") {
			aFired = true
			break
		}
	}
	if !aFired {
		t.Errorf("negative-control sub-case 1: removing KUBECTL_CONTEXT default did NOT trigger check (a) — guard incomplete.")
	}

	// Sub-case 2: remove the kubectl() function override →
	// check (b) should fire.
	mutated2 := reKubectlFnOverride.ReplaceAllString(body, "# REMOVED FOR NEGATIVE CONTROL")
	if mutated2 == body {
		t.Fatalf("negative-control sub-case 2 setup failed: canonical kubectl() function override not found.")
	}
	errs2 := auditGenerateSecretsContextPin(mutated2)
	bFired := false
	for _, e := range errs2 {
		if strings.HasPrefix(e, "(b)") {
			bFired = true
			break
		}
	}
	if !bFired {
		t.Errorf("negative-control sub-case 2: removing kubectl() function override did NOT trigger check (b) — guard incomplete.")
	}

	// Log expected fires for audit transparency.
	for _, e := range errs1 {
		t.Logf("negative-control sub-case 1 verdict: %s", e)
	}
	for _, e := range errs2 {
		t.Logf("negative-control sub-case 2 verdict: %s", e)
	}
}
