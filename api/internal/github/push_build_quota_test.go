// SPDX-License-Identifier: Apache-2.0
// Copyright 2025-2026 The LoopRuntime Authors

package github

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type fixedBuildQuota struct {
	result BuildQuotaResult
	err    error
	asked  []string
}

func (q *fixedBuildQuota) CheckBuildQuota(_ context.Context, tenantID string) (BuildQuotaResult, error) {
	q.asked = append(q.asked, tenantID)
	return q.result, q.err
}

// pushUnderQuota runs HandlePush for a deployable repository under quota and
// returns the build it submitted, the commit statuses GitHub received, whether
// the source was downloaded, and HandlePush's error.
func pushUnderQuota(t *testing.T, quota BuildQuota) (*WebhookBuildRequest, []CommitStatus, bool, error) {
	t.Helper()
	tarball := makeTarGz(t, map[string]string{
		"owner-repo-abc123/loopzero.yaml": "project: hello\napps:\n  web:\n    runtime: node\n    start: node server.js\n",
	}).Bytes()
	var mu sync.Mutex
	var statuses []CommitStatus
	downloaded := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/tarball/"):
			mu.Lock()
			downloaded = true
			mu.Unlock()
			w.Header().Set("Content-Type", "application/gzip")
			_, _ = w.Write(tarball)
		case strings.Contains(r.URL.Path, "/statuses/"):
			body, _ := io.ReadAll(r.Body)
			var sent struct {
				State       string `json:"state"`
				TargetURL   string `json:"target_url"`
				Description string `json:"description"`
				Context     string `json:"context"`
			}
			_ = json.Unmarshal(body, &sent)
			mu.Lock()
			statuses = append(statuses, CommitStatus{State: sent.State, TargetURL: sent.TargetURL, Description: sent.Description, Context: sent.Context})
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
	deployer := NewAutoDeployer(AutoDeployConfig{
		AppClient: appClient, ObjectStore: &fakeObjectUploader{}, BuildSubmitter: submitter,
		BuildQuota: quota, Logger: slog.Default(),
	})

	err := deployer.HandlePush(context.Background(), PushRequest{
		RepoFullName: "owner/repo", Branch: "main", CommitSHA: "sha123", InstallationID: 1,
		TenantID: "tenant-1", ProjectName: "hello", CommitMessage: "msg", SenderLogin: "user",
	})
	mu.Lock()
	defer mu.Unlock()
	return submitter.submittedReq, append([]CommitStatus(nil), statuses...), downloaded, err
}

// Seen on staging: with the tenant at its plan's builds_per_month, the deploy
// route answered 402 and a push to the connected repository built anyway.
func TestHandlePush_OverTheBuildLimitBuildsNothingAndSaysSoOnTheCommit(t *testing.T) {
	const refusal = "Plan limit reached: Starter allows 50 builds per month (current: 50). Upgrade your plan to continue."
	quota := &fixedBuildQuota{result: BuildQuotaResult{Message: refusal, UpgradeURL: "https://app.example.test/billing"}}

	submitted, statuses, downloaded, err := pushUnderQuota(t, quota)

	if err != nil {
		t.Fatalf("HandlePush: %v", err)
	}
	if submitted != nil {
		t.Fatal("a build was submitted past the plan's build limit")
	}
	if downloaded {
		t.Error("the source was downloaded for a push the plan refuses")
	}
	if len(quota.asked) != 1 || quota.asked[0] != "tenant-1" {
		t.Errorf("quota asked for %v, want [tenant-1]", quota.asked)
	}
	if len(statuses) != 1 {
		t.Fatalf("commit statuses = %+v, want exactly one", statuses)
	}
	got := statuses[0]
	if got.State != "failure" || got.Context != "loopscale/deploy" {
		t.Errorf("status = %+v, want failure on loopscale/deploy", got)
	}
	if got.Description != refusal {
		t.Errorf("description = %q, want the plan refusal", got.Description)
	}
	if got.TargetURL != "https://app.example.test/billing" {
		t.Errorf("target_url = %q, want the upgrade page", got.TargetURL)
	}
}

func TestHandlePush_UnderTheBuildLimitBuilds(t *testing.T) {
	submitted, _, _, err := pushUnderQuota(t, &fixedBuildQuota{result: BuildQuotaResult{Allowed: true}})

	if err != nil {
		t.Fatalf("HandlePush: %v", err)
	}
	if submitted == nil {
		t.Fatal("no build was submitted for a push the plan allows")
	}
}

// An unreadable plan is not an allow, the same rule the deploy route follows.
func TestHandlePush_AnUnreadableBuildLimitBuildsNothing(t *testing.T) {
	submitted, _, _, err := pushUnderQuota(t, &fixedBuildQuota{err: errors.New("plan store down")})

	if err == nil || !strings.Contains(err.Error(), "plan store down") {
		t.Fatalf("HandlePush error = %v, want the quota error", err)
	}
	if submitted != nil {
		t.Fatal("a build was submitted although the plan limit could not be read")
	}
}

// The commit status link is an absolute URL or absent: GitHub refuses a path.
func TestHandlePush_ARelativeUpgradePathIsNotSentAsTheStatusLink(t *testing.T) {
	quota := &fixedBuildQuota{result: BuildQuotaResult{Message: "Plan limit reached.", UpgradeURL: "/billing"}}

	_, statuses, _, _ := pushUnderQuota(t, quota)

	if len(statuses) != 1 || statuses[0].TargetURL != "" {
		t.Fatalf("commit statuses = %+v, want one without a link", statuses)
	}
}
