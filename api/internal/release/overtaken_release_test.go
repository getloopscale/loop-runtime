// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// latestOfEnv answers the newest release of an environment.
type latestOfEnv struct{ latest *db.Release }

func (l latestOfEnv) ListByEnv(context.Context, string, string, string, int) ([]db.Release, error) {
	if l.latest == nil {
		return nil, nil
	}
	return []db.Release{*l.latest}, nil
}

// deployFailures collects what the loop reports through the deploy-failed callback.
func deployFailures(t *testing.T) <-chan DeployFailure {
	t.Helper()
	got := make(chan DeployFailure, 1)
	RegisterDeployFailedCallback(func(_ context.Context, failure DeployFailure) { got <- failure })
	t.Cleanup(func() { RegisterDeployFailedCallback(nil) })
	return got
}

func awaitFailure(t *testing.T, got <-chan DeployFailure) DeployFailure {
	t.Helper()
	select {
	case failure := <-got:
		return failure
	case <-time.After(2 * time.Second):
		t.Fatal("the loop reported no deploy failure")
		return DeployFailure{}
	}
}

// Two deploys of one environment a few seconds apart: the second one's
// manifests replace the first one's in the GitOps repository, so the first can
// never see its own revision synced. It is cancelled, not timed out, and the
// Argo CD operation in flight (the second one's) is left alone.
func TestVerifyRelease_OvertakenReleaseIsCancelledWithoutTouchingArgo(t *testing.T) {
	failures := deployFailures(t)
	client := &verifySyncClient{}
	f := newVerifyFixture(client, 2*time.Minute, nil)
	newer := &db.Release{
		ID: "rel-newer", BuildID: "build-newer", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production",
		Status: string(statemachine.ReleasePending), CreatedAt: f.release.CreatedAt.Add(30 * time.Second),
	}
	f.loop.WithLatestReleases(latestOfEnv{latest: newer})

	f.verify()

	if f.status() != string(statemachine.RPEventCancelled) {
		t.Errorf("status event = %q, want Cancelled", f.status())
	}
	if client.triggers != 0 || client.terminates != 0 || client.forces != 0 {
		t.Errorf("an overtaken release must not touch the Application; triggers=%d terminates=%d forces=%d",
			client.triggers, client.terminates, client.forces)
	}
	var event struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(f.events.eventData("deploy.sync_failed"), &event); err != nil {
		t.Fatalf("no deploy.sync_failed event to end the build's stream: %v", err)
	}
	if !strings.Contains(event.Reason, "build-newer") {
		t.Errorf("event reason = %q, want it to name the deploy that replaced this one", event.Reason)
	}
	failure := awaitFailure(t, failures)
	if !failure.Superseded || failure.BuildID != "build-v" {
		t.Errorf("reported failure = %+v, want build-v superseded", failure)
	}
}

func TestVerifyRelease_ReleaseThatIsTheLatestStillConverges(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true}
	f := newVerifyFixture(client, time.Second, nil)
	f.loop.WithLatestReleases(latestOfEnv{latest: f.release})

	f.verify()

	if f.status() != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("status event = %q, want SyncCompleted", f.status())
	}
}

// A newer release that has not pushed its manifests yet has replaced nothing.
func TestVerifyRelease_NewerReleaseNotYetInGitDoesNotOvertake(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true}
	f := newVerifyFixture(client, time.Second, nil)
	f.loop.WithLatestReleases(latestOfEnv{latest: &db.Release{
		ID: "rel-newer", BuildID: "build-newer", Status: string(statemachine.ReleaseReady),
		CreatedAt: f.release.CreatedAt.Add(time.Second),
	}})

	f.verify()

	if f.status() != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("status event = %q, want SyncCompleted", f.status())
	}
}

// A deploy that fails has to be reported, or a push deploy's commit status
// stays pending for good.
func TestVerifyRelease_TimedOutReleaseReportsTheDeployFailure(t *testing.T) {
	failures := deployFailures(t)
	f := newVerifyFixture(&verifySyncClient{}, 10*time.Minute, nil)

	f.verify()

	failure := awaitFailure(t, failures)
	if failure.Superseded || failure.BuildID != "build-v" || failure.TenantID != "tn-1" || failure.ProjectName != "proj-1" {
		t.Errorf("reported failure = %+v, want the failed build of tn-1/proj-1, not superseded", failure)
	}
	if !strings.Contains(failure.Reason, "Sync timeout") {
		t.Errorf("reason = %q, want the sync timeout", failure.Reason)
	}
}
