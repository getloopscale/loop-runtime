// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/db"
)

// --- BuildOrchestratorAdapter additional tests ---

// --- GetBuildStatus with error message ---

func TestBuildOrchestratorAdapter_GetBuildStatus_WithErrorMessage(t *testing.T) {
	store := &fakeBuildStore{
		build: &db.Build{
			ID:           "bld-err-001",
			ProjectID:    "my-app",
			Environment:  "production",
			Status:       "FAILED",
			CommitSHA:    "abc123",
			CreatedBy:    "user@test.com",
			ErrorMessage: "OOMKilled",
		},
		apps: []db.BuildAppSpec{{Name: "api", Type: "server"}},
	}

	adapter := &BuildOrchestratorAdapter{BuildStore: store}

	resp, err := adapter.GetBuildStatus(context.Background(), "bld-err-001")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ErrorMessage != "OOMKilled" {
		t.Errorf("expected error message 'OOMKilled', got %q", resp.ErrorMessage)
	}
	if resp.Status != "FAILED" {
		t.Errorf("expected FAILED, got %q", resp.Status)
	}
}
