// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// Failure paths of the activation kernel. Every step before the outbox is
// fatal and must stop the sequence; deploy.completed is retried because the
// CLI waits on it.

var errKernelStep = errors.New("injected store failure")

type kernelReleaseStore struct {
	*fakeConvergenceReleaseStore
	refetchErr, previousErr error
	failUpdateFor           string
}

func (s *kernelReleaseStore) GetByIDTx(ctx context.Context, tx db.DBTX, id string) (*db.Release, error) {
	if s.refetchErr != nil {
		return nil, s.refetchErr
	}
	return s.fakeConvergenceReleaseStore.GetByIDTx(ctx, tx, id)
}

func (s *kernelReleaseStore) GetActiveByEnvTx(ctx context.Context, tx db.DBTX, t, p, e string) (*db.Release, error) {
	if s.previousErr != nil {
		return nil, s.previousErr
	}
	return s.fakeConvergenceReleaseStore.GetActiveByEnvTx(ctx, tx, t, p, e)
}

func (s *kernelReleaseStore) UpdateStatus(ctx context.Context, tx db.DBTX, id, event string) (*db.Release, error) {
	if id == s.failUpdateFor {
		return nil, errKernelStep
	}
	return s.fakeConvergenceReleaseStore.UpdateStatus(ctx, tx, id, event)
}

type kernelEnvTargetStore struct {
	*fakeConvergenceEnvTargetStore
	setActiveErr error
}

func (s *kernelEnvTargetStore) SetActive(ctx context.Context, tx db.DBTX, t, p, e, rel string) error {
	if s.setActiveErr != nil {
		return s.setActiveErr
	}
	return s.fakeConvergenceEnvTargetStore.SetActive(ctx, tx, t, p, e, rel)
}

type failingDeployEventStore struct{}

func (failingDeployEventStore) AppendDeployEvent(context.Context, db.DBTX, store.DeployEvent) error {
	return errKernelStep
}

// flakyCompletedEventStore fails the first failures deploy.completed appends.
type flakyCompletedEventStore struct {
	failures, completedAttempts int
	onFailure                   func()
}

func (s *flakyCompletedEventStore) Append(_ context.Context, _ string, eventType string, _ json.RawMessage) (int64, error) {
	if eventType != "deploy.completed" {
		return 1, nil
	}
	s.completedAttempts++
	if s.completedAttempts <= s.failures {
		if s.onFailure != nil {
			s.onFailure()
		}
		return 0, errKernelStep
	}
	return 1, nil
}

type kernelFixture struct {
	rel      db.Release
	releases *kernelReleaseStore
	targets  *kernelEnvTargetStore
	events   ReleaseActivationEventStore
	outbox   store.DeployEventStore
}

func newKernelFixture() *kernelFixture {
	rel := pendingReleaseFixture()
	releases := &kernelReleaseStore{fakeConvergenceReleaseStore: newFakeConvergenceReleaseStore()}
	releases.releases[rel.ID] = &rel
	return &kernelFixture{
		rel:      rel,
		releases: releases,
		targets:  &kernelEnvTargetStore{fakeConvergenceEnvTargetStore: newFakeConvergenceEnvTargetStore()},
		events:   &fakeConvergenceEventStore{},
	}
}

func (f *kernelFixture) withPreviousActive() string {
	prev := pendingReleaseFixture()
	prev.ID = "rel-prev000000000000"
	prev.Status = string(statemachine.RPhActive)
	f.releases.activeByEnv[f.rel.TenantID+"-"+f.rel.ProjectID+"-"+f.rel.Environment] = &prev
	return prev.ID
}

func (f *kernelFixture) activate(ctx context.Context) error {
	return ActivationKernel{
		Releases: f.releases, EnvTargets: f.targets, Events: f.events, DeployEvents: f.outbox, Logger: slog.Default(),
	}.Activate(ctx, nil, &f.rel, []ActivationAppInfo{{Name: "web", Status: "Healthy"}}, 3)
}

func TestActivationKernel_RefetchFailure_StopsBeforeAnyTransition(t *testing.T) {
	f := newKernelFixture()
	f.releases.refetchErr = errKernelStep

	err := f.activate(context.Background())

	if !errors.Is(err, errKernelStep) {
		t.Fatalf("expected the re-fetch error, got %v", err)
	}
	if len(f.releases.lastStatus) != 0 {
		t.Errorf("no transition may run, got %v", f.releases.lastStatus)
	}
}

func TestActivationKernel_PreviousLookupFailure_StopsBeforeAnyTransition(t *testing.T) {
	f := newKernelFixture()
	f.releases.previousErr = errKernelStep

	err := f.activate(context.Background())

	if !errors.Is(err, errKernelStep) {
		t.Fatalf("expected the previous-release lookup error, got %v", err)
	}
	if len(f.releases.lastStatus) != 0 {
		t.Errorf("no transition may run, got %v", f.releases.lastStatus)
	}
}

func TestActivationKernel_SupersedeFailure_LeavesNewReleaseNotActive(t *testing.T) {
	f := newKernelFixture()
	prevID := f.withPreviousActive()
	f.releases.failUpdateFor = prevID

	err := f.activate(context.Background())

	if !errors.Is(err, errKernelStep) || !strings.Contains(err.Error(), "supersede previous") {
		t.Fatalf("expected the supersede error, got %v", err)
	}
	if _, moved := f.releases.lastStatus[f.rel.ID]; moved {
		t.Error("the new release must not transition when the previous cannot be superseded")
	}
}

func TestActivationKernel_ActivateTransitionFailure_ReturnsError(t *testing.T) {
	f := newKernelFixture()
	f.releases.failUpdateFor = f.rel.ID

	err := f.activate(context.Background())

	if !errors.Is(err, errKernelStep) || !strings.Contains(err.Error(), "transition to ACTIVE") {
		t.Fatalf("expected the ACTIVE transition error, got %v", err)
	}
}

func TestActivationKernel_SetActiveFailure_ReturnsError(t *testing.T) {
	f := newKernelFixture()
	f.targets.setActiveErr = errKernelStep

	err := f.activate(context.Background())

	if !errors.Is(err, errKernelStep) || !strings.Contains(err.Error(), "set active") {
		t.Fatalf("expected the SetActive error, got %v", err)
	}
}

func TestActivationKernel_OutboxFailure_ReturnsError(t *testing.T) {
	f := newKernelFixture()
	f.outbox = failingDeployEventStore{}

	err := f.activate(context.Background())

	if !errors.Is(err, errKernelStep) || !strings.Contains(err.Error(), "stage deploy event") {
		t.Fatalf("expected the outbox error, got %v", err)
	}
}

func TestActivationKernel_CompletedEmitRetriedUntilItLands(t *testing.T) {
	f := newKernelFixture()
	events := &flakyCompletedEventStore{failures: 2}
	f.events = events

	err := f.activate(context.Background())

	if err != nil {
		t.Fatalf("a transient emit failure must not fail activation, got %v", err)
	}
	if events.completedAttempts != 3 {
		t.Errorf("expected 3 deploy.completed attempts, got %d", events.completedAttempts)
	}
}

func TestActivationKernel_CompletedEmitRetryHonoursCancellation(t *testing.T) {
	f := newKernelFixture()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.events = &flakyCompletedEventStore{failures: 3, onFailure: cancel}

	err := f.activate(ctx)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled while waiting to retry, got %v", err)
	}
}
