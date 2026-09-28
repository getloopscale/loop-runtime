// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package manifests

import (
	"strings"
	"testing"
)

// argo-rollouts-progressive-delivery-plan T1.3 — Rollout rendering tests.

func rolloutBaseInput() RenderInput {
	return RenderInput{
		TenantID:            "acme",
		ProjectID:           "webapp",
		Environment:         "production",
		BuildID:             "build-1",
		ReleaseID:           "rel-1",
		BaseDomain:          "loopzero.local",
		ClusterIssuerName:   "loopzero-ca-issuer",
		ProgressiveDelivery: true,
		PrometheusAddress:   "http://prometheus.monitoring.svc.cluster.local:9090",
		Apps: []AppManifestInput{
			{Name: "api", Type: "server", Port: 3000, ImageRef: "k3d-loopzero-registry:5000/acme/webapp/api@sha256:abc"},
		},
	}
}

func TestBuildRollout_StarterPlan(t *testing.T) {
	input := rolloutBaseInput()
	input.Plan = "starter"
	rollout := buildRollout(scopeFor(t, input), input.Apps[0], 3000)

	if rollout.APIVersion != "argoproj.io/v1alpha1" {
		t.Errorf("APIVersion = %q, want argoproj.io/v1alpha1", rollout.APIVersion)
	}
	if rollout.Kind != "Rollout" {
		t.Errorf("Kind = %q, want Rollout", rollout.Kind)
	}
	if rollout.Spec.Strategy.Canary == nil {
		t.Fatal("Strategy.Canary is nil")
	}
	steps := rollout.Spec.Strategy.Canary.Steps
	if len(steps) != 2 {
		t.Errorf("starter steps = %d, want 2 (50%% setWeight + pause)", len(steps))
	}
	if steps[0].SetWeight == nil || *steps[0].SetWeight != 50 {
		t.Errorf("starter step 0 setWeight should be 50, got %v", steps[0].SetWeight)
	}
	if steps[1].Pause == nil {
		t.Errorf("starter step 1 should be pause")
	}
}

// TestBuildRollout_ProgressDeadlineSeconds verifies that EVERY canary
// Rollout — regardless of plan or fixture — declares a 5min deadline so
// readinessProbe-driven aborts (e.g., bad-health canary in T5.2) are
// detected within E2E test budgets and ops SLOs. Default 600s would
// blow through the 8min spec timeout and cause flakes.
func TestBuildRollout_ProgressDeadlineSeconds(t *testing.T) {
	cases := []struct{ name, plan string }{
		{"starter", "starter"},
		{"pro", "pro"},
		{"team", "team"},
		{"enterprise", "enterprise"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := rolloutBaseInput()
			input.Plan = tc.plan
			rollout := buildRollout(scopeFor(t, input), input.Apps[0], 3000)
			if rollout.Spec.ProgressDeadlineSeconds != 300 {
				t.Errorf("%s plan progressDeadlineSeconds = %d, want 300",
					tc.plan, rollout.Spec.ProgressDeadlineSeconds)
			}
		})
	}
}

func TestBuildRollout_ProPlanWithAnalysis(t *testing.T) {
	input := rolloutBaseInput()
	input.Plan = "pro"
	rollout := buildRollout(scopeFor(t, input), input.Apps[0], 3000)

	steps := rollout.Spec.Strategy.Canary.Steps
	if len(steps) != 5 {
		t.Errorf("pro steps = %d, want 5 (20%% / pause / Analysis / 50%% / pause)", len(steps))
	}
	// Step 0: setWeight 20
	if steps[0].SetWeight == nil || *steps[0].SetWeight != 20 {
		t.Errorf("pro step 0 setWeight should be 20, got %v", steps[0].SetWeight)
	}
	// Step 2: analysis
	if steps[2].Analysis == nil {
		t.Errorf("pro step 2 should be analysis (Prometheus configured)")
	} else if len(steps[2].Analysis.Templates) != 1 {
		t.Errorf("pro step 2 should reference 1 template, got %d", len(steps[2].Analysis.Templates))
	} else if steps[2].Analysis.Templates[0].TemplateName != "loopzero-canary-analysis" {
		t.Errorf("template name = %q, want loopzero-canary-analysis", steps[2].Analysis.Templates[0].TemplateName)
	}
	// Step 3: setWeight 50
	if steps[3].SetWeight == nil || *steps[3].SetWeight != 50 {
		t.Errorf("pro step 3 setWeight should be 50, got %v", steps[3].SetWeight)
	}
}

func TestBuildRollout_ProPlanNoAnalysis(t *testing.T) {
	input := rolloutBaseInput()
	input.Plan = "pro"
	input.PrometheusAddress = "" // Disables Analysis steps -> Pause fallback
	rollout := buildRollout(scopeFor(t, input), input.Apps[0], 3000)

	steps := rollout.Spec.Strategy.Canary.Steps
	if len(steps) != 5 {
		t.Errorf("pro steps with no analysis = %d, want 5 (20%%/pause/PAUSE/50%%/pause)", len(steps))
	}
	// Step 2 should be Pause now (analysis fallback), not Analysis
	if steps[2].Analysis != nil {
		t.Errorf("pro step 2 should NOT be Analysis when Prometheus disabled")
	}
	if steps[2].Pause == nil {
		t.Errorf("pro step 2 should be Pause when Prometheus disabled")
	}
}

func TestBuildRollout_TeamPlan(t *testing.T) {
	input := rolloutBaseInput()
	input.Plan = "team"
	rollout := buildRollout(scopeFor(t, input), input.Apps[0], 3000)

	steps := rollout.Spec.Strategy.Canary.Steps
	if len(steps) != 7 {
		t.Errorf("team steps = %d, want 7 (10/pause/Analysis/30/pause/60/pause)", len(steps))
	}
	weights := []int{10, 30, 60}
	indices := []int{0, 3, 5}
	for i, expected := range weights {
		idx := indices[i]
		if steps[idx].SetWeight == nil || *steps[idx].SetWeight != expected {
			t.Errorf("team step %d setWeight should be %d, got %v", idx, expected, steps[idx].SetWeight)
		}
	}
}

func TestBuildRollout_EmptyPlanDefaultsToStarter(t *testing.T) {
	input := rolloutBaseInput()
	input.Plan = ""
	rollout := buildRollout(scopeFor(t, input), input.Apps[0], 3000)
	steps := rollout.Spec.Strategy.Canary.Steps
	if len(steps) != 2 {
		t.Errorf("empty plan should default to starter (2 steps), got %d", len(steps))
	}
}

func TestBuildRollout_WorkerHasPauseOnlyNoTraffic(t *testing.T) {
	input := rolloutBaseInput()
	input.Apps = []AppManifestInput{
		{Name: "background", Type: "worker", ImageRef: "k3d-loopzero-registry:5000/acme/webapp/background@sha256:abc"},
	}
	rollout := buildRollout(scopeFor(t, input), input.Apps[0], 0)

	if rollout.Spec.Strategy.Canary.StableService != "" {
		t.Errorf("worker rollout should have empty StableService (no traffic), got %q", rollout.Spec.Strategy.Canary.StableService)
	}
	if rollout.Spec.Strategy.Canary.TrafficRouting != nil {
		t.Errorf("worker rollout should have nil TrafficRouting (no traffic split)")
	}
	steps := rollout.Spec.Strategy.Canary.Steps
	if len(steps) != 1 || steps[0].Pause == nil {
		t.Errorf("worker should have single Pause step, got %v", steps)
	}
}

func TestBuildStableAndCanaryServices(t *testing.T) {
	input := rolloutBaseInput()
	stable := buildStableService(scopeFor(t, input), input.Apps[0], 3000)
	canary := buildCanaryService(scopeFor(t, input), input.Apps[0], 3000)

	if !strings.HasSuffix(stable.Metadata.Name, "-stable") {
		t.Errorf("stable service name should end with -stable, got %q", stable.Metadata.Name)
	}
	if !strings.HasSuffix(canary.Metadata.Name, "-canary") {
		t.Errorf("canary service name should end with -canary, got %q", canary.Metadata.Name)
	}
	// Both should have same selector (Argo Rollouts injects pod-template-hash)
	if stable.Spec.Selector["app"] != canary.Spec.Selector["app"] {
		t.Errorf("stable+canary selectors should match (Argo Rollouts injects hash); stable=%v canary=%v",
			stable.Spec.Selector, canary.Spec.Selector)
	}
}

func TestBuildTraefikService_InitialWeights(t *testing.T) {
	input := rolloutBaseInput()
	ts := buildTraefikService(scopeFor(t, input), input.Apps[0], 3000)

	if ts.APIVersion != "traefik.io/v1alpha1" {
		t.Errorf("APIVersion = %q, want traefik.io/v1alpha1", ts.APIVersion)
	}
	if ts.Kind != "TraefikService" {
		t.Errorf("Kind = %q, want TraefikService", ts.Kind)
	}
	svcs := ts.Spec.Weighted.Services
	if len(svcs) != 2 {
		t.Fatalf("TraefikService should have 2 services, got %d", len(svcs))
	}
	// Order matters: stable first, canary second.
	if !strings.HasSuffix(svcs[0].Name, "-stable") || svcs[0].Weight != 100 {
		t.Errorf("first service should be -stable with weight 100, got %v", svcs[0])
	}
	if !strings.HasSuffix(svcs[1].Name, "-canary") || svcs[1].Weight != 0 {
		t.Errorf("second service should be -canary with weight 0, got %v", svcs[1])
	}
}

func TestBuildAnalysisTemplate_PrometheusQuery(t *testing.T) {
	input := rolloutBaseInput()
	input.PrometheusAddress = "http://prom.example:9090"
	at := buildAnalysisTemplate(scopeFor(t, input), input.Apps[0])

	if at.APIVersion != "argoproj.io/v1alpha1" {
		t.Errorf("APIVersion = %q, want argoproj.io/v1alpha1", at.APIVersion)
	}
	if at.Kind != "AnalysisTemplate" {
		t.Errorf("Kind = %q, want AnalysisTemplate", at.Kind)
	}
	if at.Metadata.Name != "loopzero-canary-analysis" {
		t.Errorf("name = %q, want loopzero-canary-analysis", at.Metadata.Name)
	}
	if len(at.Spec.Metrics) != 1 {
		t.Fatalf("should have 1 metric, got %d", len(at.Spec.Metrics))
	}
	m := at.Spec.Metrics[0]
	if m.Provider.Prometheus == nil {
		t.Fatal("Prometheus provider is nil")
	}
	if m.Provider.Prometheus.Address != "http://prom.example:9090" {
		t.Errorf("address = %q, want http://prom.example:9090", m.Provider.Prometheus.Address)
	}
	if !strings.Contains(m.Provider.Prometheus.Query, "traefik_service_requests_total") {
		t.Errorf("query should reference traefik_service_requests_total, got %q", m.Provider.Prometheus.Query)
	}
	// EC-3: NaN inconclusive
	if m.Inconclusive == "" {
		t.Errorf("EC-3: Inconclusive should be set (isNaN result[0])")
	}
	// EC-7: consecutiveErrorLimit
	if m.ConsecutiveErrorLimit < 1 {
		t.Errorf("EC-7: ConsecutiveErrorLimit should be > 0, got %d", m.ConsecutiveErrorLimit)
	}
}

func TestRenderServerApp_ProgressiveDelivery_RolloutAndServices(t *testing.T) {
	input := rolloutBaseInput()
	input.Plan = "pro"
	docs, err := renderServerApp(scopeFor(t, input), input.Apps[0])
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	combined := strings.Join(docs, "\n---\n")

	// Should contain Rollout (NOT Deployment)
	if !strings.Contains(combined, "kind: Rollout") {
		t.Errorf("output should contain kind: Rollout")
	}
	if strings.Contains(combined, "kind: Deployment") {
		t.Errorf("output should NOT contain kind: Deployment when ProgressiveDelivery=true")
	}
	// Should contain stable + canary services
	if !strings.Contains(combined, "-stable") {
		t.Errorf("output should contain -stable Service")
	}
	if !strings.Contains(combined, "-canary") {
		t.Errorf("output should contain -canary Service")
	}
	// TraefikService
	if !strings.Contains(combined, "kind: TraefikService") {
		t.Errorf("output should contain kind: TraefikService")
	}
	// AnalysisTemplate (Prometheus configured)
	if !strings.Contains(combined, "kind: AnalysisTemplate") {
		t.Errorf("output should contain kind: AnalysisTemplate when Prometheus configured")
	}
}

func TestRenderServerApp_LegacyDeployment_FlagFalse(t *testing.T) {
	input := rolloutBaseInput()
	input.ProgressiveDelivery = false
	docs, err := renderServerApp(scopeFor(t, input), input.Apps[0])
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	combined := strings.Join(docs, "\n---\n")

	if !strings.Contains(combined, "kind: Deployment") {
		t.Errorf("legacy path should contain kind: Deployment")
	}
	if strings.Contains(combined, "kind: Rollout") {
		t.Errorf("legacy path should NOT contain kind: Rollout when ProgressiveDelivery=false")
	}
	if strings.Contains(combined, "kind: TraefikService") {
		t.Errorf("legacy path should NOT contain kind: TraefikService")
	}
	if strings.Contains(combined, "kind: AnalysisTemplate") {
		t.Errorf("legacy path should NOT contain kind: AnalysisTemplate")
	}
}

func TestRenderWorkerApp_ProgressiveDelivery_RolloutNoServices(t *testing.T) {
	input := rolloutBaseInput()
	input.Apps = []AppManifestInput{
		{Name: "background", Type: "worker", ImageRef: "k3d-loopzero-registry:5000/acme/webapp/background@sha256:abc"},
	}
	docs, err := renderWorkerApp(scopeFor(t, input), input.Apps[0])
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	combined := strings.Join(docs, "\n---\n")

	if !strings.Contains(combined, "kind: Rollout") {
		t.Errorf("worker progressive should contain Rollout")
	}
	// Worker should NOT have any Service/TraefikService/AnalysisTemplate
	if strings.Contains(combined, "-stable") || strings.Contains(combined, "-canary") {
		t.Errorf("worker should NOT have -stable / -canary services")
	}
	if strings.Contains(combined, "kind: TraefikService") {
		t.Errorf("worker should NOT have TraefikService")
	}
	if strings.Contains(combined, "kind: AnalysisTemplate") {
		t.Errorf("worker should NOT have AnalysisTemplate (no traffic to analyze)")
	}
}

func TestIngressRoute_PointsToTraefikService_Progressive(t *testing.T) {
	input := rolloutBaseInput()
	docs, err := buildIngressResources(scopeFor(t, input), input.Apps[0], 3000)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	combined := strings.Join(docs, "\n---\n")

	// IngressRoute should have services entry with kind: TraefikService
	if !strings.Contains(combined, "kind: TraefikService") {
		t.Errorf("IngressRoute services should reference Kind: TraefikService when ProgressiveDelivery=true")
	}
}

func TestIngressRoute_PointsToService_Legacy(t *testing.T) {
	input := rolloutBaseInput()
	input.ProgressiveDelivery = false
	docs, err := buildIngressResources(scopeFor(t, input), input.Apps[0], 3000)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	combined := strings.Join(docs, "\n---\n")

	// Legacy path should NOT mention TraefikService kind in services
	// (the IngressRoute itself is part of traefik.io/v1alpha1 but services entries
	// shouldn't have explicit kind)
	if strings.Count(combined, "kind: TraefikService") > 0 {
		t.Errorf("Legacy IngressRoute services should not have Kind: TraefikService")
	}
}

// argo-rollouts-progressive-delivery-plan T3.1 — KEDA + HPA target Rollout

func TestBuildHPA_TargetsRollout_Progressive(t *testing.T) {
	input := rolloutBaseInput()
	app := AppManifestInput{
		Name:        "api",
		Type:        "server",
		Port:        3000,
		ImageRef:    "k3d-loopzero-registry:5000/acme/webapp/api@sha256:abc",
		Autoscaling: &AutoscalingConfig{MinReplicas: 1, MaxReplicas: 5, TargetCPU: 70},
	}
	hpa := buildHPA(scopeFor(t, input), app)
	if hpa.Spec.ScaleTargetRef.Kind != "Rollout" {
		t.Errorf("HPA Kind = %q, want Rollout when ProgressiveDelivery=true", hpa.Spec.ScaleTargetRef.Kind)
	}
	if hpa.Spec.ScaleTargetRef.APIVersion != "argoproj.io/v1alpha1" {
		t.Errorf("HPA APIVersion = %q, want argoproj.io/v1alpha1", hpa.Spec.ScaleTargetRef.APIVersion)
	}
}

func TestBuildHPA_TargetsDeployment_Legacy(t *testing.T) {
	input := rolloutBaseInput()
	input.ProgressiveDelivery = false
	app := AppManifestInput{
		Name:        "api",
		Type:        "server",
		Port:        3000,
		ImageRef:    "k3d-loopzero-registry:5000/acme/webapp/api@sha256:abc",
		Autoscaling: &AutoscalingConfig{MinReplicas: 1, MaxReplicas: 5, TargetCPU: 70},
	}
	hpa := buildHPA(scopeFor(t, input), app)
	if hpa.Spec.ScaleTargetRef.Kind != "Deployment" {
		t.Errorf("HPA Kind = %q, want Deployment when ProgressiveDelivery=false", hpa.Spec.ScaleTargetRef.Kind)
	}
}

func TestBuildScaledObject_TargetsRollout_Progressive(t *testing.T) {
	input := rolloutBaseInput()
	app := AppManifestInput{
		Name:        "api",
		Type:        "server",
		Port:        3000,
		ImageRef:    "k3d-loopzero-registry:5000/acme/webapp/api@sha256:abc",
		Autoscaling: &AutoscalingConfig{MinReplicas: 0, MaxReplicas: 5, TargetCPU: 70},
	}
	so := buildScaledObject(scopeFor(t, input), app)
	if so.Spec.ScaleTargetRef.Kind != "Rollout" {
		t.Errorf("ScaledObject Kind = %q, want Rollout", so.Spec.ScaleTargetRef.Kind)
	}
	if so.Spec.ScaleTargetRef.APIVersion != "argoproj.io/v1alpha1" {
		t.Errorf("ScaledObject APIVersion = %q, want argoproj.io/v1alpha1", so.Spec.ScaleTargetRef.APIVersion)
	}
	// EC-5: transfer-hpa-ownership annotation
	if so.Metadata.Annotations["scaledobject.keda.sh/transfer-hpa-ownership"] != "true" {
		t.Errorf("EC-5: ScaledObject should have transfer-hpa-ownership annotation when progressive delivery on")
	}
}

func TestBuildScaledObject_LegacyDeployment(t *testing.T) {
	input := rolloutBaseInput()
	input.ProgressiveDelivery = false
	app := AppManifestInput{
		Name:        "api",
		Type:        "server",
		Port:        3000,
		ImageRef:    "k3d-loopzero-registry:5000/acme/webapp/api@sha256:abc",
		Autoscaling: &AutoscalingConfig{MinReplicas: 0, MaxReplicas: 5, TargetCPU: 70},
	}
	so := buildScaledObject(scopeFor(t, input), app)
	// Legacy: Kind defaults to empty (KEDA infers Deployment)
	if so.Spec.ScaleTargetRef.Kind != "" {
		t.Errorf("Legacy ScaledObject Kind should be empty (KEDA defaults Deployment), got %q", so.Spec.ScaleTargetRef.Kind)
	}
	if _, hasAnnotation := so.Metadata.Annotations["scaledobject.keda.sh/transfer-hpa-ownership"]; hasAnnotation {
		t.Errorf("Legacy ScaledObject should NOT have transfer-hpa-ownership annotation")
	}
}

func TestBuildHTTPScaledObject_PointsToStableService_Progressive(t *testing.T) {
	input := rolloutBaseInput()
	app := AppManifestInput{
		Name:        "api",
		Type:        "server",
		Port:        3000,
		ImageRef:    "k3d-loopzero-registry:5000/acme/webapp/api@sha256:abc",
		Autoscaling: &AutoscalingConfig{MinReplicas: 0, MaxReplicas: 3},
	}
	httpSO := buildHTTPScaledObject(scopeFor(t, input), app)
	expected := resourceName(input.ProjectID, app.Name) + "-stable"
	if httpSO.Spec.ScaleTargetRef.Service != expected {
		t.Errorf("HTTPScaledObject Service = %q, want %q (-stable suffix when progressive delivery on)",
			httpSO.Spec.ScaleTargetRef.Service, expected)
	}
}

func TestBuildHTTPScaledObject_PointsToServiceLegacy(t *testing.T) {
	input := rolloutBaseInput()
	input.ProgressiveDelivery = false
	app := AppManifestInput{
		Name:        "api",
		Type:        "server",
		Port:        3000,
		ImageRef:    "k3d-loopzero-registry:5000/acme/webapp/api@sha256:abc",
		Autoscaling: &AutoscalingConfig{MinReplicas: 0, MaxReplicas: 3},
	}
	httpSO := buildHTTPScaledObject(scopeFor(t, input), app)
	expected := resourceName(input.ProjectID, app.Name)
	if httpSO.Spec.ScaleTargetRef.Service != expected {
		t.Errorf("Legacy HTTPScaledObject Service = %q, want %q", httpSO.Spec.ScaleTargetRef.Service, expected)
	}
}
