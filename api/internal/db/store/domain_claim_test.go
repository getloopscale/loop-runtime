// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package store

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/generated"
)

var errClaimRecorded = errors.New("claim recorded")

// claimRecorder keeps the arguments of the claim statement. In that statement
// $1 is the status the rows hold and $2 is the status they move to.
type claimRecorder struct {
	args []any
}

// claimBridge hands the store the recorder as its sqlc handle.
type claimBridge struct {
	db.DBTX
	rec *claimRecorder
}

func (b claimBridge) GeneratedDB() generated.DBTX { return b.rec }

func (r *claimRecorder) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errClaimRecorded
}

func (r *claimRecorder) Query(_ context.Context, _ string, args ...any) (pgx.Rows, error) {
	r.args = args
	return nil, errClaimRecorded
}

func (r *claimRecorder) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func TestFindAndClaimVerifiedSelectsVerifiedAndMovesToProvisioning(t *testing.T) {
	rec := &claimRecorder{}
	s := &PostgresDomainStore{DB: claimBridge{rec: rec}}

	if _, err := s.FindAndClaimVerified(context.Background()); !errors.Is(err, errClaimRecorded) {
		t.Fatalf("FindAndClaimVerified error = %v; want the recorder's error", err)
	}
	if len(rec.args) != 2 {
		t.Fatalf("claim statement got %d arguments; want 2", len(rec.args))
	}
	if from, to := rec.args[0], rec.args[1]; from != "verified" || to != "provisioning" {
		t.Fatalf("claim selects status %q and writes %q; want it to select \"verified\" and write \"provisioning\"", from, to)
	}
}

func TestFindRemovingKeepsRemovingStatus(t *testing.T) {
	rec := &claimRecorder{}
	s := &PostgresDomainStore{DB: claimBridge{rec: rec}}

	if _, err := s.FindRemoving(context.Background()); !errors.Is(err, errClaimRecorded) {
		t.Fatalf("FindRemoving error = %v; want the recorder's error", err)
	}
	if len(rec.args) != 2 || rec.args[0] != "removing" || rec.args[1] != "removing" {
		t.Fatalf("claim arguments = %v; want [removing removing]", rec.args)
	}
}
