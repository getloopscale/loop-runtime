// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// T1.2 of plan dev-public-cross-cluster-secrets (issue #111).
//
// TestCrossClusterSecretStoreContract locks the dev-public ESO
// ClusterSecretStore overlay (loop-runtime-external-secrets chart) onto the
// canonical SPLIT-CONTEXT contract: workload identity (Kubernetes-auth)
// against the OpenBao instance running in the SEPARATE k3d-loop-runtime-secrets
// cluster (Approach A sponsor mandate UNBREAKABLE 2026-06-19). The legacy
// AppRole shape (static secret_id mounted as K8s Secret in mgmt) is
// replaced with auth.kubernetes pointing at the ESO controller's
// ServiceAccount + a bounded TokenRequest audience that matches the
// OpenBao role's `bound_audiences` (EC-5).
//
// Hermetic: source-level grep on the overlay file + chart values.yaml.
// No kubectl, no helm template — the cross-package install-contract
// suite (loop_runtime_api_install_contract_test.go) already enforces render-time
// invariants for loop-runtime-api; this test guards the AUTH MODE choice in the
// overlay against silent drift back to AppRole (which would re-require
// the cross-cluster Secret bridge that T1.1 just deleted).
//
// Cross-references:
//   - Plan: knowledge-base/plans/dev-public-cross-cluster-secrets-plan.md T1.2
//   - Blueprint D1: workload identity replaces static credential
//   - EC-1: OpenBao kubernetes-auth MUST review tokens against mgmt
//     (JWKS-static OR kubernetes_host=mgmt + disable_local_ca_jwt)
//   - EC-2: CoreDNS host-alias preserves the SAME hostname
//     (`openbao.secrets-system.svc.cluster.local`) → cert SAN survives
//   - EC-5: TokenRequest audience == OpenBao role `bound_audiences`
//   - Issue #111 (Cleanroom canonical chain run 2026-06-18 Finding F-001)
package charts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readOverlay reads a helmfile values overlay relative to the repo root
// resolved from this package's path (infra/tests/charts/).
func readOverlay(t *testing.T, rel string) string {
	t.Helper()
	p := filepath.Join(repoRoot(t), "infra", rel)
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("%s not found at %s: %v", rel, p, err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read %s: %v", p, err)
	}
	return string(b)
}

// TestCrossClusterSecretStoreContract — RED until T1.2 GREEN.
func TestCrossClusterSecretStoreContract(t *testing.T) {
	overlay := readOverlay(t, "helmfile/values/dev-public/loop-runtime-external-secrets.yaml.gotmpl")

	// T1. dev-public overlay declares Kubernetes-auth mode for the
	// ClusterSecretStore. Look for an `auth:` block with `kubernetes:`
	// sub-key OR explicit `auth.kind: kubernetes` selector.
	hasAuthKubernetes := strings.Contains(overlay, "kind: kubernetes") ||
		(strings.Contains(overlay, "auth:") && strings.Contains(overlay, "kubernetes:"))
	if !hasAuthKubernetes {
		t.Error("dev-public ESO overlay missing K8s-auth declaration — workload identity contract (blueprint D1) requires `auth.kind: kubernetes` OR `auth.kubernetes:` block; current shape still uses legacy AppRole")
	}

	// T2. ESO controller ServiceAccount reference present (the workload
	// identity that OpenBao trusts via Kubernetes-auth role binding).
	// The serviceAccountRef.name canonical default is `external-secrets`
	// (chart default) — overlay MAY override.
	if !strings.Contains(overlay, "serviceAccountRef") {
		t.Error("dev-public ESO overlay missing serviceAccountRef — auth.kubernetes requires `serviceAccountRef: { name, namespace, audiences }` per ESO v1beta1 schema")
	}

	// T3. EC-5: TokenRequest audience set. Audiences MUST match the
	// OpenBao role's `bound_audiences` (configured by T1.2 chart change
	// or operator-side `bao write auth/kubernetes/role/loop-runtime-eso ...`).
	// Empty audiences fail-closed: the OpenBao kubernetes-auth role
	// rejects tokens with no `aud` claim.
	if !strings.Contains(overlay, "audiences") {
		t.Error("dev-public ESO overlay missing `audiences` — EC-5 contract: TokenRequest audience MUST match OpenBao role's bound_audiences; empty audiences → 403 on every secret fetch")
	}

	// T4. Legacy AppRole `roleId` literal removed from the overlay. T1.1
	// already deleted the cross-cluster Secret bridge (loop-runtime-openbao-approle
	// in TARGET_CONTEXT/mgmt); leaving roleId here would silently re-route
	// the chart template into the appRole branch and break the chain.
	if strings.Contains(overlay, "roleId:") && !strings.Contains(overlay, "# roleId") {
		t.Error("dev-public ESO overlay still declares `roleId:` — legacy AppRole config conflicts with the K8s-auth mode; remove or comment-out the AppRole fields")
	}

	// T5. EC-2: `server` URL preserves canonical intra-cluster hostname
	// (`openbao.secrets-system.svc.cluster.local`). CoreDNS host-alias
	// (T2.2) rewrites this DNS to the loop-runtime-secrets NodePort IP cross-cluster
	// while preserving the cert SAN. If the overlay literally points at
	// the node-cluster docker DNS or a 127.0.0.1 host port, the cert SAN
	// validation would fail.
	if !strings.Contains(overlay, "openbao.secrets-system.svc.cluster.local") {
		t.Error("dev-public ESO overlay `server` does not contain canonical hostname `openbao.secrets-system.svc.cluster.local` — EC-2 contract requires the SAN-preserving DNS name; CoreDNS host-alias rewrites this to the secrets-cluster IP, NOT the overlay")
	}

	// T6. Traceability — issue #111 / Approach A / T1.2 cited somewhere in
	// the overlay so a future operator understands why the workload-
	// identity shape exists and how to debug it.
	traceability := strings.Contains(overlay, "issue #111") ||
		strings.Contains(overlay, "dev-public-cross-cluster-secrets") ||
		strings.Contains(overlay, "Approach A")
	if !traceability {
		t.Error("dev-public ESO overlay missing traceability — add a comment citing `issue #111` OR `dev-public-cross-cluster-secrets` OR `Approach A` so future operator understands the K8s-auth choice")
	}

	// T7. loop-runtime-api dev-public overlay parameterizes OPENBAO_VAULT_ADDR
	// — does not literally embed the URL but pulls from a value that
	// can be re-sourced post-T2.2 to point at the cross-cluster IP /
	// NodePort. For dev-public the LITERAL stays the canonical
	// intra-cluster DNS (EC-2), but the chain MUST traceably reference
	// the cross-cluster contract.
	apiOverlay := readOverlay(t, "helmfile/values/dev-public/loop-runtime-api.yaml.gotmpl")
	if !strings.Contains(apiOverlay, "openbao.secrets-system.svc.cluster.local") {
		t.Error("dev-public loop-runtime-api overlay missing canonical OpenBao address — OPENBAO_VAULT_ADDR must point at the SAN-preserving DNS name")
	}
	apiCross := strings.Contains(apiOverlay, "cross-cluster") ||
		strings.Contains(apiOverlay, "issue #111") ||
		strings.Contains(apiOverlay, "dev-public-cross-cluster-secrets") ||
		strings.Contains(apiOverlay, "CoreDNS")
	if !apiCross {
		t.Error("dev-public loop-runtime-api overlay missing cross-cluster traceability — add a comment referencing T1.2 / issue #111 / CoreDNS host-alias so operator understands the cross-cluster wiring")
	}

	// T8. Base values keep the in-cluster default unchanged (Q3 of plan).
	// The base chart values.yaml MUST still default `server: ""` (chart-
	// level safe default; per-env overlay populates). Regressing this
	// would inject the cross-cluster topology into single-cluster envs
	// (standalone-multi, prod) that share the base default.
	baseValues := readOverlay(t, "helm/loop-runtime-external-secrets/values.yaml")
	if !strings.Contains(baseValues, "openbaoTransit:") {
		t.Error("base loop-runtime-external-secrets values.yaml missing `openbaoTransit:` block — schema contract corrupted")
	}
}
