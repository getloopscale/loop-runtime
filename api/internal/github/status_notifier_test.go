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
	"testing"
	"time"
)

type fakeProjectRepoLookup struct{}

func (fakeProjectRepoLookup) GetByProject(context.Context, string, string) (string, int64, error) {
	return "owner/repo", 7, nil
}

type fakeServedAppsLookup struct{ apps []string }

func (fakeServedAppsLookup) GetCommitSHA(context.Context, string) (string, error) {
	return "abc123", nil
}
func (f fakeServedAppsLookup) GetServedAppNames(context.Context, string) ([]string, error) {
	return f.apps, nil
}

// statusCapture serves the GitHub API and records the last commit status posted.
func statusCapture(t *testing.T) (*AppClient, *map[string]string) {
	t.Helper()
	got := map[string]string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/statuses/") {
			body, _ := io.ReadAll(r.Body)
			if err := json.Unmarshal(body, &got); err != nil {
				t.Errorf("status body is not JSON: %s", body)
			}
			w.WriteHeader(http.StatusCreated)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"token": "ghs_x", "expires_at": time.Now().Add(time.Hour).Format(time.RFC3339)})
	}))
	t.Cleanup(srv.Close)
	c := newTestAppClient(t, srv)
	c.httpClient = redirectingClient(srv.URL)
	return c, &got
}

func TestOnBuildSucceededLinksToTheDeployedAppsRealURL(t *testing.T) {
	// Arrange: the project's one served app is called "checkout", not "api".
	client, got := statusCapture(t)
	n := NewStatusNotifier(client, fakeProjectRepoLookup{}, fakeServedAppsLookup{apps: []string{"checkout"}}, "getloopscale.com", slog.Default())

	// Act
	n.OnBuildSucceeded(context.Background(), "tn-1", "shop", "build-1", "production")

	// Assert
	if want := "https://tn-1-shop-checkout-production.getloopscale.app"; (*got)["target_url"] != want {
		t.Errorf("target_url = %q, want %q", (*got)["target_url"], want)
	}
}

func TestOnBuildSucceededDoesNotGuessAURLForSeveralApps(t *testing.T) {
	client, got := statusCapture(t)
	n := NewStatusNotifier(client, fakeProjectRepoLookup{}, fakeServedAppsLookup{apps: []string{"web", "admin"}}, "getloopscale.com", slog.Default())

	n.OnBuildSucceeded(context.Background(), "tn-1", "shop", "build-1", "production")

	if (*got)["state"] != "success" {
		t.Fatalf("state = %q, want success", (*got)["state"])
	}
	if (*got)["target_url"] != "" {
		t.Errorf("target_url = %q, want none — there is no single URL for several apps", (*got)["target_url"])
	}
}
