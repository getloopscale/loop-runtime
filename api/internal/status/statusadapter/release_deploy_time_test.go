// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package statusadapter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
)

type fakeRollbackTimes struct {
	finishedAt *time.Time
	err        error
	gotRelease string
}

func (f *fakeRollbackTimes) LastRollbackTo(_ context.Context, _, releaseID string) (*time.Time, error) {
	f.gotRelease = releaseID
	return f.finishedAt, f.err
}

func activeReleaseCreatedAt(createdAt time.Time) *fakeStatusReleaseStore {
	return &fakeStatusReleaseStore{release: &db.Release{ID: "rel-old", Status: "ACTIVE", CreatedAt: createdAt}}
}

// A rollback reactivates an older release. The environment changed when the
// rollback ended, not when that release was first created.
func TestStatusRelease_DeployedAtIsTheEndOfTheRollbackThatReactivatedIt(t *testing.T) {
	createdAt := time.Date(2026, 11, 25, 18, 17, 0, 0, time.UTC)
	rolledBackAt := time.Date(2026, 11, 25, 22, 3, 0, 0, time.UTC)
	rollbacks := &fakeRollbackTimes{finishedAt: &rolledBackAt}
	adapter := &StatusReleaseStoreAdapter{Store: activeReleaseCreatedAt(createdAt), Rollbacks: rollbacks}

	got, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "my-app", "production")
	if err != nil {
		t.Fatalf("GetActiveByEnv: %v", err)
	}
	if !got.DeployedAt.Equal(rolledBackAt) {
		t.Errorf("DeployedAt = %s, want the end of the rollback %s", got.DeployedAt, rolledBackAt)
	}
	if rollbacks.gotRelease != "rel-old" {
		t.Errorf("asked for rollbacks to %q, want the active release", rollbacks.gotRelease)
	}
}

func TestStatusRelease_DeployedAtIsTheCreationTimeWithoutARollback(t *testing.T) {
	createdAt := time.Date(2026, 11, 25, 18, 17, 0, 0, time.UTC)
	for name, rollbacks := range map[string]rollbackTimes{
		"no rollback ended on the release": &fakeRollbackTimes{},
		"no rollback reader wired":         nil,
		"the rollback read failed":         &fakeRollbackTimes{err: errors.New("db down")},
	} {
		t.Run(name, func(t *testing.T) {
			adapter := &StatusReleaseStoreAdapter{Store: activeReleaseCreatedAt(createdAt), Rollbacks: rollbacks}
			got, err := adapter.GetActiveByEnv(context.Background(), "tn-acme", "my-app", "production")
			if err != nil {
				t.Fatalf("GetActiveByEnv: %v", err)
			}
			if !got.DeployedAt.Equal(createdAt) {
				t.Errorf("DeployedAt = %s, want the creation time %s", got.DeployedAt, createdAt)
			}
		})
	}
}
