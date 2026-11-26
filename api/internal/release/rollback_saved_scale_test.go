// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package release

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// recordingSavedScale records the environment it was asked to deliver to, and
// the status of the target release at that moment.
type recordingSavedScale struct {
	releases     *fakeRollbackReleaseStore
	target       string
	calls        []string
	targetStatus string
	err          error
}

func (r *recordingSavedScale) DeliverSaved(_ context.Context, tenantID, project, env string) (bool, error) {
	r.calls = append(r.calls, tenantID+"/"+project+"/"+env)
	r.targetStatus = r.releases.releases[r.target].Status
	return r.err == nil, r.err
}

func rollbackWithSavedScale(scaleErr error) (*RollbackOrchestrator, *recordingSavedScale) {
	rs := newFakeRollbackReleaseStore()
	active := &db.Release{
		ID: "rel-2", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseActive),
	}
	target := &db.Release{
		ID: "rel-1", TenantID: "tn-1", ProjectID: "proj-1",
		Environment: "production", Status: string(statemachine.ReleaseSuperseded), GitCommit: "abc123",
	}
	rs.releases["rel-2"], rs.releases["rel-1"] = active, target
	rs.activeByEnv["tn-1-proj-1-production"] = active
	rs.previousByEnv["tn-1-proj-1-production"] = target
	scale := &recordingSavedScale{releases: rs, target: "rel-1", err: scaleErr}
	return NewRollbackOrchestrator(RollbackOrchestratorDeps{
		Releases:   rs,
		EnvTargets: newFakeRollbackEnvTargetStore(),
		Locker:     &fakeRollbackLocker{},
		Delivery:   &fakeDeliveryActivator{},
		SavedScale: scale,
		Logger:     slog.Default(),
	}), scale
}

// The rolled back manifests carry the replica count of the older release, so a
// rollback used to undo a scale made since.
func TestRollback_DeliversTheSavedScaleOnceTheTargetIsActive(t *testing.T) {
	orch, scale := rollbackWithSavedScale(nil)

	_, err := orch.Rollback(context.Background(), RollbackRequest{TenantID: "tn-1", ProjectID: "proj-1", Environment: "production"})
	orch.Wait()

	if err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if len(scale.calls) != 1 || scale.calls[0] != "tn-1/proj-1/production" {
		t.Fatalf("saved scale delivered to %v, want once to tn-1/proj-1/production", scale.calls)
	}
	if scale.targetStatus != string(statemachine.ReleaseActive) {
		t.Errorf("saved scale delivered while the target was %s, want ACTIVE: the scaler renders the active release", scale.targetStatus)
	}
}

func TestRollback_SucceedsWhenTheSavedScaleIsNotDelivered(t *testing.T) {
	orch, scale := rollbackWithSavedScale(errors.New("gitops down"))

	result, err := orch.Rollback(context.Background(), RollbackRequest{TenantID: "tn-1", ProjectID: "proj-1", Environment: "production"})
	orch.Wait()

	if err != nil || result == nil {
		t.Fatalf("Rollback = %v, %v; want the rollback to stand: the older release is serving", result, err)
	}
	if len(scale.calls) != 1 {
		t.Errorf("saved scale delivery attempted %d times, want 1", len(scale.calls))
	}
}
