// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
)

// --- DispatchingDeliveryActivator tests ---

type fakeStaticDeliveryActivator struct {
	activateCalled bool
	rollbackCalled bool
}

func (f *fakeStaticDeliveryActivator) Activate(_ context.Context, _ *db.Build, _ BuildCallbackRequest) error {
	f.activateCalled = true
	return nil
}
func (f *fakeStaticDeliveryActivator) Rollback(_ context.Context, _ RollbackDeliveryRequest) error {
	f.rollbackCalled = true
	return nil
}

func TestDispatchingDeliveryActivator_Activate_DispatchesToContainer(t *testing.T) {
	container := &fakeDeliveryActivator{}
	static := &fakeStaticDeliveryActivator{}

	dispatcher := &DispatchingDeliveryActivator{
		Container: container,
		Static:    static,
	}

	build := &db.Build{ID: "b-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "prod"}
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "api", Type: "server"}},
	}

	_ = dispatcher.Activate(context.Background(), build, result)

	if static.activateCalled {
		t.Error("expected container activator, not static")
	}
}

func TestDispatchingDeliveryActivator_Activate_DispatchesToStatic(t *testing.T) {
	container := &fakeDeliveryActivator{}
	static := &fakeStaticDeliveryActivator{}

	dispatcher := &DispatchingDeliveryActivator{
		Container: container,
		Static:    static,
	}

	build := &db.Build{ID: "b-1", TenantID: "tn-1", ProjectID: "proj-1", Environment: "prod"}
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "web", Type: "frontend"}},
	}

	_ = dispatcher.Activate(context.Background(), build, result)

	if !static.activateCalled {
		t.Error("expected static activator for frontend app")
	}
}

func TestDispatchingDeliveryActivator_Rollback_DispatchesToContainer(t *testing.T) {
	container := &fakeDeliveryActivator{}
	static := &fakeStaticDeliveryActivator{}

	dispatcher := &DispatchingDeliveryActivator{
		Container: container,
		Static:    static,
	}

	req := RollbackDeliveryRequest{
		TargetRelease: &db.Release{ID: "rel-1", Artifacts: json.RawMessage(`{"manifests": "yaml"}`)},
	}

	_ = dispatcher.Rollback(context.Background(), req)

	if static.rollbackCalled {
		t.Error("expected container rollback for non-static release")
	}
}

func TestDispatchingDeliveryActivator_Rollback_DispatchesToStatic(t *testing.T) {
	container := &fakeDeliveryActivator{}
	static := &fakeStaticDeliveryActivator{}

	dispatcher := &DispatchingDeliveryActivator{
		Container: container,
		Static:    static,
	}

	req := RollbackDeliveryRequest{
		TargetRelease: &db.Release{ID: "rel-1", Artifacts: json.RawMessage(`{"delivery": "static"}`)},
	}

	_ = dispatcher.Rollback(context.Background(), req)

	if !static.rollbackCalled {
		t.Error("expected static rollback for static release")
	}
}

func TestDispatchingDeliveryActivator_NilStatic_FallsBackToContainer(t *testing.T) {
	container := &fakeDeliveryActivator{}

	dispatcher := &DispatchingDeliveryActivator{
		Container: container,
		Static:    nil,
	}

	build := &db.Build{ID: "b-1"}
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "web", Type: "frontend"}},
	}

	_ = dispatcher.Activate(context.Background(), build, result)
	// Should not panic when Static is nil
}

// --- isStaticRelease tests ---

func TestIsStaticRelease_WithStaticArtifacts(t *testing.T) {
	release := &db.Release{
		Artifacts: json.RawMessage(`{"delivery": "static"}`),
	}
	if !isStaticRelease(release) {
		t.Error("expected true for static delivery artifacts")
	}
}

func TestIsStaticRelease_WithContainerArtifacts(t *testing.T) {
	release := &db.Release{
		Artifacts: json.RawMessage(`{"manifests": "yaml"}`),
	}
	if isStaticRelease(release) {
		t.Error("expected false for container artifacts")
	}
}

func TestIsStaticRelease_NilRelease(t *testing.T) {
	if isStaticRelease(nil) {
		t.Error("expected false for nil release")
	}
}

func TestIsStaticRelease_NilArtifacts(t *testing.T) {
	release := &db.Release{Artifacts: nil}
	if isStaticRelease(release) {
		t.Error("expected false for nil artifacts")
	}
}

func TestIsStaticRelease_InvalidJSON(t *testing.T) {
	release := &db.Release{Artifacts: json.RawMessage(`not json`)}
	if isStaticRelease(release) {
		t.Error("expected false for invalid JSON")
	}
}

// --- hasStaticApps tests ---

func TestHasStaticApps_WithFrontend(t *testing.T) {
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "web", Type: "frontend"}},
	}
	if !hasStaticApps(result) {
		t.Error("expected true for frontend app")
	}
}

func TestHasStaticApps_WithoutFrontend(t *testing.T) {
	result := BuildCallbackRequest{
		Apps: []AppRelease{{Name: "api", Type: "server"}},
	}
	if hasStaticApps(result) {
		t.Error("expected false without frontend app")
	}
}

func TestHasStaticApps_EmptyApps(t *testing.T) {
	result := BuildCallbackRequest{Apps: nil}
	if hasStaticApps(result) {
		t.Error("expected false for empty apps")
	}
}

func TestHasStaticApps_MixedTypes(t *testing.T) {
	result := BuildCallbackRequest{
		Apps: []AppRelease{
			{Name: "api", Type: "server"},
			{Name: "web", Type: "frontend"},
			{Name: "worker", Type: "worker"},
		},
	}
	if !hasStaticApps(result) {
		t.Error("expected true when at least one frontend app exists")
	}
}
