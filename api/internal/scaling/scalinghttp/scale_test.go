// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package scalinghttp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/routestest"
	"github.com/getloopscale/loopzero/pkg/enforcement"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// fakePlanTierStore satisfies ports.PlanTierStore so the handler's
// ResolvePlanTier call can be exercised without a real DB.
//
// Duplicated from the parent's projects_test.go rather than exported from it:
// DRY governs knowledge, not lines, and a package that owns its own fixtures can
// be read, moved and deleted on its own. Sharing it would re-create the
// parent→family edge the routes fence forbids, in the direction that looks
// harmless.
type fakePlanTierStore struct {
	plan string
	err  error
}

func (f fakePlanTierStore) GetTenantPlan(_ context.Context, _ string) (string, error) {
	return f.plan, f.err
}

// --- Fake ScaleProjectStore ---

type fakeScaleProjectStore struct {
	project *ports.ProjectResult
}

func (f *fakeScaleProjectStore) GetByName(_ context.Context, _, name string) (*ports.ProjectResult, error) {
	if f.project != nil && f.project.Name == name {
		return f.project, nil
	}
	return nil, nil
}

// --- Fake PlanTierStore for J-20 T1.1 EC-3 absorption tests ---

type fakeScalePlanStore struct {
	raw string
	err error
}

func (s fakeScalePlanStore) GetTenantPlan(_ context.Context, _ string) (string, error) {
	return s.raw, s.err
}

// scaleHandlerWithPlan wires a fake plan-tier store onto the handler so a
// test can simulate a Pro/Team/Enterprise tenant. Starter is the default
// when no override is supplied.
func scaleHandlerWithPlan(projects ScaleProjectStore, apply ScaleApplyFunc, kedaEnabled bool, planRaw string) *ScaleHandler {
	h := NewScaleHandler(projects, apply, kedaEnabled)
	h.SetPlanTierStore(fakeScalePlanStore{raw: planRaw})
	return h
}

// --- Tests ---

func TestScale_HappyPath_Autoscaling(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	// J-20 v1.1 T1.1 EC-3 absorption: scale-to-zero (min=0) requires plan
	// tier ≥ Pro. Inject pro to keep the happy path on the canonical
	// scale-to-zero scenario; the dedicated rejection path is covered by
	// TestScale_KEDARequires_Pro below.
	h := scaleHandlerWithPlan(projects, nil, true, "pro") // KEDA enabled + Pro tier

	body := `{"environment":"production","app":"api","min":0,"max":5,"cpu":80}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp scaleResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.App != "api" {
		t.Errorf("want app=api, got %s", resp.App)
	}
	if resp.Status != "acknowledged" {
		t.Errorf("want status=acknowledged, got %s", resp.Status)
	}
	if resp.Autoscaling == nil {
		t.Fatal("expected autoscaling in response")
	}
	if resp.Autoscaling.Min != 0 {
		t.Errorf("want min=0, got %d", resp.Autoscaling.Min)
	}
	if resp.Autoscaling.Max != 5 {
		t.Errorf("want max=5, got %d", resp.Autoscaling.Max)
	}
	if resp.Autoscaling.CPUTarget != 80 {
		t.Errorf("want cpu_target=80, got %d", resp.Autoscaling.CPUTarget)
	}
	if resp.Autoscaling.Cooldown == nil || *resp.Autoscaling.Cooldown != 300 {
		t.Errorf("want cooldown=300 for scale-to-zero, got %v", resp.Autoscaling.Cooldown)
	}
}

func TestScale_HappyPath_FixedReplicas(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	replicas := 3
	body, _ := json.Marshal(scaleRequest{Environment: "production", App: "api", Replicas: &replicas})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBuffer(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Replicas == nil || *resp.Replicas != 3 {
		t.Errorf("want replicas=3, got %v", resp.Replicas)
	}
	if resp.Autoscaling != nil {
		t.Error("expected no autoscaling for fixed replicas")
	}
}

func TestScale_MutualExclusion(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	replicas := 3
	min := 1
	body, _ := json.Marshal(scaleRequest{App: "api", Replicas: &replicas, Min: &min})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBuffer(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_MinZeroWithoutKEDA(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	// J-20 v1.1 T1.1: tier check passes (Pro), but cluster KEDA disabled.
	// Two-layer defense — Pro tenant on a cluster without KEDA still
	// rejected at the cluster-feature gate (after tier gate passes).
	h := scaleHandlerWithPlan(projects, nil, false, "pro") // Pro + KEDA disabled at cluster

	min := 0
	max := 5
	body, _ := json.Marshal(scaleRequest{App: "api", Min: &min, Max: &max})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBuffer(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400 (KEDA disabled), got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_Unauthenticated(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{}, nil, false)

	body := `{"app":"api","min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", w.Code)
	}
}

func TestScale_ProjectNotFound(t *testing.T) {
	projects := &fakeScaleProjectStore{project: nil}
	h := NewScaleHandler(projects, nil, false)

	body := `{"app":"api","min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/nonexistent/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "nonexistent")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_MissingApp(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	body := `{"min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_NoScalingParams(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	body := `{"app":"api"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScale_DefaultEnvironment(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := NewScaleHandler(projects, nil, false)

	body := `{"app":"api","min":1,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Environment != "production" {
		t.Errorf("want default environment=production, got %s", resp.Environment)
	}
}

// Plan T2.2 — tests for ScaleApplyFunc real-persistence path (200 not 202).

func TestScale_RealPersistence_Applied(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	var captured ScaleConfigInput
	apply := func(_ context.Context, c ScaleConfigInput) (bool, error) {
		captured = c
		return true, nil
	}
	h := NewScaleHandler(projects, apply, true)
	h.SetDeliverer(&fakeScaleDeliverer{delivered: true})

	body := `{"environment":"production","app":"api","replicas":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d body=%s", w.Code, w.Body.String())
	}
	if captured.AppName != "api" {
		t.Errorf("captured app=%q want api", captured.AppName)
	}
	if captured.MinReplicas != 3 || captured.MaxReplicas != 3 {
		t.Errorf("captured min=%d max=%d want 3,3", captured.MinReplicas, captured.MaxReplicas)
	}

	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "applied" {
		t.Errorf("want status=applied, got %q", resp.Status)
	}
}

func TestScale_RealPersistence_NoChange(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	apply := func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return false, nil
	}
	h := NewScaleHandler(projects, apply, true)
	h.SetDeliverer(&fakeScaleDeliverer{delivered: true})

	body := `{"environment":"production","app":"api","replicas":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", w.Code)
	}
	var resp scaleResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != "no_change" {
		t.Errorf("want status=no_change, got %q", resp.Status)
	}
}

// Audit gap M02 #14: production boot must panic if scaleHandler is
// constructed without a real ScaleApplyFunc. Tests intentionally pass
// nil and must NEVER call EnsureProductionWired — this test asserts
// the guard works.
func TestScaleHandler_EnsureProductionWired_PanicsOnNilApply(t *testing.T) {
	projects := &fakeScaleProjectStore{}
	h := NewScaleHandler(projects, nil, false)

	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("EnsureProductionWired must panic on nil apply; got no panic")
		}
		msg, ok := r.(string)
		if !ok {
			t.Fatalf("panic payload not string: %T = %v", r, r)
		}
		// Specific substring so log search can correlate.
		if !strings.Contains(msg, "audit M02 #14") {
			t.Errorf("panic message missing audit ref; got %q", msg)
		}
	}()

	h.EnsureProductionWired()
}

// Inverse: a handler wired with a real apply MUST NOT panic.
func TestScaleHandler_EnsureProductionWired_NoPanicOnRealApply(t *testing.T) {
	projects := &fakeScaleProjectStore{}
	apply := func(_ context.Context, _ ScaleConfigInput) (bool, error) { return false, nil }
	h := NewScaleHandler(projects, apply, false)
	// Issue #137 — "production-wired" now means BOTH the apply closure and the
	// replica-cap checker. This test's model of production was as incomplete as
	// the guard it exercises; wiring only apply no longer represents a
	// production handler.
	h.SetReplicasChecker(&fakeScaleReplicasChecker{cap: 2, plan: "starter"})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("EnsureProductionWired must NOT panic when apply AND replicasChecker are wired; got panic=%v", r)
		}
	}()

	h.EnsureProductionWired()
}

// --- J-20 Scale plan v1.1 T1.1 absorption tests ---

// EC-3 absorption: Starter tenant requesting scale-to-zero MUST be rejected
// at the handler boundary with HTTP 402 + PLAN_UPGRADE_REQUIRED code so the
// dashboard / CLI surfaces the canonical upgrade CTA. Free user bypassing
// the client UI via raw API lands here, not in the cluster-KEDA branch.
func TestScale_KEDARequires_Pro_ReturnsUpgradePrompt(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	// Cluster has KEDA installed, but tenant is Starter — must be rejected
	// on tier check BEFORE cluster check (deterministic order: tier first
	// because upgrade is the actionable user surface).
	h := scaleHandlerWithPlan(projects, nil, true, "starter")

	body := `{"app":"api","min":0,"max":5}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusPaymentRequired {
		t.Fatalf("want 402, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "PLAN_UPGRADE_REQUIRED") {
		t.Errorf("response missing PLAN_UPGRADE_REQUIRED code; got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Pro") {
		t.Errorf("response missing Pro tier reference; got %s", w.Body.String())
	}
}

// EC-4 absorption: HPA target below min bound (CPU=1%) is rejected with
// HTTP 400 + HPA_TARGET_OUT_OF_BOUNDS code + field=cpu_target.
func TestScale_HPATargetBelowMin_Returns400(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := scaleHandlerWithPlan(projects, nil, false, "pro")

	body := `{"app":"api","min":1,"max":5,"cpu":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "HPA_TARGET_OUT_OF_BOUNDS") {
		t.Errorf("response missing HPA_TARGET_OUT_OF_BOUNDS code; got %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "cpu_target") {
		t.Errorf("response missing cpu_target field reference; got %s", w.Body.String())
	}
}

// EC-4 absorption: HPA memory target above max bound (memory=99%) is
// rejected with HTTP 400 + field=memory_target.
func TestScale_HPAMemoryAboveMax_Returns400(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := scaleHandlerWithPlan(projects, nil, false, "pro")

	body := `{"app":"api","min":1,"max":5,"memory":99}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "memory_target") {
		t.Errorf("response missing memory_target field reference; got %s", w.Body.String())
	}
}

// EC-2 absorption: server-authoritative monthly_cost_usd MUST be present in
// every successful response so the dashboard can reconcile its preview
// against the canonical computed value. plan_tier echoes the resolved
// tier for downstream CTA (eligible/upgrade) rendering.
func TestScale_ResponseCarriesMonthlyCostAndPlanTier(t *testing.T) {
	projects := &fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "my-api", TeamID: "tn-test-123"},
	}
	h := scaleHandlerWithPlan(projects, nil, false, "pro")

	body := `{"app":"api","replicas":3}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/my-api/scale", bytes.NewBufferString(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "my-api")
	w := httptest.NewRecorder()

	h.Scale(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("want 202, got %d body=%s", w.Code, w.Body.String())
	}

	var resp scaleResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// 3 replicas * $0.05/hr * 730hr/mo = $109.50/mo
	want := 3.0 * enforcement.DefaultPodCostPerHour * 730.0
	if resp.MonthlyCostUSD != want {
		t.Errorf("want monthly_cost_usd=%.4f, got %.4f", want, resp.MonthlyCostUSD)
	}
	if resp.PlanTier != "pro" {
		t.Errorf("want plan_tier=pro, got %q", resp.PlanTier)
	}
}

// Red Team loopzero-cloud#101 regression-protector (2026-06-27): the
// /api/v1/projects/{name}/scale endpoint MUST reject a request whose
// replicas (or autoscaling.max) exceeds the tenant's plan cap. Before
// the fix, the scale path had ZERO max-replica enforcement — Starter
// could set replicas:999.

type fakeScaleReplicasChecker struct {
	cap  int
	plan string
}

func (f *fakeScaleReplicasChecker) CheckReplicasValue(_ context.Context, _ string, requested int) (bool, string, int, error) {
	return requested <= f.cap, f.plan, f.cap, nil
}

// unreadableReplicasChecker cannot read the plan, as the limiter reports when
// its store is down.
type unreadableReplicasChecker struct{}

func (unreadableReplicasChecker) CheckReplicasValue(context.Context, string, int) (bool, string, int, error) {
	return false, "", 0, loopzeroerrors.ErrPlatformDegraded
}

func TestScaleHandler_RefusesWhenTheReplicaCapCannotBeRead(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		t.Fatal("a scale whose plan cap is unreadable must not be applied")
		return true, nil
	}, true)
	h.SetReplicasChecker(unreadableReplicasChecker{})
	h.SetPlanTierStore(fakePlanTierStore{plan: "starter"})

	body, _ := json.Marshal(map[string]any{"app": "web", "environment": "production", "replicas": 2})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("an unreadable plan cap must refuse, never allow: got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScaleHandler_FixedReplicasOverCap_Returns422(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return true, nil
	}, true)
	h.SetReplicasChecker(&fakeScaleReplicasChecker{cap: 2, plan: "starter"})
	h.SetPlanTierStore(fakePlanTierStore{plan: "starter"})

	body, _ := json.Marshal(map[string]any{
		"app":         "web",
		"environment": "production",
		"replicas":    999,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Red Team #101: want 422 REPLICAS_EXCEED_PLAN, got %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "REPLICAS_EXCEED_PLAN") {
		t.Errorf("Red Team #101: body must include REPLICAS_EXCEED_PLAN code, got %s", w.Body.String())
	}
}

func TestScaleHandler_AutoscalingMaxOverCap_Returns422(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return true, nil
	}, true)
	h.SetReplicasChecker(&fakeScaleReplicasChecker{cap: 2, plan: "starter"})
	h.SetPlanTierStore(fakePlanTierStore{plan: "starter"})

	body, _ := json.Marshal(map[string]any{
		"app":         "web",
		"environment": "production",
		"min":         1,
		"max":         999, // overshoot via autoscaling ceiling
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("Red Team #101: want 422 on autoscaling.max overshoot, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestScaleHandler_FixedReplicasUnderCap_AllowsThrough(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return true, nil
	}, true)
	h.SetReplicasChecker(&fakeScaleReplicasChecker{cap: 2, plan: "starter"})
	h.SetPlanTierStore(fakePlanTierStore{plan: "starter"})

	body, _ := json.Marshal(map[string]any{
		"app":         "web",
		"environment": "production",
		"replicas":    2,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code/100 != 2 {
		t.Fatalf("want 2xx success for replicas == cap, got %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "REPLICAS_EXCEED_PLAN") {
		t.Errorf("Red Team #101: replicas at cap MUST NOT trip REPLICAS_EXCEED_PLAN, got %s", w.Body.String())
	}
}

func TestScaleHandler_NilChecker_NoEnforcement(t *testing.T) {
	// Backwards-compat: no checker wired (tests/dev) → cap not enforced.
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return true, nil
	}, true)
	h.SetPlanTierStore(fakePlanTierStore{plan: "starter"})

	body, _ := json.Marshal(map[string]any{
		"app":         "web",
		"environment": "production",
		"replicas":    999,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)
	if w.Code/100 != 2 {
		t.Fatalf("nil checker should not enforce — want 2xx success, got %d body=%s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "REPLICAS_EXCEED_PLAN") {
		t.Errorf("Red Team #101: nil checker MUST NOT enforce REPLICAS_EXCEED_PLAN, got %s", w.Body.String())
	}
}

// Red Team loopzero-cloud#103 regression-protector — input validation MUST
// return HTTP 400 with the client-classified `INVALID_REPLICAS` code,
// never `500 INVALID_ARGUMENT` (status×code mismatch + server-failure
// connotation for a client-input error). The 3 scenarios lock the 3
// negative-input axes: fixed replicas <= 0 / autoscaling min < 0 /
// autoscaling max <= 0.

func TestScaleHandler_FixedReplicasNegative_Returns400InvalidReplicas(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return true, nil
	}, true)

	body, _ := json.Marshal(map[string]any{
		"app":         "web",
		"environment": "production",
		"replicas":    -5, // verbatim Red Team #101/#103 secondary observation
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Red Team #103: replicas:-5 must be 400 (NOT 500), got %d body=%s",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "INVALID_REPLICAS") {
		t.Errorf("Red Team #103: body must include INVALID_REPLICAS code (NOT INVALID_ARGUMENT), got %s",
			w.Body.String())
	}
}

func TestScaleHandler_AutoscalingMinNegative_Returns400InvalidReplicas(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return true, nil
	}, true)

	body, _ := json.Marshal(map[string]any{
		"app":         "web",
		"environment": "production",
		"min":         -1,
		"max":         5,
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Red Team #103: autoscaling.min:-1 must be 400, got %d body=%s",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "INVALID_REPLICAS") {
		t.Errorf("Red Team #103: body must include INVALID_REPLICAS, got %s", w.Body.String())
	}
}

func TestScaleHandler_AutoscalingMaxZero_Returns400InvalidReplicas(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{
		project: &ports.ProjectResult{ID: "p1", Name: "p", TeamID: "tn-test-123"},
	}, func(_ context.Context, _ ScaleConfigInput) (bool, error) {
		return true, nil
	}, true)

	body, _ := json.Marshal(map[string]any{
		"app":         "web",
		"environment": "production",
		"min":         1,
		"max":         0, // sentinel: not just negative, but zero too
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/projects/p/scale", bytes.NewReader(body))
	req = routestest.WithAuth(req, routestest.DefaultUserContext())
	req = routestest.WithChiParam(req, "name", "p")
	w := httptest.NewRecorder()
	h.Scale(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("Red Team #103: autoscaling.max:0 must be 400, got %d body=%s",
			w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "INVALID_REPLICAS") {
		t.Errorf("Red Team #103: body must include INVALID_REPLICAS, got %s", w.Body.String())
	}
}

// TestScaleHandler_EnsureProductionWired_PanicsOnNilReplicasChecker closes the
// residual half of issue #137.
//
// The plan replica cap IS enforced today — Scale() calls enforceReplicaCap,
// which consults h.replicasChecker and returns REPLICAS_EXCEED_PLAN. But the
// checker is nil-disabled by design ("nil disables enforcement (tests + legacy
// callsites)"), and production only gets it because setup_orchestrator.go calls
// SetReplicasChecker. Delete that one line and every tenant may request any
// number of replicas, with no error anywhere — the cap does not fail, it
// evaporates.
//
// That is precisely the failure mode EnsureProductionWired already exists to
// prevent for the apply closure ("scaling would silently no-op", audit M02 #14).
// The guard simply never grew to cover the second thing that can silently
// vanish. #137 was reported as "cap not enforced"; the enforcement landed later
// via Red Team #101, and this is what was left undefended.
func TestScaleHandler_EnsureProductionWired_PanicsOnNilReplicasChecker(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{}, func(context.Context, ScaleConfigInput) (bool, error) { return true, nil }, false)
	// apply is wired; the checker deliberately is not.
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("EnsureProductionWired must panic when replicasChecker is nil — otherwise a dropped " +
				"SetReplicasChecker call disables the plan cap silently, which is the exact regression " +
				"class this guard exists for (issue #137)")
		}
		msg, _ := r.(string)
		if !strings.Contains(msg, "replicasChecker") {
			t.Errorf("panic message %q must name replicasChecker so the operator knows WHICH wiring is missing", msg)
		}
	}()
	h.EnsureProductionWired()
}

// TestScaleHandler_EnsureProductionWired_WaiveIsAccepted proves the escape
// hatch works — a boot that genuinely cannot enforce (no database, hence no
// PlanLimiter) must be able to say so and pass, or the guard would force
// callers to fake a checker just to boot, which is worse than no guard.
func TestScaleHandler_EnsureProductionWired_WaiveIsAccepted(t *testing.T) {
	h := NewScaleHandler(&fakeScaleProjectStore{},
		func(context.Context, ScaleConfigInput) (bool, error) { return true, nil }, false)
	h.WaiveReplicaCapEnforcement("no database in this boot")

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("an explicit waive must satisfy the guard; got panic=%v", r)
		}
	}()
	h.EnsureProductionWired()
}
