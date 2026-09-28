// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package preview

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	dbstore "github.com/getloopscale/loopzero/api/internal/db/store"
	"github.com/getloopscale/loopzero/api/internal/deploycontracts"
	"github.com/getloopscale/loopzero/api/internal/ports"
)

// lockTx is a distinguishable transaction handle handed out by the fake lock.
type lockTx struct{ db.DBTX }

type recordingLocker struct {
	tenant, project, env string
	tx                   db.DBTX
}

func (l *recordingLocker) WithLock(_ context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	l.tenant, l.project, l.env = tenant, project, env
	return fn(l.tx)
}

// The preview row must be written inside the advisory-lock transaction it is
// wrapped in; writing through the pool made the lock serialize nothing.
func TestCreatePreview_PersistsInsideTheLockTransaction(t *testing.T) {
	var gotTx db.DBTX
	store := &previewFakeStore{createFn: func(_ context.Context, tx db.DBTX, rec *dbstore.PreviewRecord) error {
		gotTx = tx
		rec.ID = "prv-1"
		return nil
	}}
	locker := &recordingLocker{tx: &lockTx{}}
	svc, err := NewPreviewService(PreviewServiceDeps{Store: store, Locker: locker})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreatePreview(context.Background(), newRequest()); err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}

	if gotTx != locker.tx {
		t.Errorf("Store.Create got tx %v, want the lock transaction %v", gotTx, locker.tx)
	}
}

// The lock is keyed on the preview environment the build path locks on, not
// on the branch name, so a preview create and its build serialize together.
func TestCreatePreview_LocksOnThePreviewEnvironment(t *testing.T) {
	locker := &recordingLocker{}
	svc, err := NewPreviewService(PreviewServiceDeps{Store: &previewFakeStore{}, Locker: locker})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	req := newRequest()

	if _, err := svc.CreatePreview(context.Background(), req); err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}

	if want := previewEnvName(req.PRNumber); locker.env != want {
		t.Errorf("lock env = %q, want %q", locker.env, want)
	}
	if locker.tenant != req.TenantID() || locker.project != req.ProjectID {
		t.Errorf("lock scope = (%q, %q), want (%q, %q)", locker.tenant, locker.project, req.TenantID(), req.ProjectID)
	}
}

type fakeBuildApps struct {
	apps []deploycontracts.AppSpec
	err  error
}

func (f fakeBuildApps) LatestSucceededApps(context.Context, string, string) ([]deploycontracts.AppSpec, error) {
	return f.apps, f.err
}

// submittedApps runs CreatePreview and returns the apps the build was
// submitted with, and the environment it targeted.
func submittedApps(t *testing.T, req ports.CreatePreviewRequest, lookup PreviewBuildAppsLookup) ([]deploycontracts.AppSpec, string) {
	t.Helper()
	var got deploycontracts.SubmitBuildRequest
	builds := &previewFakeBuilds{submitFn: func(_ context.Context, r deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error) {
		got = r
		return &deploycontracts.SubmitBuildResponse{BuildID: "bld-1"}, nil
	}}
	svc, err := NewPreviewService(PreviewServiceDeps{Store: &previewFakeStore{}, Builds: builds, BuildApps: lookup})
	if err != nil {
		t.Fatalf("init: %v", err)
	}
	if _, err := svc.CreatePreview(context.Background(), req); err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}
	return got.Apps, got.Environment
}

func TestCreatePreview_BuildUsesAppsFromThePRLoopZeroYaml(t *testing.T) {
	req := newRequest()
	req.LoopZeroYamlApps = map[string]map[string]interface{}{"web": {"type": "server", "port": 3000}}

	apps, env := submittedApps(t, req, fakeBuildApps{apps: []deploycontracts.AppSpec{{Name: "stale"}}})

	if len(apps) != 1 || apps[0].Name != "web" || apps[0].Port != 3000 {
		t.Errorf("want the PR loopzero.yaml app, got %+v", apps)
	}
	if env != previewEnvName(req.PRNumber) {
		t.Errorf("build env = %q, want %q", env, previewEnvName(req.PRNumber))
	}
}

func TestCreatePreview_BuildFallsBackToLatestSucceededApps(t *testing.T) {
	apps, _ := submittedApps(t, newRequest(), fakeBuildApps{apps: []deploycontracts.AppSpec{{Name: "api"}}})

	if len(apps) != 1 || apps[0].Name != "api" {
		t.Errorf("want the latest succeeded build's apps, got %+v", apps)
	}
}

func TestCreatePreview_BuildSubmittedWithoutAppsWhenLookupFails(t *testing.T) {
	apps, _ := submittedApps(t, newRequest(), fakeBuildApps{err: errors.New("db down")})

	if len(apps) != 0 {
		t.Errorf("want no apps when the lookup fails, got %+v", apps)
	}
}

func TestCreatePreview_BuildSubmittedWithoutAppsWhenNoPriorBuild(t *testing.T) {
	apps, _ := submittedApps(t, newRequest(), fakeBuildApps{})

	if len(apps) != 0 {
		t.Errorf("want no apps with no prior build, got %+v", apps)
	}
}

func TestCreatePreview_LinksTheSubmittedBuildToThePreview(t *testing.T) {
	var linked string
	store := &previewFakeStore{
		createFn: func(_ context.Context, _ db.DBTX, rec *dbstore.PreviewRecord) error { rec.ID = "prv-7"; return nil },
		updateBuildFn: func(_ context.Context, _ db.DBTX, previewID, buildID, _ string) error {
			linked = previewID + "/" + buildID
			return nil
		},
	}
	svc, err := NewPreviewService(PreviewServiceDeps{Store: store, Builds: &previewFakeBuilds{}})
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	if _, err := svc.CreatePreview(context.Background(), newRequest()); err != nil {
		t.Fatalf("CreatePreview: %v", err)
	}

	if linked != "prv-7/build-fake" {
		t.Errorf("linked = %q, want prv-7/build-fake", linked)
	}
}
