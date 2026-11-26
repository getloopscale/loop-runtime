// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

// Hermetic unit tests for clusterKubectlArgs — exercises the resolver
// without touching real clusters. Validates T5.2 of plan 2026-05-20
// (EC-23): legacy DRILL_KUBECONFIG_DIR-alone fallback REMOVED.

package e2e

import (
	"os"
	"strings"
	"testing"
)

// withEnv temporarily sets env vars for a test and restores them on cleanup.
// Restores even on panic via t.Cleanup so a failing assert doesn't leak.
func withEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	saved := make(map[string]string, len(vars))
	for k := range vars {
		saved[k] = os.Getenv(k)
	}
	for k, v := range vars {
		if v == "" {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, v)
		}
	}
	t.Cleanup(func() {
		for k, v := range saved {
			if v == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, v)
			}
		}
	})
}

func TestClusterKubectlArgs_DefaultK3D(t *testing.T) {
	withEnv(t, map[string]string{
		"LOOPRUNTIME_TEST_ENV": "",
		"DRILL_KUBECONFIG_DIR": "",
	})

	args := clusterKubectlArgs(ClusterMgmt)
	if len(args) < 3 || args[0] != "kubectl" || args[1] != "--context" {
		t.Fatalf("default (k3d) should emit kubectl --context <ctx>, got %v", args)
	}
	if !strings.Contains(args[2], "k3d") && !strings.Contains(args[2], "loopruntime-core") {
		t.Errorf("default context should target k3d mgmt, got %q", args[2])
	}
}

func TestClusterKubectlArgs_LOOPRUNTIME_TEST_ENV_K3D_Explicit(t *testing.T) {
	withEnv(t, map[string]string{
		"LOOPRUNTIME_TEST_ENV": "k3d",
		"DRILL_KUBECONFIG_DIR": "",
	})

	args := clusterKubectlArgs(ClusterMgmt)
	if args[1] != "--context" {
		t.Fatalf("LOOPRUNTIME_TEST_ENV=k3d should emit --context, got %v", args)
	}
}

// T5.2 critical assertion: legacy fallback REMOVED. Setting only
// DRILL_KUBECONFIG_DIR (without LOOPRUNTIME_TEST_ENV) must NOT route to drill —
// it falls through to k3d default. This catches regressions if anyone
// re-adds the fallback.
func TestClusterKubectlArgs_LegacyFallback_Removed(t *testing.T) {
	withEnv(t, map[string]string{
		"LOOPRUNTIME_TEST_ENV": "",
		"DRILL_KUBECONFIG_DIR": "/tmp/drill-kc",
	})

	args := clusterKubectlArgs(ClusterMgmt)
	if len(args) < 3 {
		t.Fatalf("expected at least 3 args, got %v", args)
	}
	// Must be k3d --context path, NOT drill --kubeconfig path.
	if args[1] == "--kubeconfig" {
		t.Errorf("T5.2 regression: DRILL_KUBECONFIG_DIR alone routed to drill — fallback should be removed; got %v", args)
	}
	if args[1] != "--context" {
		t.Errorf("expected default k3d --context resolver, got %v", args)
	}
}

func TestClusterKubectlArgs_Drill_Explicit(t *testing.T) {
	withEnv(t, map[string]string{
		"LOOPRUNTIME_TEST_ENV": "drill",
		"DRILL_KUBECONFIG_DIR": "/tmp/drill-kc",
	})

	args := clusterKubectlArgs(ClusterMgmt)
	if args[1] != "--kubeconfig" {
		t.Fatalf("LOOPRUNTIME_TEST_ENV=drill should emit --kubeconfig, got %v", args)
	}
	if !strings.HasSuffix(args[2], "mgmt.yaml") {
		t.Errorf("expected /tmp/drill-kc/mgmt.yaml suffix, got %q", args[2])
	}
}

func TestClusterKubectlArgs_Drill_MissingKubeconfigDir_Panics(t *testing.T) {
	withEnv(t, map[string]string{
		"LOOPRUNTIME_TEST_ENV": "drill",
		"DRILL_KUBECONFIG_DIR": "",
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("LOOPRUNTIME_TEST_ENV=drill without DRILL_KUBECONFIG_DIR should panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic should carry string, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "DRILL_KUBECONFIG_DIR") {
			t.Errorf("panic message should mention DRILL_KUBECONFIG_DIR, got %q", msg)
		}
	}()

	_ = clusterKubectlArgs(ClusterMgmt)
}

func TestClusterKubectlArgs_Prod_Stub_Panics(t *testing.T) {
	withEnv(t, map[string]string{
		"LOOPRUNTIME_TEST_ENV": "prod",
	})

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("LOOPRUNTIME_TEST_ENV=prod should panic (stub per ADR D5)")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic should carry string, got %T: %v", r, r)
		}
		if !strings.Contains(msg, "prod") || !strings.Contains(msg, "ADR D5") {
			t.Errorf("panic message should reference prod + ADR D5, got %q", msg)
		}
	}()

	_ = clusterKubectlArgs(ClusterMgmt)
}

func TestClusterKubectlArgs_PerClusterRole(t *testing.T) {
	withEnv(t, map[string]string{
		"LOOPRUNTIME_TEST_ENV": "drill",
		"DRILL_KUBECONFIG_DIR": "/tmp/drill-kc",
	})

	cases := []struct {
		role   ClusterRole
		suffix string
		label  string
	}{
		{ClusterMgmt, "mgmt.yaml", "mgmt"},
		{ClusterBuild, "build.yaml", "build"},
		{ClusterWorkload, "runtime.yaml", "workload→runtime.yaml (drill naming)"},
	}
	for _, tc := range cases {
		args := clusterKubectlArgs(tc.role)
		if !strings.HasSuffix(args[2], tc.suffix) {
			t.Errorf("%s: expected suffix %q, got %q", tc.label, tc.suffix, args[2])
		}
	}
}
