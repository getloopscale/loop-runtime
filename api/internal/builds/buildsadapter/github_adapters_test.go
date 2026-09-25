// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package buildsadapter

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/getloopscale/loopzero/api/internal/github"
)

// The push webhook's commit SHA is untrusted input. A short or empty one must be
// refused with an error; slicing it for the correlation id used to panic the
// webhook goroutine before the orchestrator was ever reached.
func TestBuildSubmitterAdapter_RefusesACommitSHAShorterThanTheCorrelationPrefix(t *testing.T) {
	a := &BuildSubmitterAdapter{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, sha := range []string{"", "abc1234"} {
		err := a.SubmitBuildFromWebhook(context.Background(), github.WebhookBuildRequest{
			TenantID: "tn-1", ProjectName: "web", Environment: "production",
			CommitSHA: sha, RepoFullName: "acme/web", LoopZeroYaml: &github.LoopZeroYamlMinimal{},
		})
		if err == nil {
			t.Errorf("commit SHA %q must be refused", sha)
		}
	}
}
