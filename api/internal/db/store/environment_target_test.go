// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package store

import (
	"context"
	"fmt"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtest"
)

func TestEnvironmentTargetStore_SetDesired_ExecError(t *testing.T) {
	fake := &dbtest.FakeDBTX{ExecErr: fmt.Errorf("pg down")}
	store := NewPostgresEnvironmentTargetStore(fake, fake)

	err := store.SetDesired(context.Background(), fake, "t1", "p1", "prod", "rel-1", "u1", "c1")
	if err == nil {
		t.Fatal("expected error")
	}
}
