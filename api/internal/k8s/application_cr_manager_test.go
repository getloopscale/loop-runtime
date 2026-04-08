// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package k8s

import (
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

	// Verify deterministic name: {tenant}-{project}-{app}-{env}
	expectedName := "tn-loopzero-my-project-api-production"
	if cr.GetName() != expectedName {
		t.Errorf("expected name %q, got %q", expectedName, cr.GetName())
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
	tests := []struct {
		tenant  string
		project string
		app     string
		env     string
		want    string
	}{
		{"tn-loopzero", "my-project", "api", "production", "tn-loopzero-my-project-api-production"},
		{"tn-abc", "app", "web", "staging", "tn-abc-app-web-staging"},
	}
	for _, tt := range tests {
		got := loopzeroApplicationCRName(tt.tenant, tt.project, tt.app, tt.env)
		if got != tt.want {
			t.Errorf("loopzeroApplicationCRName(%q,%q,%q,%q) = %q, want %q",
				tt.tenant, tt.project, tt.app, tt.env, got, tt.want)
		}
	}
}
