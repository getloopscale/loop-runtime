// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package secrets

import "testing"

// scoped-kubeconfig.sh mints the kubeconfig loopruntime-api uses to reach a peer cluster.
// Since T7.3 of plan 2026-06-02-openbao-adoption it asks OpenBao's kubernetes
// engine for the ServiceAccount token first, and falls back to a legacy
// service-account-token Secret. The fallback is not dead code: a cleanroom has no
// OpenBao yet, so it is the ONLY path that runs on a cold bootstrap, and when it
// breaks every cell's wiring breaks with it.

const (
	scopedKubeconfig = "../../helmfile/scripts/lib/scoped-kubeconfig.sh"
	vaultTokenLib    = "../../helmfile/scripts/lib/vault-k8s-engine-token.sh"
)

func TestVaultTokenLibDeclaresTheFunctionsScopedKubeconfigCalls(t *testing.T) {
	requireClauses(t, vaultTokenLib,
		clause(`(?m)^vault_k8s_engine_available\(\)`, "the availability probe the Vault path starts from is gone"),
		clause(`(?m)^mint_vault_k8s_token\(\)`, "scoped-kubeconfig.sh gates its Vault path on this function existing"),
	)
}

func TestScopedKubeconfigTriesVaultFirst(t *testing.T) {
	requireClauses(t, scopedKubeconfig,
		clause(`source.*vault-k8s-engine-token\.sh|\. .*vault-k8s-engine-token\.sh`,
			"the Vault token library is not sourced, so the Vault path can never run"),
		clause(`if declare -f mint_vault_k8s_token`,
			"the Vault path is not gated on the function existing; without the gate a missing library aborts the script under set -e"),
	)
}

// Three roles, because cells and workload clusters share one: a `cell*` CLUSTER_ID
// that fell through to the default would mint a management token for a cell.
func TestScopedKubeconfigMapsEveryClusterKindToItsVaultRole(t *testing.T) {
	requireClauses(t, scopedKubeconfig,
		clause(`loopruntime-cell-0-build`, "build clusters have no Vault role"),
		clause(`loopruntime-cell-0-workload`, "workload clusters have no Vault role"),
		clause(`loopruntime-cell-0-mgmt`, "the management role — the default — is gone"),
		clause(`cell\*\|workload\*`, "cells no longer share the workload branch and fall through to the management role"),
	)
}

// The cold-bootstrap half: with no library on disk the script must still finish.
func TestScopedKubeconfigSurvivesAColdClusterWithoutOpenBao(t *testing.T) {
	requireClauses(t, scopedKubeconfig,
		clause(`\[ -f.*vault-k8s-engine-token\.sh.*\] && source`,
			"the library is sourced unguarded, so a cleanroom without it fails before the fallback runs"),
		clause(`kubernetes\.io/service-account-token`,
			"the legacy service-account-token Secret is gone, and a cold cluster has no other way to get a token"),
		clause(`if \[\[ -z "\$\{TOKEN\}" \]\]`,
			"the legacy path is not gated on Vault returning nothing, so it runs even when Vault answered"),
		clause(`kube-root-ca\.crt`,
			"with a Vault token no Secret is created, so the CA must come from kube-root-ca.crt — and nothing reads it"),
		clause(`for _ in 1 2 3 4 5 6 7 8 9 10`,
			"the wait for the legacy token to be populated is gone, reintroducing the race on a fresh Secret"),
	)
}
