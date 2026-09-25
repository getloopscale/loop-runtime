// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/dbtx"
	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/github"
	ghwebhook "github.com/getloopscale/loopzero/api/internal/github/webhook"
	"github.com/getloopscale/loopzero/api/internal/ports"
	"github.com/getloopscale/loopzero/api/internal/preview"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

type failingRepoLookup struct{ err error }

func (f failingRepoLookup) GetByRepo(context.Context, string) (*store.ProjectRepository, error) {
	return nil, f.err
}

type unusedDeployer struct{}

func (unusedDeployer) HandlePush(context.Context, github.PushRequest) error {
	return nil
}

func defaultBranchPush() ghwebhook.PushEvent {
	var e ghwebhook.PushEvent
	e.Ref = "refs/heads/main"
	e.Repository.FullName = "acme/web"
	e.Repository.DefaultBranch = "main"
	e.After = "0123456789abcdef0123456789abcdef01234567"
	return e
}

// A database failure while resolving the linked project is a failed delivery,
// not a push that was handled.
func TestPushHandler_ReportsAFailedProjectLookup(t *testing.T) {
	dbErr := errors.New("connection refused")
	handle := newPushHandler(context.Background(), failingRepoLookup{err: dbErr}, unusedDeployer{}, discardLogger)

	if err := handle(context.Background(), defaultBranchPush()); !errors.Is(err, dbErr) {
		t.Fatalf("want the lookup error, got %v", err)
	}
}

type failingInstallations struct{ err error }

func (f failingInstallations) SaveInstallation(context.Context, int64, string, string, *string) error {
	return f.err
}
func (f failingInstallations) DeleteInstallation(context.Context, int64) error { return f.err }

func TestInstallationHandler_ReportsAFailedSaveAndDelete(t *testing.T) {
	dbErr := errors.New("connection refused")
	handle := newInstallationHandler(failingInstallations{err: dbErr}, discardLogger)
	for _, action := range []string{"created", "deleted"} {
		var e ghwebhook.InstallationEvent
		e.Action = action
		e.Installation.ID = 42
		if err := handle(context.Background(), e); !errors.Is(err, dbErr) {
			t.Errorf("action %s: want the store error, got %v", action, err)
		}
	}
}

// When the preview service cannot be built the caller must see a nil interface;
// a typed nil *PreviewService passes the deployer's nil check and panics later.
func TestNewPreviewLifecycle_FailureIsAnUntypedNil(t *testing.T) {
	svc, err := newPreviewLifecycle(preview.PreviewServiceDeps{})
	if err == nil {
		t.Fatal("a preview service with no store must not be built")
	}
	if svc != nil {
		t.Fatalf("want a nil interface on failure, got %#v", svc)
	}
}

// scopeRecordingPreviews records the database scope each preview call ran under.
type scopeRecordingPreviews struct{ scopes []string }

func (p *scopeRecordingPreviews) record(ctx context.Context) {
	scope, _ := dbtx.TenantScope(ctx)
	p.scopes = append(p.scopes, scope)
}

func (p *scopeRecordingPreviews) CreatePreview(ctx context.Context, _ ports.CreatePreviewRequest) (*ports.Preview, error) {
	p.record(ctx)
	return &ports.Preview{}, nil
}

func (p *scopeRecordingPreviews) DeletePreviewByPR(ctx context.Context, _, _ string, _ int) error {
	p.record(ctx)
	return nil
}

// A pull request is delivered by GitHub with no user and no tenant scope; the
// tenant is the one the repository is linked to. The preview service's stores
// run on the request pool, whose tenant tables are fail-closed under Row Level
// Security, so creating and tearing down a preview must act in that tenant.
func TestPreviewLifecycleActsScopedToThePreviewsTenant(t *testing.T) {
	inner := &scopeRecordingPreviews{}
	previews := tenantScopedPreviews{inner: inner}
	var req ports.CreatePreviewRequest
	req.SetTenantID("tn-linked")

	if _, err := previews.CreatePreview(context.Background(), req); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := previews.DeletePreviewByPR(context.Background(), "tn-linked", "api", 42); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if len(inner.scopes) != 2 || inner.scopes[0] != "tn-linked" || inner.scopes[1] != "tn-linked" {
		t.Fatalf("preview calls ran under scopes %q; want tn-linked for both", inner.scopes)
	}
}
