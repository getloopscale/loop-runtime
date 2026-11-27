// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package sse

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

func TestBuildStoreReaderAdapter_GetByID_MapsFields(t *testing.T) {
	store := &fakeBuildStore{
		build: &db.Build{
			ID:       "bld-sse-001",
			TenantID: "tn-acme",
			Status:   "RUNNING",
		},
	}

	adapter := &BuildStoreReaderAdapter{Store: store}

	info, err := adapter.GetByID(context.Background(), "bld-sse-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.ID != "bld-sse-001" || info.TenantID != "tn-acme" || info.Status != "RUNNING" {
		t.Errorf("unexpected mapping: %+v", info)
	}
}

func TestBuildStoreReaderAdapter_GetByID_Error(t *testing.T) {
	store := &fakeBuildStoreWithError{err: errors.New("db unreachable")}
	adapter := &BuildStoreReaderAdapter{Store: store}

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

type fakeBuildStoreWithError struct {
	err error
}

func (f *fakeBuildStoreWithError) GetByID(_ context.Context, _ string) (*db.Build, error) {
	return nil, f.err
}

func TestBuildStoreReaderAdapter_EmptyStore_ReturnsErrNoRows(t *testing.T) {
	_, err := (&BuildStoreReaderAdapter{Store: &fakeBuildStore{}}).GetByID(context.Background(), "bld-nope")
	if !errors.Is(err, db.ErrNoRows) {
		t.Errorf("expected db.ErrNoRows, got %v", err)
	}
}
