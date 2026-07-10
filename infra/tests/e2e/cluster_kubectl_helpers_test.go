// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// Shared kubectl helpers for E2E tests that exercise the mgmt, build, and
// workload (runtime) clusters. Env-aware via LOOPZERO_TEST_ENV so the same
// Ginkgo binary works on local k3d AND prod-identical drill DOKS.
//
// Motivation (feedback_velero_must_be_in_drill, 2026-05-20):
// before this helper, every journey/edge spec hardcoded
// `kubectl --context k3d-loopzero-mgmt` (or build/workload equivalents). On
// drill DOKS those contexts do not exist — the test silently SKIPPED or
// timed out, masking real prod-parity gaps. This file centralizes the
// env detection so a single var (LOOPZERO_TEST_ENV=drill + DRILL_KUBECONFIG_DIR
// pointing at `/tmp/drill-kc/{mgmt,build,runtime}.yaml`) flips every test.
//
// T5.2 of plan 2026-05-20 (EC-23 1-sprint deprecation): the legacy
// implicit-mode fallback (DRILL_KUBECONFIG_DIR alone inferring drill mode)
// was removed 2026-05-27 — Task targets already set LOOPZERO_TEST_ENV
// explicitly. Callers without LOOPZERO_TEST_ENV default to local k3d.
//
// Pattern mirrors `seed.kubectlArgs` so the same convention applies
// across the suite (seed binary, helpers, journey tests).
package e2e

import (
	"log"
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
// role. Resolves env via LOOPZERO_TEST_ENV explicitly.
//
// T5.2 of plan 2026-05-20-infra-standardization-and-secrets (EC-23
// 1-sprint deprecation window): legacy fallback that inferred
// "drill mode" from DRILL_KUBECONFIG_DIR alone has been REMOVED.
// Callers MUST set LOOPZERO_TEST_ENV explicitly. DRILL_KUBECONFIG_DIR
// remains as the PATH variable (where the drill kubeconfigs live)
// but no longer flips the resolver mode by itself.
//
// Resolution order:
//   1. LOOPZERO_TEST_ENV=drill + DRILL_KUBECONFIG_DIR set → drill DOKS resolver
//   2. LOOPZERO_TEST_ENV=k3d (or unset) → local k3d resolver (default)
//   3. LOOPZERO_TEST_ENV=prod → stub (per ADR-2026-05-20-D5)
//
// All Task targets (`task e2e:drill`, `task drill:l2-scale`) already
// set LOOPZERO_TEST_ENV; this removal closes the implicit-mode foot-gun
// that motivated the EnvResolver pattern in the first place.
//
// Example:
//
//	base := clusterKubectlArgs(ClusterMgmt)
//	cmd := exec.Command(base[0], append(base[1:], "get", "ns")...)
func clusterKubectlArgs(role ClusterRole) []string {
	testEnv := os.Getenv("LOOPZERO_TEST_ENV")

	// Drill DOKS resolver
	if testEnv == "drill" {
		drillDir := os.Getenv("DRILL_KUBECONFIG_DIR")
		if drillDir == "" {
			// M23 T4.1 (F18): panic() -> log.Fatalf() to remove stack-trace noise
			// and provide a clean single-line CI error. Full t.Skipf/t.Fatalf
			// refactor would require signature cascade to 40+ callers (many of
			// which have no *testing.T in scope, e.g., kubectlForCluster and its
			// wrappers kubectlBuild, kubectlMgmt, kubectlWorkload). Per parsimony
			// ladder rung 2 (stdlib does it), log.Fatalf is the FAANG-appropriate
			// pragmatic fix: fail-fast, fail-clear, no whole-suite stack noise.
			log.Fatalf("[cluster_kubectl_helpers] LOOPZERO_TEST_ENV=drill requires DRILL_KUBECONFIG_DIR pointing at /tmp/drill-kc/ — set DRILL_KUBECONFIG_DIR before running drill tests")
		}
		return []string{"kubectl", "--kubeconfig", drillDir + "/" + drillFileFor(role), "--insecure-skip-tls-verify"}
	}

	// Prod resolver — stub (future-ready per ADR-2026-05-20-D5)
	if testEnv == "prod" {
		// M23 T4.1 (F18): same rationale as drill branch above.
		log.Fatalf("[cluster_kubectl_helpers] LOOPZERO_TEST_ENV=prod not yet supported — operator action: implement prod kubeconfig resolver per ADR-2026-05-20-D5")
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
