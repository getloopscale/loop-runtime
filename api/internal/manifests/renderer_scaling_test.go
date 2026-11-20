// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"strings"
	"testing"

	sigsyaml "sigs.k8s.io/yaml"
)

// parseHPA extracts the first HorizontalPodAutoscaler from rendered output.
func parseHPA(t *testing.T, rendered string) k8sHPA {
	t.Helper()
	docs := splitDocs(rendered)
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "HorizontalPodAutoscaler" {
			var hpa k8sHPA
			if err := sigsyaml.Unmarshal([]byte(doc), &hpa); err != nil {
				t.Fatalf("failed to parse HPA: %v", err)
			}
			return hpa
		}
	}
	t.Fatal("no HorizontalPodAutoscaler found in rendered output")
	return k8sHPA{}
}

// hasHPA checks whether a HorizontalPodAutoscaler exists in rendered output.
func hasHPA(rendered string) bool {
	docs := splitDocs(rendered)
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "HorizontalPodAutoscaler" {
			return true
		}
	}
	return false
}

// splitDocs splits rendered YAML into individual documents.
func splitDocs(rendered string) []string {
	return strings.Split(rendered, "\n---\n")
}

func TestRenderDeployment_DefaultReplicas(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	dep := parseDeployment(t, mustRender(t, input))

	// Default is 2 (HA) per renderer_deployment.go:104 — chaos C03 fix
	// 2026-05-14 raised the default from 1 to 2 so pod-kill chaos
	// produces 50% (not 0%) availability during the kill window. To
	// opt out set replicas: 1 in loopzero.yaml — see TestRenderDeployment_
	// CustomReplicas for the explicit-override case.
	if dep.Spec.Replicas != 2 {
		t.Errorf("default replicas (HA): got %d, want 2", dep.Spec.Replicas)
	}
}

func TestRenderDeployment_CustomReplicas(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Replicas: 3,
		},
	}

	dep := parseDeployment(t, mustRender(t, input))

	if dep.Spec.Replicas != 3 {
		t.Errorf("custom replicas: got %d, want 3", dep.Spec.Replicas)
	}
}

func TestRenderDeployment_WithHPA(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 2,
				MaxReplicas: 5,
				TargetCPU:   75,
			},
		},
	}

	result, _ := Render(input)

	if !hasHPA(result) {
		t.Fatal("expected HPA to be generated when autoscaling is configured")
	}

	hpa := parseHPA(t, result)

	if hpa.APIVersion != "autoscaling/v2" {
		t.Errorf("HPA apiVersion: got %q, want autoscaling/v2", hpa.APIVersion)
	}
	if hpa.Kind != "HorizontalPodAutoscaler" {
		t.Errorf("HPA kind: got %q, want HorizontalPodAutoscaler", hpa.Kind)
	}
	// Achado #17 fix: HPA name + scaleTargetRef are project-scoped to avoid
	// collisions when two projects in the same tenant env share an app name.
	if hpa.Metadata.Name != "webapp-api" {
		t.Errorf("HPA name: got %q, want webapp-api", hpa.Metadata.Name)
	}
	// Canonical refactor 2026-06-07: namespace = loopzero-<tenant>-<project>-<env>.
	if hpa.Metadata.Namespace != "loopzero-acme-webapp-production" {
		t.Errorf("HPA namespace: got %q, want loopzero-acme-webapp-production", hpa.Metadata.Namespace)
	}
	if hpa.Spec.MinReplicas != 2 {
		t.Errorf("HPA minReplicas: got %d, want 2", hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 5 {
		t.Errorf("HPA maxReplicas: got %d, want 5", hpa.Spec.MaxReplicas)
	}
	if hpa.Spec.ScaleTargetRef.APIVersion != "apps/v1" {
		t.Errorf("HPA scaleTargetRef.apiVersion: got %q, want apps/v1", hpa.Spec.ScaleTargetRef.APIVersion)
	}
	if hpa.Spec.ScaleTargetRef.Kind != "Deployment" {
		t.Errorf("HPA scaleTargetRef.kind: got %q, want Deployment", hpa.Spec.ScaleTargetRef.Kind)
	}
	if hpa.Spec.ScaleTargetRef.Name != "webapp-api" {
		t.Errorf("HPA scaleTargetRef.name: got %q, want webapp-api", hpa.Spec.ScaleTargetRef.Name)
	}

	// Verify metrics
	if len(hpa.Spec.Metrics) != 1 {
		t.Fatalf("HPA metrics: got %d, want 1", len(hpa.Spec.Metrics))
	}
	metric := hpa.Spec.Metrics[0]
	if metric.Type != "Resource" {
		t.Errorf("HPA metric type: got %q, want Resource", metric.Type)
	}
	if metric.Resource.Name != "cpu" {
		t.Errorf("HPA metric resource name: got %q, want cpu", metric.Resource.Name)
	}
	if metric.Resource.Target.Type != "Utilization" {
		t.Errorf("HPA metric target type: got %q, want Utilization", metric.Resource.Target.Type)
	}
	if metric.Resource.Target.AverageUtilization != 75 {
		t.Errorf("HPA metric targetCPU: got %d, want 75", metric.Resource.Target.AverageUtilization)
	}
}

func TestRenderDeployment_NoHPA_WithoutAutoscaling(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{Name: "api", Type: "server", Port: 8080, ImageRef: "reg:5000/acme/webapp:abc123"},
	}

	result, _ := Render(input)

	if hasHPA(result) {
		t.Error("HPA should not be generated when autoscaling is not configured")
	}
}

func TestRenderDeployment_HPA_DefaultValues(t *testing.T) {
	// Autoscaling with zero values should use defaults: min=1, max=3, cpu=80
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:        "api",
			Type:        "server",
			Port:        8080,
			ImageRef:    "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{},
		},
	}

	result, _ := Render(input)
	hpa := parseHPA(t, result)

	if hpa.Spec.MinReplicas != 1 {
		t.Errorf("HPA default minReplicas: got %d, want 1", hpa.Spec.MinReplicas)
	}
	if hpa.Spec.MaxReplicas != 3 {
		t.Errorf("HPA default maxReplicas: got %d, want 3", hpa.Spec.MaxReplicas)
	}
	if hpa.Spec.Metrics[0].Resource.Target.AverageUtilization != 80 {
		t.Errorf("HPA default targetCPU: got %d, want 80", hpa.Spec.Metrics[0].Resource.Target.AverageUtilization)
	}
}

func TestRenderDeployment_HPA_SetsReplicasToMin(t *testing.T) {
	// When autoscaling is configured, the Deployment replicas should equal minReplicas
	// so the HPA can manage scaling from that baseline.
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 2,
				MaxReplicas: 10,
				TargetCPU:   70,
			},
		},
	}

	dep := parseDeployment(t, mustRender(t, input))

	if dep.Spec.Replicas != 2 {
		t.Errorf("with autoscaling, deployment replicas should equal minReplicas: got %d, want 2", dep.Spec.Replicas)
	}
}

func TestRenderDeployment_HPA_ValidYAML(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 1,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)
	docs := splitDocs(result)
	for i, doc := range docs {
		var parsed interface{}
		if err := sigsyaml.Unmarshal([]byte(doc), &parsed); err != nil {
			t.Errorf("document %d is not valid YAML: %v", i, err)
		}
	}
}

func TestRenderDeployment_ServerWithHPA_ProducesSixDocuments(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 1,
				MaxReplicas: 3,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)
	docs := splitDocs(result)

	// Deployment + PDB + Service + Certificate + IngressRoute + HPA = 6
	if len(docs) != 6 {
		t.Fatalf("expected 6 YAML documents (Deployment, PDB, Service, Certificate, IngressRoute, HPA), got %d", len(docs))
	}
}

func TestRenderDeployment_WorkerWithHPA(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "processor",
			Type:     "worker",
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 1,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)
	docs := splitDocs(result)

	// Deployment + PDB + HPA = 3
	if len(docs) != 3 {
		t.Fatalf("expected 3 YAML documents (Deployment, PDB, HPA), got %d", len(docs))
	}

	hpa := parseHPA(t, result)
	if hpa.Spec.ScaleTargetRef.Name != "webapp-processor" {
		t.Errorf("HPA scaleTargetRef.name: got %q, want webapp-processor (project-scoped)", hpa.Spec.ScaleTargetRef.Name)
	}
}

func TestRenderDeployment_HPA_HasLabels(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 1,
				MaxReplicas: 3,
				TargetCPU:   80,
			},
		},
	}

	hpa := parseHPA(t, mustRender(t, input))

	// Achado #17: `app` label carries the project-scoped resource name so
	// Service.spec.selector matches only this project's pods. The raw app
	// name remains available via `loopzero.io/app-name`.
	if hpa.Metadata.Labels["app"] != "webapp-api" {
		t.Errorf("HPA label 'app': got %q, want webapp-api", hpa.Metadata.Labels["app"])
	}
	if hpa.Metadata.Labels["loopzero.io/app-name"] != "api" {
		t.Errorf("HPA label 'loopzero.io/app-name': got %q, want api", hpa.Metadata.Labels["loopzero.io/app-name"])
	}
	if hpa.Metadata.Labels["loopzero.io/tenant"] != "acme" {
		t.Errorf("HPA label 'loopzero.io/tenant': got %q, want acme", hpa.Metadata.Labels["loopzero.io/tenant"])
	}
}

// --- Scale-to-zero (KEDA) tests ---

// parseScaledObject extracts the first KEDA ScaledObject from rendered output.
func parseScaledObject(t *testing.T, rendered string) kedaScaledObject {
	t.Helper()
	docs := splitDocs(rendered)
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "ScaledObject" {
			var so kedaScaledObject
			if err := sigsyaml.Unmarshal([]byte(doc), &so); err != nil {
				t.Fatalf("failed to parse ScaledObject: %v", err)
			}
			return so
		}
	}
	t.Fatal("no ScaledObject found in rendered output")
	return kedaScaledObject{}
}

// hasScaledObject checks whether a KEDA ScaledObject exists in rendered output.
func hasScaledObject(rendered string) bool {
	docs := splitDocs(rendered)
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "ScaledObject" {
			return true
		}
	}
	return false
}

// parseHTTPScaledObject extracts the first KEDA HTTPScaledObject from rendered output.
func parseHTTPScaledObject(t *testing.T, rendered string) kedaHTTPScaledObject {
	t.Helper()
	docs := splitDocs(rendered)
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "HTTPScaledObject" {
			var hso kedaHTTPScaledObject
			if err := sigsyaml.Unmarshal([]byte(doc), &hso); err != nil {
				t.Fatalf("failed to parse HTTPScaledObject: %v", err)
			}
			return hso
		}
	}
	t.Fatal("no HTTPScaledObject found in rendered output")
	return kedaHTTPScaledObject{}
}

// hasHTTPScaledObject checks whether a KEDA HTTPScaledObject exists in rendered output.
func hasHTTPScaledObject(rendered string) bool {
	docs := splitDocs(rendered)
	for _, doc := range docs {
		var resource struct {
			Kind string `json:"kind"`
		}
		if err := sigsyaml.Unmarshal([]byte(doc), &resource); err != nil {
			continue
		}
		if resource.Kind == "HTTPScaledObject" {
			return true
		}
	}
	return false
}

func TestRenderDeployment_ScaleToZero_GeneratesScaledObject(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 0,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)

	if !hasScaledObject(result) {
		t.Fatal("expected ScaledObject to be generated when MinReplicas == 0")
	}
	if hasHPA(result) {
		t.Error("HPA should NOT be generated when MinReplicas == 0 (KEDA generates HPA internally)")
	}

	so := parseScaledObject(t, result)
	if so.APIVersion != "keda.sh/v1alpha1" {
		t.Errorf("ScaledObject apiVersion: got %q, want keda.sh/v1alpha1", so.APIVersion)
	}
	if so.Spec.MinReplicaCount != 0 {
		t.Errorf("ScaledObject minReplicaCount: got %d, want 0", so.Spec.MinReplicaCount)
	}
	if so.Spec.MaxReplicaCount != 5 {
		t.Errorf("ScaledObject maxReplicaCount: got %d, want 5", so.Spec.MaxReplicaCount)
	}
	if so.Spec.ScaleTargetRef.Name != "webapp-api" {
		t.Errorf("ScaledObject scaleTargetRef.name: got %q, want webapp-api (project-scoped)", so.Spec.ScaleTargetRef.Name)
	}
}

func TestRenderDeployment_ScaleToZero_GeneratesHTTPScaledObject(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 0,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)

	if !hasHTTPScaledObject(result) {
		t.Fatal("expected HTTPScaledObject for server app with MinReplicas == 0")
	}

	hso := parseHTTPScaledObject(t, result)
	if hso.APIVersion != "http.keda.sh/v1alpha1" {
		t.Errorf("HTTPScaledObject apiVersion: got %q, want http.keda.sh/v1alpha1", hso.APIVersion)
	}

	expectedHost := "acme-webapp-api-production.getloopscale.app.com"
	if len(hso.Spec.Hosts) == 0 || hso.Spec.Hosts[0] != expectedHost {
		t.Errorf("HTTPScaledObject hosts: got %v, want [%s]", hso.Spec.Hosts, expectedHost)
	}
	if hso.Spec.ScaleTargetRef.Name != "webapp-api" {
		t.Errorf("HTTPScaledObject scaleTargetRef.name: got %q, want webapp-api (project-scoped)", hso.Spec.ScaleTargetRef.Name)
	}
	if hso.Spec.ScaleTargetRef.Port != 8080 {
		t.Errorf("HTTPScaledObject scaleTargetRef.port: got %d, want 8080", hso.Spec.ScaleTargetRef.Port)
	}
	if hso.Spec.Replicas == nil {
		t.Fatal("HTTPScaledObject replicas should not be nil")
	}
	if hso.Spec.Replicas.Min != 0 {
		t.Errorf("HTTPScaledObject replicas.min: got %d, want 0", hso.Spec.Replicas.Min)
	}
	if hso.Spec.Replicas.Max != 5 {
		t.Errorf("HTTPScaledObject replicas.max: got %d, want 5", hso.Spec.Replicas.Max)
	}
}

func TestRenderDeployment_ScaleToZero_WorkerNoHTTPScaledObject(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "processor",
			Type:     "worker",
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 0,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)

	if !hasScaledObject(result) {
		t.Error("expected ScaledObject for worker with MinReplicas == 0")
	}
	if hasHTTPScaledObject(result) {
		t.Error("HTTPScaledObject should NOT be generated for workers (no HTTP traffic)")
	}
}

func TestRenderDeployment_ScaleToZero_DeploymentReplicasZero(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 0,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	dep := parseDeployment(t, mustRender(t, input))

	if dep.Spec.Replicas != 0 {
		t.Errorf("with scale-to-zero, deployment replicas should be 0: got %d", dep.Spec.Replicas)
	}
}

func TestRenderDeployment_ScaleToZero_ServerDocCount(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 0,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)
	docs := splitDocs(result)

	// Deployment + PDB + Service + Certificate + IngressRoute + ScaledObject + HTTPScaledObject = 7
	if len(docs) != 7 {
		t.Fatalf("expected 7 YAML documents for server with scale-to-zero, got %d", len(docs))
	}
}

func TestRenderDeployment_MinOne_StillGeneratesHPA(t *testing.T) {
	// Backward compatibility: min >= 1 still uses native HPA, not KEDA
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 1,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
		},
	}

	result, _ := Render(input)

	if !hasHPA(result) {
		t.Error("expected HPA for MinReplicas >= 1")
	}
	if hasScaledObject(result) {
		t.Error("ScaledObject should NOT be generated when MinReplicas >= 1")
	}
	if hasHTTPScaledObject(result) {
		t.Error("HTTPScaledObject should NOT be generated when MinReplicas >= 1")
	}
}

func TestRenderDeployment_ScaleToZero_CooldownDefault(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 0,
				MaxReplicas: 3,
				TargetCPU:   80,
				// CooldownPeriod: 0 → should default to 300
			},
		},
	}

	so := parseScaledObject(t, mustRender(t, input))

	if so.Spec.CooldownPeriod != 300 {
		t.Errorf("ScaledObject default cooldownPeriod: got %d, want 300", so.Spec.CooldownPeriod)
	}
}

func TestRenderDeployment_ScaleToZero_CooldownCustom(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas:    0,
				MaxReplicas:    3,
				TargetCPU:      80,
				CooldownPeriod: 600,
			},
		},
	}

	so := parseScaledObject(t, mustRender(t, input))

	if so.Spec.CooldownPeriod != 600 {
		t.Errorf("ScaledObject custom cooldownPeriod: got %d, want 600", so.Spec.CooldownPeriod)
	}
}

func TestRenderDeployment_ScaleToZero_CustomDomains(t *testing.T) {
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{
				MinReplicas: 0,
				MaxReplicas: 5,
				TargetCPU:   80,
			},
			CustomDomains: []CustomDomainInput{
				{Domain: "api.mycompany.com", ID: "dom-1", CFManaged: true},
			},
		},
	}

	hso := parseHTTPScaledObject(t, mustRender(t, input))

	expectedHost := "acme-webapp-api-production.getloopscale.app.com"
	customHost := "api.mycompany.com"

	if len(hso.Spec.Hosts) != 2 {
		t.Fatalf("HTTPScaledObject hosts: expected 2 (auto + custom), got %d: %v", len(hso.Spec.Hosts), hso.Spec.Hosts)
	}

	hostsSet := make(map[string]bool)
	for _, h := range hso.Spec.Hosts {
		hostsSet[h] = true
	}
	if !hostsSet[expectedHost] {
		t.Errorf("HTTPScaledObject missing auto hostname %q", expectedHost)
	}
	if !hostsSet[customHost] {
		t.Errorf("HTTPScaledObject missing custom domain %q", customHost)
	}
}
