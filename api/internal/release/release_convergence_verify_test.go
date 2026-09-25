// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// Basis-path tests for ReleaseConvergenceLoop.verifyRelease. They call it
// synchronously, so no test waits on a goroutine.

// verifySyncClient is a scriptable SyncClient that records every call.
type verifySyncClient struct {
	synced, healthy bool
	statusErr       error
	revision        string
	revisionErr     error
	triggerErr      error
	forceErr        error

	triggers, terminates, forces, revisionReads int
	deployedApps                                []string
}

func (c *verifySyncClient) TriggerSync(context.Context, string) error {
	c.triggers++
	return c.triggerErr
}
func (c *verifySyncClient) WaitForSync(context.Context, string, time.Duration, []string) error {
	return nil
}
func (c *verifySyncClient) GetAppStatus(_ context.Context, _ string, deployed []string) (bool, bool, error) {
	c.deployedApps = deployed
	return c.synced, c.healthy, c.statusErr
}
func (c *verifySyncClient) ForceReconcile(context.Context, string) error {
	c.forces++
	return c.forceErr
}
func (c *verifySyncClient) GetSyncRevision(context.Context, string) (string, error) {
	c.revisionReads++
	return c.revision, c.revisionErr
}
func (c *verifySyncClient) TerminateOperation(context.Context, string) error {
	c.terminates++
	return nil
}

type verifyFixture struct {
	loop    *ReleaseConvergenceLoop
	store   *fakeConvergenceReleaseStore
	events  *fakeConvergenceEventStore
	release *db.Release
}

func newVerifyFixture(client *verifySyncClient, age time.Duration, mutate func(*db.Release)) verifyFixture {
	rel := &db.Release{
		ID: "rel-v", BuildID: "build-v", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleasePending),
		CreatedAt: time.Now().Add(-age),
		Apps:      json.RawMessage(`[{"name":"api","rendering":"container"}]`),
	}
	if mutate != nil {
		mutate(rel)
	}
	rs := newFakeConvergenceReleaseStore()
	rs.releases[rel.ID] = rel
	es := &fakeConvergenceEventStore{}
	loop := NewReleaseConvergenceLoop(rs, newFakeConvergenceEnvTargetStore(), client,
		&fakeConvergenceLocker{}, es, "example.com", "", slog.Default(),
		ReleaseConvergenceLoopConfig{SyncTimeout: 5 * time.Minute})
	return verifyFixture{loop: loop, store: rs, events: es, release: rel}
}

func (f verifyFixture) verify() { f.loop.verifyRelease(context.Background(), f.release) }

func (f verifyFixture) status() string { return f.store.getStatus(f.release.ID) }

func exhaustRetriggers(f verifyFixture) *atomic.Int32 {
	counter := new(atomic.Int32)
	counter.Store(int32(maxSyncRetriggers))
	f.loop.syncRetriggers.Store(f.release.ID, counter)
	return counter
}

func TestVerifyRelease_TimedOutReleaseIsTerminatedAndFailed(t *testing.T) {
	client := &verifySyncClient{}
	f := newVerifyFixture(client, 10*time.Minute, nil)

	f.verify()

	if client.terminates != 1 {
		t.Errorf("terminates = %d, want 1", client.terminates)
	}
	if f.status() != string(statemachine.RPEventSyncFailed) {
		t.Errorf("status event = %q, want SyncFailed", f.status())
	}
	if !f.events.hasEvent("deploy.sync_failed") {
		t.Error("a timed-out release must emit deploy.sync_failed")
	}
}

func TestVerifyRelease_TimedOutReleaseWithoutProjectIsLeftAlone(t *testing.T) {
	client := &verifySyncClient{}
	f := newVerifyFixture(client, 10*time.Minute, func(r *db.Release) { r.ProjectID = "" })

	f.verify()

	if client.terminates != 0 || f.status() != "" {
		t.Errorf("a release whose application cannot be named must not be terminated or failed; terminates=%d status=%q",
			client.terminates, f.status())
	}
}

func TestVerifyRelease_ReleaseWithoutProjectIsNotChecked(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true}
	f := newVerifyFixture(client, time.Minute, func(r *db.Release) { r.ProjectID = "" })

	f.verify()

	if client.deployedApps != nil || f.status() != "" {
		t.Error("a release whose application cannot be named must not be checked or activated")
	}
}

func TestVerifyRelease_StaticAppsAreExcludedFromTheHealthFilter(t *testing.T) {
	client := &verifySyncClient{}
	f := newVerifyFixture(client, time.Second, func(r *db.Release) {
		r.Apps = json.RawMessage(`[{"name":"api","rendering":"container"},{"name":"site","rendering":"static"}]`)
	})

	f.verify()

	if fmt.Sprint(client.deployedApps) != "[proj-1-api]" {
		t.Errorf("deployed apps = %v, want only the project-scoped container app", client.deployedApps)
	}
}

func TestVerifyRelease_UnparseableAppSpecsStillActivateAHealthyRelease(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true}
	f := newVerifyFixture(client, time.Second, func(r *db.Release) { r.Apps = json.RawMessage(`not-json`) })

	f.verify()

	if f.status() != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("status event = %q, want SyncCompleted", f.status())
	}
}

func TestVerifyRelease_StatusCheckErrorLeavesTheReleasePending(t *testing.T) {
	client := &verifySyncClient{statusErr: errors.New("argocd unreachable")}
	f := newVerifyFixture(client, time.Minute, nil)

	f.verify()

	if f.status() != "" || client.triggers != 0 {
		t.Errorf("a failed status check must retry next tick; status=%q triggers=%d", f.status(), client.triggers)
	}
}

func TestVerifyRelease_YoungUnsyncedReleaseIsNotRetriggered(t *testing.T) {
	client := &verifySyncClient{}
	f := newVerifyFixture(client, 5*time.Second, nil)

	f.verify()

	if client.triggers != 0 {
		t.Errorf("triggers = %d, want 0 before the 30s grace period", client.triggers)
	}
}

func TestVerifyRelease_UnsyncedReleasePastGraceIsRetriggered(t *testing.T) {
	client := &verifySyncClient{}
	f := newVerifyFixture(client, time.Minute, nil)

	f.verify()

	if client.triggers != 1 {
		t.Errorf("triggers = %d, want 1", client.triggers)
	}
}

func TestVerifyRelease_SyncedButUnhealthyReleaseIsNotRetriggered(t *testing.T) {
	client := &verifySyncClient{synced: true}
	f := newVerifyFixture(client, time.Minute, nil)

	f.verify()

	if client.triggers != 0 || f.status() != "" {
		t.Errorf("a synced but unhealthy release waits; triggers=%d status=%q", client.triggers, f.status())
	}
}

func TestVerifyRelease_ExhaustedRetriggersPastTwoMinutesForceReconcileAndResetTheCounter(t *testing.T) {
	client := &verifySyncClient{}
	f := newVerifyFixture(client, 3*time.Minute, nil)
	counter := exhaustRetriggers(f)

	f.verify()

	if client.forces != 1 {
		t.Errorf("forces = %d, want 1", client.forces)
	}
	if counter.Load() != 0 {
		t.Errorf("retrigger counter = %d, want reset to 0 after a successful ForceReconcile", counter.Load())
	}
}

func TestVerifyRelease_FailedForceReconcileKeepsTheCounterExhausted(t *testing.T) {
	client := &verifySyncClient{forceErr: errors.New("patch refused")}
	f := newVerifyFixture(client, 3*time.Minute, nil)
	counter := exhaustRetriggers(f)

	f.verify()

	if counter.Load() != int32(maxSyncRetriggers) {
		t.Errorf("retrigger counter = %d, want it left exhausted so the next tick escalates again", counter.Load())
	}
}

func TestVerifyRelease_ExhaustedRetriggersBeforeTwoMinutesDoNotForceReconcile(t *testing.T) {
	client := &verifySyncClient{}
	f := newVerifyFixture(client, time.Minute, nil)
	exhaustRetriggers(f)

	f.verify()

	if client.forces != 0 {
		t.Errorf("forces = %d, want 0 before the 2-minute escalation threshold", client.forces)
	}
}

func TestVerifyRelease_ReleaseWithoutCommitActivatesWithoutARevisionCheck(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true}
	f := newVerifyFixture(client, time.Second, nil)

	f.verify()

	if client.revisionReads != 0 || f.status() != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("revision reads=%d status=%q", client.revisionReads, f.status())
	}
}

func TestVerifyRelease_RevisionReadErrorLeavesTheReleasePending(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true, revisionErr: errors.New("timeout")}
	f := newVerifyFixture(client, time.Second, func(r *db.Release) { r.GitCommit = "sha-new" })

	f.verify()

	if f.status() != "" {
		t.Errorf("status event = %q, want none", f.status())
	}
}

func TestVerifyRelease_RevisionMismatchRetriggersInsteadOfActivating(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true, revision: "sha-old"}
	f := newVerifyFixture(client, time.Second, func(r *db.Release) { r.GitCommit = "sha-new" })

	f.verify()

	if f.status() != "" || client.triggers != 1 {
		t.Errorf("a stale revision must retrigger, not activate; status=%q triggers=%d", f.status(), client.triggers)
	}
}

func TestVerifyRelease_UnknownRevisionActivates(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true, revision: ""}
	f := newVerifyFixture(client, time.Second, func(r *db.Release) { r.GitCommit = "sha-new" })

	f.verify()

	if f.status() != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("status event = %q, want SyncCompleted", f.status())
	}
}

func TestVerifyRelease_MatchingRevisionActivates(t *testing.T) {
	client := &verifySyncClient{synced: true, healthy: true, revision: "sha-new"}
	f := newVerifyFixture(client, time.Second, func(r *db.Release) { r.GitCommit = "sha-new" })

	f.verify()

	if f.status() != string(statemachine.ReleaseSyncCompleted) {
		t.Errorf("status event = %q, want SyncCompleted", f.status())
	}
}
