// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
	"strings"
	"testing"
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
