// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/ports"
)

func TestAuditStore_ListAuditEntries_NilDB(t *testing.T) {
	store := &PostgresAuditStore{DB: nil}

	entries, total, err := store.ListAuditEntries(context.Background(), "t1", ports.AuditFilter{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 0 {
		t.Errorf("expected 0 total, got: %d", total)
	}
	if entries != nil {
		t.Error("expected nil entries for nil DB")
	}
}
