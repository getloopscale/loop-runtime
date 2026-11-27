// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// F1 regression-lock — guards the SAN-aligned kubernetes_host fix landed in
// loop-runtime commit `7c255524`. The fix derives `k3d-${cluster}-serverlb` (a name
// included in the k3d mgmt API cert SAN list) for the `kubernetes_host=`
// value written to `auth/k8s-core/config` by `vault-bootstrap-dev.sh`.
//
// Using `host.k3d.internal` as that `kubernetes_host` triggers TLS SAN
// mismatch ("no alternative certificate subject name matches target
// hostname 'host.k3d.internal'") that OpenBao's strict Go HTTP client
// classifies as a generic blanket `permission denied` from
// `auth/k8s-core/login` for EVERY role (eso, loop-runtime-api, loop-runtime-app-operator,
// ...) — the F1 symptom of the dev-public T3.1 LIVE iteration 2026-06-20.
//
// SCOPE DISCIPLINE — read this before extending the guard:
//
//	`host.k3d.internal` is STILL correct for the vault REACH (the
//	ClusterSecretStore `server: https://host.k3d.internal:8200` → OpenBao's
//	own cert, whose SAN includes `host.k3d.internal`). This guard ONLY
//	asserts the `auth/k8s-core/config` `kubernetes_host` value — it does
//	NOT forbid `host.k3d.internal` globally in the script. Extending the
//	guard to a global forbid would re-break the canonical CSS reach.
//
// The audit is encoded in a pure function so the negative-control test can
// mutate the in-memory body and PROVE the guard would catch a revert. A
// guard that passes the negative control is theatre.
//
// Cross-references:
//   - F1 root cause + fix commit: loop-runtime commit 7c255524
//   - Reach plan (host.k3d.internal canonical for vault reach): cb7cc254 (T1.2)
//   - R3 bao_auth ordering canonical: 96ab943e
//   - Mandate: .claude/rules/cleanroom-up-live-test-mandate.md UNBREAKABLE
//   - Prompt: docs/f1-san-aligned-guard-prompt.md (umbrella)
//   - Sister gate (script-level, vault reach): host_k3d_internal_canonical_test.go
package charts

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Regex inventory — all use raw strings so the in-file escape sequences map
// cleanly to the on-disk bytes. The script line that ships F1 (7c255524) is:
//
//	CORE_CLUSTER="${TARGET_CONTEXT#k3d-}"
//	SERVERLB_HOST="k3d-${CORE_CLUSTER}-serverlb"
//	MGMT_HOST=$(echo "$MGMT_HOST" | sed -E "s|//(0\\.0\\.0\\.0\|127\\.0\\.0\\.1\|localhost\|host\\.k3d\\.internal)(:[0-9]+)?|//${SERVERLB_HOST}\\2|")
var (
	reServerlbHostVar  = regexp.MustCompile(`SERVERLB_HOST="k3d-\$\{CORE_CLUSTER\}-serverlb"`)
	reMgmtClusterDeriv = regexp.MustCompile(`CORE_CLUSTER="\$\{TARGET_CONTEXT#k3d-\}"`)
	reSedTargetServ    = regexp.MustCompile(`//\$\{SERVERLB_HOST\}\\\\2\|`)
	reSedAltHostK3d    = regexp.MustCompile(`host\\\\\.k3d\\\\\.internal`)
	reBaoWriteConfig   = regexp.MustCompile(`bao write auth/k8s-core/config`)
	reKubeHostMgmtVar  = regexp.MustCompile(`kubernetes_host="\$MGMT_HOST"`)
)

func loadVaultBootstrapDev(t *testing.T) string {
	t.Helper()
	root := repoRoot(t)
	p := filepath.Join(root, "infra/scripts/local/vault-bootstrap-dev.sh")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// auditSANAlignedKubernetesHost encodes the F1 regression-lock contract. It
// returns failure messages (empty slice => guard passes). Pulling the
// assertions into a pure function lets the negative-control test mutate the
// body and prove the guard would catch the regression.
func auditSANAlignedKubernetesHost(body string) []string {
	var errs []string

	// (a1) Script derives `CORE_CLUSTER` from `TARGET_CONTEXT` — the serverlb
	//      name depends on this assignment.
	if !reMgmtClusterDeriv.MatchString(body) {
		errs = append(errs, "(a1) script does NOT derive `CORE_CLUSTER=\"${TARGET_CONTEXT#k3d-}\"` — the serverlb container name in the cert SAN depends on this derivation (F1 fix, loop-runtime commit 7c255524).")
	}
	// (a2) Script derives `SERVERLB_HOST="k3d-${CORE_CLUSTER}-serverlb"` —
	//      the SAN-aligned name `vault-bootstrap-dev.sh` writes into
	//      `auth/k8s-core/config`.
	if !reServerlbHostVar.MatchString(body) {
		errs = append(errs, "(a2) script does NOT assign `SERVERLB_HOST=\"k3d-${CORE_CLUSTER}-serverlb\"` — required to produce a `kubernetes_host` whose hostname is in the k3d mgmt API cert SAN list (F1 fix, loop-runtime commit 7c255524).")
	}

	// (b1) The MGMT_HOST rewrite sed expression MUST use `${SERVERLB_HOST}`
	//      as the replacement target (NOT `host.k3d.internal`). The OpenBao
	//      Go HTTP client verifies TLS hostname strictly against the cert
	//      SAN; `host.k3d.internal` is NOT in the SAN and triggers the F1
	//      blanket-403.
	if !reSedTargetServ.MatchString(body) {
		errs = append(errs, "(b1) MGMT_HOST rewrite sed replacement target is NOT `${SERVERLB_HOST}\\2` — auth/k8s-core/config `kubernetes_host` would not be SAN-aligned, re-introducing the F1 blanket-403 (loop-runtime commit 7c255524).")
	}
	// (b2) The MGMT_HOST rewrite sed alternation MUST include
	//      `host.k3d.internal` in the FROM-set, so a kubeconfig variant whose
	//      server URL points at host.k3d.internal is also rewritten to the
	//      SAN-aligned serverlb name (not silently passed through).
	if !reSedAltHostK3d.MatchString(body) {
		errs = append(errs, "(b2) MGMT_HOST rewrite sed alternation does NOT include `host.k3d.internal` in the FROM-set — a future kubeconfig that exposes host.k3d.internal would slip past the rewrite and re-introduce the F1 SAN mismatch.")
	}
	// (b3) The `bao write auth/k8s-core/config` block MUST consume the
	//      rewritten value via `kubernetes_host="$MGMT_HOST"` — a direct
	//      literal would bypass the SAN-aligned rewrite.
	if !reBaoWriteConfig.MatchString(body) || !reKubeHostMgmtVar.MatchString(body) {
		errs = append(errs, "(b3) `bao write auth/k8s-core/config` block does NOT consume `kubernetes_host=\"$MGMT_HOST\"` — a hardcoded literal would bypass the SAN-aligned rewrite, breaking F1.")
	}

	// (c) Audit-trail comment cites F1 + SAN mismatch + the commit. Operators
	//     reading this in 6 months must be able to find the rationale without
	//     spelunking commits.
	hasCommit := strings.Contains(body, "7c255524")
	hasSAN := strings.Contains(body, "SAN-aligned") ||
		strings.Contains(body, "SAN mismatch") ||
		strings.Contains(body, "SAN list")
	if !hasCommit {
		errs = append(errs, "(c) audit-trail comment does NOT cite loop-runtime commit `7c255524` (F1 fix) — operators reading this in 6 months won't know why the serverlb derivation matters.")
	}
	if !hasSAN {
		errs = append(errs, "(c) audit-trail comment does NOT cite SAN-aligned / SAN mismatch — operators reading this in 6 months won't know the root cause the rewrite addresses.")
	}

	return errs
}

// TestVaultBootstrapSANAlignedHost runs the F1 regression-lock guard against
// the canonical vault-bootstrap-dev.sh on disk. Expected verdict: GREEN
// (no failure messages) at the SAN-aligned fix commit 7c255524 and later.
func TestVaultBootstrapSANAlignedHost(t *testing.T) {
	body := loadVaultBootstrapDev(t)
	if errs := auditSANAlignedKubernetesHost(body); len(errs) > 0 {
		for _, e := range errs {
			t.Error(e)
		}
	}
}

// TestVaultBootstrapSANAlignedHost_NegativeControl proves the guard would
// catch the F1 regression. It mutates the in-memory script body to revert
// the sed replacement target from `${SERVERLB_HOST}` back to
// `host.k3d.internal` (the pre-7c255524 bug shape) and asserts
// auditSANAlignedKubernetesHost() reports at least one error. A guard that
// passes the negative control is theatre.
//
// The mutation is purely in-memory; the on-disk script is NEVER touched.
func TestVaultBootstrapSANAlignedHost_NegativeControl(t *testing.T) {
	body := loadVaultBootstrapDev(t)
	mutated := reSedTargetServ.ReplaceAllString(body, `//host.k3d.internal\\2|`)
	if mutated == body {
		t.Fatalf("negative-control setup failed: could not find `//${SERVERLB_HOST}\\2|` in script body — the GREEN test is vacuous; either the script changed shape or the regex drifted.")
	}
	errs := auditSANAlignedKubernetesHost(mutated)
	if len(errs) == 0 {
		t.Errorf("negative control: auditSANAlignedKubernetesHost() PASSED against a script whose MGMT_HOST rewrite was reverted to `host.k3d.internal` — the guard would NOT catch the F1 regression (theatre).")
	}
	for _, e := range errs {
		t.Logf("negative-control verdict (expected to fire): %s", e)
	}
}
