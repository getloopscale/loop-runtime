// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtest"
)

func TestIdempotencyStore_Get_QueryError(t *testing.T) {
	fake := &dbtest.FakeDBTX{QueryErr: fmt.Errorf("db down")}
	store := &PostgresIdempotencyStore{DB: fake}

	_, err := store.Get(context.Background(), "key-1", "t1")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestIdempotencyStore_Cleanup_Error(t *testing.T) {
	fake := &dbtest.FakeDBTX{ExecErr: fmt.Errorf("db locked")}
	store := &PostgresIdempotencyStore{DB: fake}

	_, err := store.Cleanup(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}
