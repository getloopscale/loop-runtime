// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"context"
	"fmt"
	"log/slog"
	"testing"
	"time"

	loopzeroerrors "github.com/getloopscale/loopzero/pkg/errors"
)

// busyThenFreeSubmitter refuses with DEPLOY_IN_PROGRESS a number of times,
// as the orchestrator does while the environment deploys, then accepts.
type busyThenFreeSubmitter struct {
	busyFor  int
	attempts int
	accepted *WebhookBuildRequest
}

func (s *busyThenFreeSubmitter) SubmitBuildFromWebhook(_ context.Context, req WebhookBuildRequest) error {
	s.attempts++
	if s.attempts <= s.busyFor {
		return fmt.Errorf("build submit failed: %w", loopzeroerrors.ErrDeployInProgress)
	}
	s.accepted = &req
	return nil
}

func busyEnvironmentDeployer(t *testing.T, submitter BuildSubmitter, head func() string) *AutoDeployer {
	t.Helper()
	server := newGitHubTarballServer(t, "project: my-app\napps:\n  api:\n    runtime: node\n    start: node server.js\n")
	t.Cleanup(server.Close)
	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)
	return NewAutoDeployer(AutoDeployConfig{
		AppClient:      appClient,
		BuildSubmitter: submitter,
		SourceBucket:   "test-bucket",
		ObjectStore:    &fakeObjectUploader{},
		Logger:         slog.Default(),
		BusyRetryEvery: time.Millisecond,
		BusyWait:       time.Second,
		BranchHead: func(context.Context, PushRequest) (string, error) {
			return head(), nil
		},
	})
}

var pushOfSHA123 = PushRequest{
	RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
	TenantID: "tenant-1", ProjectName: "my-app", CommitMessage: "msg", SenderLogin: "user",
}

// A push that lands while the environment deploys is built when the
// environment is free. On staging it was refused once and never built (#125).
func TestHandlePush_BuildsWhenTheBusyEnvironmentIsFree(t *testing.T) {
	submitter := &busyThenFreeSubmitter{busyFor: 2}
	deployer := busyEnvironmentDeployer(t, submitter, func() string { return "sha123" })

	if err := deployer.HandlePush(context.Background(), pushOfSHA123); err != nil {
		t.Fatalf("HandlePush: %v", err)
	}

	if submitter.accepted == nil || submitter.accepted.CommitSHA != "sha123" {
		t.Fatalf("the push was not built after the environment was free (attempts: %d)", submitter.attempts)
	}
}

// Only the newest commit needs to be built: a waiting push gives way when the
// branch moved on, because the newer push has a handler of its own.
func TestHandlePush_AWaitingPushGivesWayToANewerCommit(t *testing.T) {
	submitter := &busyThenFreeSubmitter{busyFor: 1}
	deployer := busyEnvironmentDeployer(t, submitter, func() string { return "newer-sha" })

	if err := deployer.HandlePush(context.Background(), pushOfSHA123); err != nil {
		t.Fatalf("HandlePush: %v", err)
	}

	if submitter.accepted != nil {
		t.Fatal("a superseded commit was built")
	}
	if submitter.attempts != 1 {
		t.Errorf("attempts = %d, want 1", submitter.attempts)
	}
}

func TestHandlePush_GivesUpWithAnErrorWhenTheEnvironmentStaysBusy(t *testing.T) {
	submitter := &busyThenFreeSubmitter{busyFor: 1 << 30}
	deployer := busyEnvironmentDeployer(t, submitter, func() string { return "sha123" })
	deployer.cfg.BusyWait = 20 * time.Millisecond

	err := deployer.HandlePush(context.Background(), pushOfSHA123)

	if err == nil {
		t.Fatal("expected an error after the wait ran out")
	}
	if submitter.attempts < 2 {
		t.Errorf("attempts = %d, want more than one", submitter.attempts)
	}
}
