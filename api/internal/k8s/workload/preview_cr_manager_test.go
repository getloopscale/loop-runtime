// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package workload

import (
	"context"
	"testing"
	"time"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/getloopscale/loop-runtime/api/internal/k8s"
	apiv1 "github.com/getloopscale/loop-runtime/operators/api/v1"
)

// newPreviewMgr builds a PreviewCRManager backed by the controller-runtime
// fake typed client (M8). UNIT test — no live cluster, no envtest.
func newPreviewMgr(t *testing.T) *PreviewCRManager {
	t.Helper()
	scheme, err := k8s.NewScheme()
	if err != nil {
		t.Fatalf("NewScheme: %v", err)
	}
	return NewPreviewCRManager(fake.NewClientBuilder().WithScheme(scheme).Build())
}

func samplePreviewSpec() PreviewCRSpec {
	return PreviewCRSpec{
		Name:      "prev-1111",
		TenantID:  "tn-loopzero",
		ProjectID: "my-project",
		PRNumber:  42,
		Branch:    "feat/x",
		ReleaseID: "rel-abc",
		ExpiresAt: time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC),
	}
}

func TestEnsurePreviewCR_CreateWhenNotFound(t *testing.T) {
	mgr := newPreviewMgr(t)
	spec := samplePreviewSpec()

	if err := mgr.EnsurePreviewCR(context.Background(), spec); err != nil {
		t.Fatalf("EnsurePreviewCR() unexpected error: %v", err)
	}

	var got apiv1.PreviewEnvironment
	if err := mgr.Client.Get(context.Background(), client.ObjectKey{Namespace: k8s.ControlPlaneNamespace, Name: spec.Name}, &got); err != nil {
		t.Fatalf("expected PreviewEnvironment CR to exist: %v", err)
	}
	if got.Spec.TenantID != "tn-loopzero" {
		t.Errorf("tenantId = %q, want tn-loopzero", got.Spec.TenantID)
	}
	if got.Spec.ProjectID != "my-project" {
		t.Errorf("projectId = %q, want my-project", got.Spec.ProjectID)
	}
	if got.Spec.PRNumber != 42 {
		t.Errorf("prNumber = %d, want 42", got.Spec.PRNumber)
	}
	if got.Spec.Branch != "feat/x" {
		t.Errorf("branch = %q, want feat/x", got.Spec.Branch)
	}
	if got.Spec.ReleaseID != "rel-abc" {
		t.Errorf("releaseId = %q, want rel-abc", got.Spec.ReleaseID)
	}
	if got.Spec.ExpiresAt == nil || !got.Spec.ExpiresAt.Time.Equal(spec.ExpiresAt) {
		t.Errorf("expiresAt = %v, want %v", got.Spec.ExpiresAt, spec.ExpiresAt)
	}
	// Labels enable the DeletePreviewCR label-selector lookup (J-23 bug #5).
	if got.Labels["loopzero.io/tenant"] != "tn-loopzero" {
		t.Errorf("label loopzero.io/tenant = %q, want tn-loopzero", got.Labels["loopzero.io/tenant"])
	}
	if got.Labels["loopzero.io/pr-number"] != "42" {
		t.Errorf("label loopzero.io/pr-number = %q, want 42", got.Labels["loopzero.io/pr-number"])
	}
}

func TestEnsurePreviewCR_UpsertOnConflict(t *testing.T) {
	mgr := newPreviewMgr(t)
	spec := samplePreviewSpec()
	if err := mgr.EnsurePreviewCR(context.Background(), spec); err != nil {
		t.Fatalf("first EnsurePreviewCR: %v", err)
	}

	// PR sync to a new commit — same name, new branch/release. Must update.
	spec.Branch = "feat/x-rev2"
	spec.ReleaseID = "rel-def"
	if err := mgr.EnsurePreviewCR(context.Background(), spec); err != nil {
		t.Fatalf("second EnsurePreviewCR (upsert): %v", err)
	}

	var got apiv1.PreviewEnvironment
	if err := mgr.Client.Get(context.Background(), client.ObjectKey{Namespace: k8s.ControlPlaneNamespace, Name: spec.Name}, &got); err != nil {
		t.Fatalf("get after upsert: %v", err)
	}
	if got.Spec.Branch != "feat/x-rev2" {
		t.Errorf("branch after upsert = %q, want feat/x-rev2", got.Spec.Branch)
	}
	if got.Spec.ReleaseID != "rel-def" {
		t.Errorf("releaseId after upsert = %q, want rel-def", got.Spec.ReleaseID)
	}
}

func TestGetPhases(t *testing.T) {
	mgr := newPreviewMgr(t)
	// Seed two CRs, one with an observed phase, one without.
	withPhase := buildPreviewCR(PreviewCRSpec{Name: "prev-active", TenantID: "t", ProjectID: "p", PRNumber: 1, ExpiresAt: time.Now()})
	withPhase.Status.Phase = apiv1.PreviewPhaseActive
	noPhase := buildPreviewCR(PreviewCRSpec{Name: "prev-creating", TenantID: "t", ProjectID: "p", PRNumber: 2, ExpiresAt: time.Now()})
	if err := mgr.Client.Create(context.Background(), withPhase); err != nil {
		t.Fatalf("seed withPhase: %v", err)
	}
	if err := mgr.Client.Create(context.Background(), noPhase); err != nil {
		t.Fatalf("seed noPhase: %v", err)
	}

	phases, err := mgr.GetPhases(context.Background(), []string{"prev-active", "prev-creating", "prev-missing"})
	if err != nil {
		t.Fatalf("GetPhases: %v", err)
	}
	if phases["prev-active"] != "Active" {
		t.Errorf("prev-active phase = %q, want Active", phases["prev-active"])
	}
	// No status phase → absent (caller falls back to DB status).
	if _, ok := phases["prev-creating"]; ok {
		t.Errorf("prev-creating should be absent (no status phase), got %q", phases["prev-creating"])
	}
	// NotFound → absent, not an error.
	if _, ok := phases["prev-missing"]; ok {
		t.Errorf("prev-missing should be absent (NotFound), got %q", phases["prev-missing"])
	}
}

func TestGetPhases_EmptyInput(t *testing.T) {
	mgr := newPreviewMgr(t)
	phases, err := mgr.GetPhases(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetPhases(nil): %v", err)
	}
	if len(phases) != 0 {
		t.Errorf("expected empty map, got %v", phases)
	}
}

func TestDeletePreviewCR_FindsByLabelSelector(t *testing.T) {
	mgr := newPreviewMgr(t)
	spec := samplePreviewSpec()
	if err := mgr.EnsurePreviewCR(context.Background(), spec); err != nil {
		t.Fatalf("seed EnsurePreviewCR: %v", err)
	}

	// Delete by the (tenant, project, pr) trio — NOT by the synthetic name.
	if err := mgr.DeletePreviewCR(context.Background(), spec.TenantID, spec.ProjectID, spec.PRNumber); err != nil {
		t.Fatalf("DeletePreviewCR: %v", err)
	}

	var got apiv1.PreviewEnvironment
	err := mgr.Client.Get(context.Background(), client.ObjectKey{Namespace: k8s.ControlPlaneNamespace, Name: spec.Name}, &got)
	if !k8serrors.IsNotFound(err) {
		t.Errorf("expected CR deleted (NotFound), got err=%v", err)
	}
}

func TestDeletePreviewCR_IdempotentWhenAbsent(t *testing.T) {
	mgr := newPreviewMgr(t)
	// Closing a PR that never had a preview must be a no-op, not an error.
	if err := mgr.DeletePreviewCR(context.Background(), "tn-x", "proj", 99); err != nil {
		t.Fatalf("DeletePreviewCR on absent CR should be nil, got %v", err)
	}
}

func TestEnsurePreviewCR_CarriesThePublicURL(t *testing.T) {
	mgr := newPreviewMgr(t)
	spec := samplePreviewSpec()
	spec.PublicURL = "https://tn-loopzero-my-project-web-preview-pr-42.preview.example.test"

	if err := mgr.EnsurePreviewCR(context.Background(), spec); err != nil {
		t.Fatalf("EnsurePreviewCR: %v", err)
	}

	var got apiv1.PreviewEnvironment
	if err := mgr.Client.Get(context.Background(), client.ObjectKey{Namespace: k8s.ControlPlaneNamespace, Name: spec.Name}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Spec.PublicURL != spec.PublicURL {
		t.Fatalf("spec.publicURL = %q, want %q", got.Spec.PublicURL, spec.PublicURL)
	}
}
