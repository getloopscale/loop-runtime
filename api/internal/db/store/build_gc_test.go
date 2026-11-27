// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtest"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

func TestBuildGCStore_DeleteOldBuilds_Success(t *testing.T) {
	fake := &dbtest.FakeDBTX{ExecResult: db.ExecResult{RowsAffected: 15}}
	store := &PostgresBuildGCStore{DB: fake}

	count, err := store.DeleteOldBuilds(context.Background(), 30*24*time.Hour, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 15 {
		t.Errorf("expected 15, got: %d", count)
	}
}

func TestBuildGCStore_DeleteOldBuilds_Error(t *testing.T) {
	fake := &dbtest.FakeDBTX{ExecErr: fmt.Errorf("table locked")}
	store := &PostgresBuildGCStore{DB: fake}

	_, err := store.DeleteOldBuilds(context.Background(), 30*24*time.Hour, 10)
	if err == nil {
		t.Fatal("expected error")
	}
}
