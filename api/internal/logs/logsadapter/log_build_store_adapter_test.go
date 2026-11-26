// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package logsadapter

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

func TestLogBuildStoreAdapter_GetByID_MapsFields(t *testing.T) {
	store := &fakeBuildStore{
		build: &db.Build{
			ID:       "bld-log-001",
			TenantID: "tn-acme",
			Status:   "SUCCEEDED",
		},
	}

	adapter := &LogBuildStoreAdapter{Store: store}

	logBuild, err := adapter.GetByID(context.Background(), "bld-log-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if logBuild.ID != "bld-log-001" || logBuild.TenantID != "tn-acme" || logBuild.Status != "SUCCEEDED" {
		t.Errorf("unexpected mapping: %+v", logBuild)
	}
}

func TestLogBuildStoreAdapter_GetByID_Error(t *testing.T) {
	store := &fakeBuildStoreWithError{err: errors.New("db unreachable")}
	adapter := &LogBuildStoreAdapter{Store: store}

	_, err := adapter.GetByID(context.Background(), "bld-err")
	if err == nil {
		t.Fatal("expected error")
	}
}

// fakeBuildStore serves one build by id; any other id is db.ErrNoRows, the
// production "not found" sentinel.
type fakeBuildStore struct {
	build *db.Build
}

func (f *fakeBuildStore) GetByID(_ context.Context, id string) (*db.Build, error) {
	if f.build != nil && f.build.ID == id {
		return f.build, nil
	}
	return nil, db.ErrNoRows
}

// GetByIDForTenant emulates the production SQL filter for the unit-test scope
// (T0.4 of deep-review-2026-05-10-remediation-plan).
func (f *fakeBuildStore) GetByIDForTenant(ctx context.Context, tenantID, buildID string) (*db.Build, error) {
	b, err := f.GetByID(ctx, buildID)
	if err != nil || b == nil {
		return b, err
	}
	if b.TenantID != tenantID {
		return nil, nil
	}
	return b, nil
}

type fakeBuildStoreWithError struct {
	err error
}

func (f *fakeBuildStoreWithError) GetByID(_ context.Context, _ string) (*db.Build, error) {
	return nil, f.err
}

func (f *fakeBuildStoreWithError) GetByIDForTenant(_ context.Context, _, _ string) (*db.Build, error) {
	return nil, f.err
}

func TestLogBuildStoreAdapter_EmptyStore_ReturnsErrNoRows(t *testing.T) {
	_, err := (&LogBuildStoreAdapter{Store: &fakeBuildStore{}}).GetByID(context.Background(), "bld-nope")
	if !errors.Is(err, db.ErrNoRows) {
		t.Errorf("expected db.ErrNoRows, got %v", err)
	}
}
