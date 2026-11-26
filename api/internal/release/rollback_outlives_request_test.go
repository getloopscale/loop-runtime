// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// A release rolls its pods through canary steps, so the sync of a rollback
// takes longer than an HTTP request lives. These tests pin what the rollback
// owes the environment once the request is gone.

// slowSyncDelivery delivers at once and confirms the sync only when the test
// says so. It records the state of the context each step ran under.
type slowSyncDelivery struct {
	mu sync.Mutex

	startErr  error
	awaitErr  error
	converged bool
	// convergedErr fails the convergence check itself.
	convergedErr error

	release     chan struct{} // closed by the test to let the sync wait return
	awaiting    chan struct{} // closed when the sync wait began
	startCtxErr error
	awaitCtxErr error
	starts      int
	// delivered lists, in order, the release each delivery re-delivered.
	delivered []string
	// restoreErr fails every delivery after the first: the restore of the
	// release kept active.
	restoreErr error
	awaitOnce  sync.Once
}

func newSlowSyncDelivery() *slowSyncDelivery {
	return &slowSyncDelivery{release: make(chan struct{}), awaiting: make(chan struct{})}
}

func (d *slowSyncDelivery) Activate(context.Context, deploycontracts.ActivationBuild, deploycontracts.BuildCallbackRequest) error {
	return nil
}

func (d *slowSyncDelivery) Rollback(ctx context.Context, req deploycontracts.RollbackDeliveryRequest) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.starts++
	d.delivered = append(d.delivered, req.TargetRelease.ID)
	if d.starts > 1 {
		return d.restoreErr
	}
	d.startCtxErr = ctx.Err()
	return d.startErr
}

func (d *slowSyncDelivery) deliveries() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.delivered...)
}

func (d *slowSyncDelivery) AwaitRollback(ctx context.Context, _ deploycontracts.RollbackDeliveryRequest) error {
	d.awaitOnce.Do(func() { close(d.awaiting) })
	select {
	case <-d.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.awaitCtxErr = ctx.Err()
	return d.awaitErr
}

func (d *slowSyncDelivery) RollbackConverged(context.Context, deploycontracts.RollbackDeliveryRequest) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.converged, d.convergedErr
}

type rollbackFixture struct {
	orch     *RollbackOrchestrator
	releases *fakeRollbackReleaseStore
	targets  *fakeRollbackEnvTargetStore
	delivery *slowSyncDelivery
}

const fixtureEnvKey = "tn-1-proj-1-production"

func newRollbackFixture(t *testing.T) *rollbackFixture {
	t.Helper()
	rs := newFakeRollbackReleaseStore()
	current := &db.Release{
		ID: "rel-current", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive), Apps: json.RawMessage(`[]`),
	}
	target := &db.Release{
		ID: "rel-target", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "abc123", Apps: json.RawMessage(`[]`),
	}
	rs.releases["rel-current"], rs.releases["rel-target"] = current, target
	rs.activeByEnv[fixtureEnvKey] = current
	rs.previousByEnv[fixtureEnvKey] = target
	f := &rollbackFixture{releases: rs, targets: newFakeRollbackEnvTargetStore(), delivery: newSlowSyncDelivery()}
	f.orch = NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases: rs, EnvTargets: f.targets, Locker: &fakeRollbackLocker{},
		Delivery: f.delivery, Logger: slog.Default(),
	})
	return f
}

var fixtureRequest = RollbackRequest{TenantID: "tn-1", ProjectID: "proj-1", Environment: "production", UserID: "user-1"}

// activeReleases lists the releases of the fixture that are ACTIVE.
func (f *rollbackFixture) activeReleases() []string {
	var active []string
	for _, id := range []string{"rel-current", "rel-target"} {
		if f.releases.status(id) == string(statemachine.ReleaseActive) {
			active = append(active, id)
		}
	}
	return active
}

func (f *rollbackFixture) waitForTheSyncWaitToBegin(t *testing.T) {
	t.Helper()
	select {
	case <-f.delivery.awaiting:
	case <-time.After(5 * time.Second):
		t.Fatal("the rollback never began to wait for the sync")
	}
}

func (f *rollbackFixture) waitForCompletion(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() { f.orch.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the rollback did not complete")
	}
}

func TestRollback_AnswersOnceTheSyncIsTriggeredNotOnceItIsConfirmed(t *testing.T) {
	f := newRollbackFixture(t)

	type answer struct {
		result *RollbackResult
		err    error
	}
	answered := make(chan answer, 1)
	go func() {
		result, err := f.orch.Rollback(context.Background(), fixtureRequest)
		answered <- answer{result, err}
	}()

	select {
	case a := <-answered:
		if a.err != nil {
			t.Fatalf("Rollback: %v", a.err)
		}
		if a.result.ReactivatedRelease.ID != "rel-target" || a.result.RolledBackRelease.ID != "rel-current" {
			t.Errorf("result = %+v, want the target and the release being rolled back", a.result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Rollback did not answer while the sync was still unconfirmed")
	}
	if got := f.releases.status("rel-current"); got != string(statemachine.ReleaseRollingBack) {
		t.Errorf("while the sync is unconfirmed the current release is %s, want ROLLING_BACK", got)
	}

	close(f.delivery.release)
	f.waitForCompletion(t)
}

// The issue's case: the request ends while the cells are still rolling, and
// the rollback must finish all the same.
func TestRollback_ASyncWaitLongerThanTheRequestStillEndsWithTheTargetActive(t *testing.T) {
	f := newRollbackFixture(t)
	requestCtx, endRequest := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer endRequest()

	if _, err := f.orch.Rollback(requestCtx, fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForTheSyncWaitToBegin(t)
	<-requestCtx.Done() // the request's deadline passes while the sync wait runs
	close(f.delivery.release)
	f.waitForCompletion(t)

	if active := f.activeReleases(); len(active) != 1 || active[0] != "rel-target" {
		t.Fatalf("active releases = %v, want only rel-target", active)
	}
	if got := f.releases.status("rel-current"); got != string(statemachine.ReleaseRolledBack) {
		t.Errorf("the release rolled back from is %s, want ROLLED_BACK", got)
	}
	if f.targets.active[fixtureEnvKey] != "rel-target" || f.targets.desired[fixtureEnvKey] != "rel-target" {
		t.Errorf("environment target: active=%q desired=%q, want rel-target for both",
			f.targets.active[fixtureEnvKey], f.targets.desired[fixtureEnvKey])
	}
}

func TestRollback_ACancelledRequestDoesNotCancelDelivery(t *testing.T) {
	f := newRollbackFixture(t)
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	// The client goes away while the environment lock is held, before the
	// manifests are pushed.
	f.orch.deps.Locker = lockerFunc(func(fn func(tx db.DBTX) error) error {
		err := fn(nil)
		cancelRequest()
		return err
	})

	if _, err := f.orch.Rollback(requestCtx, fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForTheSyncWaitToBegin(t)
	close(f.delivery.release)
	f.waitForCompletion(t)

	if f.delivery.startCtxErr != nil {
		t.Errorf("the manifests were pushed under a cancelled context: %v", f.delivery.startCtxErr)
	}
	if f.delivery.awaitCtxErr != nil {
		t.Errorf("the sync wait ran under a cancelled context: %v", f.delivery.awaitCtxErr)
	}
	if active := f.activeReleases(); len(active) != 1 || active[0] != "rel-target" {
		t.Errorf("active releases = %v, want only rel-target", active)
	}
}

func TestRollback_AFailedSyncWaitKeepsThePreviousReleaseActive(t *testing.T) {
	f := newRollbackFixture(t)
	f.delivery.awaitErr = errors.New("argocd wait: timeout after 5m0s")
	close(f.delivery.release)

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForCompletion(t)

	if active := f.activeReleases(); len(active) != 1 || active[0] != "rel-current" {
		t.Fatalf("active releases = %v, want only rel-current: the cluster did not converge", active)
	}
	if got := f.releases.status("rel-target"); got != string(statemachine.ReleaseSuperseded) {
		t.Errorf("the target is %s, want it left SUPERSEDED", got)
	}
	if _, moved := f.targets.active[fixtureEnvKey]; moved {
		t.Errorf("the environment target moved to %q although the rollback failed", f.targets.active[fixtureEnvKey])
	}
	failure, _ := f.releases.sideEffects["rel-current"]["rollback_failure"].(map[string]any)
	if failure == nil || failure["target_release"] != "rel-target" || failure["reason"] != "argocd wait: timeout after 5m0s" {
		t.Errorf("recorded failure = %v, want the target and the reason on the release that stays active", failure)
	}
}

// The wait can fail although the cells finished: ArgoCD answered late, or the
// API lost its connection. What the cluster runs decides.
func TestRollback_AFailedSyncWaitActivatesTheTargetWhenTheClusterConverged(t *testing.T) {
	f := newRollbackFixture(t)
	f.delivery.awaitErr = errors.New("argocd wait: connection reset")
	f.delivery.converged = true
	close(f.delivery.release)

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForCompletion(t)

	if active := f.activeReleases(); len(active) != 1 || active[0] != "rel-target" {
		t.Fatalf("active releases = %v, want only rel-target: the cluster runs it", active)
	}
}

func TestRollback_AnUnknownClusterStateAfterAFailedWaitKeepsThePreviousReleaseActive(t *testing.T) {
	f := newRollbackFixture(t)
	f.delivery.awaitErr = errors.New("argocd wait: connection reset")
	f.delivery.convergedErr = errors.New("argocd: connection refused")
	close(f.delivery.release)

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForCompletion(t)

	if active := f.activeReleases(); len(active) != 1 || active[0] != "rel-current" {
		t.Fatalf("active releases = %v, want only rel-current", active)
	}
}

func TestRollback_ASyncWaitPastItsOwnDeadlineKeepsOneActiveRelease(t *testing.T) {
	f := newRollbackFixture(t)
	f.orch.deps.CompletionTimeout = 30 * time.Millisecond // the sync is never confirmed

	if _, err := f.orch.Rollback(context.Background(), fixtureRequest); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	f.waitForCompletion(t)

	if active := f.activeReleases(); len(active) != 1 || active[0] != "rel-current" {
		t.Fatalf("active releases = %v, want only rel-current after the wait timed out", active)
	}
}

func TestRollback_AFailedPushKeepsThePreviousReleaseActiveAndIsReported(t *testing.T) {
	f := newRollbackFixture(t)
	f.delivery.startErr = errors.New("container rollback: push manifests: gitea down")

	_, err := f.orch.Rollback(context.Background(), fixtureRequest)
	f.waitForCompletion(t)

	if err == nil {
		t.Fatal("Rollback answered success although the manifests were not pushed")
	}
	if active := f.activeReleases(); len(active) != 1 || active[0] != "rel-current" {
		t.Fatalf("active releases = %v, want only rel-current", active)
	}
}

// lockerFunc adapts a function to the environment locker.
type lockerFunc func(fn func(tx db.DBTX) error) error

func (l lockerFunc) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return l(fn)
}
