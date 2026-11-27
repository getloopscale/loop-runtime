// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package scripts

import (
	"regexp"
	"strings"
	"testing"
)

// TestVaultBootstrapDev_SplitContexts locks T1.1 of plan
// dev-public-cross-cluster-secrets (issue #111). The legacy
// vault-bootstrap-dev.sh defaulted CONTEXT to k3d-loop-runtime-core and applied
// every kubectl call (both the OpenBao read-side AND the mgmt-side
// Secret writes) against the SAME context — incompatible with Approach A
// canonical (sponsor mandate UNBREAKABLE 2026-06-19) which keeps
// OpenBao in a separate k3d-loop-runtime-secrets cluster.
//
// Post-T1.1 the script splits into two contexts:
//   - OPENBAO_CONTEXT (default k3d-loop-runtime-secrets) → reads bao operator init
//     state + mints AppRole + reads SPKI
//   - TARGET_CONTEXT (default k3d-loop-runtime-core) → applies the
//     loop-runtime-openbao-approle / openbao-spki-pin Secrets where loop-runtime-api
//     Deployment envFrom can reach them
//
// Plus EC-4 absorption: `kubectl create ns --dry-run=client -o yaml |
// kubectl apply -f -` is invoked against TARGET_CONTEXT BEFORE the Secret
// apply (idempotent namespace ensure — fail-fast if ESO ns missing).
//
// RED state expected: current script body has CONTEXT default mgmt + no
// TARGET_CONTEXT variable + no ns-ensure dry-run-apply pattern.
func TestVaultBootstrapDev_SplitContexts(t *testing.T) {
	body := readScript(t, "vault-bootstrap-dev.sh")

	// T1. OPENBAO_CONTEXT default is k3d-loop-runtime-secrets (Approach A canonical).
	// Pattern accepts the canonical assignment whether the variable is
	// renamed to OPENBAO_CONTEXT (preferred) OR kept as CONTEXT with the
	// new default — but the DEFAULT VALUE must be k3d-loop-runtime-secrets.
	openbaoDefaultRe := regexp.MustCompile(`(?m)^(?:OPENBAO_CONTEXT|CONTEXT)="\$\{OPENBAO_CONTEXT:-([^}]+)\}"$`)
	openbaoDefault := openbaoDefaultRe.FindStringSubmatch(body)
	if openbaoDefault == nil {
		t.Fatalf("could not find OPENBAO_CONTEXT default-resolution line — expected pattern like `OPENBAO_CONTEXT=\"${OPENBAO_CONTEXT:-k3d-loop-runtime-secrets}\"`")
	}
	if got, want := openbaoDefault[1], "k3d-loop-runtime-secrets"; got != want {
		t.Errorf("OPENBAO_CONTEXT default = %q; want %q (Approach A sponsor mandate UNBREAKABLE 2026-06-19 — OpenBao lives in separate loop-runtime-secrets cluster, NOT mgmt)", got, want)
	}

	// T2. TARGET_CONTEXT variable exists and defaults to k3d-loop-runtime-core
	// (consumer side where loop-runtime-openbao-approle Secret + loop-runtime-api
	// Deployment envFrom both live).
	targetContextRe := regexp.MustCompile(`(?m)^TARGET_CONTEXT="\$\{TARGET_CONTEXT:-([^}]+)\}"$`)
	targetContextMatch := targetContextRe.FindStringSubmatch(body)
	if targetContextMatch == nil {
		t.Fatalf("TARGET_CONTEXT variable declaration missing — split-context contract requires `TARGET_CONTEXT=\"${TARGET_CONTEXT:-k3d-loop-runtime-core}\"` (the mgmt-side write context)")
	}
	if got, want := targetContextMatch[1], "k3d-loop-runtime-core"; got != want {
		t.Errorf("TARGET_CONTEXT default = %q; want %q (loop-runtime-api Deployment envFrom reads from mgmt cluster loop-runtime-system ns)", got, want)
	}

	// T3. EC-4 ns-ensure: `kubectl create ns --dry-run=client -o yaml |
	// kubectl apply -f -` against TARGET_CONTEXT precedes the Secret apply.
	// Fail-fast if mgmt-side namespace missing (loop-runtime-system canonical per
	// the [[reference_canonical_tests_rewrite_v2_session]] decision and
	// the T1.1 EC-4 absorption in the plan).
	nsEnsureRe := regexp.MustCompile(`kubectl[^\n]*--context "\$TARGET_CONTEXT"[^\n]*create (?:namespace|ns) [^\n]*--dry-run=client -o yaml`)
	if !nsEnsureRe.MatchString(body) {
		t.Error("EC-4 ns-ensure pattern missing — expected `kubectl --context \"$TARGET_CONTEXT\" create ns $TARGET_NS --dry-run=client -o yaml | kubectl --context \"$TARGET_CONTEXT\" apply -f -` BEFORE the Secret apply")
	}

	// T4. loop-runtime-openbao-approle Secret apply uses TARGET_CONTEXT (not
	// OPENBAO_CONTEXT) — the consumer-side write path.
	secretApplyRe := regexp.MustCompile(`kubectl[^\n]*--context "\$TARGET_CONTEXT"[^\n]*-n "\$TARGET_NS"[^\n]*create secret generic loop-runtime-openbao-approle`)
	if !secretApplyRe.MatchString(body) {
		t.Error("loop-runtime-openbao-approle Secret apply does not use --context \"$TARGET_CONTEXT\" — split-context drift: would write to OpenBao cluster instead of mgmt")
	}

	// T5. openbao-spki-pin Secret apply also uses TARGET_CONTEXT.
	spkiApplyRe := regexp.MustCompile(`kubectl[^\n]*--context "\$TARGET_CONTEXT"[^\n]*create secret generic openbao-spki-pin`)
	if !spkiApplyRe.MatchString(body) {
		t.Error("openbao-spki-pin Secret apply does not use --context \"$TARGET_CONTEXT\" — chart envFrom would never see the SPKI pin")
	}

	// T6. Bao reads (operator init / kv put / approle mint) use
	// OPENBAO_CONTEXT (the secrets-cluster side). Look for at least one
	// kubectl exec / kubectl get that uses --context "$OPENBAO_CONTEXT"
	// targeting the openbao pod.
	openbaoExecRe := regexp.MustCompile(`kubectl[^\n]*--context "\$OPENBAO_CONTEXT"[^\n]*(?:exec|get)[^\n]*"\$POD"`)
	if !openbaoExecRe.MatchString(body) {
		t.Error("OpenBao read-side calls do not use --context \"$OPENBAO_CONTEXT\" — Approach A split-context contract requires the bao operator/exec calls to target the loop-runtime-secrets cluster")
	}

	// T7. No literal `-n external-secrets` regression (preserves the
	// loop-runtime-system canonical default from PR #102 — Finding #2 of issue #91).
	if strings.Contains(body, "-n external-secrets") {
		t.Error("literal '-n external-secrets' found — regressing PR #102 / Finding #2 / issue #91")
	}

	// T8. Traceability: issue #111 / dev-public-cross-cluster-secrets
	// must appear in the script body so a future operator understands
	// why the split-context shape exists.
	traceRe := regexp.MustCompile(`issue #111|dev-public-cross-cluster-secrets|Approach A`)
	if !traceRe.MatchString(body) {
		t.Error("canonical traceability comment missing — future operator will not understand why split-context (Approach A UNBREAKABLE)")
	}

	// T9. EC-6 idempotency guard preserved: `bao operator init` is
	// gated behind a status-read of `initialized:true` — never re-init
	// an already-initialized OpenBao (destructive operation per
	// docs/operations/openbao-init-runbook.md). Locks the existing
	// guard against accidental removal during the split-context refactor.
	idempotencyRe := regexp.MustCompile(`initialized`)
	if !idempotencyRe.MatchString(body) {
		t.Error("EC-6 idempotency guard missing — no `initialized` state-check before potential `bao operator init` (anti-pattern: re-init destroys Shamir shards)")
	}
}
