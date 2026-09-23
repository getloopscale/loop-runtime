// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/github"
	ghwebhook "github.com/getloopscale/loopzero/api/internal/github/webhook"
	"github.com/getloopscale/loopzero/api/internal/services/preview"
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

	if err := handle(defaultBranchPush()); !errors.Is(err, dbErr) {
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
	handle := newInstallationHandler(context.Background(), failingInstallations{err: dbErr}, discardLogger)
	for _, action := range []string{"created", "deleted"} {
		var e ghwebhook.InstallationEvent
		e.Action = action
		e.Installation.ID = 42
		if err := handle(e); !errors.Is(err, dbErr) {
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
