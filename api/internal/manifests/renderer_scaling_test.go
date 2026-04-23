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

	dep := parseDeployment(t, Render(input))

	if dep.Spec.Replicas != 1 {
		t.Errorf("default replicas: got %d, want 1", dep.Spec.Replicas)
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

	dep := parseDeployment(t, Render(input))

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
				MinReplicas:  2,
				MaxReplicas:  5,
				TargetCPU:    75,
			},
		},
	}

	result := Render(input)

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
	if hpa.Metadata.Name != "api" {
		t.Errorf("HPA name: got %q, want api", hpa.Metadata.Name)
	}
	if hpa.Metadata.Namespace != "loopzero-acme-production" {
		t.Errorf("HPA namespace: got %q, want loopzero-acme-production", hpa.Metadata.Namespace)
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
	if hpa.Spec.ScaleTargetRef.Name != "api" {
		t.Errorf("HPA scaleTargetRef.name: got %q, want api", hpa.Spec.ScaleTargetRef.Name)
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

	result := Render(input)

	if hasHPA(result) {
		t.Error("HPA should not be generated when autoscaling is not configured")
	}
}

func TestRenderDeployment_HPA_DefaultValues(t *testing.T) {
	// Autoscaling with zero values should use defaults: min=1, max=3, cpu=80
	input := baseInput()
	input.Apps = []AppManifestInput{
		{
			Name:     "api",
			Type:     "server",
			Port:     8080,
			ImageRef: "reg:5000/acme/webapp:abc123",
			Autoscaling: &AutoscalingConfig{},
		},
	}

	result := Render(input)
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

	dep := parseDeployment(t, Render(input))

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

	result := Render(input)
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

	result := Render(input)
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

	result := Render(input)
	docs := splitDocs(result)

	// Deployment + PDB + HPA = 3
	if len(docs) != 3 {
		t.Fatalf("expected 3 YAML documents (Deployment, PDB, HPA), got %d", len(docs))
	}

	hpa := parseHPA(t, result)
	if hpa.Spec.ScaleTargetRef.Name != "processor" {
		t.Errorf("HPA scaleTargetRef.name: got %q, want processor", hpa.Spec.ScaleTargetRef.Name)
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

	hpa := parseHPA(t, Render(input))

	if hpa.Metadata.Labels["app"] != "api" {
		t.Errorf("HPA label 'app': got %q, want api", hpa.Metadata.Labels["app"])
	}
	if hpa.Metadata.Labels["loopzero.io/tenant"] != "acme" {
		t.Errorf("HPA label 'loopzero.io/tenant': got %q, want acme", hpa.Metadata.Labels["loopzero.io/tenant"])
	}
}
