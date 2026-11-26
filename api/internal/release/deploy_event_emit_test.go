// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package release

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/getloopscale/loop-runtime/api/internal/db"
	"github.com/getloopscale/loop-runtime/api/internal/db/store"
	"github.com/getloopscale/loop-runtime/pkg/statemachine"
)

// captureDeployEventStore records the outbox event the kernel emits, so we can
// assert its shape hermetically (no DB — the store's atomicity is proven
// separately against a real tx in deploy_event_store_pg_test.go).
type captureDeployEventStore struct {
	called bool
	ev     store.DeployEvent
}

func (c *captureDeployEventStore) AppendDeployEvent(_ context.Context, _ db.DBTX, ev store.DeployEvent) error {
	c.called = true
	c.ev = ev
	return nil
}

func pendingReleaseFixture() db.Release {
	return db.Release{
		ID:            "rel-abcdef0123456789",
		BuildID:       "bld-1",
		TenantID:      "tn-1",
		ProjectID:     "my-proj",
		Environment:   "production",
		Status:        string(statemachine.RPhPending),
		CorrelationID: "corr-xyz",
		CreatedAt:     time.Now(),
	}
}

// The kernel stages a customer-safe deploy.succeeded event on the outbox when a
// store is wired (J-16 slice 3, MEET-2026-07-03-005).
func TestActivationKernel_EmitsDeploySucceededOutbox(t *testing.T) {
	rel := pendingReleaseFixture()
	relStore := newFakeConvergenceReleaseStore()
	relStore.releases[rel.ID] = &rel
	capture := &captureDeployEventStore{}

	err := ActivationKernel{
		Releases: relStore, EnvTargets: newFakeConvergenceEnvTargetStore(), Events: &fakeConvergenceEventStore{}, DeployEvents: capture, Logger: slog.Default(),
	}.Activate(context.Background(), nil, &rel, []ActivationAppInfo{{Name: "web", Status: "Healthy", URL: "https://web.example.test"}}, 5)
	if err != nil {
		t.Fatalf("ActivationKernel.Activate: %v", err)
	}
	if !capture.called {
		t.Fatal("kernel did not emit a deploy event to the outbox")
	}
	if capture.ev.EventType != "deploy.succeeded" {
		t.Errorf("EventType = %q, want deploy.succeeded", capture.ev.EventType)
	}
	if capture.ev.TenantID != "tn-1" || capture.ev.ProjectID != "my-proj" {
		t.Errorf("event scoping wrong: tenant=%q project=%q", capture.ev.TenantID, capture.ev.ProjectID)
	}
	if capture.ev.CorrelationID != "corr-xyz" {
		t.Errorf("CorrelationID = %q, want corr-xyz (forwarded)", capture.ev.CorrelationID)
	}
	if !strings.HasPrefix(capture.ev.TransitionID, "dpx-") {
		t.Errorf("TransitionID = %q, want dpx- prefix", capture.ev.TransitionID)
	}
	if capture.ev.TransitionID == rel.ID {
		t.Error("TransitionID must NOT be the release id (a Release re-enters ACTIVE after rollback recovery)")
	}

	// Rule 3: the delivered payload must carry ZERO internal terms.
	payloadJSON, _ := json.Marshal(capture.ev.Payload)
	lower := strings.ToLower(string(payloadJSON))
	for _, banned := range []string{"argocd", "gitea", "minio", "kubeconfig", "pg_advisory", "kaniko", "envtest", "tenant_id", "svc.cluster.local"} {
		if strings.Contains(lower, banned) {
			t.Errorf("payload leaks internal term %q: %s", banned, string(payloadJSON))
		}
	}
	// Customer-safe fields present.
	if capture.ev.Payload["status"] != "success" || capture.ev.Payload["project"] != "my-proj" {
		t.Errorf("payload missing customer-safe fields: %v", capture.ev.Payload)
	}
	if _, ok := capture.ev.Payload["deploy_id"]; !ok {
		t.Error("payload missing deploy_id")
	}
}

// A nil store is the optional-dependency path (unit callers that don't exercise
// the outbox) — the kernel must not panic and must not emit.
func TestActivationKernel_NilDeployEventStore_SkipsEmit(t *testing.T) {
	rel := pendingReleaseFixture()
	rel.ID = "rel-nil000000000000"
	relStore := newFakeConvergenceReleaseStore()
	relStore.releases[rel.ID] = &rel

	err := ActivationKernel{
		Releases: relStore, EnvTargets: newFakeConvergenceEnvTargetStore(), Events: &fakeConvergenceEventStore{}, DeployEvents: nil, Logger: slog.Default(),
	}.Activate(context.Background(), nil, &rel, nil, 1)
	if err != nil {
		t.Fatalf("kernel with nil deployEventStore returned error: %v", err)
	}
}
