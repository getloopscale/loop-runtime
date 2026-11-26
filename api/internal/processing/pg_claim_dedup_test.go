// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package processing

import (
	"context"
	"errors"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/dbtest"
)

func dedupOver(tx db.DBTX) *PGClaimDedup {
	return NewPGClaimDedup(NewClaimStore(tx), true, "pod-1")
}

func TestTryClaimReportsADatabaseFailureAsAnError(t *testing.T) {
	// Arrange: the database is unreachable.
	d := dedupOver(&dbtest.FakeDBTX{ExecErr: errors.New("connection refused")})

	// Act
	claimed, err := d.TryClaim(context.Background(), KindBuild, "bld-1")

	// Assert: an outage is not "a peer holds it".
	if claimed || err == nil {
		t.Fatalf("TryClaim = (%v, %v), want (false, error)", claimed, err)
	}
}

func TestTryClaimReportsAPeerHoldingTheClaimAsNotClaimed(t *testing.T) {
	d := dedupOver(&dbtest.FakeDBTX{ExecResult: db.ExecResult{RowsAffected: 0}})

	claimed, err := d.TryClaim(context.Background(), KindBuild, "bld-1")

	if claimed || err != nil {
		t.Fatalf("TryClaim = (%v, %v), want (false, nil)", claimed, err)
	}
}

func TestTryClaimClaims(t *testing.T) {
	d := dedupOver(&dbtest.FakeDBTX{ExecResult: db.ExecResult{RowsAffected: 1}})

	claimed, err := d.TryClaim(context.Background(), KindRelease, "rel-1")

	if !claimed || err != nil {
		t.Fatalf("TryClaim = (%v, %v), want (true, nil)", claimed, err)
	}
}

func TestTryClaimIsAlwaysGrantedWhenPGClaimsAreDisabled(t *testing.T) {
	claimed, err := NewPGClaimDedup(nil, false, "pod-1").TryClaim(context.Background(), KindBuild, "bld-1")
	if !claimed || err != nil {
		t.Fatalf("TryClaim = (%v, %v), want (true, nil)", claimed, err)
	}
}
