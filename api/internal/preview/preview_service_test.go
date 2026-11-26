// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview

import (
	"context"
	"errors"
	"testing"
	"time"

	dbstore "github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/ports"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
)

// =============================================================================
// Fakes
// =============================================================================

type previewFakeStore struct {
	createFn          func(ctx context.Context, tx db.DBTX, rec *dbstore.PreviewRecord) error
	updateStatusFn    func(ctx context.Context, tx db.DBTX, previewID, status string) error
	updateBuildFn     func(ctx context.Context, tx db.DBTX, previewID, buildID, commitSHA string) error
	getByPRFn         func(ctx context.Context, tenantID, projectID string, prNumber int) (*ports.Preview, error)
	requestDeletionFn func(ctx context.Context, tenantID, previewID string) error
}

func (f *previewFakeStore) ListByTenant(_ context.Context, _ string) ([]ports.Preview, error) {
	return nil, nil
}
func (f *previewFakeStore) ListByProject(_ context.Context, _, _ string) ([]ports.Preview, error) {
	return nil, nil
}
func (f *previewFakeStore) GetByID(_ context.Context, _, _ string) (*ports.Preview, error) {
	return nil, loopzeroerrors.ErrPreviewNotFound
}
func (f *previewFakeStore) GetByPR(ctx context.Context, tenantID, projectID string, prNumber int) (*ports.Preview, error) {
	if f.getByPRFn != nil {
		return f.getByPRFn(ctx, tenantID, projectID, prNumber)
	}
	return nil, loopzeroerrors.ErrPreviewNotFound
}
func (f *previewFakeStore) Create(ctx context.Context, tx db.DBTX, rec *dbstore.PreviewRecord) error {
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
	submitFn func(ctx context.Context, req deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error)
}

func (f *previewFakeBuilds) SubmitBuild(ctx context.Context, req deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error) {
	if f.submitFn != nil {
		return f.submitFn(ctx, req)
	}
	return &deploycontracts.SubmitBuildResponse{BuildID: "build-fake"}, nil
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

func newRequest() ports.CreatePreviewRequest {
	r := ports.CreatePreviewRequest{
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
		createFn: func(_ context.Context, _ db.DBTX, rec *dbstore.PreviewRecord) error {
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
	builds := &previewFakeBuilds{submitFn: func(_ context.Context, req deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error) {
		buildSubmitted = true
		if req.TenantID != "tn-test-123" || req.ProjectID != "api" || req.CommitSHA != "abc1234" {
			t.Errorf("build req wrong: %+v", req)
		}
		if req.Environment == "" {
			t.Errorf("build env not set")
		}
		return &deploycontracts.SubmitBuildResponse{BuildID: "build-fake"}, nil
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
		mod  func(*ports.CreatePreviewRequest)
	}{
		{"empty tenant", func(r *ports.CreatePreviewRequest) { r.SetTenantID("") }},
		{"empty project", func(r *ports.CreatePreviewRequest) { r.ProjectID = "" }},
		{"non-positive PR", func(r *ports.CreatePreviewRequest) { r.PRNumber = 0 }},
		{"empty branch", func(r *ports.CreatePreviewRequest) { r.Branch = "" }},
		{"empty commit", func(r *ports.CreatePreviewRequest) { r.CommitSHA = "" }},
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
		createFn: func(_ context.Context, _ db.DBTX, _ *dbstore.PreviewRecord) error {
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
	builds := &previewFakeBuilds{submitFn: func(_ context.Context, _ deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error) {
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
	var captured *dbstore.PreviewRecord
	store := &previewFakeStore{
		createFn: func(_ context.Context, _ db.DBTX, rec *dbstore.PreviewRecord) error {
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
		getByPRFn: func(_ context.Context, _, _ string, _ int) (*ports.Preview, error) {
			return &ports.Preview{ID: "prev-x", TenantID: "tn-test-123", ProjectID: "api", PRNumber: 42}, nil
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

// A lookup failure is not "no preview", even when its text reads like one.
// Only the typed ErrPreviewNotFound may turn a PR close into a no-op;
// matching the message swallowed a store failure and left the preview live.
func TestPreviewService_DeletePreviewByPR_UntypedNotFoundTextPropagates(t *testing.T) {
	store := &previewFakeStore{getByPRFn: func(context.Context, string, string, int) (*ports.Preview, error) {
		return nil, errors.New("scan row: preview environment not found in replica cache")
	}}
	svc, _ := NewPreviewService(PreviewServiceDeps{Store: store})

	err := svc.DeletePreviewByPR(context.Background(), "tn-test-123", "api", 999)

	if err == nil {
		t.Fatal("expected the untyped lookup error to propagate, got nil")
	}
}

// =============================================================================
// Compile-time interface assertion
// =============================================================================

func TestPreviewService_Implements_RoutesInterface(t *testing.T) {
	// Verifies ports.PreviewService is implemented (compile-time check).
	var _ PreviewCreator = (*PreviewService)(nil)
}

// =============================================================================
// J-23 wildcard URL convention (sponsor directive 2026-06-07:
// "Continue with the CF wildcard + Caddy wildcard block now")
// =============================================================================

func TestPreviewURL_CanonicalWildcardConvention(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		public   string
		tenant   string
		project  string
		entry    string
		prNumber int
		want     string
	}{
		{
			name:     "public domain + entry app — canonical clickable URL",
			public:   "preview-dev.getloopscale.app",
			tenant:   "tn-08fa2ecedc3c7e77",
			project:  "sample",
			entry:    "api",
			prNumber: 1,
			want:     "https://tn-08fa2ecedc3c7e77-sample-api-preview-pr-1.preview-dev.getloopscale.app",
		},
		{
			name:     "public empty → falls back to base domain (legacy local)",
			base:     "loopzero.local",
			tenant:   "acme",
			project:  "webapp",
			entry:    "web",
			prNumber: 42,
			want:     "https://acme-webapp-web-preview-pr-42.loopzero.local",
		},
		{
			name:     "no entry app — URL omits app segment (structurally valid, won't match IngressRoute)",
			public:   "preview-dev.getloopscale.app",
			tenant:   "acme",
			project:  "webapp",
			entry:    "",
			prNumber: 7,
			want:     "https://acme-webapp-preview-pr-7.preview-dev.getloopscale.app",
		},
		{
			name:     "both domains empty — empty URL (no surface)",
			tenant:   "acme",
			project:  "webapp",
			entry:    "api",
			prNumber: 1,
			want:     "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := NewPreviewService(PreviewServiceDeps{
				Store:               &previewFakeStore{},
				BaseDomain:          tc.base,
				PreviewPublicDomain: tc.public,
			})
			got := svc.previewURL(tc.tenant, tc.project, tc.entry, tc.prNumber)
			if got != tc.want {
				t.Errorf("previewURL = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEntryAppFromLoopZeroYaml(t *testing.T) {
	cases := []struct {
		name string
		apps map[string]map[string]interface{}
		want string
	}{
		{
			name: "single server app — picked",
			apps: map[string]map[string]interface{}{
				"api": {"type": "server"},
			},
			want: "api",
		},
		{
			name: "server + worker — server wins (workers receive no traffic)",
			apps: map[string]map[string]interface{}{
				"worker": {"type": "worker"},
				"api":    {"type": "server"},
			},
			want: "api",
		},
		{
			name: "untyped app — treated as server",
			apps: map[string]map[string]interface{}{
				"backend": {},
			},
			want: "backend",
		},
		{
			name: "sorted iteration — deterministic across reconciles",
			apps: map[string]map[string]interface{}{
				"zeta":  {"type": "server"},
				"alpha": {"type": "server"},
			},
			want: "alpha",
		},
		{
			name: "all workers — no entry",
			apps: map[string]map[string]interface{}{
				"worker": {"type": "worker"},
			},
			want: "",
		},
		{
			name: "empty map — no entry",
			apps: map[string]map[string]interface{}{},
			want: "",
		},
		{
			name: "nil map — no entry",
			apps: nil,
			want: "",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := entryAppFromLoopZeroYaml(tc.apps)
			if got != tc.want {
				t.Errorf("entryAppFromLoopZeroYaml = %q, want %q", got, tc.want)
			}
		})
	}
}

// =============================================================================
// D2/D3/D6 — single-slot preview model (plan 2026-06-07-preview-as-category)
// =============================================================================

// fakePreviewSlotStore is the minimal dbstore.PreviewSlotStore implementation
// for tests. Records every method invocation so D3 substitute behavior +
// D6 cleanup chain can be asserted with method-by-method granularity.
type fakePreviewSlotStore struct {
	upsertCalls int
	clearCalls  int
	lastUpsert  struct {
		tenantID       string
		projectID      string
		activePRNumber int32
		branch         string
		releaseID      string
		url            string
	}
	lastClear struct {
		tenantID  string
		projectID string
	}
	upsertErr error
	clearErr  error
}

func (f *fakePreviewSlotStore) Get(_ context.Context, _, _ string) (*dbstore.PreviewSlot, error) {
	return nil, nil
}
func (f *fakePreviewSlotStore) ListByTenant(_ context.Context, _ string) ([]dbstore.PreviewSlot, error) {
	return nil, nil
}
func (f *fakePreviewSlotStore) Upsert(_ context.Context, _ db.DBTX, tenantID, projectID string, prNumber int32, branch, releaseID, url string) error {
	f.upsertCalls++
	f.lastUpsert.tenantID = tenantID
	f.lastUpsert.projectID = projectID
	f.lastUpsert.activePRNumber = prNumber
	f.lastUpsert.branch = branch
	f.lastUpsert.releaseID = releaseID
	f.lastUpsert.url = url
	return f.upsertErr
}
func (f *fakePreviewSlotStore) Clear(_ context.Context, _ db.DBTX, tenantID, projectID string) error {
	f.clearCalls++
	f.lastClear.tenantID = tenantID
	f.lastClear.projectID = projectID
	return f.clearErr
}
func (f *fakePreviewSlotStore) EnsureEmpty(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}

// fakeEnvTargetStorePreview is the minimal EnvironmentTargetStore for
// preview lifecycle tests. Only DeleteByKind + SetPreviewDesired need
// to be observable.
type fakeEnvTargetStorePreview struct {
	deleteByKindCalls int
	lastDelete        struct {
		tenantID, projectID, env, kind string
	}
	setPreviewDesiredCalls int
}

func (f *fakeEnvTargetStorePreview) SetDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStorePreview) SetActive(_ context.Context, _ db.DBTX, _, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStorePreview) Get(_ context.Context, _, _, _ string) (*dbstore.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeEnvTargetStorePreview) ListByProject(_ context.Context, _, _ string) ([]dbstore.EnvironmentTarget, error) {
	return nil, nil
}
func (f *fakeEnvTargetStorePreview) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}
func (f *fakeEnvTargetStorePreview) SetPreviewDesired(_ context.Context, _ db.DBTX, _, _, _, _, _, _ string) error {
	f.setPreviewDesiredCalls++
	return nil
}
func (f *fakeEnvTargetStorePreview) CountPermanent(_ context.Context, _ string) (int, error) {
	return 0, nil
}
func (f *fakeEnvTargetStorePreview) ListPermanent(_ context.Context, _ string) ([]string, error) {
	return []string{}, nil
}
func (f *fakeEnvTargetStorePreview) DeleteByKind(_ context.Context, _ db.DBTX, tenantID, projectID, env, kind string) error {
	f.deleteByKindCalls++
	f.lastDelete.tenantID = tenantID
	f.lastDelete.projectID = projectID
	f.lastDelete.env = env
	f.lastDelete.kind = kind
	return nil
}

// TestPreviewService_CreatePreview_D3_UpsertSlotAtomically locks the
// canonical D3 substitute behavior: when a new PR opens FOR THE SAME
// PROJECT, the slot is upserted with the new PR data. Subsequent
// calls with a different PR in the same project replace the previous
// one (ON CONFLICT (tenant_id, project_id) DO UPDATE at the SQL
// layer, but observable here as a second Upsert call with the new
// values for the same (tenant, project) tuple).
//
// 2026-06-07 per-project (migration 000028): substitution requires
// matching project_id. See
// TestPreviewService_CreatePreview_D3_DifferentProjectsDoNotSubstitute
// for the orthogonal invariant.
func TestPreviewService_CreatePreview_D3_UpsertSlotAtomically(t *testing.T) {
	store := &previewFakeStore{}
	slot := &fakePreviewSlotStore{}

	svc, err := NewPreviewService(PreviewServiceDeps{
		Store:      store,
		SlotStore:  slot,
		BaseDomain: "preview.loopzero.app",
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreatePreview(context.Background(), newRequest()); err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}

	if slot.upsertCalls != 1 {
		t.Errorf("SlotStore.Upsert calls = %d, want 1", slot.upsertCalls)
	}
	if slot.lastUpsert.tenantID != "tn-test-123" {
		t.Errorf("upsert tenant = %q, want tn-test-123", slot.lastUpsert.tenantID)
	}
	if slot.lastUpsert.projectID != "api" {
		t.Errorf("upsert project = %q, want api", slot.lastUpsert.projectID)
	}
	if slot.lastUpsert.activePRNumber != 42 {
		t.Errorf("upsert PR = %d, want 42", slot.lastUpsert.activePRNumber)
	}
	if slot.lastUpsert.branch != "feature/x" {
		t.Errorf("upsert branch = %q, want feature/x", slot.lastUpsert.branch)
	}

	// Second PR — substitute. Same tenant + project, different PR.
	req2 := newRequest()
	req2.PRNumber = 99
	req2.Branch = "feature/y"
	if _, err := svc.CreatePreview(context.Background(), req2); err != nil {
		t.Fatalf("CreatePreview #2: %v", err)
	}
	if slot.upsertCalls != 2 {
		t.Errorf("after 2 creates, Upsert calls = %d, want 2", slot.upsertCalls)
	}
	if slot.lastUpsert.activePRNumber != 99 {
		t.Errorf("last upsert PR = %d, want 99 (substitute behavior)", slot.lastUpsert.activePRNumber)
	}
	if slot.lastUpsert.branch != "feature/y" {
		t.Errorf("last upsert branch = %q, want feature/y", slot.lastUpsert.branch)
	}
}

// TestPreviewService_CreatePreview_NilSlotStore_NoOp confirms the
// nil-safe legacy path: when SlotStore is nil, CreatePreview still
// succeeds and Store.Create still runs — only the slot side-effect is
// skipped. Tests that don't exercise the substitute behavior keep
// working without wiring a slot.
func TestPreviewService_CreatePreview_NilSlotStore_NoOp(t *testing.T) {
	storeCalled := false
	store := &previewFakeStore{createFn: func(_ context.Context, _ db.DBTX, _ *dbstore.PreviewRecord) error {
		storeCalled = true
		return nil
	}}

	svc, err := NewPreviewService(PreviewServiceDeps{Store: store})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := svc.CreatePreview(context.Background(), newRequest()); err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}
	if !storeCalled {
		t.Error("Store.Create not called")
	}
}

// TestPreviewService_CreatePreview_D3_DifferentProjectsDoNotSubstitute
// locks the per-project model (migration 000028): when a tenant has
// PRs open in TWO different projects, each project has its OWN slot.
// Opening a PR in project B MUST NOT touch the slot of project A.
//
// This is the canonical fix for the "PR in proj B sobrescreve PR em
// proj A" latent bug that PK=(tenant_id) caused. Without per-project
// PK, a user with two repos would lose previews silently when both
// had open PRs simultaneously.
func TestPreviewService_CreatePreview_D3_DifferentProjectsDoNotSubstitute(t *testing.T) {
	store := &previewFakeStore{}
	slot := &fakePreviewSlotStore{}

	svc, err := NewPreviewService(PreviewServiceDeps{
		Store:      store,
		SlotStore:  slot,
		BaseDomain: "preview.loopzero.app",
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	// PR in project A.
	reqA := newRequest()
	reqA.ProjectID = "api"
	reqA.PRNumber = 100
	reqA.Branch = "feat/api-thing"
	if _, err := svc.CreatePreview(context.Background(), reqA); err != nil {
		t.Fatalf("CreatePreview A: %v", err)
	}

	// PR in project B (same tenant).
	reqB := newRequest()
	reqB.ProjectID = "web"
	reqB.PRNumber = 200
	reqB.Branch = "feat/web-thing"
	if _, err := svc.CreatePreview(context.Background(), reqB); err != nil {
		t.Fatalf("CreatePreview B: %v", err)
	}

	// Both upserts MUST have been called — one per project.
	// The store sees 2 distinct (tenant, project) tuples.
	if slot.upsertCalls != 2 {
		t.Errorf("upsert calls = %d, want 2 (one per project)", slot.upsertCalls)
	}
	// Note: this fake records only the LAST upsert, so we assert on
	// the SECOND call's project — the first call's project is
	// preserved by the real PG store because (tenant, project) is
	// the PK; here we just verify the service routed the project
	// arg correctly to each call.
	if slot.lastUpsert.projectID != "web" {
		t.Errorf("last upsert project = %q, want web", slot.lastUpsert.projectID)
	}
	if slot.lastUpsert.activePRNumber != 200 {
		t.Errorf("last upsert PR = %d, want 200", slot.lastUpsert.activePRNumber)
	}
}

// TestPreviewService_DeletePreviewByPR_D6_CleanupChain locks the D6
// cleanup chain: on PR close, Store.RequestDeletion + SlotStore.Clear
// + EnvTargets.DeleteByKind all fire. Best-effort semantics: store
// failures abort but slot/env_target failures only log.
func TestPreviewService_DeletePreviewByPR_D6_CleanupChain(t *testing.T) {
	store := &previewFakeStore{
		getByPRFn: func(_ context.Context, _, _ string, _ int) (*ports.Preview, error) {
			return &ports.Preview{ID: "prev-1"}, nil
		},
	}
	slot := &fakePreviewSlotStore{}
	envTargets := &fakeEnvTargetStorePreview{}

	svc, err := NewPreviewService(PreviewServiceDeps{
		Store:      store,
		SlotStore:  slot,
		EnvTargets: envTargets,
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	if err := svc.DeletePreviewByPR(context.Background(), "tn-test-123", "api", 42); err != nil {
		t.Fatalf("DeletePreviewByPR: %v", err)
	}
	if slot.clearCalls != 1 {
		t.Errorf("SlotStore.Clear calls = %d, want 1", slot.clearCalls)
	}
	// 2026-06-07 per-project: Clear must receive both tenant + project.
	if slot.lastClear.tenantID != "tn-test-123" || slot.lastClear.projectID != "api" {
		t.Errorf("Clear called with (tenant=%q, project=%q), want (tn-test-123, api)",
			slot.lastClear.tenantID, slot.lastClear.projectID)
	}
	if envTargets.deleteByKindCalls != 1 {
		t.Errorf("EnvTargets.DeleteByKind calls = %d, want 1", envTargets.deleteByKindCalls)
	}
	if envTargets.lastDelete.env != "preview-pr-42" {
		t.Errorf("env name = %q, want preview-pr-42", envTargets.lastDelete.env)
	}
	if envTargets.lastDelete.kind != dbstore.EnvironmentTargetKindPreview {
		t.Errorf("kind = %q, want preview", envTargets.lastDelete.kind)
	}
}

// The public URL the api computes (pkg/naming.PreviewURL) is the one the
// PreviewEnvironment carries, so the operator reports the same URL the
// dashboard shows instead of re-deriving its own.
func TestPreviewService_CreatePreview_PassesThePublicURLToTheCR(t *testing.T) {
	var got PreviewCRSpec
	cr := &previewFakeCR{ensureFn: func(_ context.Context, spec PreviewCRSpec) error {
		got = spec
		return nil
	}}
	svc, err := NewPreviewService(PreviewServiceDeps{
		Store:               &previewFakeStore{},
		CRCreator:           cr,
		PreviewPublicDomain: "preview.example.test",
	})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	preview, err := svc.CreatePreview(context.Background(), newRequest())
	if err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}

	if preview.URL == "" {
		t.Fatal("the preview must carry a public URL")
	}
	if got.PublicURL != preview.URL {
		t.Fatalf("CR PublicURL = %q, want the preview's URL %q", got.PublicURL, preview.URL)
	}
}
