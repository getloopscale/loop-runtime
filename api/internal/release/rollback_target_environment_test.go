// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
	"github.com/getloopscale/loopzero/pkg/statemachine"
)

// A release of another environment or project is not a rollback target. On
// staging a rollback of production took the release a promote had created in
// preview: the manifests were delivered and the rollback then failed on the
// state of that release, which was ACTIVE in its own environment.
func TestRollback_RefusesATargetOutsideTheEnvironment(t *testing.T) {
	for name, target := range map[string]*db.Release{
		"another environment": {ID: "rel-other", TenantID: "tn-1", ProjectID: "proj-1", Environment: "preview", Status: string(statemachine.ReleaseActive)},
		"another project":     {ID: "rel-other", TenantID: "tn-1", ProjectID: "proj-2", Environment: "production", Status: string(statemachine.ReleaseSuperseded)},
	} {
		t.Run(name, func(t *testing.T) {
			rs := newFakeRollbackReleaseStore()
			active := &db.Release{ID: "rel-3", TenantID: "tn-1", ProjectID: "proj-1", Environment: "production", Status: string(statemachine.ReleaseActive)}
			rs.releases[active.ID] = active
			rs.releases[target.ID] = target
			rs.activeByEnv["tn-1-proj-1-production"] = active
			delivery := &fakeDeliveryActivator{}
			orch := NewRollbackOrchestrator(RollbackOrchestratorDeps{
				Releases: rs, EnvTargets: newFakeRollbackEnvTargetStore(), Locker: &fakeRollbackLocker{},
				Delivery: delivery, Logger: slog.Default(),
			})

			_, err := orch.Rollback(context.Background(), RollbackRequest{
				TenantID: "tn-1", ProjectID: "proj-1", Environment: "production", ToReleaseID: target.ID,
			})
			orch.Wait()

			if !errors.Is(err, loopzeroerrors.ErrReleaseNotFound) {
				t.Fatalf("err = %v, want ErrReleaseNotFound", err)
			}
			if active.Status != string(statemachine.ReleaseActive) {
				t.Errorf("the active release moved to %s", active.Status)
			}
		})
	}
}
