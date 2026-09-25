// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package builds

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
	"github.com/getloopscale/loopzero/api/internal/db/store"
)

type captureDeployStore struct {
	called bool
	ev     store.DeployEvent
}

func (c *captureDeployStore) AppendDeployEvent(_ context.Context, _ db.DBTX, ev store.DeployEvent) error {
	c.called = true
	c.ev = ev
	return nil
}

func TestStageDeployFailed_NilStore_NoOp(t *testing.T) {
	if err := stageDeployFailed(context.Background(), nil, nil, &db.Build{ID: "bld-1"}); err != nil {
		t.Fatalf("nil store should be a no-op, got: %v", err)
	}
}

func TestStageDeployFailed_EmitsCustomerSafePayload(t *testing.T) {
	cap := &captureDeployStore{}
	b := &db.Build{
		ID: "bld-abcdef0123456789", TenantID: "tn-1", ProjectID: "my-proj",
		Environment: "production", CommitSHA: "c0ffee", CorrelationID: "corr-1",
	}
	if err := stageDeployFailed(context.Background(), nil, cap, b); err != nil {
		t.Fatalf("stageDeployFailed: %v", err)
	}
	if !cap.called {
		t.Fatal("did not emit")
	}
	if cap.ev.EventType != "deploy.failed" {
		t.Errorf("EventType = %q, want deploy.failed", cap.ev.EventType)
	}
	if cap.ev.TenantID != "tn-1" || cap.ev.ProjectID != "my-proj" || cap.ev.CorrelationID != "corr-1" {
		t.Errorf("scoping/correlation wrong: %+v", cap.ev)
	}
	if !strings.HasPrefix(cap.ev.TransitionID, "dpx-") {
		t.Errorf("TransitionID = %q, want dpx- prefix", cap.ev.TransitionID)
	}
	if cap.ev.TransitionID == b.ID {
		t.Error("TransitionID must NOT be the build id")
	}
	// Rule 3: no internal terms in the delivered payload.
	payloadJSON, _ := json.Marshal(cap.ev.Payload)
	lower := strings.ToLower(string(payloadJSON))
	for _, banned := range []string{"argocd", "gitea", "minio", "kubeconfig", "pg_advisory", "kaniko", "envtest", "tenant_id"} {
		if strings.Contains(lower, banned) {
			t.Errorf("payload leaks internal term %q: %s", banned, string(payloadJSON))
		}
	}
	if cap.ev.Payload["status"] != "failed" || cap.ev.Payload["project"] != "my-proj" {
		t.Errorf("payload missing customer-safe fields: %v", cap.ev.Payload)
	}
	if cap.ev.Payload["commit"] != "c0ffee" {
		t.Errorf("payload missing commit: %v", cap.ev.Payload)
	}
}

func TestDeriveBuildDeployID(t *testing.T) {
	if got := deriveBuildDeployID("bld-abcdef0123456789"); got != "d-abcdef01" {
		t.Errorf("deriveBuildDeployID = %q, want d-abcdef01", got)
	}
	if got := deriveBuildDeployID("bld-ab"); got != "d-ab" {
		t.Errorf("short id = %q, want d-ab", got)
	}
}
