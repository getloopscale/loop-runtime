// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/loopzero/loopzero/api/internal/db"
	"github.com/loopzero/loopzero/api/internal/domain"
	loopzeroerrors "github.com/loopzero/loopzero/pkg/errors"
)

// =============================================================================
// Fakes
// =============================================================================

type previewFakeStore struct {
	createFn          func(ctx context.Context, tx db.DBTX, rec *db.PreviewRecord) error
	updateStatusFn    func(ctx context.Context, tx db.DBTX, previewID, status string) error
	updateBuildFn     func(ctx context.Context, tx db.DBTX, previewID, buildID, commitSHA string) error
	getByPRFn         func(ctx context.Context, tenantID, projectID string, prNumber int) (*domain.Preview, error)
	requestDeletionFn func(ctx context.Context, tenantID, previewID string) error
}

func (f *previewFakeStore) ListByTenant(_ context.Context, _ string) ([]domain.Preview, error) {
	return nil, nil
}
func (f *previewFakeStore) ListByProject(_ context.Context, _, _ string) ([]domain.Preview, error) {
	return nil, nil
}
func (f *previewFakeStore) GetByID(_ context.Context, _, _ string) (*domain.Preview, error) {
	return nil, loopzeroerrors.ErrPreviewNotFound
}
func (f *previewFakeStore) GetByPR(ctx context.Context, tenantID, projectID string, prNumber int) (*domain.Preview, error) {
	if f.getByPRFn != nil {
		return f.getByPRFn(ctx, tenantID, projectID, prNumber)
	}
	return nil, loopzeroerrors.ErrPreviewNotFound
}
func (f *previewFakeStore) Create(ctx context.Context, tx db.DBTX, rec *db.PreviewRecord) error {
	if f.createFn != nil {
		return f.createFn(ctx, tx, rec)
	}
	if rec.ID == "" {
		rec.ID = "prev-fake-id"
	}
	return nil
}
func (f *previewFakeStore) UpdateStatus(ctx context.Context, tx db.DBTX, previewID, status string) error {
	if f.updateStatusFn != nil {
		return f.updateStatusFn(ctx, tx, previewID, status)
	}
	return nil
}
func (f *previewFakeStore) UpdateBuild(ctx context.Context, tx db.DBTX, previewID, buildID, commitSHA string) error {
	if f.updateBuildFn != nil {
		return f.updateBuildFn(ctx, tx, previewID, buildID, commitSHA)
	}
	return nil
}
func (f *previewFakeStore) UpdateURL(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}
func (f *previewFakeStore) RequestDeletion(ctx context.Context, tenantID, previewID string) error {
	if f.requestDeletionFn != nil {
		return f.requestDeletionFn(ctx, tenantID, previewID)
	}
	return nil
}

type previewFakeBuilds struct {
	submitFn func(ctx context.Context, req SubmitBuildRequest) (*SubmitBuildResponse, error)
}

func (f *previewFakeBuilds) SubmitBuild(ctx context.Context, req SubmitBuildRequest) (*SubmitBuildResponse, error) {
	if f.submitFn != nil {
		return f.submitFn(ctx, req)
	}
	return &SubmitBuildResponse{BuildID: "build-fake"}, nil
}

type previewFakeCR struct {
	ensureFn func(ctx context.Context, spec PreviewCRSpec) error
	deleteFn func(ctx context.Context, tenantID, projectID string, prNumber int) error
}

func (f *previewFakeCR) EnsurePreviewCR(ctx context.Context, spec PreviewCRSpec) error {
	if f.ensureFn != nil {
		return f.ensureFn(ctx, spec)
	}
	return nil
}
func (f *previewFakeCR) DeletePreviewCR(ctx context.Context, tenantID, projectID string, prNumber int) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, tenantID, projectID, prNumber)
	}
	return nil
}

func newRequest() domain.CreatePreviewRequest {
	r := domain.CreatePreviewRequest{
		ProjectID: "api",
		PRNumber:  42,
		Branch:    "feature/x",
		CommitSHA: "abc1234",
	}
	r.SetTenantID("tn-test-123")
	return r
}

// =============================================================================
// Construction
// =============================================================================

func TestPreviewService_New_RequiresStore(t *testing.T) {
	if _, err := NewPreviewService(PreviewServiceDeps{}); err == nil {
		t.Fatal("expected error when Store is nil")
	}
}

func TestPreviewService_New_DefaultsTTL(t *testing.T) {
	svc, err := NewPreviewService(PreviewServiceDeps{Store: &previewFakeStore{}})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if svc.deps.DefaultTTL != 72*time.Hour {
		t.Errorf("default TTL = %v, want 72h", svc.deps.DefaultTTL)
	}
}

// =============================================================================
// CreatePreview
// =============================================================================

func TestPreviewService_CreatePreview_HappyPath(t *testing.T) {
	storeCreated := false
	crEnsured := false
	buildSubmitted := false

	store := &previewFakeStore{
		createFn: func(_ context.Context, _ db.DBTX, rec *db.PreviewRecord) error {
			storeCreated = true
			if rec.ID == "" {
				rec.ID = "prev-z"
			}
			if rec.TenantID != "tn-test-123" || rec.ProjectID != "api" || rec.PRNumber != 42 {
				t.Errorf("rec scoping wrong: %+v", rec)
			}
			return nil
		},
		updateBuildFn: func(_ context.Context, _ db.DBTX, _, buildID, _ string) error {
			if buildID != "build-fake" {
				t.Errorf("build linking wrong: %s", buildID)
			}
			return nil
		},
	}
	cr := &previewFakeCR{ensureFn: func(_ context.Context, spec PreviewCRSpec) error {
		crEnsured = true
		if spec.PRNumber != 42 {
			t.Errorf("CR spec PR wrong: %d", spec.PRNumber)
		}
		return nil
	}}
	builds := &previewFakeBuilds{submitFn: func(_ context.Context, req SubmitBuildRequest) (*SubmitBuildResponse, error) {
		buildSubmitted = true
		if req.TenantID != "tn-test-123" || req.ProjectID != "api" || req.CommitSHA != "abc1234" {
			t.Errorf("build req wrong: %+v", req)
		}
		if req.Environment == "" {
			t.Errorf("build env not set")
		}
		return &SubmitBuildResponse{BuildID: "build-fake"}, nil
	}}

	svc, err := NewPreviewService(PreviewServiceDeps{
		Store:      store,
		Builds:     builds,
		CRCreator:  cr,
		BaseDomain: "preview.loopzero.app",
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	preview, err := svc.CreatePreview(context.Background(), newRequest())
	if err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}
	if preview == nil {
		t.Fatal("preview must not be nil")
	}
	if !storeCreated {
		t.Error("Store.Create not called")
	}
	if !crEnsured {
		t.Error("CRCreator.EnsurePreviewCR not called")
	}
	if !buildSubmitted {
		t.Error("Builds.SubmitBuild not called")
	}
}

func TestPreviewService_CreatePreview_ValidationFailures(t *testing.T) {
	cases := []struct {
		name string
		mod  func(*domain.CreatePreviewRequest)
	}{
		{"empty tenant", func(r *domain.CreatePreviewRequest) { r.SetTenantID("") }},
		{"empty project", func(r *domain.CreatePreviewRequest) { r.ProjectID = "" }},
		{"non-positive PR", func(r *domain.CreatePreviewRequest) { r.PRNumber = 0 }},
		{"empty branch", func(r *domain.CreatePreviewRequest) { r.Branch = "" }},
		{"empty commit", func(r *domain.CreatePreviewRequest) { r.CommitSHA = "" }},
	}
	for _, tc := range cases {
		// Go 1.22+: per-iteration loop variable; no shadow needed.
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := NewPreviewService(PreviewServiceDeps{Store: &previewFakeStore{}})
			req := newRequest()
			tc.mod(&req)
			if _, err := svc.CreatePreview(context.Background(), req); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestPreviewService_CreatePreview_StoreFailure_Returns(t *testing.T) {
	store := &previewFakeStore{
		createFn: func(_ context.Context, _ db.DBTX, _ *db.PreviewRecord) error {
			return errors.New("db down")
		},
	}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store})
	if _, err := svc.CreatePreview(context.Background(), newRequest()); err == nil {
		t.Fatal("expected error from Store.Create")
	}
}

func TestPreviewService_CreatePreview_BuildFailure_DoesNotFail(t *testing.T) {
	// Build submission failure should not fail the preview create — the
	// preview row exists and the user/operator can retry.
	store := &previewFakeStore{}
	builds := &previewFakeBuilds{submitFn: func(_ context.Context, _ SubmitBuildRequest) (*SubmitBuildResponse, error) {
		return nil, errors.New("argo down")
	}}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store, Builds: builds})

	preview, err := svc.CreatePreview(context.Background(), newRequest())
	if err != nil {
		t.Fatalf("CreatePreview should not fail on build error: %v", err)
	}
	if preview == nil {
		t.Fatal("preview must not be nil")
	}
}

func TestPreviewService_CreatePreview_CRFailure_DoesNotFail(t *testing.T) {
	cr := &previewFakeCR{ensureFn: func(_ context.Context, _ PreviewCRSpec) error {
		return errors.New("k8s down")
	}}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: &previewFakeStore{}, CRCreator: cr})
	if _, err := svc.CreatePreview(context.Background(), newRequest()); err != nil {
		t.Fatalf("CreatePreview should not fail on CR error: %v", err)
	}
}

func TestPreviewService_CreatePreview_NoBuildSubmitter_PreviewStillCreated(t *testing.T) {
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: &previewFakeStore{}})
	preview, err := svc.CreatePreview(context.Background(), newRequest())
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if preview == nil {
		t.Fatal("preview must not be nil")
	}
}

func TestPreviewService_CreatePreview_TTLOverride(t *testing.T) {
	var captured *db.PreviewRecord
	store := &previewFakeStore{
		createFn: func(_ context.Context, _ db.DBTX, rec *db.PreviewRecord) error {
			captured = rec
			return nil
		},
	}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store})
	req := newRequest()
	req.TTLHours = 12
	if _, err := svc.CreatePreview(context.Background(), req); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if captured == nil {
		t.Fatal("Create not invoked")
	}
	expectedDelta := 12 * time.Hour
	delta := time.Until(captured.ExpiresAt)
	// allow ±1s skew
	if delta < expectedDelta-1*time.Second || delta > expectedDelta+1*time.Second {
		t.Errorf("ExpiresAt off: delta=%v, expected %v", delta, expectedDelta)
	}
}

// =============================================================================
// DeletePreviewByPR
// =============================================================================

func TestPreviewService_DeletePreviewByPR_HappyPath(t *testing.T) {
	deletionRequested := false
	crDeleted := false
	store := &previewFakeStore{
		getByPRFn: func(_ context.Context, _, _ string, _ int) (*domain.Preview, error) {
			return &domain.Preview{ID: "prev-x", TenantID: "tn-test-123", ProjectID: "api", PRNumber: 42}, nil
		},
		requestDeletionFn: func(_ context.Context, _, previewID string) error {
			deletionRequested = true
			if previewID != "prev-x" {
				t.Errorf("wrong preview id: %s", previewID)
			}
			return nil
		},
	}
	cr := &previewFakeCR{deleteFn: func(_ context.Context, _, _ string, _ int) error {
		crDeleted = true
		return nil
	}}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store, CRCreator: cr})
	if err := svc.DeletePreviewByPR(context.Background(), "tn-test-123", "api", 42); err != nil {
		t.Fatalf("DeletePreviewByPR: %v", err)
	}
	if !deletionRequested {
		t.Error("Store.RequestDeletion not called")
	}
	if !crDeleted {
		t.Error("CRCreator.DeletePreviewCR not called")
	}
}

func TestPreviewService_DeletePreviewByPR_NoOpWhenNotFound(t *testing.T) {
	// PR closed without a preview should be a no-op (no error).
	store := &previewFakeStore{} // GetByPR returns ErrPreviewNotFound by default
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store})
	if err := svc.DeletePreviewByPR(context.Background(), "tn-test-123", "api", 999); err != nil {
		t.Fatalf("expected no error for missing preview, got: %v", err)
	}
}

// =============================================================================
// OnBuildCompleted
// =============================================================================

func TestPreviewService_OnBuildCompleted_Succeeded_TransitionsActive(t *testing.T) {
	var capturedStatus string
	store := &previewFakeStore{
		updateStatusFn: func(_ context.Context, _ db.DBTX, _, status string) error {
			capturedStatus = status
			return nil
		},
	}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store})
	if err := svc.OnBuildCompleted(context.Background(), "prev-x", "build-1", "succeeded"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if capturedStatus != db.PreviewStatusActive {
		t.Errorf("status = %q, want %q", capturedStatus, db.PreviewStatusActive)
	}
}

func TestPreviewService_OnBuildCompleted_Failed_TransitionsFailed(t *testing.T) {
	var capturedStatus string
	store := &previewFakeStore{
		updateStatusFn: func(_ context.Context, _ db.DBTX, _, status string) error {
			capturedStatus = status
			return nil
		},
	}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store})
	if err := svc.OnBuildCompleted(context.Background(), "prev-x", "build-1", "failed"); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if capturedStatus != db.PreviewStatusFailed {
		t.Errorf("status = %q, want %q", capturedStatus, db.PreviewStatusFailed)
	}
}

func TestPreviewService_OnBuildCompleted_UnknownStatus_Errors(t *testing.T) {
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: &previewFakeStore{}})
	if err := svc.OnBuildCompleted(context.Background(), "prev-x", "build-1", "weird"); err == nil {
		t.Fatal("expected error for unknown status")
	}
}

// =============================================================================
// Compile-time interface assertion
// =============================================================================

func TestPreviewService_Implements_RoutesInterface(t *testing.T) {
	// Verifies domain.PreviewService is implemented (compile-time check).
	var _ domain.PreviewService = (*PreviewService)(nil)
}
