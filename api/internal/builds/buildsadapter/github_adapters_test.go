// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package buildsadapter

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loop-runtime/api/internal/db/dbtx"
	"github.com/getloopscale/loop-runtime/api/internal/deploycontracts"
	"github.com/getloopscale/loop-runtime/api/internal/github"
)

// The push webhook's commit SHA is untrusted input. A short or empty one must be
// refused with an error; slicing it for the correlation id used to panic the
// webhook goroutine before the orchestrator was ever reached.
func TestBuildSubmitterAdapter_RefusesACommitSHAShorterThanTheCorrelationPrefix(t *testing.T) {
	a := &BuildSubmitterAdapter{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, sha := range []string{"", "abc1234"} {
		err := a.SubmitBuildFromWebhook(context.Background(), github.WebhookBuildRequest{
			TenantID: "tn-1", ProjectName: "web", Environment: "production",
			CommitSHA: sha, RepoFullName: "acme/web", LoopRuntimeYaml: &github.LoopRuntimeYamlMinimal{},
		})
		if err == nil {
			t.Errorf("commit SHA %q must be refused", sha)
		}
	}
}

// scopeRecordingSubmitter records the database scope a submission ran under.
type scopeRecordingSubmitter struct{ scope string }

func (s *scopeRecordingSubmitter) SubmitBuild(ctx context.Context, _ deploycontracts.SubmitBuildRequest) (*deploycontracts.SubmitBuildResponse, error) {
	s.scope, _ = dbtx.TenantScope(ctx)
	return &deploycontracts.SubmitBuildResponse{}, nil
}

// A push is delivered by GitHub with no user and no tenant scope; the tenant is
// the one the repository is linked to, resolved server-side. The orchestrator's
// stores run on the request pool, whose tenant tables are fail-closed under Row
// Level Security, so the submission must run scoped to that tenant — unscoped,
// the scaling config read empty and the workflow id and status writes that
// follow the lock matched no row.
func TestBuildSubmitterAdapter_SubmitsScopedToTheLinkedTenant(t *testing.T) {
	orch := &scopeRecordingSubmitter{}
	a := &BuildSubmitterAdapter{Orchestrator: orch, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	err := a.SubmitBuildFromWebhook(context.Background(), github.WebhookBuildRequest{
		TenantID: "tn-linked", ProjectName: "web", Environment: "production",
		CommitSHA: "0123456789abcdef", RepoFullName: "acme/web", LoopRuntimeYaml: &github.LoopRuntimeYamlMinimal{},
	})

	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if orch.scope != "tn-linked" {
		t.Fatalf("submission scope = %q; want tn-linked", orch.scope)
	}
}
