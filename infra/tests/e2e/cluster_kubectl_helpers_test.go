// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Shared kubectl helpers for E2E tests that exercise the mgmt, build, and
// workload (runtime) clusters. Drill-aware via DRILL_KUBECONFIG_DIR so the
// same Ginkgo binary works on local k3d AND prod-identical drill DOKS.
//
// Motivation (feedback_velero_must_be_in_drill, 2026-05-20):
// before this helper, every journey/edge spec hardcoded
// `kubectl --context k3d-loopzero-mgmt` (or build/workload equivalents). On
// drill DOKS those contexts do not exist — the test silently SKIPPED or
// timed out, masking real prod-parity gaps. This file centralizes the
// drill detection so a single env (DRILL_KUBECONFIG_DIR pointing at
// `/tmp/drill-kc/{mgmt,build,runtime}.yaml`) flips every test.
//
// Pattern mirrors `seed.kubectlArgs` so the same convention applies
// across the suite (seed binary, helpers, journey tests).
package e2e

import (
	"os"
	"os/exec"
)

// ClusterRole identifies which of the 3 LoopZero clusters a kubectl call
// targets. Local k3d has separate contexts; drill DOKS has 4 separate
// kubeconfig files mounted via DRILL_KUBECONFIG_DIR.
type ClusterRole int

const (
	ClusterMgmt ClusterRole = iota
	ClusterBuild
	ClusterWorkload
)

// drillFileFor returns the drill kubeconfig filename per cluster role.
// runtime.yaml is the drill name for the workload cluster.
func drillFileFor(role ClusterRole) string {
	switch role {
	case ClusterBuild:
		return "build.yaml"
	case ClusterWorkload:
		return "runtime.yaml"
	default:
		return "mgmt.yaml"
	}
}

// localContextFor returns the k3d context name per cluster role.
func localContextFor(role ClusterRole) string {
	switch role {
	case ClusterBuild:
		return buildKubeContext
	case ClusterWorkload:
		return workloadKubeContext
	default:
		return mgmtKubeContext
	}
}

// clusterKubectlArgs returns the base kubectl argv for the given cluster
// role. Resolves env via LOOPZERO_TEST_ENV (preferred) or DRILL_KUBECONFIG_DIR
// (legacy, deprecated per plan v1.2 T5.1 — removal in T5.2 after
// 1-sprint deprecation window per EC-23).
//
// Resolution order:
//   1. LOOPZERO_TEST_ENV=drill + DRILL_KUBECONFIG_DIR set → drill DOKS resolver
//   2. LOOPZERO_TEST_ENV=k3d (or unset) → local k3d resolver (default)
//   3. LOOPZERO_TEST_ENV=prod → stub (returns error placeholder)
//
// Legacy fallback (DEPRECATED — removed in T5.2):
//   DRILL_KUBECONFIG_DIR set without LOOPZERO_TEST_ENV → routes to drill
//   resolver with stderr deprecation warning.
//
// Example:
//
//	base := clusterKubectlArgs(ClusterMgmt)
//	cmd := exec.Command(base[0], append(base[1:], "get", "ns")...)
func clusterKubectlArgs(role ClusterRole) []string {
	testEnv := os.Getenv("LOOPZERO_TEST_ENV")
	drillDir := os.Getenv("DRILL_KUBECONFIG_DIR")

	// EC-23 deprecation warning: legacy env var still works during deprecation window.
	if testEnv == "" && drillDir != "" {
		_, _ = os.Stderr.WriteString("[cluster_kubectl_helpers] DEPRECATION: DRILL_KUBECONFIG_DIR alone is deprecated per plan v1.2 T5.1 / EC-23.\n[cluster_kubectl_helpers] Set LOOPZERO_TEST_ENV=drill (and keep DRILL_KUBECONFIG_DIR for path). Legacy fallback removed in T5.2.\n")
		testEnv = "drill"
	}

	// Drill DOKS resolver
	if testEnv == "drill" {
		if drillDir == "" {
			panic("[cluster_kubectl_helpers] LOOPZERO_TEST_ENV=drill requires DRILL_KUBECONFIG_DIR pointing at /tmp/drill-kc/")
		}
		return []string{"kubectl", "--kubeconfig", drillDir + "/" + drillFileFor(role), "--insecure-skip-tls-verify"}
	}

	// Prod resolver — stub (future-ready per D5 ADR)
	if testEnv == "prod" {
		panic("[cluster_kubectl_helpers] LOOPZERO_TEST_ENV=prod not yet supported — operator action: implement prod kubeconfig resolver per ADR D5")
	}

	// Default: local k3d (LOOPZERO_TEST_ENV=k3d or unset)
	return []string{"kubectl", "--context", localContextFor(role)}
}

// kubectlForCluster builds an *exec.Cmd targeting the given cluster role,
// appending the caller-provided kubectl args. Drill-aware via
// clusterKubectlArgs.
func kubectlForCluster(role ClusterRole, args ...string) *exec.Cmd {
	base := clusterKubectlArgs(role)
	all := append(base[1:], args...)
	return exec.Command(base[0], all...)
}

// Convenience wrappers — kubectlMgmt lives in journey_velero_dr_test.go,
// kubectlWorkload lives in journey_rollout_test.go, kubectlBuild here.
// All delegate to kubectlForCluster() so a single env (DRILL_KUBECONFIG_DIR)
// flips them simultaneously.
func kubectlBuild(args ...string) *exec.Cmd {
	return kubectlForCluster(ClusterBuild, args...)
}
