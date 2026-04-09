// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"context"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestBuildLoopZeroApplicationCR(t *testing.T) {
	cr := buildLoopZeroApplicationCR("platform-system", LoopZeroApplicationInput{
		TenantID:    "tn-loopzero",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 3001,
	})

	// Verify GVK
	if cr.GetAPIVersion() != "loopzero.io/v1" {
		t.Errorf("expected apiVersion loopzero.io/v1, got %s", cr.GetAPIVersion())
	}
	if cr.GetKind() != "LoopZeroApplication" {
		t.Errorf("expected kind LoopZeroApplication, got %s", cr.GetKind())
	}

	// Verify deterministic name includes hash suffix
	expectedName := loopzeroApplicationCRName("tn-loopzero", "my-project", "api", "production")
	if cr.GetName() != expectedName {
		t.Errorf("expected name %q, got %q", expectedName, cr.GetName())
	}
	// Name should start with the human-readable prefix
	if !strings.HasPrefix(cr.GetName(), "tn-loopzero-my-project-api-production-") {
		t.Errorf("expected name to start with human-readable prefix, got %q", cr.GetName())
	}

	// Verify namespace
	if cr.GetNamespace() != "platform-system" {
		t.Errorf("expected namespace platform-system, got %s", cr.GetNamespace())
	}

	// Verify spec fields
	spec, _ := cr.Object["spec"].(map[string]interface{})
	if spec["tenantRef"] != "tn-loopzero" {
		t.Errorf("expected tenantRef tn-loopzero, got %v", spec["tenantRef"])
	}
	if spec["project"] != "my-project" {
		t.Errorf("expected project my-project, got %v", spec["project"])
	}
	if spec["environment"] != "production" {
		t.Errorf("expected environment production, got %v", spec["environment"])
	}
	if spec["serviceName"] != "api" {
		t.Errorf("expected serviceName api, got %v", spec["serviceName"])
	}
	if spec["servicePort"] != int64(3001) {
		t.Errorf("expected servicePort 3001, got %v", spec["servicePort"])
	}

	// Verify labels for controller selection
	labels := cr.GetLabels()
	if labels["loopzero.io/tenant"] != "tn-loopzero" {
		t.Errorf("expected label loopzero.io/tenant=tn-loopzero, got %v", labels["loopzero.io/tenant"])
	}
	if labels["loopzero.io/project"] != "my-project" {
		t.Errorf("expected label loopzero.io/project=my-project, got %v", labels["loopzero.io/project"])
	}
}

func TestBuildLoopZeroApplicationCR_DefaultPort(t *testing.T) {
	cr := buildLoopZeroApplicationCR("platform-system", LoopZeroApplicationInput{
		TenantID:    "tn-loopzero",
		Project:     "web-app",
		Environment: "staging",
		AppName:     "web",
		ServiceName: "web",
		ServicePort: 0, // should default to 80
	})

	spec, _ := cr.Object["spec"].(map[string]interface{})
	if spec["servicePort"] != int64(80) {
		t.Errorf("expected default servicePort 80, got %v", spec["servicePort"])
	}
}

func TestLoopZeroApplicationCRName_LongInputsTruncated(t *testing.T) {
	// K8s metadata.name max is 253 chars. With very long inputs, the name
	// must be truncated while preserving the 8-char hash suffix for uniqueness.
	longTenant := "tn-" + strings.Repeat("a", 60)
	longProject := strings.Repeat("b", 60)
	longApp := strings.Repeat("c", 60)
	longEnv := strings.Repeat("d", 60)

	name := loopzeroApplicationCRName(longTenant, longProject, longApp, longEnv)
	if len(name) > 253 {
		t.Errorf("CR name exceeds K8s 253-char limit: len=%d, name=%q", len(name), name)
	}
	// Must still end with 8-char hex suffix
	parts := strings.Split(name, "-")
	suffix := parts[len(parts)-1]
	if len(suffix) != 8 {
		t.Errorf("expected 8-char hash suffix after truncation, got %q (len=%d)", suffix, len(suffix))
	}
}

func TestLoopZeroApplicationCRName_TruncatedStillUnique(t *testing.T) {
	// Two inputs that share a long prefix but differ at the end must produce
	// different names even after truncation (the hash is computed on full input).
	base := strings.Repeat("x", 60)
	nameA := loopzeroApplicationCRName("tn-org", base+"alpha", "api", "prod")
	nameB := loopzeroApplicationCRName("tn-org", base+"bravo", "api", "prod")
	if nameA == nameB {
		t.Errorf("truncated names must differ for different inputs, both got %q", nameA)
	}
}

func TestLoopZeroApplicationCRName(t *testing.T) {
	// Deterministic: same input always produces same output
	name1 := loopzeroApplicationCRName("tn-loopzero", "my-project", "api", "production")
	name2 := loopzeroApplicationCRName("tn-loopzero", "my-project", "api", "production")
	if name1 != name2 {
		t.Errorf("expected deterministic names, got %q and %q", name1, name2)
	}

	// Collision-free: different inputs that would collide with simple hyphen join
	// project="my-app", app="web" vs project="my", app="app-web"
	nameA := loopzeroApplicationCRName("tn-x", "my-app", "web", "prod")
	nameB := loopzeroApplicationCRName("tn-x", "my", "app-web", "prod")
	if nameA == nameB {
		t.Errorf("expected different names for different inputs, both got %q", nameA)
	}

	// Human-readable prefix preserved
	if !strings.HasPrefix(name1, "tn-loopzero-my-project-api-production-") {
		t.Errorf("expected human-readable prefix, got %q", name1)
	}

	// Hash suffix is 8 hex chars
	parts := strings.Split(name1, "-")
	suffix := parts[len(parts)-1]
	if len(suffix) != 8 {
		t.Errorf("expected 8-char hash suffix, got %q (len=%d)", suffix, len(suffix))
	}
}

// newFakeDynamicClient returns a fake dynamic client with the LoopZeroApplication GVR registered.
func newFakeDynamicClient(objects ...runtime.Object) *dynamicfake.FakeDynamicClient {
	scheme := runtime.NewScheme()
	return dynamicfake.NewSimpleDynamicClientWithCustomListKinds(scheme,
		map[schema.GroupVersionResource]string{
			loopzeroApplicationGVR: "LoopZeroApplicationList",
		},
		objects...,
	)
}

func TestEnsureLoopZeroApplication_CreateWhenNotFound(t *testing.T) {
	client := newFakeDynamicClient()
	mgr := NewApplicationCRManager(client, "platform-system")

	input := LoopZeroApplicationInput{
		TenantID:    "tn-loopzero",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 3001,
	}

	err := mgr.EnsureLoopZeroApplication(context.Background(), input)
	if err != nil {
		t.Fatalf("EnsureLoopZeroApplication() unexpected error: %v", err)
	}

	// Verify CR was created
	crName := loopzeroApplicationCRName("tn-loopzero", "my-project", "api", "production")
	got, err := client.Resource(loopzeroApplicationGVR).Namespace("platform-system").Get(
		context.Background(), crName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get CR after create: %v", err)
	}
	if got.GetName() != crName {
		t.Errorf("CR name = %q, want %q", got.GetName(), crName)
	}
	spec, _ := got.Object["spec"].(map[string]interface{})
	if spec["servicePort"] != int64(3001) {
		t.Errorf("servicePort = %v, want 3001", spec["servicePort"])
	}
}

func TestEnsureLoopZeroApplication_UpdateWhenExists(t *testing.T) {
	// Pre-populate with a CR that has port 80
	existing := buildLoopZeroApplicationCR("platform-system", LoopZeroApplicationInput{
		TenantID:    "tn-loopzero",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 80,
	})
	client := newFakeDynamicClient(existing)
	mgr := NewApplicationCRManager(client, "platform-system")

	// Update to port 3001
	input := LoopZeroApplicationInput{
		TenantID:    "tn-loopzero",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 3001,
	}
	err := mgr.EnsureLoopZeroApplication(context.Background(), input)
	if err != nil {
		t.Fatalf("EnsureLoopZeroApplication() unexpected error: %v", err)
	}

	// Verify CR was updated
	crName := loopzeroApplicationCRName("tn-loopzero", "my-project", "api", "production")
	got, err := client.Resource(loopzeroApplicationGVR).Namespace("platform-system").Get(
		context.Background(), crName, metav1.GetOptions{})
	if err != nil {
		t.Fatalf("Get CR after update: %v", err)
	}
	spec, _ := got.Object["spec"].(map[string]interface{})
	if spec["servicePort"] != int64(3001) {
		t.Errorf("servicePort after update = %v, want 3001", spec["servicePort"])
	}
}

func TestEnsureLoopZeroApplication_IdempotentCreate(t *testing.T) {
	client := newFakeDynamicClient()
	mgr := NewApplicationCRManager(client, "platform-system")

	input := LoopZeroApplicationInput{
		TenantID:    "tn-loopzero",
		Project:     "web",
		Environment: "staging",
		AppName:     "frontend",
		ServiceName: "frontend",
		ServicePort: 80,
	}

	// Call twice — both must succeed
	if err := mgr.EnsureLoopZeroApplication(context.Background(), input); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := mgr.EnsureLoopZeroApplication(context.Background(), input); err != nil {
		t.Fatalf("second call (idempotent): %v", err)
	}

	// Verify only one CR exists
	crName := loopzeroApplicationCRName("tn-loopzero", "web", "frontend", "staging")
	list, err := client.Resource(loopzeroApplicationGVR).Namespace("platform-system").List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("List CRs: %v", err)
	}
	count := 0
	for _, item := range list.Items {
		if item.GetName() == crName {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 CR named %q, found %d", crName, count)
	}
}
