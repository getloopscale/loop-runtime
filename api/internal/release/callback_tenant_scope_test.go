// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
)

// The activation and build-failure callbacks (the GitHub commit status) read
// the build through the request pool, where every tenant table is fail-closed
// under Row Level Security. They run detached from the caller — often a
// background loop or a system callback with no tenant scope of its own — so the
// release package scopes them to the tenant the event belongs to.

// awaitScope waits for a callback to report the scope it ran under.
func awaitScope(t *testing.T, got <-chan string) string {
	t.Helper()
	select {
	case scope := <-got:
		return scope
	case <-time.After(5 * time.Second):
		t.Fatal("the callback never ran")
		return ""
	}
}

func TestActivationCallbackRunsScopedToTheReleasesTenant(t *testing.T) {
	got := make(chan string, 1)
	RegisterReleaseActivatedCallback(func(ctx context.Context, _, _, _, _ string) {
		scope, _ := dbtx.TenantScope(ctx)
		got <- scope
	})
	t.Cleanup(func() { RegisterReleaseActivatedCallback(nil) })
	f := newKernelFixture()

	if err := f.activate(context.Background()); err != nil {
		t.Fatalf("activate: %v", err)
	}

	if scope := awaitScope(t, got); scope != f.rel.TenantID {
		t.Fatalf("activation callback scope = %q; want the release's tenant %q", scope, f.rel.TenantID)
	}
}

func TestBuildFailedCallbackRunsScopedToTheBuildsTenant(t *testing.T) {
	got := make(chan string, 1)
	RegisterBuildFailedCallback(func(ctx context.Context, _, _, _, _ string) {
		scope, _ := dbtx.TenantScope(ctx)
		got <- scope
	})
	t.Cleanup(func() { RegisterBuildFailedCallback(nil) })

	NotifyBuildFailed(context.Background(), "tn-failed", "proj", "sha", "b-1")

	if scope := awaitScope(t, got); scope != "tn-failed" {
		t.Fatalf("build-failed callback scope = %q; want tn-failed", scope)
	}
}

// scopeRecordingReleaseStore records the scope the pending-release lookup ran
// under and finds nothing.
type scopeRecordingReleaseStore struct {
	*fakeConvergenceReleaseStore
	scope string
}

func (s *scopeRecordingReleaseStore) FindPendingReleasesByTenantEnv(ctx context.Context, _, _ string) ([]db.Release, error) {
	s.scope, _ = dbtx.TenantScope(ctx)
	return nil, nil
}

// The ArgoCD sync callback is a system entry point, authenticated as the GitOps
// controller: it carries no user and no tenant scope. It acts for the tenant
// the synced Application is labelled with, and its stores run on the request
// pool — unscoped, the pending releases were invisible and no deploy was ever
// activated by a sync.
func TestSyncCallbackReadsPendingReleasesScopedToTheApplicationsTenant(t *testing.T) {
	const appName = "tn-acme-web-production"
	releases := &scopeRecordingReleaseStore{fakeConvergenceReleaseStore: newFakeConvergenceReleaseStore()}
	a := NewSyncCallbackActivator(releases, nil, nil, nil, "", "", slog.Default()).
		WithAppLabelReader(labelsFor(appName, "tn-acme", "production"))

	if err := a.OnSyncCompleted(appName, "rev"); err != nil {
		t.Fatalf("OnSyncCompleted: %v", err)
	}

	if releases.scope != "tn-acme" {
		t.Fatalf("pending releases were read under scope %q; want tn-acme", releases.scope)
	}
}
