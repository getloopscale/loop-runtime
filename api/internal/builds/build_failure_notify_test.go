// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package builds

import (
	"context"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/release"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

type failedBuildNotice struct{ tenant, project, sha, build string }

func captureBuildFailedNotices(t *testing.T) <-chan failedBuildNotice {
	t.Helper()
	notices := make(chan failedBuildNotice, 4)
	release.RegisterBuildFailedCallback(func(_ context.Context, tenant, project, sha, build string) {
		notices <- failedBuildNotice{tenant, project, sha, build}
	})
	t.Cleanup(func() { release.RegisterBuildFailedCallback(nil) })
	return notices
}

// A push deploy marks its commit "pending" on GitHub, and only the build-failed
// callback turns that into "failure". When the finalizer stopped calling it, a
// failed push deploy left the commit pending forever.
func TestFailingABuildTellsTheBuildFailedCallbackAboutItsCommit(t *testing.T) {
	notices := captureBuildFailedNotices(t)
	bs := newFakeBuildStoreOrch()
	bs.builds["b1"] = &db.Build{ID: "b1", TenantID: "tn-1", ProjectID: "hello", CommitSHA: "0bfe7b5", Status: string(statemachine.BPhRunning)}

	if _, err := finalizerOver(bs, &fakeEventStoreContainer{}).fail(context.Background(), bs.builds["b1"],
		buildFailure{reason: "boom", route: routeToFailed, stageDeployFailed: true}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-notices:
		want := failedBuildNotice{"tn-1", "hello", "0bfe7b5", "b1"}
		if got != want {
			t.Errorf("callback got %+v, want %+v", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the build-failed callback was not called")
	}
}

func TestABuildAlreadyFinalDoesNotNotifyAFailure(t *testing.T) {
	notices := captureBuildFailedNotices(t)
	bs := newFakeBuildStoreOrch()
	bs.builds["b1"] = &db.Build{ID: "b1", CommitSHA: "0bfe7b5", Status: string(statemachine.BPhSucceeded)}

	if _, err := finalizerOver(bs, &fakeEventStoreContainer{}).fail(context.Background(), bs.builds["b1"],
		buildFailure{reason: "late", route: routeToFailed, stageDeployFailed: true}); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-notices:
		t.Errorf("a build that was already final notified a failure: %+v", got)
	case <-time.After(200 * time.Millisecond):
	}
}
