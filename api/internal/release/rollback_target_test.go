// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
)

// GitCommit and ID are load-bearing for the delivery (Apps, which the sync
// confirmation reads, has its own test): an empty GitCommit is the guard that refuses a rollback
// with no manifests to fetch, and ID is the release the pushed manifests are
// attributed to. Dropping either in the adapter would turn a rollback into a
// silent no-op or a mis-attributed push.
func TestRollbackTargetFromCarriesIDAndGitCommit(t *testing.T) {
	src := &db.Release{
		ID:        "rel-1",
		GitCommit: "abc123",
		// Persistence detail the delivery contract does not see.
		Status:  "SUPERSEDED",
		BuildID: "bld-1",
	}

	got := rollbackTargetFrom(src)

	if got == nil {
		t.Fatal("rollbackTargetFrom() = nil, want a target")
	}
	want := deploycontracts.RollbackTarget{ID: "rel-1", GitCommit: "abc123"}
	if got.ID != want.ID || got.GitCommit != want.GitCommit || len(got.Apps) != 0 {
		t.Errorf("rollbackTargetFrom() = %+v, want %+v", *got, want)
	}
}

// The rollback orchestrator has a nil-delivery fallback path; the adapter must
// not panic before that branch is even reached.
func TestRollbackTargetFromNilIsNil(t *testing.T) {
	if got := rollbackTargetFrom(nil); got != nil {
		t.Errorf("rollbackTargetFrom(nil) = %+v, want nil", got)
	}
}
