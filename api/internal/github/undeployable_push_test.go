// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopZero Authors

package github

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// pushTo runs HandlePush against a repository with the given files and
// returns the commit statuses GitHub received.
func pushTo(t *testing.T, files map[string]string) []CommitStatus {
	t.Helper()
	tarball := makeTarGz(t, files).Bytes()
	var mu sync.Mutex
	var statuses []CommitStatus
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/tarball/"):
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarball)
		case strings.Contains(r.URL.Path, "/statuses/"):
			body, _ := io.ReadAll(r.Body)
			var sent struct {
				State       string `json:"state"`
				Description string `json:"description"`
				Context     string `json:"context"`
			}
			_ = json.Unmarshal(body, &sent)
			mu.Lock()
			statuses = append(statuses, CommitStatus{State: sent.State, Description: sent.Description, Context: sent.Context})
			mu.Unlock()
			w.WriteHeader(http.StatusCreated)
		default:
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"token":      "ghs_test_token",
				"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
			})
		}
	}))
	t.Cleanup(server.Close)
	appClient := newTestAppClient(t, server)
	appClient.httpClient = redirectingClient(server.URL)
	submitter := &fakeBuildSubmitter{}
	deployer := NewAutoDeployer(AutoDeployConfig{AppClient: appClient, BuildSubmitter: submitter, Logger: slog.Default()})

	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "project-1", CommitMessage: "msg", SenderLogin: "user",
	})
	if err != nil {
		t.Fatalf("HandlePush: %v", err)
	}
	if submitter.submittedReq != nil {
		t.Fatal("a build was submitted for a repository that cannot be deployed")
	}
	mu.Lock()
	defer mu.Unlock()
	return append([]CommitStatus(nil), statuses...)
}

func TestHandlePush_AnInvalidDescriptorMarksTheCommitFailedWithTheReason(t *testing.T) {
	statuses := pushTo(t, map[string]string{
		"owner-repo-abc123/main.go":       "package main",
		"owner-repo-abc123/loopzero.yaml": "project: hello\napps:\n  web:\n    runtime: node\n",
	})

	if len(statuses) != 1 {
		t.Fatalf("commit statuses = %+v, want exactly one", statuses)
	}
	got := statuses[0]
	if got.State != "failure" || got.Context != "loopzero/deploy" {
		t.Errorf("status = %+v, want failure on loopzero/deploy", got)
	}
	if !strings.HasPrefix(got.Description, `loopzero.yaml app "web" needs `+"`start`") {
		t.Errorf("description = %q, want the descriptor's own reason", got.Description)
	}
	if len(got.Description) > commitStatusDescriptionLimit {
		t.Errorf("description has %d characters, GitHub accepts %d", len(got.Description), commitStatusDescriptionLimit)
	}
}

func TestHandlePush_AMissingDescriptorMarksTheCommitFailed(t *testing.T) {
	statuses := pushTo(t, map[string]string{"owner-repo-abc123/main.go": "package main"})

	if len(statuses) != 1 || statuses[0].State != "failure" {
		t.Fatalf("commit statuses = %+v, want one failure", statuses)
	}
	if !strings.Contains(statuses[0].Description, "loopscale.yaml") {
		t.Errorf("description = %q, want it to name loopscale.yaml", statuses[0].Description)
	}
}
