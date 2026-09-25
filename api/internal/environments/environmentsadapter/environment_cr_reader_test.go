// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package environmentsadapter

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

// TestEnvCRName_CanonicalAndLegacy asserts the M2 v1.2 naming formula:
//   - canonical: "<tenant>-<project>-<env>" when project is non-empty
//   - legacy:    "<tenant>-<env>" when project is empty (ADR D2)
//
// The formula is the single bridge between EnvironmentCRReader and the
// operator-side tenant_gitops.go emitter — drift here resurfaces as
// "CR not found" loops in EnsureEnvironment.
func TestEnvCRName_CanonicalAndLegacy(t *testing.T) {
	tests := []struct {
		name    string
		tenant  string
		project string
		env     string
		want    string
	}{
		{
			name:    "canonical 3-segment",
			tenant:  "tn-08fa",
			project: "web",
			env:     "staging",
			want:    "tn-08fa-web-staging",
		},
		{
			name:    "legacy production no project",
			tenant:  "tn-08fa",
			project: "",
			env:     "production",
			want:    "tn-08fa-production",
		},
		{
			name:    "canonical preview-pr-N",
			tenant:  "tn-x",
			project: "api",
			env:     "preview-pr-7",
			want:    "tn-x-api-preview-pr-7",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := envCRName(tc.tenant, tc.project, tc.env)
			if got != tc.want {
				t.Errorf("envCRName(%q, %q, %q) = %q; want %q",
					tc.tenant, tc.project, tc.env, got, tc.want)
			}
		})
	}
}

// makeReadyEnvCR builds a fake Environment CR with status.phase=Active.
func makeReadyEnvCR(name, namespace string) *unstructured.Unstructured {
	return &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "loopzero.io/v1",
			"kind":       "Environment",
			"metadata": map[string]interface{}{
				"name":      name,
				"namespace": namespace,
			},
			"status": map[string]interface{}{
				"phase": "Active",
			},
		},
	}
}

// envCRGvrToListKind is the minimum schema mapping needed by the fake
// dynamic client for Environment CR queries.
func envCRGvrToListKind() map[schema.GroupVersionResource]string {
	return map[schema.GroupVersionResource]string{
		environmentCRGVR: "EnvironmentList",
	}
}

// TestEnvironmentCRReader_IsReady_UsesCanonicalNamespace asserts the
// canonical 3-segment CR-name path: IsReady looks for
// `<tenant>-<project>-<env>` when project is non-empty.
func TestEnvironmentCRReader_IsReady_UsesCanonicalNamespace(t *testing.T) {
	scheme := runtime.NewScheme()
	// Pre-create the canonical CR. The fake client returns it when
	// the lookup name matches.
	canonical := makeReadyEnvCR("tn-x-web-staging", "loopzero-system")
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme, envCRGvrToListKind(), canonical,
	)
	r := NewEnvironmentCRReader(client, "loopzero-system")

	ready, err := r.IsReady(context.Background(), "tn-x", "web", "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Errorf("expected ready=true for canonical (tn-x, web, staging) CR with phase=Active")
	}

	// Sanity: the SAME env name under a different project must NOT match.
	other, err := r.IsReady(context.Background(), "tn-x", "api", "staging")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if other {
		t.Errorf("expected ready=false for (tn-x, api, staging) — CR does not exist")
	}
}

// TestEnvironmentCRReader_IsReady_FallsBackLegacyWhenProjectEmpty
// asserts the backward-compat path: when project is empty, the lookup
// targets `<tenant>-<env>` for production-legacy entries (ADR D2).
func TestEnvironmentCRReader_IsReady_FallsBackLegacyWhenProjectEmpty(t *testing.T) {
	scheme := runtime.NewScheme()
	// Legacy production CR — no project segment.
	legacy := makeReadyEnvCR("tn-x-production", "loopzero-system")
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme, envCRGvrToListKind(), legacy,
	)
	r := NewEnvironmentCRReader(client, "loopzero-system")

	ready, err := r.IsReady(context.Background(), "tn-x", "", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ready {
		t.Errorf("expected ready=true for legacy production CR with phase=Active")
	}
}

// TestEnvironmentCRReader_IsReady_AbsentCRReturnsNotReadyNoError asserts
// that a missing CR is the operator-still-reconciling case, not an
// error condition. Caller's poll loop continues until timeout.
func TestEnvironmentCRReader_IsReady_AbsentCRReturnsNotReadyNoError(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme, envCRGvrToListKind(),
		// No objects — CR absent.
	)
	r := NewEnvironmentCRReader(client, "loopzero-system")

	ready, err := r.IsReady(context.Background(), "tn-x", "web", "missing")
	if err != nil {
		t.Errorf("absent CR should NOT surface as error, got %v", err)
	}
	if ready {
		t.Errorf("expected ready=false for absent CR, got true")
	}
}

// TestEnvironmentCRReader_IsReady_RejectsEmptyTenantOrEnv asserts the
// defensive guard against silently passing zero-value lookups.
func TestEnvironmentCRReader_IsReady_RejectsEmptyTenantOrEnv(t *testing.T) {
	scheme := runtime.NewScheme()
	client := dynamicfake.NewSimpleDynamicClientWithCustomListKinds(
		scheme, envCRGvrToListKind(),
	)
	r := NewEnvironmentCRReader(client, "loopzero-system")

	if _, err := r.IsReady(context.Background(), "", "web", "staging"); err == nil {
		t.Errorf("expected error for empty tenantID, got nil")
	}
	if _, err := r.IsReady(context.Background(), "tn-x", "web", ""); err == nil {
		t.Errorf("expected error for empty envName, got nil")
	}
}

// unused-symbol guard — avoid "imported and not used" if a future
// refactor strips the import. Catches the regression at compile time.
var _ = metav1.ListOptions{}
