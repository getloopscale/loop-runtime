// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release_test

import (
	"context"
	"fmt"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	loopzeroerrors "github.com/getloopscale/loop-runtime/pkg/errors"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// safeFakeTransition catches panics from the state machine.
func safeFakeTransition(m *statemachine.Machine, current statemachine.State, event statemachine.Event) (newState statemachine.State, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = loopzeroerrors.NewInvalidTransition(string(current), string(event))
		}
	}()
	newState = m.Transition(current, event)
	return newState, nil
}

var releaseMachine = statemachine.NewReleaseMachine()

// fakeReleaseStoreOrch implements db.ReleaseStore.
type fakeReleaseStoreOrch struct {
	releases    map[string]*db.Release
	activeByEnv map[string]*db.Release // key: tenant:project:env
	createErr   error
	updateErr   error
}

func newFakeReleaseStoreOrch() *fakeReleaseStoreOrch {
	return &fakeReleaseStoreOrch{
		releases:    make(map[string]*db.Release),
		activeByEnv: make(map[string]*db.Release),
	}
}

func (f *fakeReleaseStoreOrch) Create(ctx context.Context, tx db.DBTX, release *db.Release) error {
	if f.createErr != nil {
		return f.createErr
	}
	if release.ID == "" {
		release.ID = fmt.Sprintf("rel-%08x", time.Now().UnixNano()&0xFFFFFFFF)
	}
	release.Status = string(statemachine.RPhReady)
	release.CreatedAt = time.Now().UTC()
	f.releases[release.ID] = release
	return nil
}

func (f *fakeReleaseStoreOrch) GetByID(ctx context.Context, id string) (*db.Release, error) {
	r, ok := f.releases[id]
	if !ok {
		return nil, loopzeroerrors.ErrReleaseNotFound
	}
	copy := *r
	return &copy, nil
}

func (f *fakeReleaseStoreOrch) UpdateStatus(ctx context.Context, tx db.DBTX, id string, event string) (*db.Release, error) {
	if f.updateErr != nil {
		return nil, f.updateErr
	}
	r, ok := f.releases[id]
	if !ok {
		return nil, loopzeroerrors.ErrReleaseNotFound
	}
	newState, err := safeFakeTransition(releaseMachine, statemachine.State(r.Status), statemachine.Event(event))
	if err != nil {
		return nil, err
	}
	r.Status = string(newState)
	copy := *r
	return &copy, nil
}

func (f *fakeReleaseStoreOrch) SetGitCommit(ctx context.Context, tx db.DBTX, id string, commitSHA string) error {
	r, ok := f.releases[id]
	if !ok {
		return loopzeroerrors.ErrReleaseNotFound
	}
	r.GitCommit = commitSHA
	return nil
}

func (f *fakeReleaseStoreOrch) GetActiveByEnv(ctx context.Context, tenantID, projectID, env string) (*db.Release, error) {
	key := fmt.Sprintf("%s:%s:%s", tenantID, projectID, env)
	r, ok := f.activeByEnv[key]
	if !ok {
		return nil, nil
	}
	copy := *r
	return &copy, nil
}

func (f *fakeReleaseStoreOrch) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenantID, projectID, env string) (*db.Release, error) {
	return f.GetActiveByEnv(context.Background(), tenantID, projectID, env)
}

func (f *fakeReleaseStoreOrch) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	return f.GetByID(context.Background(), id)
}

func (f *fakeReleaseStoreOrch) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

func (f *fakeReleaseStoreOrch) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreOrch) GetByIDForTenant(_ context.Context, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreOrch) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreOrch) GetLatestByBuildID(_ context.Context, buildID string) (*db.Release, error) {
	for _, r := range f.releases {
		if r.BuildID == buildID {
			return r, nil
		}
	}
	return nil, nil
}

func (f *fakeReleaseStoreOrch) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	var pending []db.Release
	for _, r := range f.releases {
		if r.Status == "PENDING" {
			pending = append(pending, *r)
		}
	}
	return pending, nil
}

func (f *fakeReleaseStoreOrch) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	var active []db.Release
	for _, r := range f.releases {
		if r.Status == "ACTIVE" {
			active = append(active, *r)
		}
	}
	return active, nil
}

// AppendSideEffect — fix-all-platform-issues-plan T4.1 stub for tests.
// In-memory fake just no-ops; real RCA captures only matter in LIVE PG.
func (f *fakeReleaseStoreOrch) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}

// fakeEnvironmentLocker runs fn with a nil tx, or fails with lockErr.
type fakeEnvironmentLocker struct {
	lockErr error
}

func (f *fakeEnvironmentLocker) WithLock(ctx context.Context, tenant, project, env string, fn func(tx db.DBTX) error) error {
	if f.lockErr != nil {
		return f.lockErr
	}
	return fn(nil)
}
