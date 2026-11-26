// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// --- Test doubles for ReleaseReaper ---

type fakeReleaseStoreReaper struct {
	releases   map[string]*db.Release
	lastStatus map[string]string
}

func newFakeReleaseStoreReaper() *fakeReleaseStoreReaper {
	return &fakeReleaseStoreReaper{
		releases:   make(map[string]*db.Release),
		lastStatus: make(map[string]string),
	}
}

func (f *fakeReleaseStoreReaper) Create(_ context.Context, _ db.DBTX, r *db.Release) error {
	f.releases[r.ID] = r
	return nil
}
func (f *fakeReleaseStoreReaper) GetByID(_ context.Context, id string) (*db.Release, error) {
	r, ok := f.releases[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return r, nil
}
func (f *fakeReleaseStoreReaper) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	f.lastStatus[id] = event
	return f.releases[id], nil
}
func (f *fakeReleaseStoreReaper) SetGitCommit(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}
func (f *fakeReleaseStoreReaper) GetActiveByEnv(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) GetActiveByEnvTx(_ context.Context, _ db.DBTX, _, _, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) GetByIDForTenant(_ context.Context, _, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) GetLatestByBuildID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}
func (f *fakeReleaseStoreReaper) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	return f.releases[id], nil
}
func (f *fakeReleaseStoreReaper) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

type fakeStaleReleaseChecker struct {
	result []StaleRelease
	err    error
}

func (f *fakeStaleReleaseChecker) FindStaleReleases(_ context.Context, _ string, _, _ time.Duration) ([]StaleRelease, error) {
	return f.result, f.err
}

type fakeLockerReleaseReaper struct{}

func (f *fakeLockerReleaseReaper) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

type fakeEventStoreReleaseReaper struct {
	events []struct {
		BuildID   string
		EventType string
		Data      json.RawMessage
	}
	nextID int64
}

func (f *fakeEventStoreReleaseReaper) Append(_ context.Context, buildID, eventType string, data json.RawMessage) (int64, error) {
	f.nextID++
	f.events = append(f.events, struct {
		BuildID   string
		EventType string
		Data      json.RawMessage
	}{buildID, eventType, data})
	return f.nextID, nil
}

// --- Tests ---

func TestReleaseReaper_ReapsStaleReadyRelease(t *testing.T) {
	rs := newFakeReleaseStoreReaper()
	rs.releases["rel-1"] = &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: "READY", BuildID: "build-1",
	}

	checker := &fakeStaleReleaseChecker{
		result: []StaleRelease{
			{ID: "rel-1", State: statemachine.RPhReady, Event: statemachine.RPEventGitPushTimeout,
				TenantID: "tn-1", ProjectID: "proj-1", Environment: "production", BuildID: "build-1"},
		},
	}
	es := &fakeEventStoreReleaseReaper{}

	reaper := NewReleaseReaper(rs, checker, &fakeLockerReleaseReaper{}, es,
		slog.Default(), DefaultReleaseReaperConfig())

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 1 {
		t.Fatalf("expected 1 reaped, got %d", reaped)
	}
	if rs.lastStatus["rel-1"] != string(statemachine.RPEventGitPushTimeout) {
		t.Errorf("expected event %s, got %s", statemachine.RPEventGitPushTimeout, rs.lastStatus["rel-1"])
	}
	if len(es.events) != 1 || es.events[0].EventType != "deploy.git_push_failed" {
		t.Errorf("expected deploy.git_push_failed SSE event, got %v", es.events)
	}
}

func TestReleaseReaper_ReapsStalePendingRelease(t *testing.T) {
	rs := newFakeReleaseStoreReaper()
	rs.releases["rel-2"] = &db.Release{
		ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "staging", Status: "PENDING", BuildID: "build-2",
	}

	checker := &fakeStaleReleaseChecker{
		result: []StaleRelease{
			{ID: "rel-2", State: statemachine.RPhPending, Event: statemachine.RPEventSyncTimeout,
				TenantID: "tn-1", ProjectID: "proj-1", Environment: "staging", BuildID: "build-2"},
		},
	}
	es := &fakeEventStoreReleaseReaper{}

	reaper := NewReleaseReaper(rs, checker, &fakeLockerReleaseReaper{}, es,
		slog.Default(), DefaultReleaseReaperConfig())

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 1 {
		t.Fatalf("expected 1 reaped, got %d", reaped)
	}
	if es.events[0].EventType != "deploy.sync_failed" {
		t.Errorf("expected deploy.sync_failed, got %s", es.events[0].EventType)
	}
}

func TestReleaseReaper_SkipsAlreadyTransitioned(t *testing.T) {
	rs := newFakeReleaseStoreReaper()
	rs.releases["rel-3"] = &db.Release{
		ID: "rel-3", Status: "ACTIVE", // Already transitioned out of READY
	}

	checker := &fakeStaleReleaseChecker{
		result: []StaleRelease{
			{ID: "rel-3", State: statemachine.RPhReady, Event: statemachine.RPEventGitPushTimeout,
				TenantID: "tn-1", ProjectID: "proj-1", Environment: "production", BuildID: "build-3"},
		},
	}

	reaper := NewReleaseReaper(rs, checker, &fakeLockerReleaseReaper{}, nil,
		slog.Default(), DefaultReleaseReaperConfig())

	reaped := reaper.ReapOnce(context.Background())

	if reaped != 0 {
		t.Fatalf("expected 0 (already transitioned), got %d", reaped)
	}
}

func TestReleaseReaper_NoStaleReleases(t *testing.T) {
	checker := &fakeStaleReleaseChecker{result: nil}
	reaper := NewReleaseReaper(newFakeReleaseStoreReaper(), checker, &fakeLockerReleaseReaper{}, nil,
		slog.Default(), DefaultReleaseReaperConfig())

	reaped := reaper.ReapOnce(context.Background())
	if reaped != 0 {
		t.Fatalf("expected 0, got %d", reaped)
	}
}

func TestReleaseReaper_CheckerError_ReturnsZero(t *testing.T) {
	checker := &fakeStaleReleaseChecker{err: fmt.Errorf("db down")}
	reaper := NewReleaseReaper(newFakeReleaseStoreReaper(), checker, &fakeLockerReleaseReaper{}, nil,
		slog.Default(), DefaultReleaseReaperConfig())

	reaped := reaper.ReapOnce(context.Background())
	if reaped != 0 {
		t.Fatalf("expected 0, got %d", reaped)
	}
}

// AppendSideEffect — fix-all-platform-issues-plan T4.1 stub.
func (f *fakeReleaseStoreReaper) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}
