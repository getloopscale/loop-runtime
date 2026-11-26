// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	loopzerov1 "github.com/getloopscale/loop-runtime/operators/api/v1"
	"github.com/getloopscale/loop-runtime/operators/internal/conditions"
)

// T0.2 / D6: validate that the controller's namespace naming uses
// pkg/naming.PreviewNamespace and produces the canonical format
// {tenantSlug}-{projectSlug}-pr-{prNumber}.
func TestPreviewReconciler_NamespaceName_Canonical(t *testing.T) {
	t.Parallel()

	r := &PreviewReconciler{}
	cases := []struct {
		name     string
		preview  *loopzerov1.PreviewEnvironment
		want     string
		wantErr  bool
		errMatch string
	}{
		{
			// J-23 canonical refactor 2026-06-07 (the sponsor asked for
			// every environment, previews included, to be separated by
			// project rather than special-cased): namespace formula is uniformly
			// `loopzero-<tenant>-<project>-<env>` for ALL envs including
			// preview-pr-N. Pre-refactor was `<tenant>-<project>-pr-N`
			// (no `loopzero-` prefix, no `preview-` env segment) — which
			// collided with EnvironmentNamespace shape and broke ArgoCD
			// destination matching. Tests aligned to canonical.
			name: "happy path: tenant + project + PR number",
			preview: &loopzerov1.PreviewEnvironment{
				ObjectMeta: metav1.ObjectMeta{Name: "p"},
				Spec: loopzerov1.PreviewEnvironmentSpec{
					TenantID:  "acme",
					ProjectID: "api",
					PRNumber:  42,
				},
			},
			want: "loopzero-acme-api-preview-pr-42",
		},
		{
			name: "single-character slugs are RFC 1123 valid",
			preview: &loopzerov1.PreviewEnvironment{
				Spec: loopzerov1.PreviewEnvironmentSpec{
					TenantID:  "a",
					ProjectID: "b",
					PRNumber:  1,
				},
			},
			want: "loopzero-a-b-preview-pr-1",
		},
		{
			name: "empty tenant slug returns error",
			preview: &loopzerov1.PreviewEnvironment{
				Spec: loopzerov1.PreviewEnvironmentSpec{
					TenantID:  "",
					ProjectID: "api",
					PRNumber:  42,
				},
			},
			wantErr:  true,
			errMatch: "tenant slug",
		},
		{
			name: "empty project slug returns error",
			preview: &loopzerov1.PreviewEnvironment{
				Spec: loopzerov1.PreviewEnvironmentSpec{
					TenantID:  "acme",
					ProjectID: "",
					PRNumber:  42,
				},
			},
			wantErr:  true,
			errMatch: "project slug",
		},
		{
			name: "non-positive PR number returns error",
			preview: &loopzerov1.PreviewEnvironment{
				Spec: loopzerov1.PreviewEnvironmentSpec{
					TenantID:  "acme",
					ProjectID: "api",
					PRNumber:  0,
				},
			},
			wantErr:  true,
			errMatch: "PR number",
		},
		{
			name: "uppercase slugs violate RFC 1123",
			preview: &loopzerov1.PreviewEnvironment{
				Spec: loopzerov1.PreviewEnvironmentSpec{
					TenantID:  "ACME",
					ProjectID: "api",
					PRNumber:  42,
				},
			},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		// Go 1.22+: per-iteration loop variable; no shadow needed.
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := r.previewNamespaceName(tc.preview)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil (got %q)", got)
				}
				if tc.errMatch != "" && !strings.Contains(err.Error(), tc.errMatch) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errMatch)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("namespace name = %q, want %q", got, tc.want)
			}
		})
	}
}

// T1.3: bumpCleanupAttempts must persist the counter on the CR
// annotations and return monotonically increasing values.
func TestPreviewReconciler_BumpCleanupAttempts_Increments(t *testing.T) {
	t.Parallel()

	r := &PreviewReconciler{}
	preview := &loopzerov1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "p1"},
	}

	first := r.bumpCleanupAttempts(preview)
	if first != 1 {
		t.Errorf("first bump = %d, want 1", first)
	}
	if got := preview.Annotations[previewCleanupAnnotation]; got != "1" {
		t.Errorf("annotation = %q, want %q", got, "1")
	}

	second := r.bumpCleanupAttempts(preview)
	if second != 2 {
		t.Errorf("second bump = %d, want 2", second)
	}
	if got := preview.Annotations[previewCleanupAnnotation]; got != "2" {
		t.Errorf("annotation = %q, want %q", got, "2")
	}

	// Up to the max — verify the threshold semantics.
	for i := 3; i <= previewMaxCleanupAttempts; i++ {
		got := r.bumpCleanupAttempts(preview)
		if got != i {
			t.Errorf("bump #%d = %d, want %d", i, got, i)
		}
	}
}

// =============================================================================
// T1.2: evaluateWorkloadGate — workload-readiness condition flow
// =============================================================================

func newPreviewWithCreatedAt(createdAt time.Time, conds ...metav1.Condition) *loopzerov1.PreviewEnvironment {
	creation := metav1.NewTime(createdAt)
	return &loopzerov1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "p", CreationTimestamp: creation},
		Status: loopzerov1.PreviewEnvironmentStatus{
			CreatedAt:  &creation,
			Conditions: conds,
		},
	}
}

func cond(name string, status metav1.ConditionStatus) metav1.Condition {
	return metav1.Condition{
		Type:               name,
		Status:             status,
		Reason:             "Test",
		Message:            "test",
		LastTransitionTime: metav1.Now(),
	}
}

func TestEvaluateWorkloadGate_WithinGrace_NoBuildSubmitted_Requeues(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := newPreviewWithCreatedAt(time.Now()) // just created
	result, gated := r.evaluateWorkloadGate(preview)
	if !gated {
		t.Fatal("expected gated=true within grace period without BuildSubmitted")
	}
	if result.RequeueAfter != previewBuildSubmittedRequeueWait {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, previewBuildSubmittedRequeueWait)
	}
}

func TestEvaluateWorkloadGate_OutsideGrace_NoBuildSubmitted_Proceeds(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	// Created long ago — grace period should have lapsed.
	preview := newPreviewWithCreatedAt(time.Now().Add(-10 * time.Minute))
	_, gated := r.evaluateWorkloadGate(preview)
	if gated {
		t.Fatal("expected gated=false after grace expired (graceful degradation)")
	}
}

func TestEvaluateWorkloadGate_BuildSubmittedFalse_TransitionsFailed(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := newPreviewWithCreatedAt(time.Now(),
		cond(conditions.TypePreviewBuildSubmitted, metav1.ConditionFalse),
	)
	preview.Status.Phase = loopzerov1.PreviewPhaseCreating
	_, gated := r.evaluateWorkloadGate(preview)
	if !gated {
		t.Fatal("expected gated=true when BuildSubmitted=False")
	}
	if preview.Status.Phase != loopzerov1.PreviewPhaseFailed {
		t.Errorf("expected phase=Failed when BuildSubmitted=False, got %q", preview.Status.Phase)
	}
}

func TestEvaluateWorkloadGate_BuildSubmittedTrue_BuildCompletedMissing_Requeues(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := newPreviewWithCreatedAt(time.Now(),
		cond(conditions.TypePreviewBuildSubmitted, metav1.ConditionTrue),
	)
	result, gated := r.evaluateWorkloadGate(preview)
	if !gated {
		t.Fatal("expected gated=true when BuildCompleted missing")
	}
	if result.RequeueAfter != previewBuildCompletedRequeueWait {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, previewBuildCompletedRequeueWait)
	}
}

func TestEvaluateWorkloadGate_BuildCompletedFalse_TransitionsFailed(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := newPreviewWithCreatedAt(time.Now(),
		cond(conditions.TypePreviewBuildSubmitted, metav1.ConditionTrue),
		cond(conditions.TypePreviewBuildCompleted, metav1.ConditionFalse),
	)
	preview.Status.Phase = loopzerov1.PreviewPhaseCreating
	_, gated := r.evaluateWorkloadGate(preview)
	if !gated {
		t.Fatal("expected gated=true when BuildCompleted=False")
	}
	if preview.Status.Phase != loopzerov1.PreviewPhaseFailed {
		t.Errorf("expected phase=Failed when BuildCompleted=False, got %q", preview.Status.Phase)
	}
}

func TestEvaluateWorkloadGate_AllTrueExceptHealthy_Requeues(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := newPreviewWithCreatedAt(time.Now(),
		cond(conditions.TypePreviewBuildSubmitted, metav1.ConditionTrue),
		cond(conditions.TypePreviewBuildCompleted, metav1.ConditionTrue),
	)
	result, gated := r.evaluateWorkloadGate(preview)
	if !gated {
		t.Fatal("expected gated=true when WorkloadHealthy missing")
	}
	if result.RequeueAfter != previewWorkloadHealthyRequeueWait {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, previewWorkloadHealthyRequeueWait)
	}
}

func TestEvaluateWorkloadGate_WorkloadHealthyFalse_Requeues(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := newPreviewWithCreatedAt(time.Now(),
		cond(conditions.TypePreviewBuildSubmitted, metav1.ConditionTrue),
		cond(conditions.TypePreviewBuildCompleted, metav1.ConditionTrue),
		cond(conditions.TypePreviewWorkloadHealthy, metav1.ConditionFalse),
	)
	result, gated := r.evaluateWorkloadGate(preview)
	if !gated {
		t.Fatal("expected gated=true when WorkloadHealthy=False")
	}
	if result.RequeueAfter != previewWorkloadHealthyRequeueWait {
		t.Errorf("RequeueAfter = %v, want %v", result.RequeueAfter, previewWorkloadHealthyRequeueWait)
	}
}

func TestEvaluateWorkloadGate_AllReady_Proceeds(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := newPreviewWithCreatedAt(time.Now(),
		cond(conditions.TypePreviewBuildSubmitted, metav1.ConditionTrue),
		cond(conditions.TypePreviewBuildCompleted, metav1.ConditionTrue),
		cond(conditions.TypePreviewWorkloadHealthy, metav1.ConditionTrue),
	)
	_, gated := r.evaluateWorkloadGate(preview)
	if gated {
		t.Fatal("expected gated=false when all conditions True")
	}
}

func TestPreviewWithinGracePeriod_NilCreatedAt_Conservative(t *testing.T) {
	t.Parallel()
	preview := &loopzerov1.PreviewEnvironment{ObjectMeta: metav1.ObjectMeta{Name: "p"}}
	// No CreatedAt and no CreationTimestamp set — should default to "within grace".
	if !previewWithinGracePeriod(preview, time.Now()) {
		t.Error("expected conservative behavior (within grace) for nil anchor")
	}
}

func TestPreviewWithinGracePeriod_FallsBackToCreationTimestamp(t *testing.T) {
	t.Parallel()
	preview := &loopzerov1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "p",
			CreationTimestamp: metav1.NewTime(time.Now().Add(-200 * time.Second)),
		},
	}
	// CreatedAt is nil but CreationTimestamp is older than grace → outside grace.
	if previewWithinGracePeriod(preview, time.Now()) {
		t.Error("expected outside grace using CreationTimestamp fallback")
	}
}

func TestFindCondition_AbsentReturnsNil(t *testing.T) {
	t.Parallel()
	got := findCondition([]metav1.Condition{
		cond("Other", metav1.ConditionTrue),
	}, "NotPresent")
	if got != nil {
		t.Error("expected nil for absent condition")
	}
}

func TestFindCondition_PresentReturnsPointer(t *testing.T) {
	t.Parallel()
	c := cond(conditions.TypePreviewBuildSubmitted, metav1.ConditionTrue)
	got := findCondition([]metav1.Condition{c}, conditions.TypePreviewBuildSubmitted)
	if got == nil {
		t.Fatal("expected non-nil for present condition")
	}
	if got.Type != conditions.TypePreviewBuildSubmitted {
		t.Errorf("type = %q, want %q", got.Type, conditions.TypePreviewBuildSubmitted)
	}
}

// =============================================================================
// T1.3 cleanup-attempts (existing)
// =============================================================================

func TestPreviewReconciler_BumpCleanupAttempts_NilAnnotations(t *testing.T) {
	t.Parallel()
	r := &PreviewReconciler{}
	preview := &loopzerov1.PreviewEnvironment{
		ObjectMeta: metav1.ObjectMeta{Name: "p2"},
	}
	if preview.Annotations != nil {
		t.Fatal("test precondition: Annotations should be nil")
	}
	got := r.bumpCleanupAttempts(preview)
	if got != 1 {
		t.Errorf("bump on nil annotations = %d, want 1", got)
	}
	if preview.Annotations == nil {
		t.Fatal("Annotations should be initialized after bump")
	}
}

// T0.2: regression test to ensure the legacy fmt.Sprintf format
// (preview-{tenantID}-pr{prNumber}, no projectID) is no longer produced.
func TestPreviewReconciler_NamespaceName_RejectsLegacyFormat(t *testing.T) {
	t.Parallel()

	r := &PreviewReconciler{}
	preview := &loopzerov1.PreviewEnvironment{
		Spec: loopzerov1.PreviewEnvironmentSpec{
			TenantID:  "acme",
			ProjectID: "api",
			PRNumber:  42,
		},
	}
	got, err := r.previewNamespaceName(preview)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	legacy := "preview-acme-pr42"
	if got == legacy {
		t.Fatalf("controller still using legacy format %q (T0.2 regression)", legacy)
	}
	// Must contain the project slug.
	if !strings.Contains(got, "api") {
		t.Fatalf("namespace %q does not include project slug %q", got, "api")
	}
	// Must contain the canonical "pr-{n}" separator (note the hyphen).
	if !strings.Contains(got, "pr-42") {
		t.Fatalf("namespace %q does not include canonical pr-{n} format", got)
	}
}

// Deprecation cleanup (2026-08-08): naming.PreviewNamespace was deleted. Its
// doc comment promised the legacy "{tenant}-{project}-pr-{N}" format, but the
// body already delegated to Namespace — a redundant wrapper, not a format of its
// own. This test pins the equivalence that made the deletion safe: the preview
// namespace is the canonical loopzero-{tenant}-{project}-preview-pr-{N}, like any
// other environment.
func TestPreviewNamespaceName_IsTheCanonicalFormat(t *testing.T) {
	r := &PreviewReconciler{}
	preview := &loopzerov1.PreviewEnvironment{
		Spec: loopzerov1.PreviewEnvironmentSpec{
			TenantID: "tn-acme", ProjectID: "sample", PRNumber: 3,
		},
	}

	got, err := r.previewNamespaceName(preview)
	if err != nil {
		t.Fatalf("previewNamespaceName: %v", err)
	}
	want := "loopzero-tn-acme-sample-preview-pr-3"
	if got != want {
		t.Errorf("previewNamespaceName = %q, want %q — the legacy format matches "+
			"no namespace on any cluster", got, want)
	}
}
