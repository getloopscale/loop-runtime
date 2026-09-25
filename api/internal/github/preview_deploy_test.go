// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/github/webhook"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// =============================================================================
// Fakes
// =============================================================================

type fakePreviewSvc struct {
	createCalls []ports.CreatePreviewRequest
	deleteCalls []deleteCall
	createErr   error
	deleteErr   error
}

type deleteCall struct {
	tenantID  string
	projectID string
	prNumber  int
}

func (f *fakePreviewSvc) CreatePreview(_ context.Context, req ports.CreatePreviewRequest) (*ports.Preview, error) {
	f.createCalls = append(f.createCalls, req)
	if f.createErr != nil {
		return nil, f.createErr
	}
	return &ports.Preview{ID: "prev-z", PRNumber: req.PRNumber, Branch: req.Branch, Status: "creating"}, nil
}

func (f *fakePreviewSvc) DeletePreviewByPR(_ context.Context, tenantID, projectID string, prNumber int) error {
	f.deleteCalls = append(f.deleteCalls, deleteCall{tenantID, projectID, prNumber})
	return f.deleteErr
}

type fakeRepoLookup struct {
	repos    map[string]*ConnectedRepo
	queryErr error
}

func (f *fakeRepoLookup) GetByRepo(_ context.Context, name string) (*ConnectedRepo, error) {
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	r, ok := f.repos[name]
	if !ok {
		return nil, nil
	}
	return r, nil
}

func newPREvent(action string, prNumber int, branch, sha, repoName string) webhook.PullRequestEvent {
	e := webhook.PullRequestEvent{
		Action:     action,
		Number:     prNumber,
		Repository: webhook.Repository{FullName: repoName},
		Installation: struct {
			ID int64 `json:"id"`
		}{ID: 12345},
	}
	e.PullRequest.Head.Ref = branch
	e.PullRequest.Head.SHA = sha
	e.PullRequest.Number = prNumber
	return e
}

func newConnectedRepo(repoName, tenantID, projectName string, autoDeploy bool) map[string]*ConnectedRepo {
	return map[string]*ConnectedRepo{
		repoName: {
			TenantID:    tenantID,
			ProjectName: projectName,
			AutoDeploy:  autoDeploy,
		},
	}
}

// =============================================================================
// Action dispatch
// =============================================================================

func TestPreviewDeployer_HandlePR_Opened_TriggersCreate(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	if err := d.HandlePR(context.Background(), newPREvent("opened", 42, "feat/x", "deadbeef1234", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.createCalls) != 1 {
		t.Fatalf("expected 1 CreatePreview call, got %d", len(svc.createCalls))
	}
	got := svc.createCalls[0]
	if got.TenantID() != "tn-1" {
		t.Errorf("tenant: got %q want tn-1", got.TenantID())
	}
	if got.ProjectID != "api" || got.PRNumber != 42 || got.Branch != "feat/x" || got.CommitSHA != "deadbeef1234" {
		t.Errorf("create req wrong: %+v", got)
	}
	if got.RepoFullName != "acme/api" || got.InstallationID != 12345 {
		t.Errorf("create req github fields wrong: %+v", got)
	}
}

func TestPreviewDeployer_HandlePR_Synchronize_TriggersCreate(t *testing.T) {
	// synchronize must call CreatePreview (UPSERT semantics in service).
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	if err := d.HandlePR(context.Background(), newPREvent("synchronize", 42, "feat/x", "newcommitabc", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.createCalls) != 1 {
		t.Fatalf("expected 1 CreatePreview call, got %d", len(svc.createCalls))
	}
	if svc.createCalls[0].CommitSHA != "newcommitabc" {
		t.Errorf("commit not propagated: %s", svc.createCalls[0].CommitSHA)
	}
}

func TestPreviewDeployer_HandlePR_Reopened_TriggersCreate(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	if err := d.HandlePR(context.Background(), newPREvent("reopened", 7, "feat/x", "abc", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.createCalls) != 1 {
		t.Fatalf("expected 1 CreatePreview call, got %d", len(svc.createCalls))
	}
}

func TestPreviewDeployer_HandlePR_Closed_TriggersDelete(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	if err := d.HandlePR(context.Background(), newPREvent("closed", 42, "", "", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.deleteCalls) != 1 {
		t.Fatalf("expected 1 DeletePreviewByPR call, got %d", len(svc.deleteCalls))
	}
	got := svc.deleteCalls[0]
	if got.tenantID != "tn-1" || got.projectID != "api" || got.prNumber != 42 {
		t.Errorf("delete call wrong: %+v", got)
	}
}

func TestPreviewDeployer_HandlePR_ClosedMerged_StillDeletes(t *testing.T) {
	// Merged PRs MUST also delete the preview (resource cleanup is mandatory).
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	event := newPREvent("closed", 42, "", "", "acme/api")
	event.PullRequest.Merged = true
	if err := d.HandlePR(context.Background(), event); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.deleteCalls) != 1 {
		t.Fatalf("expected 1 delete call (merged should still cleanup), got %d", len(svc.deleteCalls))
	}
}

// =============================================================================
// Guards
// =============================================================================

func TestPreviewDeployer_HandlePR_AutoDeployDisabled_NoOp(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", false)}
	d := NewPreviewDeployer(svc, repos, nil)

	if err := d.HandlePR(context.Background(), newPREvent("opened", 1, "x", "y", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.createCalls) != 0 {
		t.Errorf("auto_deploy=false should skip, got %d calls", len(svc.createCalls))
	}
}

func TestPreviewDeployer_HandlePR_UnknownRepo_NoOp(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: map[string]*ConnectedRepo{}} // empty
	d := NewPreviewDeployer(svc, repos, nil)

	if err := d.HandlePR(context.Background(), newPREvent("opened", 1, "x", "y", "unknown/repo")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.createCalls) != 0 {
		t.Errorf("unknown repo should skip, got %d calls", len(svc.createCalls))
	}
}

func TestPreviewDeployer_HandlePR_RepoStoreError_ReturnsErrorWithoutCreating(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{queryErr: errors.New("db down")}
	d := NewPreviewDeployer(svc, repos, nil)

	// A lookup failure is not "repo not connected": it must surface, so a lost
	// preview is visible instead of reading as an ignored event.
	if err := d.HandlePR(context.Background(), newPREvent("opened", 1, "x", "y", "acme/api")); err == nil {
		t.Fatal("HandlePR returned nil although the repository lookup failed")
	}
	if len(svc.createCalls) != 0 {
		t.Errorf("repo error should skip, got %d calls", len(svc.createCalls))
	}
}

func TestPreviewDeployer_HandlePR_UnknownAction_NoOp(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	for _, action := range []string{"labeled", "assigned", "unassigned", "edited", "review_requested", "ready_for_review"} {
		if err := d.HandlePR(context.Background(), newPREvent(action, 1, "x", "y", "acme/api")); err != nil {
			t.Errorf("action %q unexpected error: %v", action, err)
		}
	}
	if len(svc.createCalls) != 0 || len(svc.deleteCalls) != 0 {
		t.Errorf("unrelated actions should be no-ops, create=%d delete=%d",
			len(svc.createCalls), len(svc.deleteCalls))
	}
}

func TestPreviewDeployer_HandlePR_OpenedMissingBranch_NoOp(t *testing.T) {
	svc := &fakePreviewSvc{}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	// Branch present, commit empty → skip
	if err := d.HandlePR(context.Background(), newPREvent("opened", 1, "feat", "", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	// Branch empty, commit present → skip
	if err := d.HandlePR(context.Background(), newPREvent("opened", 1, "", "abc", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if len(svc.createCalls) != 0 {
		t.Errorf("missing data should skip create, got %d", len(svc.createCalls))
	}
}

// =============================================================================
// Error propagation
// =============================================================================

func TestPreviewDeployer_HandlePR_CreateError_Propagates(t *testing.T) {
	svc := &fakePreviewSvc{createErr: errors.New("svc down")}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	err := d.HandlePR(context.Background(), newPREvent("opened", 1, "x", "y", "acme/api"))
	if err == nil {
		t.Fatal("expected error when service create fails")
	}
}

func TestPreviewDeployer_HandlePR_DeleteError_Propagates(t *testing.T) {
	svc := &fakePreviewSvc{deleteErr: errors.New("svc down")}
	repos := &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}
	d := NewPreviewDeployer(svc, repos, nil)

	err := d.HandlePR(context.Background(), newPREvent("closed", 1, "", "", "acme/api"))
	if err == nil {
		t.Fatal("expected error when service delete fails")
	}
}

// =============================================================================
// Construction
// =============================================================================

func TestPreviewDeployer_Construction_NilLogger_DefaultsToSlogDefault(t *testing.T) {
	d := NewPreviewDeployer(&fakePreviewSvc{}, &fakeRepoLookup{}, nil)
	if d == nil || d.logger == nil {
		t.Fatal("expected default logger when nil provided")
	}
}

func TestPreviewDeployer_NoServiceWired_NoOp(t *testing.T) {
	d := NewPreviewDeployer(nil, &fakeRepoLookup{repos: newConnectedRepo("acme/api", "tn-1", "api", true)}, nil)
	if err := d.HandlePR(context.Background(), newPREvent("opened", 1, "x", "y", "acme/api")); err != nil {
		t.Fatalf("unexpected: %v", err)
	}
}
