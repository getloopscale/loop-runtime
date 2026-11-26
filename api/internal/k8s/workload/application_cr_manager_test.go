// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package workload

import (
	"context"
	"strings"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/pkg/naming"
)

// newAppMgr builds an ApplicationCRManager backed by the controller-runtime
// fake typed client (M8). UNIT test — no live cluster, no envtest.
func newAppMgr(t *testing.T) *ApplicationCRManager {
	t.Helper()
	scheme, err := k8s.NewScheme()
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	return NewApplicationCRManager(fake.NewClientBuilder().WithScheme(scheme).Build(), "loopruntime-system")
}

// getApp fetches a LoopRuntimeApplication CR by name from the manager's fake client.
func getApp(t *testing.T, m *ApplicationCRManager, name string) *apiv1.LoopRuntimeApplication {
	t.Helper()
	var got apiv1.LoopRuntimeApplication
	if err := m.Client.Get(context.Background(), client.ObjectKey{Namespace: "loopruntime-system", Name: name}, &got); err != nil {
		t.Fatalf("expected LoopRuntimeApplication CR %q to exist: %v", name, err)
	}
	return &got
}

func TestBuildLoopRuntimeApplicationCR(t *testing.T) {
	cr := buildLoopRuntimeApplicationCR("loopruntime-system", LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 3001,
	})

	// Deterministic name includes hash suffix + human-readable prefix.
	expectedName := loopruntimeApplicationCRName("tn-loopruntime", "my-project", "api", "production")
	if cr.Name != expectedName {
		t.Errorf("expected name %q, got %q", expectedName, cr.Name)
	}
	if !strings.HasPrefix(cr.Name, "tn-loopruntime-my-project-api-production-") {
		t.Errorf("expected name to start with human-readable prefix, got %q", cr.Name)
	}
	if cr.Namespace != "loopruntime-system" {
		t.Errorf("expected namespace loopruntime-system, got %s", cr.Namespace)
	}

	// Typed spec fields — the compiler now enforces these names.
	if cr.Spec.TenantRef != "tn-loopruntime" {
		t.Errorf("expected tenantRef tn-loopruntime, got %q", cr.Spec.TenantRef)
	}
	if cr.Spec.Project != "my-project" {
		t.Errorf("expected project my-project, got %q", cr.Spec.Project)
	}
	if cr.Spec.Environment != "production" {
		t.Errorf("expected environment production, got %q", cr.Spec.Environment)
	}
	if cr.Spec.ServiceName != "api" {
		t.Errorf("expected serviceName api, got %q", cr.Spec.ServiceName)
	}
	if cr.Spec.ServicePort != 3001 {
		t.Errorf("expected servicePort 3001, got %d", cr.Spec.ServicePort)
	}
	if cr.Spec.Replicas != 1 {
		t.Errorf("expected replicas 1, got %d", cr.Spec.Replicas)
	}

	// Labels for controller selection.
	if cr.Labels["runtime.getloopscale.com/tenant"] != "tn-loopruntime" {
		t.Errorf("expected label runtime.getloopscale.com/tenant=tn-loopruntime, got %v", cr.Labels["runtime.getloopscale.com/tenant"])
	}
	if cr.Labels["runtime.getloopscale.com/project"] != "my-project" {
		t.Errorf("expected label runtime.getloopscale.com/project=my-project, got %v", cr.Labels["runtime.getloopscale.com/project"])
	}
}

// Audit gap M08 #10 / §6.C.1 regression: when the caller supplies a
// CorrelationID, the builder must emit it as the `runtime.getloopscale.com/correlation-id`
// metadata annotation. The operator reconcile reads this back into the
// reconcile ctx so audit_log rows + structured logs trace back to the
// originating HTTP request.
func TestBuildLoopRuntimeApplicationCR_PropagatesCorrelationID(t *testing.T) {
	const wantID = "11111111-2222-3333-4444-555555555555"
	cr := buildLoopRuntimeApplicationCR("loopruntime-system", LoopRuntimeApplicationInput{
		TenantID:      "tn-loopruntime",
		Project:       "my-project",
		Environment:   "production",
		AppName:       "api",
		ServiceName:   "api",
		ServicePort:   3001,
		CorrelationID: wantID,
	})

	if cr.Annotations == nil {
		t.Fatal("expected annotations map populated when CorrelationID is supplied")
	}
	got, ok := cr.Annotations["runtime.getloopscale.com/correlation-id"]
	if !ok {
		t.Errorf("expected annotation runtime.getloopscale.com/correlation-id to be set; got keys=%v", cr.Annotations)
	}
	if got != wantID {
		t.Errorf("annotation runtime.getloopscale.com/correlation-id = %q, want %q", got, wantID)
	}
}

// Inverse: an empty CorrelationID must NOT emit the annotation. Storing
// "runtime.getloopscale.com/correlation-id: ”" would pollute the CR + confuse operator-side
// readers that interpret presence-of-annotation as "I have a trace context".
func TestBuildLoopRuntimeApplicationCR_OmitsAnnotationWhenCorrelationEmpty(t *testing.T) {
	cr := buildLoopRuntimeApplicationCR("loopruntime-system", LoopRuntimeApplicationInput{
		TenantID:      "tn-loopruntime",
		Project:       "my-project",
		Environment:   "production",
		AppName:       "api",
		ServiceName:   "api",
		ServicePort:   3001,
		CorrelationID: "", // explicit empty
	})

	if _, has := cr.Annotations["runtime.getloopscale.com/correlation-id"]; has {
		t.Errorf("expected NO runtime.getloopscale.com/correlation-id annotation when CorrelationID is empty; got annotations=%v", cr.Annotations)
	}
}

func TestBuildLoopRuntimeApplicationCR_DefaultPort(t *testing.T) {
	cr := buildLoopRuntimeApplicationCR("loopruntime-system", LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "web-app",
		Environment: "staging",
		AppName:     "web",
		ServiceName: "web",
		ServicePort: 0, // should default to 80
	})

	if cr.Spec.ServicePort != 80 {
		t.Errorf("expected default servicePort 80, got %d", cr.Spec.ServicePort)
	}
}

func TestLoopRuntimeApplicationCRName_LongInputsTruncated(t *testing.T) {
	// K8s metadata.name max is 253 chars. With very long inputs, the name
	// must be truncated while preserving the 8-char hash suffix for uniqueness.
	longTenant := "tn-" + strings.Repeat("a", 60)
	longProject := strings.Repeat("b", 60)
	longApp := strings.Repeat("c", 60)
	longEnv := strings.Repeat("d", 60)

	name := loopruntimeApplicationCRName(longTenant, longProject, longApp, longEnv)
	if len(name) > 253 {
		t.Errorf("CR name exceeds K8s 253-char limit: len=%d, name=%q", len(name), name)
	}
	parts := strings.Split(name, "-")
	suffix := parts[len(parts)-1]
	if len(suffix) != 8 {
		t.Errorf("expected 8-char hash suffix after truncation, got %q (len=%d)", suffix, len(suffix))
	}
}

func TestLoopRuntimeApplicationCRName_TruncatedStillUnique(t *testing.T) {
	// Two inputs that share a long prefix but differ at the end must produce
	// different names even after truncation (the hash is computed on full input).
	base := strings.Repeat("x", 60)
	nameA := loopruntimeApplicationCRName("tn-org", base+"alpha", "api", "prod")
	nameB := loopruntimeApplicationCRName("tn-org", base+"bravo", "api", "prod")
	if nameA == nameB {
		t.Errorf("truncated names must differ for different inputs, both got %q", nameA)
	}
}

func TestLoopRuntimeApplicationCRName(t *testing.T) {
	// Deterministic: same input always produces same output.
	name1 := loopruntimeApplicationCRName("tn-loopruntime", "my-project", "api", "production")
	name2 := loopruntimeApplicationCRName("tn-loopruntime", "my-project", "api", "production")
	if name1 != name2 {
		t.Errorf("expected deterministic names, got %q and %q", name1, name2)
	}

	// Collision-free: different inputs that would collide with simple hyphen join.
	nameA := loopruntimeApplicationCRName("tn-x", "my-app", "web", "prod")
	nameB := loopruntimeApplicationCRName("tn-x", "my", "app-web", "prod")
	if nameA == nameB {
		t.Errorf("expected different names for different inputs, both got %q", nameA)
	}

	if !strings.HasPrefix(name1, "tn-loopruntime-my-project-api-production-") {
		t.Errorf("expected human-readable prefix, got %q", name1)
	}
	parts := strings.Split(name1, "-")
	suffix := parts[len(parts)-1]
	if len(suffix) != 8 {
		t.Errorf("expected 8-char hash suffix, got %q (len=%d)", suffix, len(suffix))
	}
}

func TestEnsureLoopRuntimeApplication_CreateWhenNotFound(t *testing.T) {
	mgr := newAppMgr(t)
	input := LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 3001,
	}

	if err := mgr.EnsureLoopRuntimeApplication(context.Background(), input); err != nil {
		t.Fatalf("EnsureLoopRuntimeApplication() unexpected error: %v", err)
	}

	crName := loopruntimeApplicationCRName("tn-loopruntime", "my-project", "api", "production")
	got := getApp(t, mgr, crName)
	if got.Spec.ServicePort != 3001 {
		t.Errorf("servicePort = %d, want 3001", got.Spec.ServicePort)
	}
	if got.Spec.TenantRef != "tn-loopruntime" {
		t.Errorf("tenantRef = %q, want tn-loopruntime", got.Spec.TenantRef)
	}
}

func TestEnsureLoopRuntimeApplication_UpdateWhenExists(t *testing.T) {
	mgr := newAppMgr(t)
	// Pre-populate with a CR that has port 80.
	if err := mgr.Client.Create(context.Background(), buildLoopRuntimeApplicationCR("loopruntime-system", LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 80,
	})); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	// Update to port 3001.
	if err := mgr.EnsureLoopRuntimeApplication(context.Background(), LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 3001,
	}); err != nil {
		t.Fatalf("EnsureLoopRuntimeApplication() unexpected error: %v", err)
	}

	crName := loopruntimeApplicationCRName("tn-loopruntime", "my-project", "api", "production")
	got := getApp(t, mgr, crName)
	if got.Spec.ServicePort != 3001 {
		t.Errorf("servicePort after update = %d, want 3001", got.Spec.ServicePort)
	}
}

// Audit gap M08 #10 / §6.C.1: on re-deploy the CR's correlation-id annotation
// must reflect the LATEST originating request (parity with the pre-M8
// rebuild-on-update behavior) — without clobbering other metadata.
func TestEnsureLoopRuntimeApplication_RefreshesCorrelationIDOnUpdate(t *testing.T) {
	mgr := newAppMgr(t)
	base := LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "my-project",
		Environment: "production",
		AppName:     "api",
		ServiceName: "api",
		ServicePort: 3001,
	}
	// First deploy carries correlation A.
	first := base
	first.CorrelationID = "corr-aaaa"
	if err := mgr.EnsureLoopRuntimeApplication(context.Background(), first); err != nil {
		t.Fatalf("first EnsureLoopRuntimeApplication: %v", err)
	}

	// Re-deploy carries correlation B — the annotation must update to B.
	second := base
	second.CorrelationID = "corr-bbbb"
	if err := mgr.EnsureLoopRuntimeApplication(context.Background(), second); err != nil {
		t.Fatalf("second EnsureLoopRuntimeApplication: %v", err)
	}

	crName := loopruntimeApplicationCRName("tn-loopruntime", "my-project", "api", "production")
	got := getApp(t, mgr, crName)
	if got.Annotations["runtime.getloopscale.com/correlation-id"] != "corr-bbbb" {
		t.Errorf("correlation-id after re-deploy = %q, want corr-bbbb (latest request)", got.Annotations["runtime.getloopscale.com/correlation-id"])
	}
}

func TestEnsureLoopRuntimeApplication_IdempotentCreate(t *testing.T) {
	mgr := newAppMgr(t)
	input := LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "web",
		Environment: "staging",
		AppName:     "frontend",
		ServiceName: "frontend",
		ServicePort: 80,
	}

	if err := mgr.EnsureLoopRuntimeApplication(context.Background(), input); err != nil {
		t.Fatalf("first call: %v", err)
	}
	if err := mgr.EnsureLoopRuntimeApplication(context.Background(), input); err != nil {
		t.Fatalf("second call (idempotent): %v", err)
	}

	// Verify exactly one CR exists.
	var list apiv1.LoopRuntimeApplicationList
	if err := mgr.Client.List(context.Background(), &list, client.InNamespace("loopruntime-system")); err != nil {
		t.Fatalf("List CRs: %v", err)
	}
	if len(list.Items) != 1 {
		t.Errorf("expected exactly 1 CR, found %d", len(list.Items))
	}
}

// The operator composes the routed hostname (pkg/naming.AppHostname) from
// spec.appName, so the api must write the app name into the spec — not only
// into the label.
func TestBuildLoopRuntimeApplicationCR_WritesTheAppNameIntoTheSpec(t *testing.T) {
	cr := buildLoopRuntimeApplicationCR("loopruntime-system", LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "my-project",
		Environment: "production",
		AppName:     "web",
		ServiceName: "web-svc",
	})
	if cr.Spec.AppName != "web" {
		t.Errorf("spec.appName = %q, want web", cr.Spec.AppName)
	}
	if cr.Labels[naming.LabelApp] != "web" {
		t.Errorf("label %s = %q, want web", naming.LabelApp, cr.Labels[naming.LabelApp])
	}
}

// A CR created before spec.appName existed gains it on the next deploy.
func TestEnsureLoopRuntimeApplication_BackfillsTheAppNameOnAnExistingCR(t *testing.T) {
	mgr := newAppMgr(t)
	input := LoopRuntimeApplicationInput{
		TenantID:    "tn-loopruntime",
		Project:     "my-project",
		Environment: "production",
		AppName:     "web",
		ServiceName: "web",
	}
	legacy := buildLoopRuntimeApplicationCR("loopruntime-system", input)
	legacy.Spec.AppName = ""
	if err := mgr.Client.Create(context.Background(), legacy); err != nil {
		t.Fatalf("seed create: %v", err)
	}

	if err := mgr.EnsureLoopRuntimeApplication(context.Background(), input); err != nil {
		t.Fatalf("EnsureLoopRuntimeApplication() unexpected error: %v", err)
	}

	got := getApp(t, mgr, loopruntimeApplicationCRName("tn-loopruntime", "my-project", "web", "production"))
	if got.Spec.AppName != "web" {
		t.Errorf("spec.appName after re-deploy = %q, want web", got.Spec.AppName)
	}
}
