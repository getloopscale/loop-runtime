// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package tenancy

import (
	"os"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
)

// T-01 (MEET-ADR-0018 / change-validation-planner) — assert that the
// canonical namespace constant equals "loopruntime-system" regardless of any
// environmental input. Pure unit test, no cluster, no client.
func TestTenantNamespace_AlwaysLoopRuntimeSystem(t *testing.T) {
	if tenantNamespace != "loopruntime-system" {
		t.Errorf("ADR-0018 C1 regression: tenantNamespace = %q, want %q", tenantNamespace, "loopruntime-system")
	}
}

// T-02 (MEET-ADR-0018 / change-validation-planner) — confirm that the
// `TENANT_NAMESPACE` env var has NO effect. Pre-fix it was a `var`
// initialized at package-init time from os.Getenv; post-fix it is a
// compile-time `const`. Setting the env var must NOT change the value.
//
// This test guards against future regressions where someone adds a
// fallback "if env is set, override". The misplacement bug from iter 7-8
// of dogfood happened because operator/main.go was reading TENANT_NAMESPACE
// independently — that path is closed by C1.
func TestTenantNamespace_EnvVarHasNoEffect(t *testing.T) {
	original := os.Getenv("TENANT_NAMESPACE")
	defer func() {
		if original == "" {
			_ = os.Unsetenv("TENANT_NAMESPACE")
		} else {
			_ = os.Setenv("TENANT_NAMESPACE", original)
		}
	}()

	for _, tt := range []string{"default", "kube-system", "evil-namespace", ""} {
		t.Run("env="+tt, func(t *testing.T) {
			if tt == "" {
				_ = os.Unsetenv("TENANT_NAMESPACE")
			} else {
				_ = os.Setenv("TENANT_NAMESPACE", tt)
			}
			if tenantNamespace != "loopruntime-system" {
				t.Errorf("ADR-0018 C1 regression: env=%q, tenantNamespace=%q (want loopruntime-system)", tt, tenantNamespace)
			}
		})
	}
}

// T-02b — source-level guard: TENANT_NAMESPACE string MUST NOT appear in
// tenant_cr_manager.go after C1. If a future PR re-introduces an env-var
// override path, this test catches it at static analysis time. Reading
// the file at test runtime keeps the assertion close to the constraint.
func TestTenantNamespace_NoEnvVarReadInSource(t *testing.T) {
	data, err := os.ReadFile("tenant_cr_manager.go")
	if err != nil {
		t.Fatalf("read tenant_cr_manager.go: %v", err)
	}
	src := string(data)
	if strings.Contains(src, `os.Getenv("TENANT_NAMESPACE")`) {
		t.Error("ADR-0018 C1 regression: tenant_cr_manager.go must NOT read TENANT_NAMESPACE env (removed per MEET-ADR-0018)")
	}
	if !strings.Contains(src, `const tenantNamespace = "loopruntime-system"`) {
		t.Error("ADR-0018 C1 regression: tenant_cr_manager.go must declare `const tenantNamespace = \"loopruntime-system\"`")
	}
}

// TestTenantNamespace_MatchesControlPlaneNamespace — the B-058 split left the
// literal "loopruntime-system" spelled out twice: here, because ADR-0018 C1's source-level
// guard requires it spelled out in tenant_cr_manager.go, and in
// k8s.ControlPlaneNamespace, which k8s/workload reads. Before the split the workload
// files reached into this package's unexported constant, so there was one literal
// and nothing to drift. Now there are two, and this pins them together.
func TestTenantNamespace_MatchesControlPlaneNamespace(t *testing.T) {
	if tenantNamespace != k8s.ControlPlaneNamespace {
		t.Errorf("split drift: tenancy has %q, k8s.ControlPlaneNamespace has %q",
			tenantNamespace, k8s.ControlPlaneNamespace)
	}
}
