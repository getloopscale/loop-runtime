// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Package scripts holds hermetic Go content-verification tests for the
// canonical operational bash scripts (infra/scripts/local/*.sh, etc).
// These tests assert on script SOURCE — they do NOT shell out to execute
// the script — so they are 100% hermetic + zero LIVE infra required.
//
// Homed as Go per the operational-logic-capability-architecture
// (un-homed-logic gate): test logic belongs in Go, not new shell. Bash
// tests on shell scripts double the un-homed-logic surface for no
// engineering gain.
package scripts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// scriptPath resolves a path under infra/scripts/local/ relative to this
// test file (infra/tests/scripts/).
func scriptPath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("..", "..", "scripts", "local", name))
	if err != nil {
		t.Fatalf("resolve %s: %v", name, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s not found at %s: %v", name, p, err)
	}
	return p
}

// readScript reads a script source verbatim.
func readScript(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(scriptPath(t, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// TestVaultBootstrapDev_TargetNSDefaultIsLoopRuntimeSystem locks Finding #2 of
// issue #91 (Cleanroom mandate canonical chain run 2026-06-18). The
// legacy default ESO_TARGET_NS:-external-secrets wrote the
// loop-runtime-openbao-approle Secret to a namespace where loop-runtime-api Deployment
// envFrom could not reach it (envFrom reads from loop-runtime-system), so a
// fresh Cleanroom seed left loop-runtime-api in CrashLoopBackOff with
// "OPENBAO_VAULT_SECRET_ID required" and the downstream master seed
// Step 2 Layer 4 (kubectl rollout status deploy/loop-runtime-api --timeout=120s)
// aborted. Default is now loop-runtime-system (canonical consumer ns). Override
// preserved via ESO_TARGET_NS env var for pre-canonical workloads.
//
// Same stale-helper-post-architecture-refactor class as PR #88
// (rewriter) + PR #89 (Tiltfile only=[] context).
func TestVaultBootstrapDev_TargetNSDefaultIsLoopRuntimeSystem(t *testing.T) {
	body := readScript(t, "vault-bootstrap-dev.sh")

	// T1. Default ns is loop-runtime-system. The script line is the canonical
	// pattern `TARGET_NS="${ESO_TARGET_NS:-DEFAULT}"`.
	defaultLine := regexp.MustCompile(`(?m)^TARGET_NS="\$\{ESO_TARGET_NS:-([^}]+)\}"$`).FindStringSubmatch(body)
	if defaultLine == nil {
		t.Fatalf("could not find TARGET_NS default-resolution line in script body")
	}
	if got, want := defaultLine[1], "loop-runtime-system"; got != want {
		t.Errorf("TARGET_NS default = %q; want %q (loop-runtime-api Deployment envFrom reads from loop-runtime-system; setting external-secrets here corrupts the Cleanroom chain)", got, want)
	}

	// T2. ESO_TARGET_NS env var override pattern preserved (back-compat
	// for operators with workloads still bound to external-secrets ns).
	// The capture group above already proved the ${ESO_TARGET_NS:-...}
	// override pattern is intact; this assertion locks the env var name
	// against a future rename that would silently break operator workflows.
	if !strings.Contains(body, "ESO_TARGET_NS") {
		t.Error("ESO_TARGET_NS env var override missing — operator back-compat broken")
	}

	// T3. Step 10 log line references $TARGET_NS rather than a hardcoded
	// namespace. Defensive: catches a future refactor that drifts the log
	// line vs the kubectl-apply target.
	step10LogRe := regexp.MustCompile(`log "Step 10:[^"]*\$TARGET_NS`)
	if !step10LogRe.MatchString(body) {
		t.Error("Step 10 log line does not interpolate $TARGET_NS — log/kubectl-apply drift risk")
	}

	// T4. kubectl create secret loop-runtime-openbao-approle uses $TARGET_NS
	// (no hardcoded namespace).
	kubectlRe := regexp.MustCompile(`kubectl[^\n]*-n "\$TARGET_NS"[^\n]*create secret generic loop-runtime-openbao-approle`)
	if !kubectlRe.MatchString(body) {
		t.Error("kubectl create secret loop-runtime-openbao-approle does not use $TARGET_NS — drift between resolved default and actual apply target")
	}

	// T5. No literal `-n external-secrets` anywhere in the script.
	// Defensive: forbids regressing the default back to a baked-in
	// literal anywhere in the Step 10 block (or anywhere else).
	if strings.Contains(body, "-n external-secrets") {
		t.Error("literal '-n external-secrets' found — Finding #2 regression risk")
	}

	// T6. Traceability comment present (Finding #2 / issue #91 /
	// Cleanroom mandate). Catches future docs drift where the canonical
	// rationale gets pruned by a "clean up comments" refactor and the
	// next operator does not understand why loop-runtime-system is canonical.
	traceRe := regexp.MustCompile(`Finding #2|issue #91|Cleanroom mandate`)
	if !traceRe.MatchString(body) {
		t.Error("canonical traceability comment missing — future operator will not understand why loop-runtime-system default")
	}
}
