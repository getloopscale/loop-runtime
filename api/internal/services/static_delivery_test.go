// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package services

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/loopzero/loopzero/api/internal/db"
)

// COMP-007: Tests for StaticDeliveryActivator (329 lines, previously 0 tests).

// --- Test doubles ---

type fakeReleaseStoreStatic struct {
	releases    map[string]*db.Release
	lastStatus  map[string]string
	activeByEnv map[string]*db.Release // key: tenant-project-env
}

func newFakeReleaseStoreStatic() *fakeReleaseStoreStatic {
	return &fakeReleaseStoreStatic{
		releases:    make(map[string]*db.Release),
		lastStatus:  make(map[string]string),
		activeByEnv: make(map[string]*db.Release),
	}
}

func (f *fakeReleaseStoreStatic) Create(_ context.Context, _ db.DBTX, r *db.Release) error {
	r.ID = "rel-" + r.BuildID
	f.releases[r.ID] = r
	return nil
}

func (f *fakeReleaseStoreStatic) GetByID(_ context.Context, id string) (*db.Release, error) {
	return f.releases[id], nil
}

func (f *fakeReleaseStoreStatic) UpdateStatus(_ context.Context, _ db.DBTX, id, event string) (*db.Release, error) {
	f.lastStatus[id] = event
	return f.releases[id], nil
}

func (f *fakeReleaseStoreStatic) SetGitCommit(_ context.Context, _ db.DBTX, _, _ string) error {
	return nil
}

func (f *fakeReleaseStoreStatic) GetActiveByEnv(_ context.Context, tenant, project, env string) (*db.Release, error) {
	key := tenant + "-" + project + "-" + env
	return f.activeByEnv[key], nil
}

func (f *fakeReleaseStoreStatic) GetActiveByEnvTx(_ context.Context, _ db.DBTX, tenant, project, env string) (*db.Release, error) {
	key := tenant + "-" + project + "-" + env
	return f.activeByEnv[key], nil
}

func (f *fakeReleaseStoreStatic) GetByIDTx(_ context.Context, _ db.DBTX, id string) (*db.Release, error) {
	return f.releases[id], nil
}

func (f *fakeReleaseStoreStatic) GetPreviousRelease(_ context.Context, _, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreStatic) GetByIDForTenant(_ context.Context, _, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreStatic) ListByEnv(_ context.Context, _, _, _ string, _ int) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreStatic) GetLatestByBuildID(_ context.Context, _ string) (*db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreStatic) FindPendingReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

func (f *fakeReleaseStoreStatic) FindActiveReleases(_ context.Context, _ string) ([]db.Release, error) {
	return nil, nil
}

type fakeEnvTargetStoreStatic struct {
	desired map[string]string
	active  map[string]string
}

func newFakeEnvTargetStoreStatic() *fakeEnvTargetStoreStatic {
	return &fakeEnvTargetStoreStatic{
		desired: make(map[string]string),
		active:  make(map[string]string),
	}
}

func (f *fakeEnvTargetStoreStatic) SetDesired(_ context.Context, _ db.DBTX, tenant, project, env, releaseID, _, _ string) error {
	f.desired[tenant+"-"+project+"-"+env] = releaseID
	return nil
}

func (f *fakeEnvTargetStoreStatic) SetActive(_ context.Context, _ db.DBTX, tenant, project, env, releaseID string) error {
	f.active[tenant+"-"+project+"-"+env] = releaseID
	return nil
}

func (f *fakeEnvTargetStoreStatic) Get(_ context.Context, _, _, _ string) (*db.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreStatic) ListByProject(_ context.Context, _, _ string) ([]db.EnvironmentTarget, error) {
	return nil, nil
}

func (f *fakeEnvTargetStoreStatic) DeleteTarget(_ context.Context, _ db.DBTX, _, _, _ string) error {
	return nil
}

type fakeEventStoreStatic struct {
	events []struct {
		BuildID   string
		EventType string
		Data      json.RawMessage
	}
	nextID int64
}

func (f *fakeEventStoreStatic) Append(_ context.Context, buildID, eventType string, data json.RawMessage) (int64, error) {
	f.nextID++
	f.events = append(f.events, struct {
		BuildID   string
		EventType string
		Data      json.RawMessage
	}{buildID, eventType, data})
	return f.nextID, nil
}

type fakeLockerStatic struct{}

func (f *fakeLockerStatic) WithLock(_ context.Context, _, _, _ string, fn func(tx db.DBTX) error) error {
	return fn(nil)
}

// --- Tests ---

func TestStaticDeliveryActivator_NilKVClient_SkipsKVWrite(t *testing.T) {
	// When kvClient is nil (local dev), phaseBDeliver should skip KV write without error.
	rs := newFakeReleaseStoreStatic()
	es := &fakeEventStoreStatic{}
	ets := newFakeEnvTargetStoreStatic()

	activator := NewStaticDeliveryActivator(StaticDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: ets,
		EventStore:     es,
		Locker:         &fakeLockerStatic{},
		KVClient:       nil, // nil = local dev, should not error
		BaseDomain:     "loopzero.local",
	})

	build := &db.Build{
		ID:          "build-1",
		TenantID:    "tn-acme",
		ProjectID:   "my-app",
		Environment: "production",
		CommitSHA:   "abc123",
	}
	result := BuildCallbackRequest{
		Status: "succeeded",
		Apps: []AppRelease{
			{Name: "web", Type: "frontend", URL: "https://web.loopzero.local"},
		},
	}

	err := activator.Activate(context.Background(), build, result)
	if err != nil {
		t.Fatalf("Activate with nil kvClient should succeed, got: %v", err)
	}

	// Verify release was created and activated
	if len(rs.releases) != 1 {
		t.Errorf("expected 1 release, got %d", len(rs.releases))
	}

	// Verify SSE events were emitted
	eventTypes := make([]string, 0, len(es.events))
	for _, e := range es.events {
		eventTypes = append(eventTypes, e.EventType)
	}
	if !contains(eventTypes, "deploy.delivering") {
		t.Error("expected deploy.delivering event")
	}
	if !contains(eventTypes, "deploy.verifying") {
		t.Error("expected deploy.verifying event")
	}
	if !contains(eventTypes, "deploy.live") {
		t.Error("expected deploy.live event")
	}
	if !contains(eventTypes, "deploy.completed") {
		t.Error("expected deploy.completed event")
	}
}

func TestStaticDeliveryActivator_Activate_CreatesRelease(t *testing.T) {
	rs := newFakeReleaseStoreStatic()
	es := &fakeEventStoreStatic{}
	ets := newFakeEnvTargetStoreStatic()

	activator := NewStaticDeliveryActivator(StaticDeliveryDeps{
		ReleaseStore:   rs,
		EnvTargetStore: ets,
		EventStore:     es,
		Locker:         &fakeLockerStatic{},
		KVClient:       nil,
		BaseDomain:     "loopzero.local",
	})

	build := &db.Build{
		ID:          "build-2",
		TenantID:    "tn-acme",
		ProjectID:   "frontend",
		Environment: "staging",
	}
	result := BuildCallbackRequest{
		Status: "succeeded",
		Apps:   []AppRelease{{Name: "web", Type: "frontend"}},
	}

	err := activator.Activate(context.Background(), build, result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify release was created with correct fields
	rel := rs.releases["rel-build-2"]
	if rel == nil {
		t.Fatal("release not created")
	}
	if rel.TenantID != "tn-acme" {
		t.Errorf("TenantID = %q, want tn-acme", rel.TenantID)
	}
	if rel.ProjectID != "frontend" {
		t.Errorf("ProjectID = %q, want frontend", rel.ProjectID)
	}
	if rel.Environment != "staging" {
		t.Errorf("Environment = %q, want staging", rel.Environment)
	}

	// Verify env target was set
	if ets.desired["tn-acme-frontend-staging"] != "rel-build-2" {
		t.Errorf("desired target not set, got %q", ets.desired["tn-acme-frontend-staging"])
	}
	if ets.active["tn-acme-frontend-staging"] != "rel-build-2" {
		t.Errorf("active target not set, got %q", ets.active["tn-acme-frontend-staging"])
	}
}

func TestStaticDeliveryActivator_DeployCompletedEvent_ContainsURL(t *testing.T) {
	es := &fakeEventStoreStatic{}
	activator := NewStaticDeliveryActivator(StaticDeliveryDeps{
		ReleaseStore:   newFakeReleaseStoreStatic(),
		EnvTargetStore: newFakeEnvTargetStoreStatic(),
		EventStore:     es,
		Locker:         &fakeLockerStatic{},
		KVClient:       nil,
		BaseDomain:     "loopzero.example",
	})

	build := &db.Build{
		ID: "build-3", TenantID: "tn-acme", ProjectID: "web", Environment: "production",
	}
	result := BuildCallbackRequest{
		Status: "succeeded",
		Apps:   []AppRelease{{Name: "app", Type: "frontend"}},
	}

	if err := activator.Activate(context.Background(), build, result); err != nil {
		t.Fatal(err)
	}

	// Find deploy.completed event
	for _, e := range es.events {
		if e.EventType == "deploy.completed" {
			var data map[string]interface{}
			if err := json.Unmarshal(e.Data, &data); err != nil {
				t.Fatal(err)
			}
			if data["status"] != "success" {
				t.Errorf("deploy.completed status = %v, want success", data["status"])
			}
			apps, ok := data["apps"].([]interface{})
			if !ok || len(apps) == 0 {
				t.Fatal("deploy.completed missing apps")
			}
			app := apps[0].(map[string]interface{})
			if app["delivery"] != "static" {
				t.Errorf("app delivery = %v, want static", app["delivery"])
			}
			url, _ := app["url"].(string)
			if url == "" {
				t.Error("app URL is empty")
			}
			return
		}
	}
	t.Error("deploy.completed event not found")
}

// helper
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
