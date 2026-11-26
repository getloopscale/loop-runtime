// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statusadapter

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

func TestStatusBuildStoreAdapter_ListByProject_MapsFields(t *testing.T) {
	now := time.Now()
	dur := 45
	store := &fakeStatusBuildStore{
		builds: []db.Build{
			{
				ID: "bld-st-001", Environment: "production", Status: "SUCCEEDED",
				CommitSHA: "abc", CreatedAt: now, DurationSeconds: &dur,
			},
			{
				ID: "bld-st-002", Environment: "staging", Status: "FAILED",
				CommitSHA: "def", CreatedAt: now, ErrorMessage: "oom",
			},
		},
	}

	adapter := &StatusBuildStoreAdapter{Store: store}

	results, err := adapter.ListByProject(context.Background(), "tn-acme", "my-app", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ID != "bld-st-001" || results[0].Status != "SUCCEEDED" {
		t.Errorf("unexpected first result: %+v", results[0])
	}
	if results[1].ErrorMessage != "oom" {
		t.Errorf("expected error message 'oom', got %q", results[1].ErrorMessage)
	}
}

func TestStatusReleaseStoreAdapter_GetActiveByEnv_NilRelease(t *testing.T) {
	store := &fakeStatusReleaseStore{release: nil}
	adapter := &StatusReleaseStoreAdapter{Store: store}

	result, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "my-app", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result for no active release, got %+v", result)
	}
}

func TestStatusReleaseStoreAdapter_GetActiveByEnv_MapsFields(t *testing.T) {
	now := time.Now()
	store := &fakeStatusReleaseStore{
		release: &db.Release{
			ID: "rel-st-001", Status: "ACTIVE", GitCommit: "abc123", CreatedAt: now,
		},
	}
	adapter := &StatusReleaseStoreAdapter{Store: store}

	result, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "my-app", "production")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if result.ID != "rel-st-001" || result.Status != "ACTIVE" || result.GitCommit != "abc123" {
		t.Errorf("unexpected mapping: %+v", result)
	}
}

// fakeStatusBuildStore implements only ListByProject for StatusBuildStoreAdapter tests.
type fakeStatusBuildStore struct {
	builds []db.Build
}

func (f *fakeStatusBuildStore) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return f.builds, nil
}

// fakeStatusReleaseStore implements db.ReleaseStore minimally.
type fakeStatusReleaseStore struct {
	release *db.Release
}

func (f *fakeStatusReleaseStore) GetActiveByEnv(_ context.Context, _, _, _ string) (*db.Release, error) {
	return f.release, nil
}

// Stubs for interface satisfaction.
func (f *fakeStatusReleaseStore) Create(_ context.Context, _ db.DBTX, _ *db.Release) error {
	return nil
}

func (f *fakeStatusReleaseStore) GetByID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) UpdateStatus(_ context.Context, _ db.DBTX, _ string, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) SetGitCommit(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}

func (f *fakeStatusReleaseStore) GetActiveByEnvTx(_ context.Context, _ db.DBTX, _, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) GetByIDForTenant(_ context.Context, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) GetByIDTx(_ context.Context, _ db.DBTX, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) CancelAllByEnv(_ context.Context, _ db.DBTX, _, _, _ string) (int, error) {
	return 0, nil
}

func (f *fakeStatusReleaseStore) FindPendingReleasesByTenantEnv(_ context.Context, _, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) GetLatestByBuildID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeStatusReleaseStore) AppendSideEffect(_ context.Context, _ db.DBTX, _ string, _ map[string]any) error {
	return nil
}

func TestStatusReleaseStoreAdapter_GetActiveByEnv_Error(t *testing.T) {
	store := &fakeStatusReleaseStoreWithError{err: errors.New("db down")}
	adapter := &StatusReleaseStoreAdapter{Store: store}

	_, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "app", "prod")
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeStatusReleaseStoreWithError struct {
	fakeStatusReleaseStore
	err error
}

func (f *fakeStatusReleaseStoreWithError) GetActiveByEnv(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, f.err
}

func TestStatusBuildStoreAdapter_ListByProject_Error(t *testing.T) {
	store := &fakeStatusBuildStoreWithError{err: errors.New("db down")}
	adapter := &StatusBuildStoreAdapter{Store: store}

	_, err := adapter.ListByProject(context.Background(), "tn", "proj", 10, 0)
	if err == nil {
		t.Fatal("expected error")
	}
}

type fakeStatusBuildStoreWithError struct {
	err error
}

func (f *fakeStatusBuildStoreWithError) ListByProject(_ context.Context, _, _ string, _, _ int) ([]db.Build, error) {
	return nil, f.err
}

func TestExtractScalingFromAppsSpec_ReportsAShapeMismatch(t *testing.T) {
	if _, err := extractScalingFromAppsSpec(json.RawMessage(`"not-an-array"`)); err == nil {
		t.Fatal("a string where an array of apps is expected must be reported")
	}
}

func TestStatusBuildStoreAdapter_NoBuildsIsAnEmptyList(t *testing.T) {
	a := &StatusBuildStoreAdapter{Store: &fakeStatusBuildStore{}}
	results, err := a.ListByProject(context.Background(), "tn-acme", "my-app", 10, 0)
	if err != nil || len(results) != 0 {
		t.Fatalf("got (%v, %v), want an empty list", results, err)
	}
}
