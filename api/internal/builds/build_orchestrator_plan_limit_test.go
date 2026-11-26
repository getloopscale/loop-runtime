// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

// B15 (CHG-2026-04-23-B15 / MEET-2026-04-23-059): plan-limit + mutual-exclusion
// enforcement in BuildOrchestrator.SubmitBuild. This is the authoritative gate
// that covers BOTH the CLI deploy path and the GitHub App auto-deploy path —
// any bypass is a security incident.

package builds

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	errorshttp "github.com/getloopscale/loop-runtime/pkg/errors/http"
)

// fakePlanCapChecker lets tests drive plan-cap decisions without a real DB.
// Mirrors the interface that BuildOrchestrator uses (see PlanCapChecker).
type fakePlanCapChecker struct {
	// allowedRequests maps max-replica-requested → whether to allow.
	// If the request is NOT in the map, returns (allowed=true).
	// Always reports Plan/Limit/Current so error message quality is verified.
	allow    map[int]bool
	planName string
	cap      int
	err      error // non-nil: the cap cannot be verified
	// recorded captures the last requested value for assertion.
	recordedRequested int
	recordedCalled    int
}

func (f *fakePlanCapChecker) CheckReplicasValue(_ context.Context, tenantID string, requested int) (allowed bool, plan string, limit int, err error) {
	f.recordedRequested = requested
	f.recordedCalled++
	if f.err != nil {
		return false, "", 0, f.err
	}
	if f.allow == nil {
		return true, f.planName, f.cap, nil
	}
	v, ok := f.allow[requested]
	if !ok {
		return true, f.planName, f.cap, nil
	}
	return v, f.planName, f.cap, nil
}

// Starter plan: requesting 3 replicas (cap=2) → 422 REPLICAS_EXCEED_PLAN.
func TestSubmitBuild_StarterPlan_ReplicasExceedCap_Returns422(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	checker := &fakePlanCapChecker{
		allow:    map[int]bool{3: false},
		planName: "starter",
		cap:      2,
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		PlanCap:    checker,
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-starter",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Runtime: "node", Replicas: 3},
		},
	})

	if err == nil {
		t.Fatal("expected ErrReplicasExceedPlan, got nil")
	}
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !errors.As(err, &loopzeroErr) {
		t.Fatalf("expected *LoopZeroError, got: %T %v", err, err)
	}
	if loopzeroErr.Code != "REPLICAS_EXCEED_PLAN" {
		t.Errorf("expected code REPLICAS_EXCEED_PLAN, got %q (msg: %s)", loopzeroErr.Code, loopzeroErr.Message)
	}
	if got := errorshttp.Status(loopzeroErr); got != 422 {
		t.Errorf("expected HTTP 422, got %d", got)
	}

	// Build must NOT be persisted.
	if len(bs.builds) != 0 {
		t.Errorf("no build should have been created; got %d", len(bs.builds))
	}
}

// Starter plan: autoscaling.max exceeds cap → same 422.
func TestSubmitBuild_StarterPlan_AutoscalingMaxExceedsCap_Returns422(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	checker := &fakePlanCapChecker{
		allow:    map[int]bool{5: false},
		planName: "starter",
		cap:      2,
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		PlanCap:    checker,
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-starter",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{
				Name: "api", Type: "server", Port: 3000, Runtime: "node",
				Autoscaling: &deploycontracts.AppAutoscalingSpec{MinReplicas: 1, MaxReplicas: 5, TargetCPU: 70},
			},
		},
	})

	if err == nil {
		t.Fatal("expected ErrReplicasExceedPlan for autoscaling.max=5 on starter (cap=2)")
	}
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !errors.As(err, &loopzeroErr) {
		t.Fatalf("expected *LoopZeroError, got: %T %v", err, err)
	}
	if loopzeroErr.Code != "REPLICAS_EXCEED_PLAN" {
		t.Errorf("expected REPLICAS_EXCEED_PLAN, got %q", loopzeroErr.Code)
	}
	if checker.recordedRequested != 5 {
		t.Errorf("expected checker called with 5 (autoscaling.max), got %d", checker.recordedRequested)
	}
}

// Starter plan: replicas==cap → succeeds, BuildAppSpec persisted with replicas=2.
func TestSubmitBuild_StarterPlan_ReplicasAtCap_Succeeds(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	checker := &fakePlanCapChecker{planName: "starter", cap: 2}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		PlanCap:    checker,
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-starter",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Runtime: "node", Replicas: 2},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bs.builds) != 1 {
		t.Fatalf("expected one build created, got %d", len(bs.builds))
	}
	// Persisted BuildAppSpec should carry the requested replica count.
	for _, spec := range bs.appsSpec {
		if len(spec) != 1 {
			t.Fatalf("expected 1 app spec, got %d", len(spec))
		}
		if spec[0].Replicas != 2 {
			t.Errorf("expected persisted Replicas=2, got %d", spec[0].Replicas)
		}
	}
}

// Pro plan: replicas=5 (cap=5) → succeeds.
func TestSubmitBuild_ProPlan_ReplicasAtCap_Succeeds(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	checker := &fakePlanCapChecker{planName: "pro", cap: 5}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		PlanCap:    checker,
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-pro",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Runtime: "node", Replicas: 5},
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Pro plan: replicas=6 (cap=5) → 422.
func TestSubmitBuild_ProPlan_ReplicasOverCap_Returns422(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	checker := &fakePlanCapChecker{
		allow:    map[int]bool{6: false},
		planName: "pro",
		cap:      5,
	}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		PlanCap:    checker,
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-pro",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Runtime: "node", Replicas: 6},
		},
	})

	if err == nil {
		t.Fatal("expected error")
	}
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !errors.As(err, &loopzeroErr) || loopzeroErr.Code != "REPLICAS_EXCEED_PLAN" {
		t.Errorf("expected REPLICAS_EXCEED_PLAN, got: %v", err)
	}
}

// Both replicas AND autoscaling set → 422 SCALING_AMBIGUOUS.
func TestSubmitBuild_ReplicasAndAutoscalingSet_Returns422Ambiguous(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	checker := &fakePlanCapChecker{planName: "pro", cap: 5}

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		PlanCap:    checker,
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-pro",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{
				Name: "api", Type: "server", Port: 3000, Runtime: "node",
				Replicas:    2,
				Autoscaling: &deploycontracts.AppAutoscalingSpec{MinReplicas: 1, MaxReplicas: 3, TargetCPU: 70},
			},
		},
	})

	if err == nil {
		t.Fatal("expected ErrScalingAmbiguous")
	}
	var loopzeroErr *loopzeroerrors.LoopZeroError
	if !errors.As(err, &loopzeroErr) {
		t.Fatalf("expected *LoopZeroError, got %T: %v", err, err)
	}
	if loopzeroErr.Code != "SCALING_AMBIGUOUS" {
		t.Errorf("expected SCALING_AMBIGUOUS, got %q (msg: %s)", loopzeroErr.Code, loopzeroErr.Message)
	}
	if got := errorshttp.Status(loopzeroErr); got != 422 {
		t.Errorf("expected HTTP 422, got %d", got)
	}
	if len(bs.builds) != 0 {
		t.Errorf("no build should have been created on ambiguous scaling; got %d", len(bs.builds))
	}
}

// Nil checker (dev / legacy) → allowed, build created.
func TestSubmitBuild_NilPlanCapChecker_AllowsAll(t *testing.T) {
	bs := newFakeBuildStoreOrch()

	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		// PlanCap left nil — legacy callsite without enforcement configured.
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-any",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Runtime: "node", Replicas: 99},
		},
	})

	if err != nil {
		t.Fatalf("nil checker must allow all: %v", err)
	}
	if len(bs.builds) != 1 {
		t.Errorf("expected build created when checker is nil, got %d", len(bs.builds))
	}
}

// A replica cap that cannot be verified refuses the submission with the
// checker's typed error; it never reads as "allowed".
func TestSubmitBuild_ReplicaCapUnverifiable_RefusesWithPlatformDegraded(t *testing.T) {
	bs := newFakeBuildStoreOrch()
	orch := NewBuildOrchestrator(BuildOrchestratorDeps{
		BuildStore: bs,
		Locker:     &fakeLockerContainer{},
		Workflow:   &fakeWorkflowSubmitterOrch{workflowID: "wf-1"},
		EventStore: &fakeEventStoreContainer{},
		PlanCap:    &fakePlanCapChecker{err: loopzeroerrors.ErrPlatformDegraded.Wrap("limits store down")},
	})

	_, err := orch.SubmitBuild(context.Background(), deploycontracts.SubmitBuildRequest{
		TenantID:    "tn-a",
		ProjectID:   "proj-1",
		Environment: "production",
		Apps: []deploycontracts.AppSpec{
			{Name: "api", Type: "server", Port: 3000, Runtime: "node", Replicas: 2},
		},
	})

	if !errors.Is(err, loopzeroerrors.ErrPlatformDegraded) {
		t.Fatalf("err = %v, want PLATFORM_DEGRADED", err)
	}
	if len(bs.builds) != 0 {
		t.Errorf("no build may be created when the cap cannot be verified; got %d", len(bs.builds))
	}
}
